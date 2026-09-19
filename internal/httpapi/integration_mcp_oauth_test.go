package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

// MCP with a Keycloak token instead of a personal key.
//
// The authorization flow itself — PKCE, the redirect, the code exchange — is
// Keycloak's and the client's. What is this server's is the resource-server
// half of the specification, and that is what these tests hold it to: it says
// where the authorization server is, it turns a 401 into a pointer there, and
// it accepts exactly the tokens that server issued for this resource, for a
// person Momento already knows, with the powers a key would have and no more.
//
// The tokens are real: a fake issuer holds an RSA key, serves discovery and a
// JWKS, and signs whatever claims a case needs. Nothing is stubbed on the
// server's side — the same provider cache, verifier and account lookup run
// that production runs.

type fakeIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/authorize",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		public := key.Public().(*rsa.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
		}}})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func jwtSegment(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// sign issues an RS256 token with the realm key, the way Keycloak would.
func (idp *fakeIDP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	signingInput := jwtSegment(t, map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"}) + "." + jwtSegment(t, claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// signHS256 is what a forger does: a symmetric signature with a key of their
// choosing, hoping the server accepts the algorithm the header names.
func signHS256(t *testing.T, claims map[string]any, secret string) string {
	t.Helper()
	signingInput := jwtSegment(t, map[string]string{"alg": "HS256", "typ": "JWT", "kid": "test"}) + "." + jwtSegment(t, claims)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// accessToken is what Keycloak hands an MCP client after the person signed in:
// issued by the realm, for an audience, for a subject, valid for an hour.
func (idp *fakeIDP) accessToken(t *testing.T, audience any, subject string, extra map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"iss": idp.server.URL, "aud": audience, "sub": subject,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"typ": "Bearer",
	}
	for key, value := range extra {
		claims[key] = value
	}
	return idp.sign(t, claims)
}

const listTools = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

// mcpWith sends one MCP call with the given bearer.
func (f fixture) mcpWith(bearer, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Host = "momento.example.test"
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// putSetting writes a settings group the way the console does, through the
// administrator's session, so the save-time validation runs too.
func (f fixture) putSetting(t *testing.T, group string, value map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(value)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/"+group, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// keepSettings snapshots the settings rows a test rewrites and puts them back
// afterwards, because the rows are shared by every test in the package. The
// writer is cleared too: a row still naming the fixture's administrator would
// stop the next seed from deleting that account.
func keepSettings(t *testing.T, f fixture, keys ...string) {
	t.Helper()
	ctx := context.Background()
	saved := map[string][]byte{}
	for _, key := range keys {
		var raw []byte
		if err := f.server.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw); err != nil {
			t.Fatalf("snapshot setting %s: %v", key, err)
		}
		saved[key] = raw
	}
	t.Cleanup(func() {
		for key, raw := range saved {
			_, _ = f.server.DB.Exec(context.Background(), `UPDATE settings SET value=$2,updated_by=NULL WHERE key=$1`, key, raw)
		}
	})
}

// switchOnSSO configures the fixture the way an administrator would: the
// issuer on the OIDC card, the public URL on the general card, and the MCP SSO
// card switched on with the given accepted audiences.
func switchOnSSO(t *testing.T, f fixture, idp *fakeIDP, audience string) {
	t.Helper()
	if response := f.putSetting(t, "oidc", map[string]any{"issuer_url": idp.server.URL, "client_id": "momento-web"}); response.Code != http.StatusOK {
		t.Fatalf("set the issuer: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "general", map[string]any{"public_url": "https://momento.example.test"}); response.Code != http.StatusOK {
		t.Fatalf("set the public url: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": true, "audience": audience}); response.Code != http.StatusOK {
		t.Fatalf("switch SSO tokens on: %d %s", response.Code, response.Body.String())
	}
}

func TestARefusedMCPClientIsToldWhereToSignIn(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	keepSettings(t, f, "oidc", "general", "mcp.oauth")
	idp := newFakeIDP(t)

	// Off by default: a deployment without SSO advertises nothing, and a token
	// is refused exactly as it was when only keys existed — through the session
	// lookup, with no new words about SSO.
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s is served with SSO off: %d %s", path, recorder.Code, recorder.Body.String())
		}
	}
	refusedOff := f.mcpWith("", listTools)
	if refusedOff.Code != http.StatusUnauthorized || refusedOff.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("with SSO off a bare 401 is expected, got %d with WWW-Authenticate %q", refusedOff.Code, refusedOff.Header().Get("WWW-Authenticate"))
	}
	tokenOff := f.mcpWith(idp.accessToken(t, "https://momento.example.test/mcp", "subject-off", nil), listTools)
	if tokenOff.Code != http.StatusUnauthorized || !strings.Contains(tokenOff.Body.String(), "invalid session") || tokenOff.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("with SSO off a token should be refused as before: %d %s %q", tokenOff.Code, tokenOff.Body.String(), tokenOff.Header().Get("WWW-Authenticate"))
	}

	// Switching on without an issuer is refused where the administrator can
	// see it, not accepted and then ignored at /mcp.
	if response := f.putSetting(t, "oidc", map[string]any{"issuer_url": ""}); response.Code != http.StatusOK {
		t.Fatalf("clear the issuer: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": true}); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "issuer") {
		t.Fatalf("enabling without an issuer answered %d %s, want 400 naming the issuer", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"resource": "https://momento.example.test/api"}); response.Code != http.StatusBadRequest {
		t.Fatalf("a resource that is not the MCP endpoint answered %d, want 400", response.Code)
	}
	// And without a resource identifier: with an issuer but neither the public
	// URL nor the resource set there is nothing to hold a token's audience to,
	// and the save says which of the two to fill.
	if response := f.putSetting(t, "oidc", map[string]any{"issuer_url": idp.server.URL, "client_id": "momento-web"}); response.Code != http.StatusOK {
		t.Fatalf("set the issuer: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "general", map[string]any{"public_url": ""}); response.Code != http.StatusOK {
		t.Fatalf("clear the public url: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": true, "resource": ""}); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "public_url") || !strings.Contains(response.Body.String(), "mcp.oauth.resource") {
		t.Fatalf("enabling without a resource answered %d %s, want 400 naming public_url and mcp.oauth.resource", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": true, "resource": "https://mcp.example.test/mcp"}); response.Code != http.StatusOK {
		t.Fatalf("enabling with a resource in the same write was refused: %d %s", response.Code, response.Body.String())
	}
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": false, "resource": ""}); response.Code != http.StatusOK {
		t.Fatalf("switching off again: %d %s", response.Code, response.Body.String())
	}

	switchOnSSO(t, f, idp, "")

	// RFC 9728: the resource names itself and its authorization server, as a
	// bare document with CORS open — the reader is an OAuth library, possibly
	// inside a browser, not this product's console.
	request := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil)
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("metadata: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("metadata is not readable cross-origin: %v", recorder.Header())
	}
	var metadata struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		BearerMethods        []string `json:"bearer_methods_supported"`
		Scopes               []string `json:"scopes_supported"`
		Error                any      `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	if metadata.Error != nil {
		t.Errorf("the metadata is wrapped in the product's error envelope: %s", recorder.Body.String())
	}
	if metadata.Resource != "https://momento.example.test/mcp" {
		t.Errorf("resource %q, want the public URL plus /mcp", metadata.Resource)
	}
	if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != idp.server.URL {
		t.Errorf("authorization servers %v, want the configured issuer", metadata.AuthorizationServers)
	}
	if len(metadata.BearerMethods) != 1 || metadata.BearerMethods[0] != "header" {
		t.Errorf("bearer methods %v, want header", metadata.BearerMethods)
	}
	if len(metadata.Scopes) != 1 || metadata.Scopes[0] != "analytics:read" {
		t.Errorf("scopes %v, want the default analytics:read", metadata.Scopes)
	}

	// And the 401 now carries the pointer. Without it the client has no way to
	// discover the document above, and the refusal is a dead end.
	refusal := f.mcpWith("", listTools)
	if refusal.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d", refusal.Code)
	}
	header := refusal.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(header, `Bearer realm="Momento"`) || !strings.Contains(header, `resource_metadata="https://momento.example.test/.well-known/oauth-protected-resource/mcp"`) {
		t.Errorf("WWW-Authenticate %q does not point at the metadata", header)
	}
	if strings.Contains(header, "invalid_token") {
		t.Errorf("no token was presented, yet the challenge says one was invalid: %q", header)
	}
	if invalid := f.mcpWith("not.a.key", listTools).Header().Get("WWW-Authenticate"); !strings.Contains(invalid, `error="invalid_token"`) {
		t.Errorf("a refused bearer should be challenged with error=invalid_token: %q", invalid)
	}

	// MCP only. A REST 401 with this header would send browsers and scripts
	// off to an authorization server whose tokens the REST API refuses.
	rest := httptest.NewRequest(http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview", nil)
	restRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(restRecorder, rest)
	if restRecorder.Code != http.StatusUnauthorized || restRecorder.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("REST 401 = %d with WWW-Authenticate %q, want no challenge", restRecorder.Code, restRecorder.Header().Get("WWW-Authenticate"))
	}

	// An explicit resource identifier wins over the public URL.
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"resource": "https://mcp.example.test/mcp"}); response.Code != http.StatusOK {
		t.Fatalf("set the resource: %d %s", response.Code, response.Body.String())
	}
	explicit := f.mcpWith("", listTools).Header().Get("WWW-Authenticate")
	if !strings.Contains(explicit, `resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource/mcp"`) {
		t.Errorf("the challenge does not follow the configured resource: %q", explicit)
	}
}

