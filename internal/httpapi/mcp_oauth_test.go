package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestMCPResourceIdentifierIsTheConfiguredOneBeforeTheHost(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Host = "attacker.example"
	if got := server.mcpResource(request, mcpOAuthConfig{Resource: "https://momento.example.test/mcp"}); got != "https://momento.example.test/mcp" {
		t.Errorf("a configured resource was overridden by the Host header: %q", got)
	}
	if got := server.mcpResource(request, mcpOAuthConfig{}); got != "http://attacker.example/mcp" {
		t.Errorf("with nothing configured the host is the last resort: %q", got)
	}
	if got := server.mcpMetadataURL(request, mcpOAuthConfig{Resource: "https://momento.example.test/mcp"}); got != "https://momento.example.test/.well-known/oauth-protected-resource/mcp" {
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
