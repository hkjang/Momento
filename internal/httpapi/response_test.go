package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A response the encoder cannot represent used to be a 200 with a truncated
// body: the header went out before the encoding was attempted and the encoder's
// complaint was discarded. The reader got a parse failure for a request the
// service had logged as successful.
func TestAnUnencodableAnswerIsAnErrorRatherThanATruncatedOne(t *testing.T) {
	recorder := httptest.NewRecorder()
	// A rate computed from a zero denominator, which is what an empty period
	// produces if a division is not guarded.
	writeJSON(recorder, http.StatusOK, map[string]any{"conversion_rate": math.Inf(1)})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want 500: a body the reader cannot parse is not a successful answer", recorder.Code)
	}
	var decoded struct {
		Error struct {
			Code, Message string
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("the failure answer is itself not valid JSON (%v): %s", err, recorder.Body.String())
	}
	if decoded.Error.Code != "RESPONSE_NOT_ENCODABLE" {
		t.Errorf("the failure answers with code %q, which does not say what went wrong", decoded.Error.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
		t.Errorf("the failure answer is not marked as JSON: %q", recorder.Header().Get("Content-Type"))
	}
}

// And an ordinary answer has to be unchanged by that: same status, same body,
// still one JSON document per response.
func TestAnEncodableAnswerIsUnchanged(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusCreated, map[string]any{"id": "abc", "users": 42})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("answered %d, want 201", recorder.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, recorder.Body.String())
	}
	if decoded["id"] != "abc" || decoded["users"] != float64(42) {
		t.Errorf("the answer changed: %v", decoded)
	}
	if !strings.HasSuffix(recorder.Body.String(), "\n") {
		t.Error("the answer no longer ends with a newline, which the previous encoder wrote and clients may split on")
	}
}
