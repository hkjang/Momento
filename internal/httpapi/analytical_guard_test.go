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
	handler := regexp.MustCompile(`func \(s \*Server\) (\w+)\(w http\.ResponseWriter, r \*http\.Request\) \{`)
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
			if !strings.Contains(body, "analytics_events") {
				continue
			}
			checked++
			if !strings.Contains(body, "analyticalContext") {
				t.Errorf("%s:%d %s reads analytics_events without opening an analytical context: a widened range holds a connection until the browser gives up, and the reader gets a hung page instead of the advice the timeout carries",
					file, index+1, match[1])
			}
		}
	}
	// The scan has to have found the handlers it is checking. A regexp that stops
	// matching would report a package where every read is bounded.
	if checked < 15 {
		t.Fatalf("only %d handlers were found to read analytics_events, so this proves nothing about the rest", checked)
	}
	t.Logf("checked %d handlers that read analytics_events", checked)
}
