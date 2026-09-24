package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hkjang/Momento/internal/auth"
)

// The gates between a program and what only a person at the console may do
// used to ask "is this an API key". An SSO token is not an API key, and the
// account it names may be a super administrator, so two of the three gates
// would have admitted it. They now ask the one question they mean, and this
// runs each real gate with each programmatic credential kind at the highest
// role there is.
func TestEveryInteractiveGateRefusesEveryProgrammaticCredential(t *testing.T) {
	t.Parallel()
	server := &Server{}
	reached := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	gates := map[string]http.HandlerFunc{
		"admin":       server.admin(reached),
		"orgAdmin":    server.orgAdmin(reached),
		"sessionOnly": server.sessionOnly(reached),
	}
	for _, credential := range []string{"api_key", "oauth"} {
		for name, gate := range gates {
			principal := auth.Principal{Role: "super_admin", AuthType: credential}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
			recorder := httptest.NewRecorder()
			gate(recorder, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
			if recorder.Code != http.StatusForbidden {
				t.Errorf("%s admitted a super_admin holding a %s credential: %d", name, credential, recorder.Code)
			}
		}
	}
	// And a person at the console with the role still passes, so the gates
	// were not simply closed.
	for name, gate := range gates {
		principal := auth.Principal{Role: "super_admin", AuthType: "session"}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
		recorder := httptest.NewRecorder()
		gate(recorder, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
		if recorder.Code != http.StatusOK {
			t.Errorf("%s refused a super_admin session: %d", name, recorder.Code)
		}
	}
}

func TestMCPResourceIdentifierIsTheConfiguredOneAndNeverTheHost(t *testing.T) {
	t.Parallel()
	// Without a resource identifier of the operator's choosing there is nothing
	// a token's audience can be held to, so the feature is off however the
	// switch is set — a Host header is the sender's to choose.
	on := mcpOAuthConfig{Enabled: true, Issuer: "https://kc.example/realms/x", Resource: "https://momento.example.test/mcp"}
	if !on.active() {
		t.Error("switched on with an issuer and a resource is not active")
	}
	if noResource := (mcpOAuthConfig{Enabled: true, Issuer: on.Issuer}); noResource.active() || !strings.Contains(noResource.inactiveReason(), "public_url") {
		t.Errorf("switched on without a resource is active (%v) or the reason %q does not name public_url", noResource.active(), noResource.inactiveReason())
	}
	if noIssuer := (mcpOAuthConfig{Enabled: true, Resource: on.Resource}); noIssuer.active() || !strings.Contains(noIssuer.inactiveReason(), "issuer") {
		t.Errorf("switched on without an issuer is active (%v) or the reason %q does not name the issuer", noIssuer.active(), noIssuer.inactiveReason())
	}
	if got := mcpMetadataURL(on); got != "https://momento.example.test/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("metadata URL %q", got)
	}
	for raw, want := range map[string]bool{
		"https://momento.example.test/mcp":     true,
		"http://localhost:8080/mcp":            true,
		"https://momento.example.test/mcp/":    false,
		"https://momento.example.test/api/mcp": false,
		"https://momento.example.test":         false,
		"momento.example.test/mcp":             false,
		"https://user:pw@momento.example/mcp":  false,
		"https://momento.example.test/mcp?x=1": false,
	} {
		if got := validMCPResource(raw); got != want {
			t.Errorf("validMCPResource(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestAudienceListedReadsAudAndAzp(t *testing.T) {
	t.Parallel()
	accepted := strings.Fields("claude-mcp cursor-mcp")
	if !audienceListed(accepted, []string{"account"}, "claude-mcp") {
		t.Error("a listed azp was not accepted")
	}
	if !audienceListed(accepted, []string{"account", "cursor-mcp"}, "") {
		t.Error("a listed aud was not accepted")
	}
	if audienceListed(accepted, []string{"account"}, "other") {
		t.Error("an unlisted client was accepted")
	}
	if audienceListed(nil, []string{"account"}, "account") {
		t.Error("an empty list accepted something")
	}
	if audienceListed([]string{""}, []string{""}, "") {
		t.Error("empty strings matched each other")
	}
}

func TestBearerShapes(t *testing.T) {
	t.Parallel()
	for token, want := range map[string]bool{
		"a.b.c":                 true,
		"mom_key_abc":           false,
		"mom_sess_abc":          false,
		"a.b":                   false,
		"a..c":                  false,
		".b.c":                  false,
		"a.b.":                  false,
		"eyJh.eyJz.sig-with-_-": true,
	} {
		if got := auth.LooksLikeJWT(token); got != want {
			t.Errorf("LooksLikeJWT(%q) = %v, want %v", token, got, want)
		}
	}
}

// Discovery is one round trip per issuer, not one per request: callers that
// arrive while it is in flight wait for the same outcome, a failure is
// remembered for a while rather than retried on every call, and a caller whose
// request ends first leaves without holding anyone else up.
func TestDiscoveryRunsOncePerIssuerAndOutsideTheLock(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	release := make(chan struct{})
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		if code := int(status.Load()); code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"jwks_uri":%q}`, "http://"+r.Host, "http://"+r.Host+"/jwks")
	}))
	defer issuer.Close()
	server := &Server{}

	// Ten callers at once, Keycloak slow: one round trip, and the others are
	// not stuck behind a lock — a caller whose request is cancelled leaves.
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 9; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.oauthProvider(context.Background(), issuer.URL)
			errs <- err
		}()
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for hits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := server.oauthProvider(cancelled, issuer.URL); !errors.Is(err, context.Canceled) {
		t.Errorf("a caller with a cancelled request waited on discovery instead of leaving: %v", err)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil || !strings.Contains(err.Error(), "503") {
			t.Errorf("a waiting caller did not get the discovery outcome: %v", err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("discovery was performed %d times for one issuer", got)
	}
	// The failure is remembered: Keycloak is not asked again straight away.
	if _, err := server.oauthProvider(context.Background(), issuer.URL); err == nil || hits.Load() != 1 {
		t.Errorf("a failed discovery was retried on the next call (err %v, hits %d)", err, hits.Load())
	}
	// Once the failure has aged out it is tried again, and a success stays.
	server.oauthMu.Lock()
	server.oauthProviders[issuer.URL].until = time.Now().Add(-time.Second)
	server.oauthMu.Unlock()
	status.Store(http.StatusOK)
	for i := 0; i < 3; i++ {
		if _, err := server.oauthProvider(context.Background(), issuer.URL); err != nil {
			t.Fatalf("discovery after recovery: %v", err)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("after recovery discovery was performed %d times, want 2", got)
	}
}
