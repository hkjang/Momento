package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
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

// routerOperations returns "method path" for everything the server answers. The
// method matters as much as the path: comparing paths alone passed while DELETE
// on a site — which erases every event, session and aggregate it owns — was
// absent from the contract, because the path was listed for its PATCH.
func routerOperations(t *testing.T) []string {
	t.Helper()
	handler := (&Server{}).Handler()
	mux, ok := handler.(*chi.Mux)
	if !ok {
		t.Fatalf("the handler is %T, not a chi router, so its routes cannot be walked", handler)
	}
	seen := map[string]bool{}
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		// OPTIONS and HEAD are answered by the router itself, not described by a
		// contract that documents what to call.
		if method == http.MethodOptions || method == http.MethodHead {
			return nil
		}
		if apiPathPattern.MatchString(route) {
			seen[strings.ToLower(method)+" "+normalisePath(route)] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	out := make([]string, 0, len(seen))
	for operation := range seen {
		out = append(out, operation)
	}
	sort.Strings(out)
	return out
}

// documentedOperations reads the document the same way: a two-space-indented path
// key, then the four-space method keys beneath it.
func documentedOperations(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read the OpenAPI document: %v", err)
	}
	pathKey := regexp.MustCompile(`^  (/[^\s:]+):`)
	methodKey := regexp.MustCompile(`^    (get|post|put|patch|delete):`)
	out := map[string]bool{}
	path := ""
	for _, line := range strings.Split(string(source), "\n") {
		if match := pathKey.FindStringSubmatch(line); match != nil {
			path = normalisePath(match[1])
			continue
		}
		if match := methodKey.FindStringSubmatch(line); match != nil && path != "" {
			out[match[1]+" "+path] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("found no operations in the OpenAPI document, so the pattern no longer matches it")
	}
	return out
}

func TestEveryAPIOperationIsDocumented(t *testing.T) {
	t.Parallel()
	documented := documentedOperations(t)
	missing := []string{}
	for _, operation := range routerOperations(t) {
		if !documented[operation] {
			missing = append(missing, operation)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d API operations are absent from docs/openapi.yaml, so a reader of the contract cannot find them:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func TestEveryDocumentedOperationExists(t *testing.T) {
	t.Parallel()
	served := map[string]bool{}
	for _, operation := range routerOperations(t) {
		served[operation] = true
	}
	orphaned := []string{}
	for operation := range documentedOperations(t) {
		if !served[operation] {
			orphaned = append(orphaned, operation)
		}
	}
	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("docs/openapi.yaml describes %d operations this server does not serve, which is worse than an omission because a reader will build against them:\n  %s",
			len(orphaned), strings.Join(orphaned, "\n  "))
	}
}

// rangeHandlerPattern finds the handlers that read a site date range. Every one of
// them enforces the query policy through the same helper, so what matters is not
// whether the helper works but whether a new report reaches it — and whether
// anything checks that report afterwards.
var rangeHandlerPattern = regexp.MustCompile(`(?s)func \(s \*Server\) (\w+)\(w http\.ResponseWriter, r \*http\.Request\) \{.*?\n\}`)

// TestEveryRangedReportIsUnderThePolicy counts the handlers that take a range and
// compares that count against the reports rangedReports actually exercises.
//
// The policy test used to name seven of them and call it a representative spread.
// It was seven out of twenty-two, and a spread is only representative while every
// report goes through the same path: the next one written to query directly would
// have been outside both the helper and the check, and nothing would have said so.
// This does not know which handler serves which route, so it does not try — it
// fails when the number of ranged handlers moves, which is the moment to decide.
func TestEveryRangedReportIsUnderThePolicy(t *testing.T) {
	t.Parallel()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	handlers := []string{}
	for _, file := range sources {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range rangeHandlerPattern.FindAllStringSubmatch(string(source), -1) {
			body := match[0]
			if strings.Contains(body, "s.dateRange(r") || strings.Contains(body, "s.explicitDateRange(") {
				handlers = append(handlers, match[1])
			}
		}
	}
	sort.Strings(handlers)
	// The count, not the names: a handler can be renamed without changing what a
	// reader of the reports can ask for.
	const known = 30
	if len(handlers) != known {
		t.Errorf("%d handlers read a site date range, not the %d this suite accounts for.\n  %s\n\nIf a report was added, add it to rangedReports in integration_state_test.go so the policy limit is checked for it, then update known here. If one was removed, remove it there too.",
			len(handlers), known, strings.Join(handlers, "\n  "))
	}
	if len(rangedReports) < 20 {
		t.Errorf("rangedReports covers %d reports, which is fewer than the suite had; the policy limit is going unchecked somewhere", len(rangedReports))
	}
}
