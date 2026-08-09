package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/model"
	"github.com/hkjang/Momento/internal/service"
	"github.com/hkjang/Momento/internal/version"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

type Server struct {
	DB              *pgxpool.Pool
	Auth            auth.Service
	Collector       service.CollectorService
	Web             fs.FS
	Logger          *slog.Logger
	Limiter         *rateLimiter
	LoginLimiter    *rateLimiter
	securityMu      sync.RWMutex
	trustedProxies  []*net.IPNet
	maxPayloadBytes int64
}

func New(db *pgxpool.Pool, web fs.FS, logger *slog.Logger) *Server {
	server := &Server{DB: db, Auth: auth.Service{DB: db}, Collector: service.CollectorService{DB: db}, Web: web, Logger: logger, Limiter: newRateLimiter(6000), LoginLimiter: newRateLimiter(10)}
	var limit int
	if db.QueryRow(context.Background(), `SELECT coalesce((value->>'collector_rate_limit_per_minute')::int,6000) FROM settings WHERE key='security'`).Scan(&limit) == nil {
		server.Limiter.SetLimit(limit)
	}
	server.reloadSecurity(context.Background())
	return server
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.requestLog, securityHeaders)
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/health/ready", s.ready)
	r.Get("/api/v1/version", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, version.Current()) })
	r.Get("/api/v1/auth/options", s.authOptions)
	r.Post("/api/v1/auth/login", s.login)
	r.Post("/api/v1/auth/logout", s.logout)
	r.Get("/api/v1/auth/oidc/login", s.oidcLogin)
	r.Get("/api/v1/auth/oidc/callback", s.oidcCallback)
	r.Options("/collect/v1/events", s.collectOptions)
	r.Post("/collect/v1/events", s.collect)

	r.Group(func(api chi.Router) {
		api.Use(s.requireAuth)
		api.Get("/api/v1/me", s.me)
		api.Patch("/api/v1/me", s.sessionOnly(s.updateMe))
		api.Get("/api/v1/me/keys", s.sessionOnly(s.listMyKeys))
		api.Post("/api/v1/me/keys", s.sessionOnly(s.createMyKey))
		api.Delete("/api/v1/me/keys/{id}", s.sessionOnly(s.deleteMyKey))
		api.Post("/api/v1/me/keys/{id}/rotate", s.sessionOnly(s.rotateMyKey))
		api.Get("/api/v1/sites", s.listSites)
		api.Post("/api/v1/sites", s.admin(s.createSite))
		api.Patch("/api/v1/sites/{id}", s.admin(s.updateSite))
		api.Delete("/api/v1/sites/{id}", s.admin(s.deleteSite))
		api.Post("/api/v1/sites/{id}/rotate-key", s.admin(s.rotateSiteKey))
		api.Post("/api/v1/sites/{id}/rotate-server-key", s.admin(s.rotateServerKey))
		api.Get("/api/v1/sites/{id}/tracking-code", s.trackingCode)
		api.Get("/api/v1/settings", s.admin(s.listSettings))
		api.Put("/api/v1/settings/{key}", s.admin(s.putSetting))
		api.Get("/api/v1/networks", s.admin(s.listNetworks))
		api.Post("/api/v1/networks", s.admin(s.createNetwork))
		api.Delete("/api/v1/networks/{id}", s.admin(s.deleteNetwork))
		api.Get("/api/v1/users", s.admin(s.listUsers))
		api.Post("/api/v1/users", s.admin(s.createUser))
		api.Patch("/api/v1/users/{id}", s.admin(s.updateUser))
		api.Get("/api/v1/audit", s.admin(s.listAudit))
		api.Get("/api/v1/tracking-debugger", s.admin(s.trackingDebugger))
		api.Post("/api/v1/privacy/delete", s.admin(s.deleteAnalyticsData))
		api.Get("/api/v1/event-definitions", s.listEventDefinitions)
		api.Post("/api/v1/event-definitions", s.admin(s.upsertEventDefinition))
		api.Get("/api/v1/dimensions", s.listDimensions)
		api.Post("/api/v1/dimensions", s.admin(s.saveDimension))
		api.Delete("/api/v1/dimensions/{id}", s.admin(s.deleteDimension))
		api.Get("/api/v1/segments", s.listSegments)
		api.Post("/api/v1/segments", s.sessionOnly(s.createSegment))
		api.Put("/api/v1/segments/{id}", s.sessionOnly(s.updateSegment))
		api.Delete("/api/v1/segments/{id}", s.sessionOnly(s.deleteSegment))
		api.Get("/api/v1/reports", s.listSavedReports)
		api.Post("/api/v1/reports", s.sessionOnly(s.createSavedReport))
		api.Put("/api/v1/reports/{id}", s.sessionOnly(s.updateSavedReport))
		api.Delete("/api/v1/reports/{id}", s.sessionOnly(s.deleteSavedReport))
		api.Get("/api/v1/sites/{siteID}/retention", s.admin(s.getRetentionPolicy))
		api.Put("/api/v1/sites/{siteID}/retention", s.admin(s.putRetentionPolicy))
		api.Get("/api/v1/sites/{siteID}/environments", s.listEnvironments)
		api.Put("/api/v1/sites/{siteID}/environments/{name}", s.admin(s.putEnvironment))
		api.Get("/api/v1/sites/{siteID}/event-contracts", s.listEventContracts)
		api.Post("/api/v1/sites/{siteID}/event-contracts", s.admin(s.createEventContract))
		api.Post("/api/v1/sites/{siteID}/event-contracts/{eventName}/{version}/activate", s.admin(s.activateEventContract))
		api.Get("/api/v1/sites/{siteID}/semantic-metrics", s.listSemanticMetrics)
		api.Post("/api/v1/sites/{siteID}/semantic-metrics", s.admin(s.saveSemanticMetric))
		api.Get("/api/v1/sites/{siteID}/semantic-metrics/{name}/query", s.querySemanticMetric)
		api.Get("/api/v1/sites/{siteID}/data-quality", s.dataQualityReport)
		api.Get("/api/v1/sites/{siteID}/cohort", s.cohortReport)
		api.Get("/api/v1/sites/{siteID}/journeys", s.listBusinessJourneys)
		api.Post("/api/v1/sites/{siteID}/journeys", s.sessionOnly(s.saveBusinessJourney))
		api.Delete("/api/v1/sites/{siteID}/journeys/{id}", s.sessionOnly(s.deleteBusinessJourney))
		api.Post("/api/v1/sites/{siteID}/journeys/analyze", s.analyzeBusinessJourney)
		api.Get("/api/v1/sites/{siteID}/adoption-targets", s.listAdoptionTargets)
		api.Post("/api/v1/sites/{siteID}/adoption-targets", s.admin(s.saveAdoptionTarget))
		api.Delete("/api/v1/sites/{siteID}/adoption-targets/{id}", s.admin(s.deleteAdoptionTarget))
		api.Get("/api/v1/sites/{siteID}/adoption", s.adoptionReport)
		api.Get("/api/v1/sites/{siteID}/experience", s.experienceReport)
		api.Get("/api/v1/sites/{siteID}/ai-analytics", s.aiAnalyticsReport)
		api.Get("/api/v1/sites/{siteID}/insights", s.insightsReport)
		api.Post("/api/v1/sites/{siteID}/natural-query", s.naturalLanguageAnalytics)
		api.Get("/api/v1/sites/{siteID}/delivery-channels", s.admin(s.listDeliveryChannels))
		api.Post("/api/v1/sites/{siteID}/delivery-channels", s.admin(s.saveDeliveryChannel))
		api.Delete("/api/v1/sites/{siteID}/delivery-channels/{id}", s.admin(s.deleteDeliveryChannel))
		api.Get("/api/v1/sites/{siteID}/scheduled-reports", s.admin(s.listScheduledReports))
		api.Post("/api/v1/sites/{siteID}/scheduled-reports", s.admin(s.saveScheduledReport))
		api.Delete("/api/v1/sites/{siteID}/scheduled-reports/{id}", s.admin(s.deleteScheduledReport))
		api.Post("/api/v1/sites/{siteID}/scheduled-reports/{id}/run", s.admin(s.runScheduledReport))
		api.Get("/api/v1/sites/{siteID}/delivery-runs", s.admin(s.listDeliveryRuns))
		api.Get("/api/v1/sites/{siteID}/overview", s.overview)
		api.Get("/api/v1/sites/{siteID}/realtime", s.realtime)
		api.Get("/api/v1/sites/{siteID}/events", s.eventReport)
		api.Get("/api/v1/sites/{siteID}/pages", s.pageReport)
		api.Get("/api/v1/sites/{siteID}/usage", s.usageReport)
		api.Get("/api/v1/sites/{siteID}/visitors", s.visitorReport)
		api.Get("/api/v1/sites/{siteID}/identities", s.identityReport)
		api.Get("/api/v1/sites/{siteID}/visitors/{visitorID}/timeline", s.visitorTimeline)
		api.Get("/api/v1/sites/{siteID}/sessions", s.sessionReport)
		api.Get("/api/v1/sites/{siteID}/ecommerce", s.ecommerceReport)
		api.Post("/api/v1/query", s.query)
		api.Post("/api/v1/funnel", s.funnel)
		api.Get("/api/v1/sites/{siteID}/path", s.pathReport)
		api.Get("/api/v1/sites/{siteID}/export", s.exportEvents)
		api.Post("/mcp", s.mcp)
	})
	if s.Web != nil {
		r.Handle("/*", s.spaHandler())
	}
	return r
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Auth.Authenticate(r)
		if err != nil {
			writeError(w, 401, "UNAUTHENTICATED", err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if p.AuthType == "api_key" || !auth.RoleAtLeast(p.Role, "workspace_admin") {
			writeError(w, 403, "FORBIDDEN", "administrator permission required")
			return
		}
		next(w, r)
	}
}
func (s *Server) sessionOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if p.AuthType != "session" {
			writeError(w, 403, "SESSION_REQUIRED", "this operation requires an interactive session")
			return
		}
		next(w, r)
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(r.Context()); err != nil {
		writeError(w, 503, "DATABASE_UNAVAILABLE", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Logger.Error("panic", "error", v)
				writeError(w, 500, "INTERNAL", "unexpected server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/health/live" {
			s.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
		}
	})
}

