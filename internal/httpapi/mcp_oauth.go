package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/jackc/pgx/v5"
)

// MCP with SSO — no personal key, a Keycloak access token instead.
//
// The MCP authorization specification (2025-06-18 and later) is OAuth 2.1: this
// server is a *resource server*. It publishes where its authorization server is
// (RFC 9728, /.well-known/oauth-protected-resource), a client refused with 401
// reads that document, sends the person through Keycloak with PKCE, and comes
// back with an access token whose audience (RFC 8707) is this server. Nothing
// about issuing tokens happens here — Keycloak does that — and this file only
// answers two questions: where is the authorization server, and is this token
// one it issued for us.
//
// The personal key stays. It is what an automation with no person behind it
// uses, and what a deployment without Keycloak uses. A token from SSO is a
// second door into the same room: it authenticates an *existing* Momento
// account, is held to every rule a key is held to, and is accepted at /mcp and
// nowhere else. It never creates an account — signing in to the web once is
// what provisions one — and a token's role claim never becomes a Momento role.

// mcpOAuthSetting is the `mcp.oauth` settings group as the console stores it.
// Off by default, so a deployment that never opens the card is unchanged.
type mcpOAuthSetting struct {
	Enabled bool `json:"enabled"`
	// Resource is the identifier this server claims (RFC 8707). Empty means the
	// public URL plus /mcp.
	Resource string `json:"resource"`
	// Audience is a space-separated list of accepted aud or azp values — the
	// compatibility path for a Keycloak client without an Audience mapper.
	Audience string `json:"audience"`
	// Scopes are what an SSO principal is granted, space-separated. The token's
	// own scope claim is not consulted: a token does not carry this product's
	// scope vocabulary unless somebody teaches Keycloak that vocabulary, and the
	// administrator states the ceiling here once instead.
	Scopes string `json:"scopes"`
}

// defaultMCPOAuthScopes matches what a freshly issued personal key holds.
const defaultMCPOAuthScopes = "analytics:read"

// mcpOAuthConfig is the setting resolved against the OIDC and general groups
// it reuses: the issuer and email claim come from the web sign-in, the public
// URL from the general card.
type mcpOAuthConfig struct {
	Enabled    bool
	Issuer     string
	EmailClaim string
	// Resource is empty only when neither mcp.oauth.resource nor
	// general.public_url is set, in which case it is derived per request.
	Resource  string
	Audiences []string
	Scopes    []string
}

// active is the condition under which SSO tokens are accepted at all: switched
// on, and an issuer to verify them against. Switched on without an issuer
// behaves as off, and the caller says so in the log.
func (c mcpOAuthConfig) active() bool { return c.Enabled && c.Issuer != "" }

func (s *Server) mcpOAuthConfig(ctx context.Context) (mcpOAuthConfig, error) {
	rows, err := s.DB.Query(ctx, `SELECT key,value FROM settings WHERE key IN ('mcp.oauth','oidc','general')`)
	if err != nil {
		return mcpOAuthConfig{}, err
	}
	defer rows.Close()
	setting := mcpOAuthSetting{Scopes: defaultMCPOAuthScopes}
	var web oidcSetting
	var general struct {
		PublicURL string `json:"public_url"`
	}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return mcpOAuthConfig{}, err
		}
		var target any
		switch key {
		case "mcp.oauth":
			target = &setting
		case "oidc":
			target = &web
		case "general":
			target = &general
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return mcpOAuthConfig{}, fmt.Errorf("setting %s: %w", key, err)
		}
	}
	if err := rows.Err(); err != nil {
		return mcpOAuthConfig{}, err
	}
	cfg := mcpOAuthConfig{
		Enabled:    setting.Enabled,
		Issuer:     strings.TrimRight(strings.TrimSpace(web.IssuerURL), "/"),
		EmailClaim: strings.TrimSpace(web.ClaimEmail),
		Resource:   strings.TrimSpace(setting.Resource),
		Audiences:  strings.Fields(setting.Audience),
		Scopes:     strings.Fields(setting.Scopes),
	}
	if cfg.EmailClaim == "" {
		cfg.EmailClaim = "email"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = strings.Fields(defaultMCPOAuthScopes)
	}
	if cfg.Resource == "" {
		if public := strings.TrimSuffix(strings.TrimSpace(general.PublicURL), "/"); public != "" {
			cfg.Resource = public + "/mcp"
		}
	}
	return cfg, nil
}