// A resource identifier is what a token's audience is held to. With neither
// the public URL nor mcp.oauth.resource set — the state a fresh installation
// is in, and one an operator can return to by clearing the public URL after
// switching SSO on — there is nothing of the operator's to hold it to, and the
// request's Host header is the sender's to choose. So nothing is accepted:
// not a token whose aud names that host, and the metadata that would point a
// client at a sign-in is not served either.
func TestWithoutAResourceIdentifierNoTokenIsAccepted(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	keepSettings(t, f, "oidc", "general", "mcp.oauth")
	ctx := context.Background()
	idp := newFakeIDP(t)
	switchOnSSO(t, f, idp, "")
	const subject = "keycloak-subject-admin"
	if _, err := pool.Exec(ctx, `UPDATE users SET oidc_subject=$1 WHERE email='admin@test.local'`, subject); err != nil {
		t.Fatalf("link the account: %v", err)
	}
	// The save refuses this state, so it is reached the way an operator
	// reaches it: by clearing the public URL underneath a switched-on card.
	if _, err := pool.Exec(ctx, `UPDATE settings SET value=jsonb_set(value,'{public_url}','""') WHERE key='general'`); err != nil {
		t.Fatalf("clear the public url: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settings SET value=jsonb_set(value,'{resource}','""') WHERE key='mcp.oauth'`); err != nil {
		t.Fatalf("clear the resource: %v", err)
	}
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT (value->>'enabled')::bool FROM settings WHERE key='mcp.oauth'`).Scan(&enabled); err != nil || !enabled {
		t.Fatalf("the switch is not on (%v, %v), so a refusal below proves nothing", enabled, err)
	}

	for _, host := range []string{"other-app.example.test", "momento.example.test"} {
		for _, scheme := range []string{"http", "https"} {
			aud := scheme + "://" + host + "/mcp"
			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(listTools))
			request.Header.Set("Content-Type", "application/json")
			request.Host = host
			request.Header.Set("X-Forwarded-Host", host)
			request.Header.Set("X-Forwarded-Proto", scheme)
			request.Header.Set("Authorization", "Bearer "+idp.accessToken(t, aud, subject, nil))
			recorder := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("Host %s: a token whose aud %q was made from the request's own host opened MCP: %d %s", host, aud, recorder.Code, truncateBody(recorder.Body.String()))
			}
			if challenge := recorder.Header().Get("WWW-Authenticate"); challenge != "" {
				t.Errorf("Host %s: the refusal points the client somewhere made from the request: %q", host, challenge)
			}
		}
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "other-app.example.test"
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "other-app.example.test") {
			t.Errorf("%s without a resource identifier answered %d %s, want 404 naming no host", path, recorder.Code, recorder.Body.String())
		}
	}
	// Re-enabling through the console is refused until one of the two is set.
	if response := f.putSetting(t, "mcp.oauth", map[string]any{"enabled": true}); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "public_url") {
		t.Errorf("saving the switch in this state answered %d %s, want 400 naming public_url", response.Code, response.Body.String())
	}
	// And a personal key is untouched by any of this.
	if response := f.mcpWith("mom_key_never_issued", listTools); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "invalid API key") {
		t.Errorf("a key is no longer examined as a key: %d %s", response.Code, response.Body.String())
	}
}

// refusalLog is what the server wrote while one request was handled, so a
// test can hold the log to the same standard as the response.
type refusalLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *refusalLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// take returns everything written since the last take.
func (l *refusalLog) take() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.buf.String()
	l.buf.Reset()
	return out
}

// logging swaps the fixture's server for one whose logger writes to a buffer
// at every level, sharing the same database, secrets and session.
func logging(t *testing.T, f fixture) (fixture, *refusalLog) {
	t.Helper()
	log := &refusalLog{}
	f.server = New(f.server.DB, nil, slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})), f.server.Secrets)
	return f, log
}

var requestIDField = regexp.MustCompile(`(?m)\brequest_id=("([^"]*)"|(\S+))`)

// refusalLine finds the one line on which an SSO refusal was logged and holds
// it to what an operator needs: the library's own words for the cause under
// error=, and a request id the line can be tied back to the request by.
func refusalLine(t *testing.T, written, name, cause string) {
	t.Helper()
	var line string
	for _, candidate := range strings.Split(written, "\n") {
		if strings.Contains(candidate, `msg="mcp sso token refused"`) {
			if line != "" {
				t.Errorf("%s: refused twice in one request:\n%s", name, written)
			}
			line = candidate
		}
	}
	if line == "" {
		t.Errorf("%s: no 'mcp sso token refused' line was logged; the log was:\n%s", name, written)
		return
	}
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("%s: the refusal is not logged as a warning: %s", name, line)
	}
	if !strings.Contains(line, "error=") || !strings.Contains(line, cause) {
		t.Errorf("%s: the refusal line does not carry %q under error=: %s", name, cause, line)
	}
	match := requestIDField.FindStringSubmatch(line)
	if match == nil || strings.TrimSpace(match[2]+match[3]) == "" {
		t.Errorf("%s: the refusal line has no request_id to tie it to the request: %s", name, line)
	}
}

