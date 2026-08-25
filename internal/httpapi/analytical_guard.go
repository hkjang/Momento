package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// An exploratory analytics request can always be widened until it is expensive.
// Without a bound, one 10 year range holds a database connection until the browser
// gives up, and the operator sees a hung page instead of a reason. Every heavy read
// therefore runs under a deadline and fails with guidance.

// analyticalTimeout is the ceiling for one interactive analytical read.
const analyticalTimeout = 25 * time.Second

// analyticalContext bounds a heavy read. Cancelling the context also cancels the
// running statement in PostgreSQL, so the connection is released immediately.
func (s *Server) analyticalContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), analyticalTimeout)
}

// queryErrorResponse maps a failed analytical read to a status and an explanation.
// A timeout is not an internal error: the request was simply too wide, and the
// answer is to narrow it.
func queryErrorResponse(err error) (int, string, string) {
	switch {
	case err == nil:
		return http.StatusOK, "", ""
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "QUERY_TIMEOUT",
			"분석 쿼리가 25초 제한을 초과했습니다. 기간을 좁히거나 Segment로 범위를 줄이고, 반복 조회는 Fast/Preview 모드나 Scheduled Report를 사용하십시오."
	case errors.Is(err, context.Canceled):
		return 499, "QUERY_CANCELED", "요청이 취소되었습니다."
	case isQueryCanceledByServer(err):
		return http.StatusGatewayTimeout, "QUERY_TIMEOUT",
			"데이터베이스가 쿼리를 취소했습니다. 기간을 좁혀 다시 시도하십시오."
	default:
		return http.StatusInternalServerError, "QUERY_FAILED", err.Error()
	}
}

// isQueryCanceledByServer recognises PostgreSQL cancelling a statement, which is
// what a server side statement_timeout or an administrator cancel produces.
func isQueryCanceledByServer(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 57014 query_canceled, 57P01 admin_shutdown of the backend.
		return pgErr.Code == "57014" || pgErr.Code == "57P01"
	}
	return false
}

// writeQueryError reports a failed analytical read with the right status.
func writeQueryError(w http.ResponseWriter, err error) {
	status, code, message := queryErrorResponse(err)
	writeError(w, status, code, message)
}
