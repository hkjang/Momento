package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// writeJSON encodes before it commits to a status.
//
// It used to write the header first and encode into the response, discarding
// whatever the encoder said. A value the encoder cannot represent — a NaN or an
// infinity, which is what Go division by zero produces and what a rate computed
// from a database count can become — then produced a 200 with a truncated body
// and nothing anywhere saying so. The console would show a parse failure for a
// request the service believed it had answered, and the log would show a
// successful request.
//
// Encoding first means such a value is a 500 that names itself. The fallback
// body is written literally rather than through writeError, because that calls
// this function and a failure here must not become a loop.
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"RESPONSE_NOT_ENCODABLE","message":"보고서에 표현할 수 없는 값이 있어 응답을 만들지 못했습니다."}}`+"\n")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func decodeJSON(r *http.Request, dst any, max int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}
