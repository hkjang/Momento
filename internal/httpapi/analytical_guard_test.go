package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTimeoutIsExplainedNotReportedAsAnInternalError(t *testing.T) {
	t.Parallel()

	status, code, message := queryErrorResponse(fmt.Errorf("query: %w", context.DeadlineExceeded))
	if status != http.StatusGatewayTimeout || code != "QUERY_TIMEOUT" {
		t.Fatalf("status = %d code = %q, want 504 QUERY_TIMEOUT", status, code)
	}
	// The message has to tell the operator what to do differently.
	for _, want := range []string{"기간을 좁히", "Segment", "Scheduled Report"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q is missing %q", message, want)
		}
	}
}

func TestServerSideCancelIsTreatedAsATimeout(t *testing.T) {
	t.Parallel()

	status, code, _ := queryErrorResponse(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}))
	if status != http.StatusGatewayTimeout || code != "QUERY_TIMEOUT" {
		t.Fatalf("status = %d code = %q, want 504 QUERY_TIMEOUT", status, code)
	}
}

func TestClientDisconnectIsNotAServerFault(t *testing.T) {
	t.Parallel()

	status, code, _ := queryErrorResponse(context.Canceled)
	if status != 499 || code != "QUERY_CANCELED" {
		t.Fatalf("status = %d code = %q, want 499 QUERY_CANCELED", status, code)
	}
}

func TestOtherFailuresStayInternal(t *testing.T) {
	t.Parallel()

	status, code, message := queryErrorResponse(errors.New("relation \"raw_events\" does not exist"))
	if status != http.StatusInternalServerError || code != "QUERY_FAILED" {
		t.Fatalf("status = %d code = %q, want 500 QUERY_FAILED", status, code)
	}
	if !strings.Contains(message, "raw_events") {
		t.Fatalf("message = %q, want the underlying cause", message)
	}
	if status, _, _ := queryErrorResponse(nil); status != http.StatusOK {
		t.Fatalf("nil error mapped to %d, want 200", status)
	}
}

func TestUnrelatedDatabaseErrorIsNotATimeout(t *testing.T) {
	t.Parallel()

	// A constraint violation must not be presented as "narrow your range".
	status, _, _ := queryErrorResponse(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an unrelated database error", status)
	}
}

// A database that cannot grow the shared memory a parallel query needs is not a
// query that was too wide. Reported as a generic failure, it sends the reader to
// narrow a period that is not the problem, and nobody reading "could not resize
// shared memory segment" thinks of a container flag.
func TestSharedMemoryExhaustionNamesTheSetting(t *testing.T) {
	t.Parallel()
	status, code, message := queryErrorResponse(&pgconn.PgError{
		Code:    "53100",
		Message: `could not resize shared memory segment "/PostgreSQL.3275806154" to 16777216 bytes: No space left on device`,
	})
	if code != "DATABASE_SHARED_MEMORY" {
		t.Fatalf("a shared memory failure is reported as %q with status %d", code, status)
	}
	for _, want := range []string{"/dev/shm", "shm-size", "shm_size"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message does not name %q, so an operator has nothing to act on: %s", want, message)
		}
	}
	// A genuine disk-full is a different problem and must not be renamed into
	// this one: 53100 is also what PostgreSQL reports when the volume fills.
	if _, diskCode, _ := queryErrorResponse(&pgconn.PgError{
		Code: "53100", Message: "could not extend file: No space left on device",
	}); diskCode == "DATABASE_SHARED_MEMORY" {
		t.Error("a full disk is being reported as a shared memory setting, which sends the operator to the wrong place")
	}
}