func TestAKeycloakTokenOpensMCPForAnAccountMomentoKnows(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	keepSettings(t, f, "oidc", "general", "mcp.oauth")
	ctx := context.Background()
	idp := newFakeIDP(t)
	switchOnSSO(t, f, idp, "")
	f, log := logging(t, f)

	// The fixture's administrator signed in through the web once, which is
	// what stored the subject.
	const subject = "keycloak-subject-admin"
	if _, err := pool.Exec(ctx, `UPDATE users SET oidc_subject=$1 WHERE email='admin@test.local'`, subject); err != nil {
		t.Fatalf("link the account: %v", err)
	}
	const resource = "https://momento.example.test/mcp"

	t.Run("a token for this resource lists the tools", func(t *testing.T) {
		opened := f.mcpWith(idp.accessToken(t, resource, subject, nil), listTools)
		if opened.Code != http.StatusOK {
			t.Fatalf("a token for this resource was refused: %d %s", opened.Code, opened.Body.String())
		}
		if !strings.Contains(opened.Body.String(), "query_metrics") {
			t.Errorf("the token opened MCP but the listing is empty: %s", truncateBody(opened.Body.String()))
		}
		// aud may be a list, as it is when Keycloak adds a mapper to a token
		// that already carries "account".
		asList := f.mcpWith(idp.accessToken(t, []string{"account", resource}, subject, map[string]any{"azp": "claude-mcp"}), listTools)
		if asList.Code != http.StatusOK {
			t.Errorf("a token whose aud list contains the resource was refused: %d %s", asList.Code, asList.Body.String())
		}
	})

	t.Run("a token calls a tool as the account it names", func(t *testing.T) {
		from, to := f.siteDates(t, 7)
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query_metrics","arguments":{"site_id":%q,"from":%q,"to":%q}}}`, f.siteKey, from, to)
		called := f.mcpWith(idp.accessToken(t, resource, subject, nil), body)
		if called.Code != http.StatusOK || strings.Contains(called.Body.String(), `"isError":true`) {
			t.Fatalf("query_metrics under an SSO token: %d %s", called.Code, truncateBody(called.Body.String()))
		}
	})

	t.Run("a token issued to another application is refused and told what to fix", func(t *testing.T) {
		// Measured against a real Keycloak 26: an access token issued to a
		// client carries aud=["account"] and the client in azp. Without a
		// mapper and without the client in the administrator's list, that is a
		// token for some other application in the realm.
		log.take()
		other := f.mcpWith(idp.accessToken(t, "account", subject, map[string]any{"azp": "some-other-app"}), listTools)
		if other.Code != http.StatusUnauthorized {
			t.Fatalf("a token issued to another application opened MCP: %d %s", other.Code, other.Body.String())
		}
		refusalLine(t, log.take(), "another application", `audience [account] / azp \"some-other-app\" not accepted`)
		var envelope struct {
			Error struct{ Message string } `json:"error"`
		}
		if err := json.Unmarshal(other.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode %s: %v", other.Body.String(), err)
		}
		for _, want := range []string{"aud [account]", `azp "some-other-app"`, `add "some-other-app"`, "mcp.oauth.audience", "Audience mapper for \"" + resource + "\""} {
			if !strings.Contains(envelope.Error.Message, want) {
				t.Errorf("the refusal does not say %q, so the operator cannot fix it from the message: %s", want, envelope.Error.Message)
			}
		}
		if !strings.Contains(other.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Errorf("a refused token is not challenged as invalid: %q", other.Header().Get("WWW-Authenticate"))
		}
	})

	t.Run("the administrator lists the client and the same token passes", func(t *testing.T) {
		if response := f.putSetting(t, "mcp.oauth", map[string]any{"audience": "claude-mcp cursor-mcp"}); response.Code != http.StatusOK {
			t.Fatalf("list the client: %d %s", response.Code, response.Body.String())
		}
		defer f.putSetting(t, "mcp.oauth", map[string]any{"audience": ""})
		viaAzp := f.mcpWith(idp.accessToken(t, "account", subject, map[string]any{"azp": "cursor-mcp"}), listTools)
		if viaAzp.Code != http.StatusOK {
			t.Errorf("a token whose azp is listed was refused: %d %s", viaAzp.Code, viaAzp.Body.String())
		}
		viaAud := f.mcpWith(idp.accessToken(t, "claude-mcp", subject, nil), listTools)
		if viaAud.Code != http.StatusOK {
			t.Errorf("a token whose aud is listed was refused: %d %s", viaAud.Code, viaAud.Body.String())
		}
		still := f.mcpWith(idp.accessToken(t, "account", subject, map[string]any{"azp": "some-other-app"}), listTools)
		if still.Code != http.StatusUnauthorized {
			t.Errorf("listing two clients let a third through: %d", still.Code)
		}
	})

	t.Run("tokens that are not a valid access token for this issuer are refused", func(t *testing.T) {
		otherIssuer := newFakeIDP(t)
		valid := map[string]any{"iss": idp.server.URL, "aud": resource, "sub": subject, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "typ": "Bearer"}
		with := func(changes map[string]any) map[string]any {
			claims := map[string]any{}
			for key, value := range valid {
				claims[key] = value
			}
			for key, value := range changes {
				claims[key] = value
			}
			return claims
		}
		signed := strings.Split(idp.sign(t, valid), ".")
		// Each case is refused, and the refusal is logged with the verifier's own
		// words for why — go-oidc's where go-oidc decided, this server's where
		// it did — so an operator reading the log learns what a client was
		// shown a generic message about.
		type refused struct{ token, cause string }
		cases := map[string]refused{
			"expired":                 {idp.sign(t, with(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})), "token is expired"},
			"not yet valid":           {idp.sign(t, with(map[string]any{"nbf": time.Now().Add(time.Hour).Unix()})), "before the nbf"},
			"another issuer":          {otherIssuer.sign(t, with(map[string]any{"iss": otherIssuer.server.URL})), "failed to verify signature"},
			"claiming another issuer": {idp.sign(t, with(map[string]any{"iss": otherIssuer.server.URL})), "issued by a different provider"},
			"an ID token":             {idp.sign(t, with(map[string]any{"typ": "ID"})), "typ=ID"},
			"HS256 signed":            {signHS256(t, valid, "anything"), `HS256`},
			"bound to a key":          {idp.sign(t, with(map[string]any{"cnf": map[string]any{"jkt": "thumbprint"}})), "cnf present"},
			"without subject":         {idp.sign(t, with(map[string]any{"sub": ""})), "sub empty"},
			"forged signature":        {signed[0] + "." + signed[1] + "." + base64.RawURLEncoding.EncodeToString([]byte("not the realm's signature")), "failed to verify signature"},
			"claiming no signer":      {jwtSegment(t, map[string]string{"alg": "none", "typ": "JWT"}) + "." + signed[1] + "." + signed[2], `none`},
		}
		names := make([]string, 0, len(cases))
		for name := range cases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			log.take()
			response := f.mcpWith(cases[name].token, listTools)
			if response.Code != http.StatusUnauthorized {
				t.Errorf("a token that is %s answered %d, want 401: %s", name, response.Code, truncateBody(response.Body.String()))
				continue
			}
			refusalLine(t, log.take(), name, cases[name].cause)
		}
		// A bearer without a signature segment is not JWT-shaped, so it never
		// reaches the verifier: it is refused the way any unknown bearer is.
		log.take()
		unsigned := f.mcpWith(signed[0]+"."+signed[1]+".", listTools)
		if unsigned.Code != http.StatusUnauthorized || !strings.Contains(unsigned.Body.String(), "invalid session") {
			t.Errorf("an unsigned token answered %d %s, want 401 from the session lookup", unsigned.Code, unsigned.Body.String())
		}
		if written := log.take(); strings.Contains(written, "mcp sso token refused") {
			t.Errorf("an unsigned bearer was examined as an SSO token:\n%s", written)
		}
	})

	t.Run("a token for a person Momento does not know creates nothing", func(t *testing.T) {
		var before int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		unknown := f.mcpWith(idp.accessToken(t, resource, "subject-nobody", map[string]any{"email": "nobody@test.local"}), listTools)
		if unknown.Code != http.StatusUnauthorized || !strings.Contains(unknown.Body.String(), "sign in to the web console") {
			t.Errorf("an unregistered subject: %d %s, want 401 telling them to sign in on the web first", unknown.Code, unknown.Body.String())
		}
		var after int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Errorf("presenting a token created %d account(s)", after-before)
		}
	})

	t.Run("a suspended account is not revived by a token", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE users SET active=false WHERE email='admin@test.local'`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), `UPDATE users SET active=true WHERE email='admin@test.local'`)
		}()
		if response := f.mcpWith(idp.accessToken(t, resource, subject, nil), listTools); response.Code != http.StatusUnauthorized {
			t.Errorf("a suspended account's token opened MCP: %d", response.Code)
		}
	})

	t.Run("an account without a stored subject is found by the email claim the web sign-in links by", func(t *testing.T) {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name) VALUES('local.analyst@test.local','Local Analyst','hash','analyst','Test') RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id) }()
		byEmail := f.mcpWith(idp.accessToken(t, resource, "subject-local-analyst", map[string]any{"email": "Local.Analyst@test.local"}), listTools)
		if byEmail.Code != http.StatusOK {
			t.Errorf("the email fallback did not find the account: %d %s", byEmail.Code, byEmail.Body.String())
		}
		// The token's claims never become Momento's record of the person.
		var storedSubject *string
		var role string
		if err := pool.QueryRow(ctx, `SELECT oidc_subject,role FROM users WHERE id=$1`, id).Scan(&storedSubject, &role); err != nil {
			t.Fatal(err)
		}
		if storedSubject != nil || role != "analyst" {
			t.Errorf("a token changed the account: subject %v, role %s", storedSubject, role)
		}
	})

	t.Run("a personal key still works with SSO on", func(t *testing.T) {
		var owner uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email='admin@test.local'`).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		plain, hash, prefix, err := auth.NewToken(auth.KeyPrefix, 32)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO api_keys(user_id,name,key_hash,key_prefix) VALUES($1,'sso coexistence',$2,$3)`, owner, hash, prefix); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE key_hash=$1`, hash) }()
		if response := f.mcpWith(plain, listTools); response.Code != http.StatusOK {
			t.Errorf("a key was refused at /mcp once SSO tokens were on: %d %s", response.Code, response.Body.String())
		}
		if response := f.mcpWith("mom_key_never_issued", listTools); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "invalid API key") {
			t.Errorf("a bad key should be refused as a key, not examined as a token: %d %s", response.Code, response.Body.String())
		}
	})
}

