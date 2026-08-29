package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/model"
	"github.com/hkjang/Momento/internal/secret"
	"github.com/hkjang/Momento/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var eventNamePatternForProperty = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		DisplayName      string `json:"display_name"`
		Department       string `json:"department"`
		OrganizationName string `json:"organization_name"`
		CurrentPassword  string `json:"current_password"`
		NewPassword      string `json:"new_password"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		writeError(w, 400, "INVALID_NAME", "display name is required")
		return
	}
	if in.NewPassword != "" {
		if len(in.NewPassword) < 12 {
			writeError(w, 400, "WEAK_PASSWORD", "new password must be at least 12 characters")
			return
		}
		var hash *string
		if err := s.DB.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1`, p.ID).Scan(&hash); err != nil || hash == nil || !auth.ComparePassword(*hash, in.CurrentPassword) {
			writeError(w, 403, "PASSWORD_MISMATCH", "current password is incorrect")
			return
		}
		newHash, _ := auth.HashPassword(in.NewPassword)
		_, _ = s.DB.Exec(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, p.ID, newHash)
	}
	_, err := s.DB.Exec(r.Context(), `UPDATE users SET display_name=$2,department=$3,organization_name=$4,updated_at=now() WHERE id=$1`, p.ID, in.DisplayName, in.Department, in.OrganizationName)
	if err != nil {
		writeError(w, 500, "UPDATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "profile.update", "user", p.ID.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) listMyKeys(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,key_prefix,scopes,expires_at,last_used_at,created_at,token_secret FROM api_keys WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, p.ID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, prefix string
		var scopes []string
		var expires, lastUsed *time.Time
		var created time.Time
		var stored *string
		if err := rows.Scan(&id, &name, &prefix, &scopes, &expires, &lastUsed, &created, &stored); err != nil {
			continue
		}
		plain, _ := s.openSecret(stored)
		out = append(out, map[string]any{"id": id, "name": name, "prefix": prefix, "scopes": scopes, "expires_at": expires, "last_used_at": lastUsed, "created_at": created, "recoverable": plain != ""})
	}
	writeJSON(w, 200, out)
}
func (s *Server) createMyKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "Personal API key"
	}
	plain, hash, prefix, err := auth.NewToken("mom_key_", 32)
	if err != nil {
		writeError(w, 500, "KEY_FAILED", err.Error())
		return
	}
	var expires any
	if in.ExpiresInDays > 0 && in.ExpiresInDays <= 3650 {
		expires = time.Now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO api_keys(user_id,name,key_hash,key_prefix,expires_at,token_secret) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, p.ID, in.Name, hash, prefix, expires, s.sealSecret(plain)).Scan(&id)
	if err != nil {
		writeError(w, 500, "KEY_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "api_key.create", "api_key", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id, "key": plain, "prefix": prefix, "recoverable": s.Secrets.Enabled(), "message": keyStorageMessage(s.Secrets.Enabled())})
}
func (s *Server) deleteMyKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid key id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, p.ID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "key not found")
		return
	}
	s.audit(r.Context(), &p, "api_key.revoke", "api_key", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) rotateMyKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	oldID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid key id")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "KEY_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	var name string
	var scopes []string
	var expires *time.Time
	if err := tx.QueryRow(r.Context(), `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL RETURNING name,scopes,expires_at`, oldID, p.ID).Scan(&name, &scopes, &expires); err != nil {
		writeError(w, 404, "NOT_FOUND", "key not found")
		return
	}
	plain, hash, prefix, _ := auth.NewToken("mom_key_", 32)
	var newID uuid.UUID
	if err := tx.QueryRow(r.Context(), `INSERT INTO api_keys(user_id,name,key_hash,key_prefix,scopes,expires_at,token_secret) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, p.ID, name, hash, prefix, scopes, expires, s.sealSecret(plain)).Scan(&newID); err != nil {
		writeError(w, 500, "KEY_FAILED", err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "KEY_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "api_key.rotate", "api_key", newID.String(), map[string]any{"replaces": oldID}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": newID, "key": plain, "prefix": prefix, "recoverable": s.Secrets.Enabled(), "message": "The previous key is revoked. " + keyStorageMessage(s.Secrets.Enabled())})
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT s.id,s.site_key,s.name,s.service_name,s.allowed_domains,s.session_timeout_minutes,s.timezone,s.engagement_threshold_seconds,s.active,s.tracking_key_prefix,s.server_api_key_prefix,s.created_at,w.name,o.name,coalesce(q.max_exact_days,$3) FROM sites s JOIN workspaces w ON w.id=s.workspace_id JOIN organizations o ON o.id=w.organization_id LEFT JOIN query_policies q ON q.site_id=s.id WHERE $1 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$2) ORDER BY s.created_at`, p.Role, p.ID, defaultQueryPolicy().MaxExactDays)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var key, name, service, timezone, prefix, serverPrefix, workspace, org string
		var domains []string
		var timeout, engagementThreshold, maxExactDays int
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &key, &name, &service, &domains, &timeout, &timezone, &engagementThreshold, &active, &prefix, &serverPrefix, &created, &workspace, &org, &maxExactDays); err == nil {
			out = append(out, map[string]any{"id": id, "site_id": key, "name": name, "service_name": service, "allowed_domains": domains, "session_timeout_minutes": timeout, "timezone": timezone, "engagement_threshold_seconds": engagementThreshold, "active": active, "tracking_key_prefix": prefix, "server_api_key_prefix": serverPrefix, "created_at": created, "workspace": workspace, "organization": org,
				// The console builds its period options from this, so it never offers a
				// range the site's policy will refuse.
				"max_exact_days": maxExactDays})
		}
	}
	writeJSON(w, 200, out)
}
func siteKey() string {
	return "SITE_" + strings.ToUpper(strings.ReplaceAll(uuid.New().String()[:8], "-", ""))
}
func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		Name           string   `json:"name"`
		ServiceName    string   `json:"service_name"`
		AllowedDomains []string `json:"allowed_domains"`
		SessionTimeout int      `json:"session_timeout_minutes"`
		Timezone       string   `json:"timezone"`
		Engagement     int      `json:"engagement_threshold_seconds"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_NAME", "site name is required")
		return
	}
	if in.SessionTimeout == 0 {
		in.SessionTimeout = 30
	}
	if in.Timezone == "" {
		_ = s.DB.QueryRow(r.Context(), `SELECT coalesce(nullif(value->>'timezone',''),'Asia/Seoul') FROM settings WHERE key='general'`).Scan(&in.Timezone)
		if in.Timezone == "" {
			in.Timezone = "Asia/Seoul"
		}
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		writeError(w, 400, "INVALID_TIMEZONE", "timezone must be a valid IANA timezone")
		return
	}
	if in.Engagement == 0 {
		in.Engagement = 10
	}
	if in.Engagement < 1 || in.Engagement > 300 {
		writeError(w, 400, "INVALID_ENGAGEMENT_THRESHOLD", "engagement threshold must be between 1 and 300 seconds")
		return
	}
	plain, hash, prefix, _ := auth.NewToken("mom_track_", 24)
	serverPlain, serverHash, serverPrefix, _ := auth.NewToken("mom_server_", 32)
	publicSiteID := siteKey()
	var id uuid.UUID
	var workspaceID uuid.UUID
	err := s.DB.QueryRow(r.Context(), `SELECT id FROM workspaces ORDER BY created_at LIMIT 1`).Scan(&workspaceID)
	if err == nil {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,session_timeout_minutes,timezone,engagement_threshold_seconds,tracking_key_secret,server_api_key_secret) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, workspaceID, publicSiteID, in.Name, in.ServiceName, hash, prefix, serverHash, serverPrefix, normalizeDomains(in.AllowedDomains), in.SessionTimeout, in.Timezone, in.Engagement, s.sealSecret(plain), s.sealSecret(serverPlain)).Scan(&id)
	}
	if err != nil {
		writeError(w, 500, "SITE_CREATE_FAILED", err.Error())
		return
	}
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO retention_policies(site_id) VALUES($1) ON CONFLICT(site_id) DO NOTHING`, id)
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO site_environments(site_id,name,label,contract_mode,cardinality_limit) VALUES
		($1,'dev','Development','allow',50000),($1,'stg','Staging','warn',25000),($1,'prd','Production','warn',10000) ON CONFLICT DO NOTHING`, id)
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO query_policies(site_id) VALUES($1) ON CONFLICT DO NOTHING`, id)
	_, _ = s.DB.Exec(r.Context(), `INSERT INTO semantic_metrics(site_id,name,label,description,definition,format) VALUES
		($1,'events','Events','수집된 이벤트 수','{"type":"count"}','number'),
		($1,'users','Users','Canonical 사용자 수','{"type":"unique_users"}','number'),
		($1,'sessions','Sessions','세션 수','{"type":"unique_sessions"}','number'),
		($1,'page_views','Page Views','페이지 조회 수','{"type":"count","event_name":"page_view"}','number'),
		($1,'conversions','Conversions','전환 이벤트 수','{"type":"count","conversion":true}','number'),
		($1,'conversion_users','Conversion Users','전환 사용자 수','{"type":"unique_users","conversion":true}','number'),
		($1,'revenue','Revenue','구매 매출 합계','{"type":"sum","event_name":"purchase","property":"value","fallback_property":"revenue"}','currency') ON CONFLICT DO NOTHING`, id)
	s.audit(r.Context(), &p, "site.create", "site", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	endpoint := s.publicURL(r.Context(), r)
	writeJSON(w, 201, map[string]any{
		"id":                 id,
		"site_id":            publicSiteID,
		"name":               in.Name,
		"collector_endpoint": endpoint,
		"tracking_code":      trackingSnippet(endpoint, publicSiteID, "prd", "full"),
		"tracking_key":       plain,
		"server_api_key":     serverPlain,
		"recoverable":        s.Secrets.Enabled(),
		"csp":                cspGuidance(endpoint),
		"message":            keyStorageMessage(s.Secrets.Enabled()),
	})
}
func normalizeDomains(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimPrefix(v, "https://")
		v = strings.TrimPrefix(v, "http://")
		v = strings.Split(v, "/")[0]
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	if err != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	var in struct {
		Name           string   `json:"name"`
		ServiceName    string   `json:"service_name"`
		AllowedDomains []string `json:"allowed_domains"`
		SessionTimeout int      `json:"session_timeout_minutes"`
		Timezone       string   `json:"timezone"`
		Engagement     int      `json:"engagement_threshold_seconds"`
		Active         *bool    `json:"active"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.SessionTimeout < 1 || in.SessionTimeout > 1440 {
		writeError(w, 400, "INVALID_TIMEOUT", "session timeout must be between 1 and 1440 minutes")
		return
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		writeError(w, 400, "INVALID_TIMEZONE", "timezone must be a valid IANA timezone")
		return
	}
	if in.Engagement < 1 || in.Engagement > 300 {
		writeError(w, 400, "INVALID_ENGAGEMENT_THRESHOLD", "engagement threshold must be between 1 and 300 seconds")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "SITE_UPDATE_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	var previousTimezone string
	if err := tx.QueryRow(r.Context(), `SELECT timezone FROM sites WHERE id=$1 FOR UPDATE`, id).Scan(&previousTimezone); err != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	result, err := tx.Exec(r.Context(), `UPDATE sites SET name=$2,service_name=$3,allowed_domains=$4,session_timeout_minutes=$5,timezone=$6,engagement_threshold_seconds=$7,active=coalesce($8,active),updated_at=now() WHERE id=$1`, id, in.Name, in.ServiceName, normalizeDomains(in.AllowedDomains), in.SessionTimeout, in.Timezone, in.Engagement, in.Active)
	if err == nil && result.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET engaged=(extract(epoch FROM (last_event_at-started_at)) >= $2 OR conversion_count>0 OR page_views>=2 OR active_engagement_ms >= $2::bigint*1000),updated_at=now() WHERE site_id=$1`, id, in.Engagement)
	}
	if err == nil && previousTimezone != in.Timezone {
		err = service.RebuildSiteDailyAggregates(r.Context(), tx, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "SITE_UPDATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "site.update", "site", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	if err != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	var key string
	if s.DB.QueryRow(r.Context(), `SELECT site_key FROM sites WHERE id=$1`, id).Scan(&key) != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	if r.URL.Query().Get("confirm") != key {
		writeError(w, 400, "CONFIRMATION_REQUIRED", "confirm query parameter must exactly match the Site ID")
		return
	}
	s.audit(r.Context(), &p, "site.delete", "site", id.String(), map[string]any{"site_id": key}, clientIP(r))
	if _, err := s.DB.Exec(r.Context(), `DELETE FROM sites WHERE id=$1`, id); err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	w.WriteHeader(204)
}
func (s *Server) rotateSiteKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	if err != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	plain, hash, prefix, _ := auth.NewToken("mom_track_", 24)
	tag, err := s.DB.Exec(r.Context(), `UPDATE sites SET tracking_key_hash=$2,tracking_key_prefix=$3,tracking_key_secret=$4,updated_at=now() WHERE id=$1`, id, hash, prefix, s.sealSecret(plain))
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	s.audit(r.Context(), &p, "site.key.rotate", "site", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]any{"tracking_key": plain, "recoverable": s.Secrets.Enabled(), "message": "The previous key is invalid. " + keyStorageMessage(s.Secrets.Enabled())})
}
func (s *Server) rotateServerKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	if err != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	plain, hash, prefix, _ := auth.NewToken("mom_server_", 32)
	tag, err := s.DB.Exec(r.Context(), `UPDATE sites SET server_api_key_hash=$2,server_api_key_prefix=$3,server_api_key_secret=$4,updated_at=now() WHERE id=$1`, id, hash, prefix, s.sealSecret(plain))
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	s.audit(r.Context(), &p, "site.server_key.rotate", "site", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]any{"server_api_key": plain, "recoverable": s.Secrets.Enabled(), "message": "The previous server key is invalid. " + keyStorageMessage(s.Secrets.Enabled())})
}
func (s *Server) trackingCode(w http.ResponseWriter, r *http.Request) {
	id, parsed, err := s.resolveSiteByID(r, "id")
	if !parsed {
		writeError(w, 400, "INVALID_ID", "invalid site id")
		return
	}
	var siteID string
	if err != nil || s.DB.QueryRow(r.Context(), `SELECT site_key FROM sites WHERE id=$1`, id).Scan(&siteID) != nil {
		writeError(w, 404, "NOT_FOUND", "site not found")
		return
	}
	endpoint := s.publicURL(r.Context(), r)
	environment := requestEnvironment(r)
	code := trackingSnippet(endpoint, siteID, environment, "full")
	writeJSON(w, 200, map[string]any{"site_id": siteID, "environment": environment, "collector_endpoint": endpoint, "tracking_code": code, "csp": cspGuidance(endpoint)})
}

