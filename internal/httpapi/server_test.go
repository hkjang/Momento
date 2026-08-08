package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

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
