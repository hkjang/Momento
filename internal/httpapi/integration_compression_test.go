package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every response left the service uncompressed: the analytical answers, which are
// JSON and compress many times over, and the console's own 1.5MB of JavaScript
// alongside them. Browsers had been asking for gzip on every request since the
// first one. On a corporate network that is most of what a reader waits for on a
// screen the database answered in a quarter of a second — the session list below
// is 42KB of JSON that now crosses the wire as 1.4KB.
//
// The measurement is the test: the same request is made twice, once asking for
// gzip and once not, and both the ratio and the identity of what comes back are
// checked. A middleware that compressed nothing would pass a header assertion.
func TestAnalyticalAnswersAreCompressed(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)

	for _, probe := range []struct {
		name string
		path string
	}{
		{"visitor insight report", "/api/v1/sites/" + f.siteKey + "/visitor-insights?from=" + from + "&to=" + today},
		{"event report", "/api/v1/sites/" + f.siteKey + "/events?from=" + from + "&to=" + today},
		{"session list", "/api/v1/sites/" + f.siteKey + "/sessions?from=" + from + "&to=" + today},
	} {
		plain := f.rawGet(t, probe.path, "")
		if plain.Code != http.StatusOK {
			t.Fatalf("%s answered %d: %s", probe.name, plain.Code, plain.Body.String())
		}
		compressed := f.rawGet(t, probe.path, "gzip")
		if compressed.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("%s was asked for gzip and answered %q: the reader downloads %d bytes that would have been a fraction of that",
				probe.name, compressed.Header().Get("Content-Encoding"), plain.Body.Len())
			continue
		}
		// Read the size before decoding: the recorder's buffer is consumed by the
		// reader below, and asking afterwards reports zero — which would have made
		// the ratio check below pass against a middleware that compressed nothing.
		sent := compressed.Body.Len()
		reader, err := gzip.NewReader(compressed.Body)
		if err != nil {
			t.Fatalf("%s: the gzip answer does not decompress: %v", probe.name, err)
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("%s: reading the gzip answer: %v", probe.name, err)
		}
		if string(decoded) != plain.Body.String() {
			t.Errorf("%s: the compressed answer is not the same document as the uncompressed one", probe.name)
		}
		t.Logf("%s: %d bytes over the wire, %d before compression", probe.name, sent, len(decoded))
		// JSON of this shape compresses several times over. Half is a floor set
		// well below what is achievable, so only a middleware that has stopped
		// working can fail it.
		if sent*2 > len(decoded) {
			t.Errorf("%s: %d bytes compressed from %d, which is barely a saving", probe.name, sent, len(decoded))
		}
	}

	// A client that does not ask for an encoding must still be able to read the
	// answer, which is what an on-premise script or an integration usually is.
	plain := f.rawGet(t, "/api/v1/sites/"+f.siteKey+"/events?from="+from+"&to="+today, "")
	if plain.Header().Get("Content-Encoding") != "" {
		t.Errorf("a client that asked for no encoding was sent %q", plain.Header().Get("Content-Encoding"))
	}
}

func (f fixture) rawGet(t *testing.T, path, encoding string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	if encoding != "" {
		request.Header.Set("Accept-Encoding", encoding)
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	return recorder
}