// TestEveryHeavyReadRunsUnderTheDeadline holds the claim analyticalContext makes
// about itself: "Every heavy read therefore runs under a deadline and fails with
// guidance."
//
// Ten of them did not. The event list, the page list, the visitor list, the
// query builder, the search report, the feature intelligence report, both
// journey analyses, the experiment report and the natural language endpoint all
// read analytics_events through the request's own context. A widened range there
// holds a connection until the browser gives up, and cancelling the browser tab
// is the only thing that ends it — which is the exact failure the deadline was
// written for. The query builder is the worst of them: it is the one surface
// whose range and dimensions a reader chooses freely.
//
// The check reads the source because that is where the property lives. A
// behavioural test would have to make a query slow on purpose, which pins the
// fixture rather than the rule.
func TestEveryHeavyReadRunsUnderTheDeadline(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the package: %v", err)
	}
	// Any function that is handed the response writer and the request, however
	// many other parameters it takes.
	//
	// This used to require the signature to end there, and mcpCall takes the
	// decoded JSON-RPC request as a third parameter — so the entire MCP surface,
	// twenty-two tools an agent can widen at will, was never looked at by the
	// test that exists to make sure nothing is unbounded. A rule with a hole in
	// it reads exactly like a rule.
	// The rule used to be "reads analytics_events". Nine handlers read the same
	// data through the tables the view is built from and were never looked at —
	// the tracking debugger, which sorts every event ever received across every
	// site to take two hundred; the event catalog, which counts all of raw_events
	// per definition with no time bound at all; the identity report, the session
	// report, the path report and the install diagnostics. A rule named after one
	// relation covers the reads somebody thought of.
	heavyRead := regexp.MustCompile(`(?i)(?:FROM|JOIN)\s+(analytics_events|raw_events|sessions|visitors|visitor_identities|visitor_sessions|daily_site_metrics|daily_site_visitors|daily_site_sessions)\b`)

	// Two shapes must not carry it, and saying so here is the point: an exemption
	// nobody wrote down is indistinguishable from a hole.
	//
	// The exports stream deliberately — up to a hundred thousand events, with a
	// note on the last line when they stop early. Twenty-five seconds would cut
	// a legitimate download and call it a timeout.
	//
	// deleteAnalyticsData and executePrivacyRequest destroy data on purpose. A
	// deadline there aborts a purge partway and leaves an operator to guess how
	// much of it happened.
	deliberatelyUnbounded := map[string]bool{
		"exportEvents":         true,
		"exportPrivacyRequest": true,
		"deleteAnalyticsData":  true,
	}
	handler := regexp.MustCompile(`func \(s \*Server\) (\w+)\([^)]*w http\.ResponseWriter, r \*http\.Request[^)]*\)`)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		lines := strings.Split(string(source), "\n")
		for index, line := range lines {
			match := handler.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			depth, end := 0, index
			for scan := index; scan < len(lines); scan++ {
				depth += strings.Count(lines[scan], "{") - strings.Count(lines[scan], "}")
				if depth <= 0 && scan > index {
					end = scan
					break
				}
			}
			body := strings.Join(lines[index:end+1], "\n")
			if !heavyRead.MatchString(body) {
				continue
			}
			if deliberatelyUnbounded[match[1]] {
				continue
			}
			checked++
			if !strings.Contains(body, "analyticalContext") {
				t.Errorf("%s:%d %s reads the event tables without opening an analytical context: a widened range holds a connection until the browser gives up, and the reader gets a hung page instead of the advice the timeout carries",
					file, index+1, match[1])
			}
		}
	}
	// The scan has to have found the handlers it is checking. A regexp that stops
	// matching would report a package where every read is bounded.
	if checked < 20 {
		t.Fatalf("only %d handlers were found to read the event tables, so this proves nothing about the rest", checked)
	}
	t.Logf("checked %d handlers that read the event, session or visitor tables", checked)
}

// A tool that runs out of time has to say something an agent can act on.
//
// The MCP surface answered the driver's own words, so a read that hit the
// deadline told an agent "context deadline exceeded". That is not something to
// act on, and an agent with nothing to act on repeats the call — the same read,
// the same deadline, on a database that is already busy. The screens answer the
// same failure with advice: narrow the range, use a segment, have it delivered.
// An agent can follow every one of those.
func TestATimedOutToolTellsAnAgentWhatToDo(t *testing.T) {
	t.Parallel()
	timedOut := mcpFailure(context.DeadlineExceeded)
	if !strings.Contains(timedOut, "QUERY_TIMEOUT") {
		t.Errorf("a tool that ran out of time answers %q, which names no failure an agent can recognise", timedOut)
	}
	for _, advice := range []string{"기간", "Segment"} {
		if !strings.Contains(timedOut, advice) {
			t.Errorf("the answer does not mention %q: an agent given no action repeats the same call", advice)
		}
	}
	// An error that is not one of the recognised failures keeps its own words
	// rather than being dressed up as something it is not.
	plain := mcpFailure(errors.New("semantic metric not found"))
	if plain != "semantic metric not found" {
		t.Errorf("an ordinary failure was rewritten as %q", plain)
	}
}
