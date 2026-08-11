package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/Momento/internal/version"
)

func TestVersionInfoIsNeverCached(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	versionInfo(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	var got version.Info
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Version != version.Current().Version {
		t.Fatalf("version = %q, want %q", got.Version, version.Current().Version)
	}
}

func TestSPAHandlerServesRoutesWithoutRedirect(t *testing.T) {
	t.Parallel()

	server := &Server{Web: fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<main>Momento</main>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('momento')")},
	}}
	handler := server.spaHandler()

	tests := []struct {
		name        string
		path        string
		wantBody    string
		contentType string
	}{
		{name: "root", path: "/", wantBody: "<main>Momento</main>", contentType: "text/html"},
		{name: "client route", path: "/segments", wantBody: "<main>Momento</main>", contentType: "text/html"},
		{name: "static asset", path: "/assets/app.js", wantBody: "console.log('momento')", contentType: "text/javascript"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if location := recorder.Header().Get("Location"); location != "" {
				t.Fatalf("unexpected redirect to %q", location)
			}
			if body := recorder.Body.String(); body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, tt.contentType) {
				t.Fatalf("content type = %q, want prefix %q", contentType, tt.contentType)
			}
		})
	}
}

func TestSPAHandlerReportsMissingIndex(t *testing.T) {
	t.Parallel()

	server := &Server{Web: fstest.MapFS{
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('momento')")},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/segments", nil)

	server.spaHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestContentSecurityPolicyAllowsConfiguredConnectOrigins(t *testing.T) {
	t.Parallel()

	policy := contentSecurityPolicy(nil)
	if !strings.Contains(policy, "connect-src 'self';") {
		t.Fatalf("default policy = %q, want connect-src 'self'", policy)
	}

	policy = contentSecurityPolicy([]string{"https://momento.example", "wss://gateway.example"})
	if !strings.Contains(policy, "connect-src 'self' https://momento.example wss://gateway.example;") {
		t.Fatalf("policy = %q, want the extra origins in connect-src", policy)
	}
	for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("policy = %q, want %q", policy, directive)
		}
	}
}

func TestNormalizeConnectOriginKeepsOnlySchemeAndHost(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"https://momento.example/collect/v1/events": "https://momento.example",
		"http://10.0.0.5:8080":                      "http://10.0.0.5:8080",
		"momento.example":                           "https://momento.example",
		" https://momento.example/ ":                "https://momento.example",
		"wss://gateway.example":                     "wss://gateway.example",
		"javascript:alert(1)":                       "",
		"":                                          "",
		"/relative":                                 "",
	} {
		if got := normalizeConnectOrigin(input); got != want {
			t.Fatalf("normalizeConnectOrigin(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSecurityHeadersSkipCSPOnTheCollector(t *testing.T) {
	t.Parallel()

	server := &Server{}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }))

	console := httptest.NewRecorder()
	handler.ServeHTTP(console, httptest.NewRequest(http.MethodGet, "/", nil))
	if console.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("the console response must carry a policy")
	}

	// The collector answers cross origin requests from tracked applications, so a
	// document policy on that response would only be misleading.
	collect := httptest.NewRecorder()
	handler.ServeHTTP(collect, httptest.NewRequest(http.MethodPost, "/collect/v1/events", nil))
	if policy := collect.Header().Get("Content-Security-Policy"); policy != "" {
		t.Fatalf("collector policy = %q, want none", policy)
	}
	if collect.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("the collector response must keep the other security headers")
	}
}

func TestHandlerRegistersSecretAndDiagnosticRoutes(t *testing.T) {
	t.Parallel()

	server := &Server{Limiter: newRateLimiter(1), LoginLimiter: newRateLimiter(1)}
	router, ok := server.Handler().(chi.Routes)
	if !ok {
		t.Fatal("handler is not a chi router")
	}
	registered := map[string]bool{}
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	for _, route := range []string{
		"POST /api/v1/me/keys/{id}/reveal",
		"POST /api/v1/sites/{id}/reveal-keys",
		"GET /api/v1/system/encryption",
		"POST /api/v1/system/encryption/rekey",
		"GET /api/v1/sites/{siteID}/install-diagnostics",
		"GET /api/v1/sites/{siteID}/sessions",
		"GET /api/v1/sites/{siteID}/export",
		"GET /api/v1/sites/{siteID}/delivery-runs",
		"GET /api/v1/sites/{siteID}/visitor-insights",
		"GET /api/v1/sites/{siteID}/visitor-search",
		"GET /api/v1/sites/{siteID}/visitors/{visitorID}/timeline",
		"GET /api/v1/sites/{siteID}/anomalies",
		"GET /api/v1/sites/{siteID}/attribution",
	} {
		if !registered[route] {
			t.Fatalf("route %q is not registered", route)
		}
	}
}
