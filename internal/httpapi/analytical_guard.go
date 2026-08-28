package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
	case isOutOfSharedMemory(err):
		return http.StatusInternalServerError, "DATABASE_SHARED_MEMORY",
			"데이터베이스가 병렬 조회에 필요한 공유 메모리를 확보하지 못했습니다. PostgreSQL을 Docker로 운영 중이라면 컨테이너의 /dev/shm이 기본값 64MB일 가능성이 큽니다. `--shm-size=1g`(compose에서는 `shm_size: 1gb`)로 올린 뒤 다시 시도하십시오. 기간을 좁혀도 임시로 넘어갈 수 있지만 원인은 설정입니다."
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

// isOutOfSharedMemory recognises PostgreSQL failing to grow the shared memory a
// parallel query needs.
//
// This is not a query being too wide, and telling the reader to narrow the period
// sends them to fix something that is not broken. It is almost always a database
// container left on Docker's default 64MB of /dev/shm: the report slows down for
// months as the site grows, and then the day the planner chooses a parallel plan
// it stops working. The message names the setting, because nobody reads
// "could not resize shared memory segment" and thinks of a container flag.
func isOutOfSharedMemory(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 53100 disk_full is what PostgreSQL reports when a shared memory
		// segment cannot be resized; 53200 out_of_memory covers the DSM
		// allocation failing outright.
		return (pgErr.Code == "53100" && strings.Contains(pgErr.Message, "shared memory")) || pgErr.Code == "53200"
	}
	return false
}

// writeQueryError reports a failed analytical read with the right status.
func writeQueryError(w http.ResponseWriter, err error) {
	status, code, message := queryErrorResponse(err)
	writeError(w, status, code, message)
}
