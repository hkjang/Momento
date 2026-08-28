package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
