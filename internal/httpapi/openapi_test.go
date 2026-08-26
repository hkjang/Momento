package httpapi

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The OpenAPI document is the integration contract for an on-premise
// deployment: it is how a BI team, a script or another service learns what this
// server offers. It was maintained by hand alongside the router and drifted —
// nearly a third of the API was missing from it, including the page and event
// reports and the whole API-key surface — because nothing compared the two.
//
// This walks the real router and checks every path against the document. It
// needs no database, so it runs on every push.

// apiPathPattern is the surface the document is expected to describe. Health and
// version endpoints are deliberately outside it.
var apiPathPattern = regexp.MustCompile(`^/(api/v1|collect/v1|mcp)`)

// normalisePath makes a router template comparable to a document template. The
// two use different names for the same parameter — {siteID} against {siteId} —
// which is fine for either reader and fatal for a textual comparison.
func normalisePath(path string) string {
	return regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "{p}")
}

func routerPaths(t *testing.T) []string {
	t.Helper()
	handler := (&Server{}).Handler()
	mux, ok := handler.(*chi.Mux)
	if !ok {
		t.Fatalf("the handler is %T, not a chi router, so its routes cannot be walked", handler)
	}
	seen := map[string]bool{}
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if apiPathPattern.MatchString(route) {
			seen[normalisePath(route)] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func documentedPaths(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read the OpenAPI document: %v", err)
	}
	// Paths are the two-space-indented keys under `paths:`.
	pattern := regexp.MustCompile(`(?m)^  (/[^\s:]+):`)
	out := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
		out[normalisePath(match[1])] = true
	}
	if len(out) == 0 {
		t.Fatal("found no paths in the OpenAPI document, so the pattern no longer matches it")
	}
	return out
}

func TestEveryAPIPathIsDocumented(t *testing.T) {
	t.Parallel()
	documented := documentedPaths(t)
	missing := []string{}
	for _, path := range routerPaths(t) {
		if !documented[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d API paths are absent from docs/openapi.yaml, so a reader of the contract cannot find them:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func TestEveryDocumentedPathExists(t *testing.T) {
	t.Parallel()
	served := map[string]bool{}
	for _, path := range routerPaths(t) {
		served[path] = true
	}
	orphaned := []string{}
	for path := range documentedPaths(t) {
		if !served[path] {
			orphaned = append(orphaned, path)
		}
	}
	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("docs/openapi.yaml describes %d paths this server does not serve, which is worse than an omission because a reader will build against them:\n  %s",
			len(orphaned), strings.Join(orphaned, "\n  "))
	}
}