func trackingSnippet(endpoint, siteID, environment, mode string) string {
	return fmt.Sprintf(`<script async src="%s/tracker.js" data-site-id="%s" data-environment="%s" data-contract-version="1" data-mode="%s" data-auto-rum="true"></script>`,
		html.EscapeString(strings.TrimSuffix(endpoint, "/")),
		html.EscapeString(siteID),
		html.EscapeString(environment),
		html.EscapeString(mode),
	)
}

func keyStorageMessage(recoverable bool) string {
	if recoverable {
		return "The key is stored encrypted with MOMENTO_ENCRYPTION_KEY and can be shown again after a restart."
	}
	return "Store the key now; without MOMENTO_ENCRYPTION_KEY it cannot be shown again."
}

// cspGuidance explains what the measured application has to allow. A tracked page
// that only allows connect-src 'self' blocks the collector request, and the fix is
// either to allow the collector origin or to proxy it as a first party path.
func cspGuidance(endpoint string) map[string]string {
	origin := collectorOrigin(endpoint)
	return map[string]string{
		"collector_origin": origin,
		"connect_src":      origin,
		"script_src":       origin,
		"header":           fmt.Sprintf("Content-Security-Policy: script-src 'self' %s; connect-src 'self' %s", origin, origin),
		"meta":             fmt.Sprintf(`<meta http-equiv="Content-Security-Policy" content="script-src 'self' %s; connect-src 'self' %s">`, origin, origin),
		"proxy_snippet": fmt.Sprintf(`location /momento/ {
  proxy_pass %s/;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
  proxy_set_header X-Forwarded-Proto $scheme;
}`, origin),
		"proxy_snippet_note": "If the tracked application cannot change its CSP, proxy the collector on the same origin and add data-endpoint=\"/momento\" to the tracker script tag.",
	}
}

