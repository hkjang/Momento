package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Who counts as a new visitor is the one number the landing screen and the
// insight report both answer and nothing ever compared, in either direction:
// against each other, or against the events they are derived from. It used to be
// computed by reading every event the site had ever collected before the period
// ended — twice on the overview, three more times in the insight report — which
// is the cost this release removes by reading the daily visitor rollup instead.
//
// Swapping the source of a number nobody was checking is how a report starts
// quietly lying, so this checks it three ways: the two screens agree, they agree
// with the events, and the answer actually moves when a genuinely new person
// arrives and stays put when a familiar one comes back.
func TestNewVisitorsAgreeAcrossScreensAndWithTheEvents(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	// The fixture's three visitors all arrived seventy days ago, so the period
	// starts with no first visits at all and a report answering zero would agree
	// with every other report answering zero. One genuinely new person is
	// delivered first, through the collector, so the comparison has something to
	// discriminate with.
	baseline := overviewNewUsers(t, f, site, from, today)
	f.ingest(t, "visitor-first-timer", "", "first_visit_check")

	expected := newVisitorsFromEvents(t, pool, f.siteID, from, today)
	if expected == 0 {
		t.Fatal("the first visit did not reach the events, so this comparison cannot discriminate")
	}
	overview := overviewNewUsers(t, f, site, from, today)
	insight := insightNewUsers(t, f, site, from, today)
	if overview != expected || insight != expected {
		t.Fatalf("new visitors: the overview says %d, the insight report says %d, the events say %d — an operator with both screens open cannot tell which is wrong",
			overview, insight, expected)
	}
	if overview != baseline+1 {
		t.Fatalf("one visitor arrived for the first time and the count went from %d to %d", baseline, overview)
	}

	// And a familiar visitor must not move it. Without this half, a report that
	// counted everybody active in the period would have passed everything above.
	f.ingest(t, "visitor-desktop", "EMP001", "return_visit_check")
	if after := overviewNewUsers(t, f, site, from, today); after != overview {
		t.Fatalf("a visitor with seventy days of history was counted as new: %d, want %d", after, overview)
	}
	if after := insightNewUsers(t, f, site, from, today); after != insight {
		t.Fatalf("the insight report counted a visitor with seventy days of history as new: %d, want %d", after, insight)
	}
}

// TestFirstVisitOutlivesTheEventsItWasReadFrom is the correctness half of the
// change. Raw events expire on the site's retention policy while the daily
// rollups are kept unless the site sets a limit of its own, so a report that
// decided who was new by scanning the events called a long-standing visitor new
// again the moment their early events aged out — on a site with a one year
// window, every visitor becomes a new visitor once a year.
func TestFirstVisitOutlivesTheEventsItWasReadFrom(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	before := overviewNewUsers(t, f, site, from, today)

	// What retention does to a site that keeps one month of events: the rollups
	// still hold seventy days of daily rows for these visitors.
	cutoff := time.Now().AddDate(0, 0, -30)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM raw_events WHERE site_id=$1 AND event_timestamp < $2`, f.siteID, cutoff); err != nil {
		t.Fatalf("expire the old events: %v", err)
	}
	var remaining int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_timestamp < $2`, f.siteID, cutoff).Scan(&remaining); err != nil {
		t.Fatalf("count the expired events: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d events older than the period survived the deletion, so this does not reproduce retention", remaining)
	}

	if after := overviewNewUsers(t, f, site, from, today); after != before {
		t.Errorf("expiring the events older than the period changed the new visitor count from %d to %d: retention is not supposed to make a returning visitor new again",
			before, after)
	}
}

// newVisitorsFromEvents counts first visits the way the reports used to, straight
// from the events, so the comparison has a source that owes nothing to the
// rollups the reports now read.
func newVisitorsFromEvents(t *testing.T, pool *pgxpool.Pool, siteID uuid.UUID, from, to string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM (
		SELECT entity_id FROM analytics_events
		WHERE site_id=$1 AND environment='prd'
			AND event_timestamp < (($3::date + 1)::text || ' Asia/Seoul')::timestamptz
		GROUP BY entity_id
		HAVING min(event_timestamp) >= ($2::text || ' Asia/Seoul')::timestamptz
	) firsts`, siteID, from, to).Scan(&count); err != nil {
		t.Fatalf("count first visits from the events: %v", err)
	}
	return count
}

func overviewNewUsers(t *testing.T, f fixture, site, from, to string) int64 {
	t.Helper()
	report := f.get(t, site+"/overview?from="+from+"&to="+to)
	current, ok := report["current"].(map[string]any)
	if !ok {
		t.Fatalf("the overview answered without a current period: %v", report)
	}
	value, ok := current["new_users"].(float64)
	if !ok {
		t.Fatalf("the overview answered without a new visitor count: %v", current)
	}
	return int64(value)
}

func insightNewUsers(t *testing.T, f fixture, site, from, to string) int64 {
	t.Helper()
	report := f.get(t, site+"/visitor-insights?from="+from+"&to="+to)
	kpis, ok := report["kpis"].([]any)
	if !ok {
		t.Fatalf("the insight report answered without KPIs: %v", report)
	}
	for _, item := range kpis {
		kpi, ok := item.(map[string]any)
		if !ok || kpi["key"] != "new_users" {
			continue
		}
		value, ok := kpi["current"].(float64)
		if !ok {
			t.Fatalf("the new visitor KPI carries no value: %v", kpi)
		}
		return int64(value)
	}
	t.Fatal("the insight report has no new visitor KPI")
	return 0
}

// ingest delivers one event the way a browser does, through the collector and the
// durable inbox, so the rollups the reports read are written by the code that
// writes them in production rather than by the test.
func (f fixture) ingest(t *testing.T, visitorID, userID, eventName string) {
	t.Helper()
	user := ""
	if userID != "" {
		user = fmt.Sprintf(`"user_id":"%s",`, userID)
	}
	payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"%s","session_id":"session-%s",%s
		"context":{"page":{"url":"https://portal.internal/home","title":"홈","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
		"events":[{"id":"%s","name":"%s","timestamp":%d,"properties":{},"contract_version":1}]}`,
		f.siteKey, f.trackingKey, visitorID, visitorID, user, uuid.NewString(), eventName, time.Now().UnixMilli())
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("collect %s = %d: %s", eventName, recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: f.server.DB}).ProcessPending(context.Background()); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}
}