// An SSO token opens /mcp and nothing else. The REST API, the console's own
// routes and the administrative surface take keys and sessions as they always
// have; a token that opened them would make this "a new door into the whole
// API" rather than "MCP without a key".
func TestAnSSOTokenOpensNothingButMCP(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	keepSettings(t, f, "oidc", "general", "mcp.oauth")
	ctx := context.Background()
	idp := newFakeIDP(t)
	switchOnSSO(t, f, idp, "")
	const subject = "keycloak-subject-admin"
	if _, err := pool.Exec(ctx, `UPDATE users SET oidc_subject=$1 WHERE email='admin@test.local'`, subject); err != nil {
		t.Fatalf("link the account: %v", err)
	}
	token := idp.accessToken(t, "https://momento.example.test/mcp", subject, nil)
	if opened := f.mcpWith(token, listTools); opened.Code != http.StatusOK {
		t.Fatalf("the token does not open MCP, so refusals elsewhere prove nothing: %d %s", opened.Code, opened.Body.String())
	}

	mux, ok := f.server.Handler().(*chi.Mux)
	if !ok {
		t.Fatalf("the handler is %T, not a chi router", f.server.Handler())
	}
	type route struct{ method, pattern string }
	routes := []route{}
	if err := chi.Walk(mux, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		pattern = strings.TrimSuffix(pattern, "/")
		if method == http.MethodOptions || method == http.MethodHead {
			return nil
		}
		// Signing in and the collector are not behind a credential at all.
		if !strings.HasPrefix(pattern, "/api/v1") || strings.HasPrefix(pattern, "/api/v1/auth") || pattern == "/api/v1/version" {
			return nil
		}
		routes = append(routes, route{method, pattern})
		return nil
	}); err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	if len(routes) < 100 {
		t.Fatalf("found only %d API routes, so this is no longer walking the router it thinks it is", len(routes))
	}
	opened := []string{}
	for _, r := range routes {
		path := strings.ReplaceAll(r.pattern, "{siteID}", f.siteKey)
		path = strings.ReplaceAll(path, "{id}", uuid.NewString())
		path = strings.ReplaceAll(path, "{key}", "privacy")
		path = strings.ReplaceAll(path, "{name}", "probe")
		path = strings.ReplaceAll(path, "{visitorID}", f.visitorID)
		path = strings.ReplaceAll(path, "{eventName}", "probe")
		path = strings.ReplaceAll(path, "{version}", "1")
		request := httptest.NewRequest(r.method, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			opened = append(opened, fmt.Sprintf("%s %s answered %d", r.method, r.pattern, recorder.Code))
			continue
		}
		if recorder.Header().Get("WWW-Authenticate") != "" {
			opened = append(opened, fmt.Sprintf("%s %s challenged with %q", r.method, r.pattern, recorder.Header().Get("WWW-Authenticate")))
		}
	}
	sort.Strings(opened)
	if len(opened) > 0 {
		t.Errorf("an SSO token reached %d routes outside /mcp, or was pointed at SSO from one:\n  %s", len(opened), strings.Join(opened, "\n  "))
	}
}