func collectorOrigin(endpoint string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(endpoint), "/")
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return parsed.Scheme + "://" + parsed.Host
	}
	return trimmed
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT key,value,updated_at FROM settings ORDER BY key`)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		var updated time.Time
		if rows.Scan(&key, &raw, &updated) != nil {
			continue
		}
		var value any
		_ = json.Unmarshal(raw, &value)
		if key == "oidc" {
			if m, ok := value.(map[string]any); ok {
				if secret, ok := m["client_secret"].(string); ok && secret != "" {
					m["client_secret"] = "********"
				}
			}
		}
		out[key] = map[string]any{"value": value, "updated_at": updated}
	}
	writeJSON(w, 200, out)
}
func allowedSetting(key string) bool {
	switch key {
	case "general", "oidc", "privacy", "storage", "security", "automation":
		return true
	}
	return false
}
func (s *Server) putSetting(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !allowedSetting(key) {
		writeError(w, 400, "UNKNOWN_SETTING", "unsupported setting group")
		return
	}
	var value map[string]any
	if err := decodeJSON(r, &value, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if key == "oidc" {
		if value["client_secret"] == "********" {
			var raw []byte
			if s.DB.QueryRow(r.Context(), `SELECT value FROM settings WHERE key='oidc'`).Scan(&raw) == nil {
				var old map[string]any
				_ = json.Unmarshal(raw, &old)
				value["client_secret"] = old["client_secret"]
			}
		} else if plain, ok := value["client_secret"].(string); ok && plain != "" && !secret.Sealed(plain) && s.Secrets.Enabled() {
			sealed, sealErr := s.Secrets.Encrypt(plain)
			if sealErr != nil {
				writeError(w, 500, "SECRET_SEAL_FAILED", sealErr.Error())
				return
			}
			value["client_secret"] = sealed
		}
	}
	if err := validateAdminSetting(key, value); err != nil {
		writeError(w, 400, "INVALID_SETTING", err.Error())
		return
	}
	body, _ := json.Marshal(value)
	_, err := s.DB.Exec(r.Context(), `INSERT INTO settings(key,value,updated_by,updated_at) VALUES($1,$2,$3,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`, key, body, p.ID)
	if err != nil {
		writeError(w, 500, "SETTING_UPDATE_FAILED", err.Error())
		return
	}
	if key == "security" || key == "general" {
		// public_url and the extra connect origins both shape the console CSP.
		s.reloadSecurity(r.Context())
	}
	s.audit(r.Context(), &p, "setting.update", "setting", key, map[string]any{"group": key}, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}
func validateAdminSetting(key string, value map[string]any) error {
	number := func(name string, min, max float64) error {
		raw, ok := value[name]
		if !ok {
			return nil
		}
		n, ok := raw.(float64)
		if !ok || n < min || n > max {
			return fmt.Errorf("%s must be between %.0f and %.0f", name, min, max)
		}
		return nil
	}
	switch key {
	case "general":
		if raw, _ := value["public_url"].(string); raw != "" {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("public_url must be an absolute HTTP(S) URL")
			}
		}
	case "oidc":
		enabled, _ := value["enabled"].(bool)
		if enabled {
			issuer, _ := value["issuer_url"].(string)
			client, _ := value["client_id"].(string)
			u, err := url.Parse(issuer)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || client == "" {
				return fmt.Errorf("enabled OIDC requires a valid issuer_url and client_id")
			}
		}
	case "privacy":
		if err := number("raw_event_retention_months", 1, 120); err != nil {
			return err
		}
		if err := number("debug_retention_days", 1, 90); err != nil {
			return err
		}
		if mode, ok := value["pii_detection_mode"].(string); ok {
			if !map[string]bool{"detect": true, "warn": true, "mask": true, "reject": true}[mode] {
				return fmt.Errorf("pii_detection_mode must be detect, warn, mask, or reject")
			}
		} else if value["pii_detection_mode"] != nil {
			return fmt.Errorf("pii_detection_mode must be a string")
		}
	case "security":
		if err := number("collector_rate_limit_per_minute", 1, 1000000); err != nil {
			return err
		}
		if err := number("max_events_per_request", 1, 1000); err != nil {
			return err
		}
		if err := number("max_payload_bytes", 1024, 10<<20); err != nil {
			return err
		}
		if values, ok := value["trusted_proxy_cidrs"].([]any); ok {
			for _, item := range values {
				cidr, ok := item.(string)
				if !ok {
					return fmt.Errorf("trusted_proxy_cidrs must contain strings")
				}
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return fmt.Errorf("invalid trusted proxy CIDR %q", cidr)
				}
			}
		} else if _, ok := value["trusted_proxy_cidrs"].([]string); !ok && value["trusted_proxy_cidrs"] != nil {
			return fmt.Errorf("trusted_proxy_cidrs must be an array")
		}
		if values, ok := value["additional_connect_origins"].([]any); ok {
			for _, item := range values {
				origin, ok := item.(string)
				if !ok {
					return fmt.Errorf("additional_connect_origins must contain strings")
				}
				if normalizeConnectOrigin(origin) == "" {
					return fmt.Errorf("invalid connect origin %q; use scheme://host[:port]", origin)
				}
			}
		} else if value["additional_connect_origins"] != nil {
			if _, ok := value["additional_connect_origins"].([]string); !ok {
				return fmt.Errorf("additional_connect_origins must be an array")
			}
		}
	case "storage":
		engine, _ := value["engine"].(string)
		if engine != "postgres" {
			return fmt.Errorf("only the postgres storage engine is available in this release")
		}
	case "automation":
		if err := number("delivery_timeout_seconds", 1, 60); err != nil {
			return err
		}
		if err := number("max_entity_ids", 0, 1000); err != nil {
			return err
		}
		values, ok := value["allowed_webhook_hosts"].([]any)
		if !ok && value["allowed_webhook_hosts"] != nil {
			return fmt.Errorf("allowed_webhook_hosts must be an array")
		}
		for _, item := range values {
			host, ok := item.(string)
			host = strings.TrimSpace(strings.TrimPrefix(host, "*."))
			if !ok || host == "" || strings.ContainsAny(host, "/:@") {
				return fmt.Errorf("allowed_webhook_hosts contains an invalid host")
			}
		}
	}
	return nil
}

func (s *Server) listNetworks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,cidr::text,description,internal,created_at FROM network_ranges ORDER BY masklen(cidr) DESC,name`)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, cidr, desc string
		var internal bool
		var created time.Time
		if rows.Scan(&id, &name, &cidr, &desc, &internal, &created) == nil {
			out = append(out, map[string]any{"id": id, "name": name, "cidr": cidr, "description": desc, "internal": internal, "created_at": created})
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		Name        string `json:"name"`
		CIDR        string `json:"cidr"`
		Description string `json:"description"`
		Internal    *bool  `json:"internal"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if _, _, err := net.ParseCIDR(in.CIDR); err != nil {
		writeError(w, 400, "INVALID_CIDR", "CIDR is invalid")
		return
	}
	internal := true
	if in.Internal != nil {
		internal = *in.Internal
	}
	var id uuid.UUID
	err := s.DB.QueryRow(r.Context(), `INSERT INTO network_ranges(name,cidr,description,internal) VALUES($1,$2,$3,$4) RETURNING id`, in.Name, in.CIDR, in.Description, internal).Scan(&id)
	if err != nil {
		writeError(w, 500, "NETWORK_CREATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "network.create", "network", id.String(), map[string]any{"cidr": in.CIDR}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id})
}
func (s *Server) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid network id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM network_ranges WHERE id=$1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "network not found")
		return
	}
	s.audit(r.Context(), &p, "network.delete", "network", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT id,email,display_name,department,organization_name,role,active,oidc_subject IS NOT NULL,created_at FROM users ORDER BY created_at`)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var email, name, dept, org, role string
		var active, oidc bool
		var created time.Time
		if rows.Scan(&id, &email, &name, &dept, &org, &role, &active, &oidc, &created) == nil {
			out = append(out, map[string]any{"id": id, "email": email, "display_name": name, "department": dept, "organization_name": org, "role": role, "active": active, "oidc": oidc, "created_at": created})
		}
	}
	writeJSON(w, 200, out)
}
func validRole(role string) bool {
	switch role {
	case "super_admin", "organization_admin", "workspace_admin", "analyst", "viewer":
		return true
	}
	return false
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		Email            string `json:"email"`
		DisplayName      string `json:"display_name"`
		Department       string `json:"department"`
		OrganizationName string `json:"organization_name"`
		Role             string `json:"role"`
		Password         string `json:"password"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !validRole(in.Role) || len(in.Password) < 12 {
		writeError(w, 400, "INVALID_USER", "valid role and password of at least 12 characters are required")
		return
	}
	// Creating an account is a way to grant a role, so it is bounded the same way
	// as changing one.
	if auth.RoleAbove(in.Role, p.Role) {
		writeError(w, 403, "ROLE_ABOVE_CALLER", "you cannot create an account with more authority than your own")
		return
	}
	hash, _ := auth.HashPassword(in.Password)
	var id uuid.UUID
	err := s.DB.QueryRow(r.Context(), `INSERT INTO users(email,display_name,department,organization_name,role,password_hash) VALUES(lower($1),$2,$3,$4,$5,$6) RETURNING id`, in.Email, in.DisplayName, in.Department, in.OrganizationName, in.Role, hash).Scan(&id)
	if err != nil {
		writeError(w, 409, "USER_CREATE_FAILED", err.Error())
		return
	}
	if in.Role != "super_admin" && in.Role != "organization_admin" {
		_, _ = s.DB.Exec(r.Context(), `INSERT INTO user_workspace_roles(user_id,workspace_id,role) SELECT $1,id,$2 FROM workspaces ORDER BY created_at LIMIT 1 ON CONFLICT(user_id,workspace_id) DO UPDATE SET role=excluded.role`, id, in.Role)
	}
	s.audit(r.Context(), &p, "user.create", "user", id.String(), map[string]any{"email": in.Email, "role": in.Role}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id})
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid user id")
		return
	}
	var in struct {
		DisplayName      string `json:"display_name"`
		Department       string `json:"department"`
		OrganizationName string `json:"organization_name"`
		Role             string `json:"role"`
		Active           *bool  `json:"active"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !validRole(in.Role) {
		writeError(w, 400, "INVALID_ROLE", "role is invalid")
		return
	}
	if id == p.ID && in.Active != nil && !*in.Active {
		writeError(w, 400, "SELF_DISABLE", "you cannot disable your own account")
		return
	}
	// Nothing bounded the role being granted. Measured: a workspace_admin set its
	// own role to super_admin and the next request passed every workspace check in
	// the service.
	if auth.RoleAbove(in.Role, p.Role) {
		writeError(w, 403, "ROLE_ABOVE_CALLER", "you cannot grant more authority than your own")
		return
	}
	if id == p.ID {
		var current string
		if s.DB.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, id).Scan(&current) == nil && current != in.Role {
			writeError(w, 400, "SELF_ROLE", "you cannot change your own role")
			return
		}
	}
	_, err = s.DB.Exec(r.Context(), `UPDATE users SET display_name=$2,department=$3,organization_name=$4,role=$5,active=coalesce($6,active),updated_at=now() WHERE id=$1`, id, in.DisplayName, in.Department, in.OrganizationName, in.Role, in.Active)
	if err != nil {
		writeError(w, 500, "USER_UPDATE_FAILED", err.Error())
		return
	}
	if in.Role == "super_admin" || in.Role == "organization_admin" {
		_, _ = s.DB.Exec(r.Context(), `DELETE FROM user_workspace_roles WHERE user_id=$1`, id)
	} else {
		_, _ = s.DB.Exec(r.Context(), `INSERT INTO user_workspace_roles(user_id,workspace_id,role) SELECT $1,id,$2 FROM workspaces ORDER BY created_at LIMIT 1 ON CONFLICT(user_id,workspace_id) DO UPDATE SET role=excluded.role`, id, in.Role)
	}
	s.audit(r.Context(), &p, "user.update", "user", id.String(), map[string]any{"role": in.Role}, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT a.id,a.action,a.resource_type,a.resource_id,a.detail,a.client_ip::text,a.created_at,coalesce(u.display_name,'System'),coalesce(u.email,'') FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id ORDER BY a.created_at DESC LIMIT 500`)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var action, rt, name, email string
		var rid, ip *string
		var raw []byte
		var created time.Time
		if rows.Scan(&id, &action, &rt, &rid, &raw, &ip, &created, &name, &email) == nil {
			var detail any
			_ = json.Unmarshal(raw, &detail)
			out = append(out, map[string]any{"id": id, "action": action, "resource_type": rt, "resource_id": rid, "detail": detail, "client_ip": ip, "created_at": created, "actor": name, "actor_email": email})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) trackingDebugger(w http.ResponseWriter, r *http.Request) {
	siteKey := r.URL.Query().Get("site_id")
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT e.event_id,e.event_timestamp,e.received_at,e.event_name,e.visitor_id,e.session_id,e.page_url,e.client_ip::text,e.network_name,e.properties,e.traffic_class,e.environment,e.contract_version FROM raw_events e JOIN sites s ON s.id=e.site_id WHERE ($1='' OR s.site_key=$1) AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3)) ORDER BY e.received_at DESC LIMIT 200`, siteKey, p.Role, p.ID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	events, listErr := rowsToList(rows, func() (map[string]any, error) {
		var eventID uuid.UUID
		var occurred, received time.Time
		var name, visitor, session, traffic, environment string
		var contractVersion int
		// network_name is null for anything arriving from outside a named
		// internal network, which is most events. Scanning it into a string
		// failed on every one of those rows — and the failure was dropped, so the
		// debugger an operator opens to watch events arrive silently omitted
		// them, or showed nothing at all.
		var page, ip, network *string
		var raw []byte
		err := rows.Scan(&eventID, &occurred, &received, &name, &visitor, &session, &page, &ip, &network, &raw, &traffic, &environment, &contractVersion)
		var props any
		_ = json.Unmarshal(raw, &props)
		return map[string]any{"event_id": eventID, "event_timestamp": occurred, "received_at": received, "event_name": name, "visitor_id": visitor, "session_id": session, "page_url": page, "client_ip": ip, "network": network, "properties": props, "traffic_class": traffic, "environment": environment, "contract_version": contractVersion}, err
	})
	if listErr != nil {
		writeQueryError(w, listErr)
		return
	}
	errorRows, err := s.DB.Query(r.Context(), `SELECT receipt_id,site_key,attempts,error,created_at FROM (SELECT i.id receipt_id,s.site_key,i.attempts,i.last_error error,i.created_at FROM event_inbox i JOIN sites s ON s.id=i.site_id WHERE i.processed_at IS NULL AND i.last_error IS NOT NULL AND ($1='' OR s.site_key=$1) AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3)) UNION ALL SELECT d.inbox_id,s.site_key,10,d.error,d.failed_at FROM event_dead_letters d JOIN sites s ON s.id=d.site_id WHERE ($1='' OR s.site_key=$1) AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3))) failures ORDER BY created_at DESC LIMIT 100`, siteKey, p.Role, p.ID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	errorsList, listErr := rowsToList(errorRows, func() (map[string]any, error) {
		var id int64
		var site string
		var attempts int
		var message string
		var created time.Time
		err := errorRows.Scan(&id, &site, &attempts, &message, &created)
		return map[string]any{"receipt_id": id, "site_id": site, "attempts": attempts, "error": message, "created_at": created}, err
	})
	if listErr != nil {
		writeQueryError(w, listErr)
		return
	}
	writeJSON(w, 200, map[string]any{"events": events, "errors": errorsList})
}

func (s *Server) deleteAnalyticsData(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		SiteID  string `json:"site_id"`
		Mode    string `json:"mode"`
		Value   string `json:"value"`
		From    string `json:"from"`
		To      string `json:"to"`
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.Confirm != "DELETE" {
		writeError(w, 400, "CONFIRMATION_REQUIRED", "confirm must be DELETE")
		return
	}
	var siteID uuid.UUID
	if s.DB.QueryRow(r.Context(), `SELECT id FROM sites WHERE site_key=$1`, in.SiteID).Scan(&siteID) != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var deleteFrom, deleteTo time.Time
	var err error
	switch in.Mode {
	case "site", "visitor", "user_id", "property":
	case "period":
		// Deliberately not policy-limited: a deletion has to reach as far back as
		// the data goes.
		deleteFrom, deleteTo, err = s.parseDateRange(r.Context(), siteID, in.From, in.To)
		if err != nil {
			writeRangeError(w, err)
			return
		}
	default:
		writeError(w, 400, "INVALID_MODE", "mode must be site, visitor, user_id, period, or property")
		return
	}
	if in.Mode == "property" && !eventNamePatternForProperty.MatchString(in.Value) {
		writeError(w, 400, "INVALID_PROPERTY", "property name is invalid")
		return
	}
	var tag pgconn.CommandTag
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	linkedVisitors := []string{}
	if in.Mode == "user_id" {
		rows, queryErr := tx.Query(r.Context(), `SELECT visitor_id FROM visitor_identities WHERE site_id=$1 AND user_id=$2`, siteID, in.Value)
		if queryErr != nil {
			writeError(w, 500, "IDENTITY_LOOKUP_FAILED", queryErr.Error())
			return
		}
		for rows.Next() {
			var visitorID string
			if scanErr := rows.Scan(&visitorID); scanErr != nil {
				rows.Close()
				writeError(w, 500, "IDENTITY_LOOKUP_FAILED", scanErr.Error())
				return
			}
			linkedVisitors = append(linkedVisitors, visitorID)
		}
		rows.Close()
		if rows.Err() != nil {
			writeError(w, 500, "IDENTITY_LOOKUP_FAILED", rows.Err().Error())
			return
		}
	}
	// The inbox also contains the original event payload for the debugger and
	// retries. Scrub it before Raw Events so a worker already holding a row lock
	// commits first and its newly written event is covered by the deletion.
	if err := scrubQueuedAnalyticsData(r.Context(), tx, siteID, in.Mode, in.Value, linkedVisitors, deleteFrom, deleteTo); err != nil {
		writeError(w, 500, "QUEUE_SCRUB_FAILED", err.Error())
		return
	}
	switch in.Mode {
	case "site":
		tag, err = tx.Exec(r.Context(), `DELETE FROM raw_events WHERE site_id=$1`, siteID)
	case "visitor":
		tag, err = tx.Exec(r.Context(), `DELETE FROM raw_events WHERE site_id=$1 AND visitor_id=$2`, siteID, in.Value)
	case "user_id":
		tag, err = tx.Exec(r.Context(), `DELETE FROM raw_events WHERE site_id=$1 AND (user_id=$2 OR visitor_id=ANY($3))`, siteID, in.Value, linkedVisitors)
	case "period":
		tag, err = tx.Exec(r.Context(), `DELETE FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, deleteFrom, deleteTo)
	case "property":
		tag, err = tx.Exec(r.Context(), `UPDATE raw_events SET properties=properties-$2 WHERE site_id=$1 AND properties ? $2`, siteID, in.Value)
	}
	if err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	if in.Mode != "property" {
		if err := service.RebuildSiteDerivedData(r.Context(), tx, siteID); err != nil {
			writeError(w, 500, "DERIVED_REBUILD_FAILED", err.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "analytics.delete", "raw_event", in.SiteID, map[string]any{"mode": in.Mode, "value": in.Value, "affected": tag.RowsAffected()}, clientIP(r))
	writeJSON(w, 200, map[string]any{"deleted_or_updated": tag.RowsAffected()})
}

func scrubQueuedAnalyticsData(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, mode, value string, linkedVisitors []string, from, to time.Time) error {
	if mode == "site" {
		if _, err := tx.Exec(ctx, `DELETE FROM event_dead_letters WHERE site_id=$1`, siteID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM event_inbox WHERE site_id=$1`, siteID)
		return err
	}
	if mode == "visitor" || mode == "user_id" {
		for _, table := range []string{"event_inbox", "event_dead_letters"} {
			var err error
			if mode == "visitor" {
				query := fmt.Sprintf(`DELETE FROM %s WHERE site_id=$1 AND payload->'request'->>'visitor_id'=$2`, table)
				_, err = tx.Exec(ctx, query, siteID, value)
			} else {
				query := fmt.Sprintf(`DELETE FROM %s WHERE site_id=$1 AND (payload->'request'->>'user_id'=$2 OR payload->'request'->>'visitor_id'=ANY($3))`, table)
				_, err = tx.Exec(ctx, query, siteID, value, linkedVisitors)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}

	type queuedPayload struct {
		table   string
		id      int64
		payload []byte
	}
	records := []queuedPayload{}
	for _, table := range []string{"event_inbox", "event_dead_letters"} {
		query := fmt.Sprintf(`SELECT id,payload FROM %s WHERE site_id=$1 FOR UPDATE`, table)
		rows, err := tx.Query(ctx, query, siteID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var record queuedPayload
			record.table = table
			if err := rows.Scan(&record.id, &record.payload); err != nil {
				rows.Close()
				return err
			}
			records = append(records, record)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	now := time.Now()
	for _, record := range records {
		var payload model.InboxPayload
		if err := json.Unmarshal(record.payload, &payload); err != nil {
			// An unreadable payload cannot be proven free of the selected data.
			query := fmt.Sprintf(`DELETE FROM %s WHERE id=$1`, record.table)
			if _, deleteErr := tx.Exec(ctx, query, record.id); deleteErr != nil {
				return deleteErr
			}
			continue
		}
		if mode == "period" {
			kept := make([]model.IncomingEvent, 0, len(payload.Request.Events))
			for _, event := range payload.Request.Events {
				occurred := time.UnixMilli(event.Timestamp)
				if event.Timestamp <= 0 || occurred.Before(now.AddDate(-5, 0, 0)) || occurred.After(now.Add(24*time.Hour)) {
					occurred = now
				}
				if occurred.Before(from) || !occurred.Before(to) {
					kept = append(kept, event)
				}
			}
			payload.Request.Events = kept
		} else if mode == "property" {
			for index := range payload.Request.Events {
				delete(payload.Request.Events[index].Properties, value)
			}
		}
		if len(payload.Request.Events) == 0 {
			query := fmt.Sprintf(`DELETE FROM %s WHERE id=$1`, record.table)
			if _, err := tx.Exec(ctx, query, record.id); err != nil {
				return err
			}
			continue
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		query := fmt.Sprintf(`UPDATE %s SET payload=$2 WHERE id=$1`, record.table)
		if _, err := tx.Exec(ctx, query, record.id, body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) listEventDefinitions(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site_id")
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT e.id,e.site_id,s.site_key,e.name,e.description,e.schema,e.validation_mode,e.conversion,e.current_version,e.owner,e.created_at FROM event_definitions e JOIN sites s ON s.id=e.site_id WHERE ($1='' OR s.site_key=$1) AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3)) ORDER BY e.name`, site, p.Role, p.ID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, siteUUID uuid.UUID
		var siteKey, name, desc, mode, owner string
		var raw []byte
		var conversion bool
		var currentVersion int
		var created time.Time
		if rows.Scan(&id, &siteUUID, &siteKey, &name, &desc, &raw, &mode, &conversion, &currentVersion, &owner, &created) == nil {
			var schema any
			_ = json.Unmarshal(raw, &schema)
			out = append(out, map[string]any{"id": id, "site_uuid": siteUUID, "site_id": siteKey, "name": name, "description": desc, "schema": schema, "validation_mode": mode, "conversion": conversion, "current_version": currentVersion, "owner": owner, "created_at": created})
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) upsertEventDefinition(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		SiteID         string         `json:"site_id"`
		Name           string         `json:"name"`
		Description    string         `json:"description"`
		Schema         map[string]any `json:"schema"`
		ValidationMode string         `json:"validation_mode"`
		Conversion     bool           `json:"conversion"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if in.ValidationMode != "allow" && in.ValidationMode != "warn" && in.ValidationMode != "reject" {
		writeError(w, 400, "INVALID_MODE", "validation mode must be allow, warn, or reject")
		return
	}
	body, _ := json.Marshal(in.Schema)
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	var id, siteID uuid.UUID
	err = tx.QueryRow(r.Context(), `INSERT INTO event_definitions(site_id,name,description,schema,validation_mode,conversion) SELECT id,$2,$3,$4,$5,$6 FROM sites WHERE site_key=$1 ON CONFLICT(site_id,name) DO UPDATE SET description=excluded.description,conversion=excluded.conversion,updated_at=now() RETURNING id,site_id`, in.SiteID, in.Name, in.Description, body, in.ValidationMode, in.Conversion).Scan(&id, &siteID)
	if err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	var version int
	if err := tx.QueryRow(r.Context(), `SELECT coalesce(max(version),0)+1 FROM event_contract_versions WHERE site_id=$1 AND event_name=$2`, siteID, in.Name).Scan(&version); err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE event_contract_versions SET status='deprecated' WHERE site_id=$1 AND event_name=$2 AND status='active'`, siteID, in.Name)
	if _, err := tx.Exec(r.Context(), `INSERT INTO event_contract_versions(site_id,event_name,version,schema,validation_mode,status,changelog,created_by,activated_at) VALUES($1,$2,$3,$4,$5,'active','Legacy schema editor',$6,now())`, siteID, in.Name, version, body, in.ValidationMode, p.ID); err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE event_definitions SET schema=$3,validation_mode=$4,current_version=$5,updated_at=now() WHERE site_id=$1 AND name=$2`, siteID, in.Name, body, in.ValidationMode, version); err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "DEFINITION_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "event_definition.save", "event_definition", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id, "version": version})
}

var _ = pgx.ErrNoRows