// mcpResource is the identifier this deployment claims for its MCP endpoint:
// what the metadata document advertises and what a token's aud may name. The
// request's own host is the last resort — anybody can set a Host header — so
// the guide tells an operator behind a proxy to set the public URL.
func (s *Server) mcpResource(r *http.Request, cfg mcpOAuthConfig) string {
	if cfg.Resource != "" {
		return cfg.Resource
	}
	scheme := "http"
	if s.requestSecure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/mcp"
}

// mcpMetadataURL is where a refused client is sent to learn the above. RFC 9728
// puts the document under the origin with the resource path appended, and the
// save-time check holds the resource path to /mcp so this address is one the
// router answers.
func (s *Server) mcpMetadataURL(r *http.Request, cfg mcpOAuthConfig) string {
	resource := s.mcpResource(r, cfg)
	origin := resource
	if parsed, err := url.Parse(resource); err == nil && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	return origin + "/.well-known/oauth-protected-resource/mcp"
}

// validMCPResource says whether a configured resource identifier is one this
// server can honour: an absolute HTTP(S) URL whose path is the MCP endpoint.
func validMCPResource(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Path == "/mcp"
}

// oauthProvider caches discovery per issuer. Discovery is a round trip to
// Keycloak and the JWKS behind it verifies every token; doing that per request
// would put Keycloak's latency in front of every MCP call. go-oidc refetches
// the key set on an unknown key id, so key rotation needs no invalidation here.
func (s *Server) oauthProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	if provider := s.oauthProviders[issuer]; provider != nil {
		return provider, nil
	}
	// The provider outlives this request — it keeps the context for later key
	// fetches — so it is detached from the request's cancellation and given a
	// client that will not wait on Keycloak forever.
	client := &http.Client{Timeout: 10 * time.Second}
	provider, err := oidc.NewProvider(oidc.ClientContext(context.WithoutCancel(ctx), client), issuer)
	if err != nil {
		return nil, err
	}
	if s.oauthProviders == nil {
		s.oauthProviders = map[string]*oidc.Provider{}
	}
	s.oauthProviders[issuer] = provider
	return provider, nil
}

// oauthRefusal is why a token was not accepted, in two voices: the message a
// client is shown, and the cause an operator reads in the log. The middleware
// logs every refusal at the one place it turns into a response, so no path can
// answer a client without leaving the cause behind.
type oauthRefusal struct {
	message string
	cause   error
}

// asymmetricAlgorithms is every signature this server accepts. HS* would let a
// token be minted by anyone who knows a public value, and "none" by anyone.
var asymmetricAlgorithms = []string{
	oidc.RS256, oidc.RS384, oidc.RS512,
	oidc.ES256, oidc.ES384, oidc.ES512,
	oidc.PS256, oidc.PS384, oidc.PS512,
}

