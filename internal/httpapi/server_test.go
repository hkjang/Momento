package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
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

// 로그인 속도 제한이 collector 와 같은 신뢰 프록시 계약 위에 서는지 본다.
//
// 이 제품이 문서로 안내하는 배치는 리버스 프록시 뒤다(admin.go 의 설치 안내 nginx
// 블록이 `X-Forwarded-For` 를 붙인다). 그 배치에서 제한 키가 `r.RemoteAddr` 이면
// 모든 로그인 시도가 **프록시 주소 하나**로 묶여, 한 사람의 실패가 분당 한도를 다
// 써 버리고 전 직원이 429 를 받는다. 동시에 개별 출처에 대한 brute-force 제한으로는
// 동작하지 않는다 — 공격자는 한도를 혼자 쓰면서 남을 막기만 한다.
//
// 두 사례를 한 테스트 함수에 두 subtest 로 나눠 담는다. 둘이 증명하는 것은 서로
// 반대 방향이고(헤더를 믿는 경우와 믿지 않는 경우) 같은 계약의 양면이라서다.
// 요청은 손으로 만든 핸들러가 아니라 `s.Handler()` 가 등록한 실제 라우트로 보낸다 —
// 제한 거절은 `decodeJSON`·`s.Auth.Login` 보다 앞이라 429 경로는 DB 를 건드리지
// 않고, 한도 안에서 통과한 요청은 빈 본문 때문에 400 에서 멈춘다(그래서 `DB` 가
// nil 이어도 된다). `Logger` 는 반드시 채운다 — `s.requestLog` 가 nil 에서 패닉한다.
func TestLoginRateLimitUsesTheTrustedProxyClientIP(t *testing.T) {
	t.Parallel()

	// 신뢰 대역은 `reloadSecurity` 와 같은 방법으로 만든다 — `net.ParseCIDR` 이
	// 돌려준 `*net.IPNet` 을 그대로 `trustedProxies` 에 담는다.
	newServer := func(t *testing.T, limit int) *Server {
		t.Helper()
		_, trusted, err := net.ParseCIDR("192.0.2.0/24")
		if err != nil {
			t.Fatalf("parse trusted cidr: %v", err)
		}
		return &Server{
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			Limiter:        newRateLimiter(1),
			LoginLimiter:   newRateLimiter(limit),
			trustedProxies: []*net.IPNet{trusted},
		}
	}
	// `httptest.NewRequest` 의 기본 RemoteAddr 은 `192.0.2.1:1234` 이므로 위 대역을
	// 그대로 두면 "신뢰한 프록시 뒤" 가 된다. remoteAddr 이 빈 문자열이 아니면
	// 그 값으로 덮어 "신뢰 대역 밖" 을 만든다.
	login := func(t *testing.T, handler http.Handler, remoteAddr, forwardedFor string) int {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		if remoteAddr != "" {
			request.RemoteAddr = remoteAddr
		}
		request.Header.Set("X-Forwarded-For", forwardedFor)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	t.Run("behind a trusted proxy each forwarded client gets its own budget", func(t *testing.T) {
		t.Parallel()
		handler := newServer(t, 1).Handler()

		if status := login(t, handler, "", "198.51.100.7"); status == http.StatusTooManyRequests {
			t.Fatalf("첫 시도가 429 다 — 한도 안의 요청이 거절됐다")
		}
		// 같은 프록시를 통해 들어온 **다른** 클라이언트. 키가 프록시 주소라면 위
		// 요청이 분당 한도를 이미 다 썼으므로 이 사람이 429 를 받는다.
		if status := login(t, handler, "", "203.0.113.9"); status == http.StatusTooManyRequests {
			t.Fatalf("다른 클라이언트가 429 를 받았다 — 로그인 제한이 프록시 주소 하나로 묶여 있다")
		}
		// 그리고 자기 한도를 다 쓴 첫 클라이언트는 여전히 막혀야 한다 — 제한이
		// 느슨해진 것이 아니라 키가 좁아진 것임을 보인다.
		if status := login(t, handler, "", "198.51.100.7"); status != http.StatusTooManyRequests {
			t.Fatalf("한도를 넘긴 같은 클라이언트의 status = %d, want %d", status, http.StatusTooManyRequests)
		}
	})

	t.Run("outside the trusted range the forwarded header is ignored", func(t *testing.T) {
		t.Parallel()
		handler := newServer(t, 1).Handler()
		const untrusted = "203.0.113.5:9999"

		if status := login(t, handler, untrusted, "198.51.100.7"); status == http.StatusTooManyRequests {
			t.Fatalf("첫 시도가 429 다 — 한도 안의 요청이 거절됐다")
		}
		// 헤더를 바꿔 가며 보내도 한도를 벗어나지 못한다. 신뢰하지 않은 출처가
		// 자기 주소를 지어내 제한을 우회하는 것을 막는 쪽이다.
		if status := login(t, handler, untrusted, "198.51.100.8"); status != http.StatusTooManyRequests {
			t.Fatalf("X-Forwarded-For 를 바꾸자 status = %d 로 통과했다 — 신뢰하지 않은 출처의 헤더를 믿고 있다", status)
		}
	})
}