func (s *Server) spaHandler() http.Handler {
	files := http.FileServer(http.FS(s.Web))
	index, indexErr := fs.ReadFile(s.Web, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean != "." && clean != "" {
			if info, err := fs.Stat(s.Web, clean); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		if indexErr != nil {
			http.Error(w, "web console is unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

func (s *Server) collectOptions(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Momento-Key")
	w.WriteHeader(http.StatusNoContent)
}
func setCORS(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
}
func (s *Server) collect(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	var req model.CollectRequest
	if err := decodeJSON(r, &req, s.payloadLimit()); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if req.TrackingKey == "" {
		req.TrackingKey = r.Header.Get("X-Momento-Key")
	}
	ip := s.clientIP(r)
	if !s.Limiter.Allow(req.SiteID + "|" + ip) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "collector rate limit exceeded")
		return
	}
	if r.Header.Get("DNT") == "1" {
		var enabled bool
		if s.DB.QueryRow(r.Context(), `SELECT coalesce((value->>'do_not_track')::bool,false) FROM settings WHERE key='privacy'`).Scan(&enabled) == nil && enabled {
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": 0, "reason": "CONSENT_DENIED"})
			return
		}
	}
	id, err := s.Collector.Accept(r.Context(), req, r.Header.Get("Origin"), ip, r.UserAgent())
	if err != nil {
		code := "VALIDATION_ERROR"
		status := 400
		if err.Error() == "unknown site" {
			code = "UNKNOWN_SITE"
			status = 404
		}
		if strings.Contains(err.Error(), "origin") {
			code = "INVALID_DOMAIN"
			status = 403
		}
		if strings.Contains(strings.ToLower(err.Error()), "key") {
			code = "INVALID_KEY"
			status = 403
		}
		if strings.Contains(err.Error(), "schema validation") {
			code = "SCHEMA_ERROR"
			status = 422
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(req.Events), "receipt_id": id})
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func (s *Server) reloadSecurity(ctx context.Context) {
	var raw []byte
	if s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key='security'`).Scan(&raw) != nil {
		return
	}
	var cfg struct {
		Trusted    []string `json:"trusted_proxy_cidrs"`
		Limit      int      `json:"collector_rate_limit_per_minute"`
		MaxPayload int64    `json:"max_payload_bytes"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return
	}
	ranges := []*net.IPNet{}
	for _, value := range cfg.Trusted {
		_, network, err := net.ParseCIDR(value)
		if err == nil {
			ranges = append(ranges, network)
		}
	}
	s.securityMu.Lock()
	s.trustedProxies = ranges
	if cfg.MaxPayload < 1024 || cfg.MaxPayload > 10<<20 {
		cfg.MaxPayload = 256 << 10
	}
	s.maxPayloadBytes = cfg.MaxPayload
	s.securityMu.Unlock()
	if cfg.Limit > 0 {
		s.Limiter.SetLimit(cfg.Limit)
	}
}
func (s *Server) payloadLimit() int64 {
	s.securityMu.RLock()
	defer s.securityMu.RUnlock()
	if s.maxPayloadBytes <= 0 {
		return 256 << 10
	}
	return s.maxPayloadBytes
}
func (s *Server) ipTrusted(ip net.IP) bool {
	s.securityMu.RLock()
	defer s.securityMu.RUnlock()
	for _, network := range s.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
func (s *Server) clientIP(r *http.Request) string {
	remote := net.ParseIP(clientIP(r))
	if remote == nil || !s.ipTrusted(remote) {
		return clientIP(r)
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(chain[i]))
		if candidate == nil {
			continue
		}
		if !s.ipTrusted(candidate) {
			return candidate.String()
		}
	}
	return remote.String()
}
func (s *Server) requestSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	remote := net.ParseIP(clientIP(r))
	if remote == nil || !s.ipTrusted(remote) {
		return false
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	return len(forwarded) > 0 && strings.EqualFold(strings.TrimSpace(forwarded[0]), "https")
}

func (s *Server) authOptions(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	_ = s.DB.QueryRow(r.Context(), `SELECT value FROM settings WHERE key='oidc'`).Scan(&raw)
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	enabled, _ := v["enabled"].(bool)
	writeJSON(w, 200, map[string]any{"local": true, "oidc_enabled": enabled, "version": version.Current()})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.LoginLimiter.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "RATE_LIMITED", "too many login attempts")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	p, token, err := s.Auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		s.audit(r.Context(), nil, "login.failed", "session", strings.ToLower(in.Email), nil, clientIP(r))
		writeError(w, 401, "INVALID_CREDENTIALS", "email or password is incorrect")
		return
	}
	auth.SetSessionCookie(w, token, s.requestSecure(r))
	s.audit(r.Context(), &p, "login", "session", p.ID.String(), nil, clientIP(r))
	writeJSON(w, 200, p)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := ""
	if c, err := r.Cookie("momento_session"); err == nil {
		token = c.Value
	}
	s.Auth.Logout(r.Context(), token)
	auth.ClearSessionCookie(w)
	w.WriteHeader(204)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	writeJSON(w, 200, p)
}

type oidcSetting struct {
	Enabled           bool     `json:"enabled"`
	IssuerURL         string   `json:"issuer_url"`
	ClientID          string   `json:"client_id"`
	ClientSecret      string   `json:"client_secret"`
	Scopes            []string `json:"scopes"`
	ClaimEmail        string   `json:"claim_email"`
	ClaimName         string   `json:"claim_name"`
	ClaimDepartment   string   `json:"claim_department"`
	ClaimOrganization string   `json:"claim_organization"`
}

func (s *Server) loadOIDC(ctx context.Context) (oidcSetting, error) {
	var raw []byte
	var cfg oidcSetting
	if err := s.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key='oidc'`).Scan(&raw); err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if !cfg.Enabled {
		return cfg, errors.New("OIDC is disabled")
	}
	return cfg, nil
}
func externalURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
func (s *Server) publicURL(ctx context.Context, r *http.Request) string {
	var configured string
	if s.DB.QueryRow(ctx, `SELECT coalesce(value->>'public_url','') FROM settings WHERE key='general'`).Scan(&configured) == nil && configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	return externalURL(r)
}
func randomURLSafe(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadOIDC(r.Context())
	if err != nil {
		writeError(w, 400, "OIDC_DISABLED", err.Error())
		return
	}
	provider, err := oidc.NewProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, 502, "OIDC_DISCOVERY_FAILED", err.Error())
		return
	}
	state := randomURLSafe(32)
	verifier := oauth2.GenerateVerifier()
	sum := sha256.Sum256([]byte(state))
	_, err = s.DB.Exec(r.Context(), `INSERT INTO oidc_states(state_hash,verifier,return_to,expires_at) VALUES($1,$2,'/',now()+interval '10 minutes')`, base64.RawURLEncoding.EncodeToString(sum[:]), verifier)
	if err != nil {
		writeError(w, 500, "OIDC_STATE_FAILED", err.Error())
		return
	}
	oauthCfg := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: s.publicURL(r.Context(), r) + "/api/v1/auth/oidc/callback", Scopes: cfg.Scopes}
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}
func claimString(claims map[string]any, key, fallback string) string {
	if key == "" {
		key = fallback
	}
	v, _ := claims[key].(string)
	return v
}
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if msg := r.URL.Query().Get("error"); msg != "" {
		writeError(w, 401, "OIDC_ERROR", msg)
		return
	}
	state := r.URL.Query().Get("state")
	sum := sha256.Sum256([]byte(state))
	var verifier string
	err := s.DB.QueryRow(r.Context(), `DELETE FROM oidc_states WHERE state_hash=$1 AND expires_at>now() RETURNING verifier`, base64.RawURLEncoding.EncodeToString(sum[:])).Scan(&verifier)
	if err != nil {
		writeError(w, 400, "OIDC_STATE_INVALID", "login state expired or invalid")
		return
	}
	cfg, err := s.loadOIDC(r.Context())
	if err != nil {
		writeError(w, 400, "OIDC_DISABLED", err.Error())
		return
	}
	provider, err := oidc.NewProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, 502, "OIDC_DISCOVERY_FAILED", err.Error())
		return
	}
	oauthCfg := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: s.publicURL(r.Context(), r) + "/api/v1/auth/oidc/callback", Scopes: cfg.Scopes}
	token, err := oauthCfg.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		writeError(w, 401, "OIDC_EXCHANGE_FAILED", err.Error())
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		writeError(w, 401, "OIDC_TOKEN_MISSING", "provider did not return an ID token")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(r.Context(), rawID)
	if err != nil {
		writeError(w, 401, "OIDC_TOKEN_INVALID", err.Error())
		return
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		writeError(w, 401, "OIDC_CLAIMS_INVALID", err.Error())
		return
	}
	email := claimString(claims, cfg.ClaimEmail, "email")
	name := claimString(claims, cfg.ClaimName, "name")
	dept := claimString(claims, cfg.ClaimDepartment, "department")
	org := claimString(claims, cfg.ClaimOrganization, "organization")
	if email == "" {
		writeError(w, 401, "OIDC_EMAIL_MISSING", "configured email claim is empty")
		return
	}
	if name == "" {
		name = email
	}
	var userID uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO users(email,display_name,department,organization_name,role,oidc_subject) VALUES(lower($1),$2,$3,$4,'viewer',$5) ON CONFLICT(email) DO UPDATE SET display_name=excluded.display_name,department=excluded.department,organization_name=excluded.organization_name,oidc_subject=excluded.oidc_subject,updated_at=now() RETURNING id`, email, name, dept, org, idToken.Subject).Scan(&userID)
	if err != nil {
		writeError(w, 500, "OIDC_USER_FAILED", err.Error())
		return
	}
	session, err := s.Auth.CreateSession(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "SESSION_FAILED", err.Error())
		return
	}
	auth.SetSessionCookie(w, session, s.requestSecure(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) audit(ctx context.Context, p *auth.Principal, action, resourceType, resourceID string, detail any, ip string) {
	var actor any
	if p != nil {
		actor = p.ID
	}
	if detail == nil {
		detail = map[string]any{}
	}
	body, _ := json.Marshal(detail)
	_, _ = s.DB.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,resource_type,resource_id,detail,client_ip) VALUES($1,$2,$3,$4,$5,$6)`, actor, action, resourceType, resourceID, body, nullableString(ip))
}
func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// Compile-time assertions for dependencies whose concrete errors are intentionally hidden from clients.
var _ = fmt.Sprintf
var _ = pgx.ErrNoRows