// oauthPrincipal turns a bearer access token into a Momento principal, or says
// exactly why it will not. The caller has already established the token has
// the shape of a JWT and that SSO tokens are switched on.
func (s *Server) oauthPrincipal(ctx context.Context, r *http.Request, cfg mcpOAuthConfig, token string) (auth.Principal, *oauthRefusal) {
	provider, err := s.oauthProvider(ctx, cfg.Issuer)
	if err != nil {
		return auth.Principal{}, &oauthRefusal{
			message: "the SSO issuer could not be read, so the access token cannot be checked; retry shortly or tell an administrator",
			cause:   fmt.Errorf("discovery at %s: %w", cfg.Issuer, err),
		}
	}
	// Signature, issuer, expiry and nbf. The audience is checked below by
	// hand, because more than one value is acceptable and the library
	// compares one — and it reads aud only, never azp.
	verified, err := provider.Verifier(&oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: asymmetricAlgorithms}).Verify(ctx, token)
	if err != nil {
		return auth.Principal{}, &oauthRefusal{
			message: "the SSO access token is not valid (signature, issuer, expiry or not-before); sign in again from the client",
			cause:   fmt.Errorf("token rejected: %w", err),
		}
	}
	var claims struct {
		Type         string          `json:"typ"`
		AuthorizedTo string          `json:"azp"`
		Confirmation json.RawMessage `json:"cnf"`
	}
	if err := verified.Claims(&claims); err != nil {
		return auth.Principal{}, &oauthRefusal{message: "the SSO access token's claims cannot be read", cause: err}
	}
	// An ID token proves a sign-in happened; it is not a credential for an
	// API, and Keycloak marks it so.
	if strings.EqualFold(strings.TrimSpace(claims.Type), "ID") {
		return auth.Principal{}, &oauthRefusal{
			message: "an ID token is not an MCP credential; send the access token",
			cause:   errors.New("typ=ID"),
		}
	}
	// A token bound to a proof of possession (DPoP, mTLS) is only as good as
	// the proof, and this server verifies none.
	if len(claims.Confirmation) > 0 && string(claims.Confirmation) != "null" {
		return auth.Principal{}, &oauthRefusal{
			message: "a sender-constrained token (cnf) cannot be verified by this server",
			cause:   errors.New("cnf present"),
		}
	}
	subject := strings.TrimSpace(verified.Subject)
	if subject == "" {
		return auth.Principal{}, &oauthRefusal{message: "the SSO access token has no subject", cause: errors.New("sub empty")}
	}
	// Whom the token was minted for. Measured against a real Keycloak 26: an
	// access token issued to a client carries that client in azp and
	// aud=["account"] — the client id is not in aud, whatever an ID token does.
	// So the binding is "aud names this resource, or aud/azp names a client the
	// administrator listed". Either says the token is for this deployment
	// rather than passed through from another application in the realm, which
	// is what RFC 8707 and the MCP specification guard against.
	resource := s.mcpResource(r, cfg)
	azp := strings.TrimSpace(claims.AuthorizedTo)
	if !slices.Contains(verified.Audience, resource) && !audienceListed(cfg.Audiences, verified.Audience, azp) {
		suggested := azp
		if suggested == "" {
			suggested = "<the MCP client id>"
		}
		return auth.Principal{}, &oauthRefusal{
			message: fmt.Sprintf("the SSO access token was not issued for this server (aud %v, azp %q); add %q to the accepted audiences (mcp.oauth.audience) or give the Keycloak client an Audience mapper for %q",
				verified.Audience, azp, suggested, resource),
			cause: fmt.Errorf("audience %v / azp %q not accepted; resource %q, accepted %v", verified.Audience, azp, resource, cfg.Audiences),
		}
	}
	// The same lookup the web sign-in uses, without the provisioning half. The
	// subject is what the web sign-in stored; the email claim is the fallback
	// for an account that was created locally or by an administrator before
	// its first SSO sign-in, and it is the same claim the web sign-in links by.
	email := ""
	var extra map[string]any
	if verified.Claims(&extra) == nil {
		email = strings.TrimSpace(claimString(extra, cfg.EmailClaim, "email"))
	}
	p := auth.Principal{AuthType: "oauth", Scopes: slices.Clone(cfg.Scopes)}
	err = s.DB.QueryRow(ctx, `SELECT id,email,display_name,department,organization_name,role FROM users
		WHERE (oidc_subject=$1 OR ($2<>'' AND email=lower($2))) AND active
		ORDER BY oidc_subject=$1 DESC LIMIT 1`, subject, email).
		Scan(&p.ID, &p.Email, &p.DisplayName, &p.Department, &p.OrganizationName, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Principal{}, &oauthRefusal{
			message: "this SSO account is not registered in Momento or is inactive; sign in to the web console once first",
			cause:   fmt.Errorf("no active account for subject %q / email %q", subject, email),
		}
	}
	if err != nil {
		return auth.Principal{}, &oauthRefusal{message: "the account lookup failed", cause: err}
	}
	return p, nil
}

