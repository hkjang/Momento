package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A malformed date range reached writeRangeError, which answered the policy case
// and then called itself for every other case. Every report endpoint routes a bad
// from/to through that function, so one request with an unparseable date recursed
// until the goroutine stack ran out — and a Go stack overflow is fatal rather
// than a panic the recoverer middleware can absorb, so the process died and took
// every other request in flight with it. Any authenticated reader could do it by
// typing a date wrong.
//
// This is the guard. It runs against the whole router because the defect was in
// the shared helper rather than in any one handler, and the endpoints listed are
// the ones that parse a range: if one of them stops answering, the test names it.
//
// Against the code this replaces the test does not fail, it kills the test
// binary, which is the point.
func TestAMalformedDateRangeIsAnsweredRatherThanFatal(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	site := "/api/v1/sites/" + f.siteKey

	for _, path := range []string{
		site + "/overview?from=not-a-date&to=2026-01-01",
		site + "/visitor-insights?from=2026-01-01&to=nonsense",
		site + "/events?from=&to=2026-13-45",
		site + "/pages?from=2026-01-02&to=2026-01-01",
		site + "/attribution?from=oops&to=2026-01-01",
		site + "/usage?from=2026-01-01&to=oops",
		site + "/sessions?from=oops&to=oops",
		site + "/ecommerce?from=2026-01-02&to=2026-01-01",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400: a range the reader typed wrong is their mistake to correct, not the service's to fall over on",
				path, recorder.Code)
			continue
		}
		if !strings.Contains(recorder.Body.String(), "INVALID_RANGE") {
			t.Errorf("%s answered 400 without saying the range was the problem: %s", path, recorder.Body.String())
		}
	}

	// The policy case is the branch that did answer, and it has to keep its own
	// code: one is fixed by shortening the period, the other by correcting the
	// dates, and a reader cannot act on the difference if both say the same thing.
	if _, err := pool.Exec(t.Context(), `INSERT INTO query_policies(site_id,max_exact_days,max_complexity_score,background_threshold,fast_sample_percent,preview_sample_percent)
		VALUES($1,7,90,60,10,1) ON CONFLICT(site_id) DO UPDATE SET max_exact_days=excluded.max_exact_days`, f.siteID); err != nil {
		t.Fatalf("set the site query policy: %v", err)
	}
	from, today := f.siteDates(t, 30)
	request := httptest.NewRequest(http.MethodGet, site+"/overview?from="+from+"&to="+today, nil)
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "RANGE_EXCEEDS_POLICY") {
		t.Errorf("a range longer than the site's policy answered %d (%s), want 400 RANGE_EXCEEDS_POLICY",
			recorder.Code, recorder.Body.String())
	}
}