// audienceListed says whether any of the token's aud values or its azp is in
// the administrator's list.
func audienceListed(accepted, audience []string, azp string) bool {
	if len(accepted) == 0 {
		return false
	}
	if azp != "" && slices.Contains(accepted, azp) {
		return true
	}
	return slices.ContainsFunc(audience, func(value string) bool { return value != "" && slices.Contains(accepted, value) })
}

// requireMCPAuth is the gate on /mcp. The same keys and sessions the REST API
// takes pass through untouched; what is added is a Keycloak access token, told
// apart by its shape after the key prefix has been ruled out. A deployment with
// SSO tokens switched off never reaches the token path, so its refusals are the
// refusals they always were.
func (s *Server) requireMCPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := s.mcpOAuthConfig(r.Context())
		if err != nil {
			s.warn("mcp sso settings unreadable, treating as off", "error", err)
			cfg = mcpOAuthConfig{}
		}
		if cfg.Enabled && !cfg.active() {
			s.warn("mcp sso is switched on but oidc.issuer_url is empty, treating as off")
		}
		token := auth.BearerToken(r)
		if cfg.active() && !strings.HasPrefix(token, auth.KeyPrefix) && auth.LooksLikeJWT(token) {
			p, refusal := s.oauthPrincipal(r.Context(), r, cfg, token)
			if refusal != nil {
				s.warn("mcp sso token refused", "reason", refusal.message, "error", refusal.cause, "client_ip", clientIP(r))
				s.mcpChallenge(w, r, cfg, true)
				writeError(w, 401, "UNAUTHENTICATED", refusal.message)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
			return
		}
		p, err := s.Auth.Authenticate(r)
		if err != nil {
			s.mcpChallenge(w, r, cfg, token != "")
			writeError(w, 401, "UNAUTHENTICATED", err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

// mcpChallenge is the header that turns a 401 into an invitation: the MCP
// client reads resource_metadata and starts the OAuth flow from there. Without
// it a refusal is a dead end. It is set on MCP refusals only — on a REST 401 it
// would send browsers and other clients somewhere they cannot use.
func (s *Server) mcpChallenge(w http.ResponseWriter, r *http.Request, cfg mcpOAuthConfig, tokenPresented bool) {
	if !cfg.active() {
		return
	}
	value := fmt.Sprintf(`Bearer realm="Momento", resource_metadata=%q`, s.mcpMetadataURL(r, cfg))
	if tokenPresented {
		value += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", value)
}

// protectedResourceMetadata is RFC 9728: the document a refused MCP client
// reads to find the authorization server. Public by design — it says where to
// sign in, not who is signed in — and answered as the bare document rather
// than this product's envelope, because the reader is an OAuth client library
// that knows nothing about {error:…}. With SSO tokens off it is 404: metadata
// that points at a sign-in the server would then refuse sends a client round a
// login loop.
func (s *Server) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mcpOAuthConfig(r.Context())
	if err != nil {
		writeError(w, 500, "SETTING_READ_FAILED", err.Error())
		return
	}
	if !cfg.active() {
		if cfg.Enabled {
			s.warn("mcp sso is switched on but oidc.issuer_url is empty, metadata not served")
		}
		writeError(w, 404, "MCP_OAUTH_DISABLED", "this server's MCP endpoint does not accept SSO access tokens; use a personal API key (mom_key_)")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resource":                 s.mcpResource(r, cfg),
		"authorization_servers":    []string{cfg.Issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         cfg.Scopes,
		"resource_name":            "Momento Analytics MCP",
	})
}

func (s *Server) warn(message string, args ...any) {
	if s.Logger != nil {
		s.Logger.Warn(message, args...)
	}
}
