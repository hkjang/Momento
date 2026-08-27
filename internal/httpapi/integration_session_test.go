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
)

// Every session number on every screen — how many, how long, how many bounced,
// which page they landed on — is read from the sessions table, and that table is
// not measured. It is derived, one event at a time, by the collector: each event
// widens the session it belongs to and updates what the screens read.
//
// Nothing had ever delivered a visit of a known shape and checked that the
// session it produced was that shape. A defect there is invisible from the
// reports, because every report reads the same wrong row and they all agree.
// This release already found one number nobody was checking; sessions are the
// other one, and this repository has shipped a session that lasted 26 hours.
func TestASessionIsTheVisitThatProducedIt(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// The second fixture site has no events of its own, and this is a day beyond
	// the seventy the fixture fills, so the sessions read back here can only be
	// the ones delivered.
	const dayOffset = -100
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the site timezone: %v", err)
	}
	at := time.Now().In(location).AddDate(0, 0, dayOffset)
	start := time.Date(at.Year(), at.Month(), at.Day(), 12, 0, 0, 0, location)

	// A visit of a shape every field can be read off: six minutes long, four
	// events, two of them page views on different pages, one purchase.
	f.deliverAt(t, f.otherKey, "session-shape", "visitor-shape", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/home"},
		{name: "click", after: 2 * time.Minute, page: "https://portal.internal/home"},
		{name: "page_view", after: 5 * time.Minute, page: "https://portal.internal/apply"},
		{name: "purchase", after: 6 * time.Minute, page: "https://portal.internal/apply"},
	}, start)
	// And a visit that is one page view and nothing else: the bounce.
	f.deliverAt(t, f.otherKey, "session-bounce", "visitor-bounce", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/home"},
	}, start)
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	var events, pageViews, conversions int64
	var seconds float64
	var engaged bool
	var landing, exit string
	if err := pool.QueryRow(ctx, `SELECT event_count,page_views,conversion_count,engaged,
			extract(epoch FROM (last_event_at-started_at)),coalesce(landing_page,''),coalesce(exit_page,'')
		FROM sessions WHERE session_id=$1`, "session-shape").
		Scan(&events, &pageViews, &conversions, &engaged, &seconds, &landing, &exit); err != nil {
		t.Fatalf("read the session that was delivered: %v", err)
	}
	for _, check := range []struct {
		what      string
		got, want int64
	}{
		{"events", events, 4},
		{"page views", pageViews, 2},
		{"conversions", conversions, 1},
	} {
		if check.got != check.want {
			t.Errorf("the session records %d %s and %d were delivered", check.got, check.what, check.want)
		}
	}
	if seconds != 360 {
		t.Errorf("the session lasted %vs and the visit lasted 360s: session duration is on the landing screen and in the insight report", seconds)
	}
	if !engaged {
		t.Error("a six minute visit with two page views and a purchase is not counted as engaged")
	}
	if landing != "https://portal.internal/home" || exit != "https://portal.internal/apply" {
		t.Errorf("the visit landed on /home and left from /apply; the session says it landed on %q and left from %q", landing, exit)
	}

	// The bounce has to stay a bounce. Engagement is what separates a visit from
	// somebody who arrived and left, and every rate on the landing page report
	// divides by it.
	var bounceEngaged bool
	var bouncePageViews int64
	if err := pool.QueryRow(ctx, `SELECT engaged,page_views FROM sessions WHERE session_id=$1`, "session-bounce").
		Scan(&bounceEngaged, &bouncePageViews); err != nil {
		t.Fatalf("read the bounce: %v", err)
	}
	if bounceEngaged || bouncePageViews != 1 {
		t.Errorf("one page view and nothing else is a bounce; the session says engaged=%v with %d page views", bounceEngaged, bouncePageViews)
	}

	// And the screen has to report what the table holds. Two sessions, one
	// engaged, six minutes and nothing averaging to three.
	day := f.siteDate(t, dayOffset)
	report := f.get(t, "/api/v1/sites/"+f.otherKey+"/overview?from="+day+"&to="+day)
	current, _ := report["current"].(map[string]any)
	if current == nil {
		t.Fatalf("the overview answered without a period: %v", report)
	}
	if sessions, _ := current["sessions"].(float64); sessions != 2 {
		t.Errorf("the overview reports %v sessions for two visits", sessions)
	}
	if duration, _ := current["avg_session_duration"].(float64); duration != 180 {
		t.Errorf("the overview reports an average session of %vs; a 360s visit and a 0s visit average 180s", duration)
	}
}

// TestAVisitDeliveredOutOfOrderIsStillTheSameVisit covers the case the offline
// queue creates. A batch held while the network was down arrives after the
// events that happened later, so the collector sees a session's last event
// before its first — and the landing page, the exit page and the duration are
// all decided by which event came first.
func TestAVisitDeliveredOutOfOrderIsStillTheSameVisit(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	location, _ := time.LoadLocation("Asia/Seoul")
	at := time.Now().In(location).AddDate(0, 0, -100)
	start := time.Date(at.Year(), at.Month(), at.Day(), 15, 0, 0, 0, location)

	// The later half arrives first, as it would after a reconnect.
	f.deliverAt(t, f.otherKey, "session-reordered", "visitor-reordered", []timedEvent{
		{name: "page_view", after: 4 * time.Minute, page: "https://portal.internal/done"},
	}, start)
	f.deliverAt(t, f.otherKey, "session-reordered", "visitor-reordered", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/start"},
	}, start)
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	var landing, exit string
	var seconds float64
	var events int64
	if err := pool.QueryRow(ctx, `SELECT coalesce(landing_page,''),coalesce(exit_page,''),
			extract(epoch FROM (last_event_at-started_at)),event_count
		FROM sessions WHERE session_id=$1`, "session-reordered").Scan(&landing, &exit, &seconds, &events); err != nil {
		t.Fatalf("read the reordered session: %v", err)
	}
	if landing != "https://portal.internal/start" {
		t.Errorf("the visit started on /start and the session says it landed on %q: the page a visitor arrives on is the acquisition report's whole subject", landing)
	}
	if exit != "https://portal.internal/done" {
		t.Errorf("the visit ended on /done and the session says it left from %q", exit)
	}
	if seconds != 240 || events != 2 {
		t.Errorf("the session lasted %vs across %d events; the visit was 240s across 2", seconds, events)
	}
}

type timedEvent struct {
	name  string
	after time.Duration
	page  string
}

// deliverAt posts one batch through the collector with each event stamped at its
// own moment, the way a browser does.
func (f fixture) deliverAt(t *testing.T, siteKey, sessionID, visitorID string, events []timedEvent, start time.Time) {
	t.Helper()
	parts := make([]string, 0, len(events))
	for _, item := range events {
		properties := "{}"
		if item.name == "purchase" {
			properties = `{"value":"1000"}`
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":%s,"contract_version":1,"context":{"page":{"url":%q,"title":"페이지","referrer":""}}}`,
			uuid.NewString(), item.name, start.Add(item.after).UnixMilli(), properties, item.page))
	}
	f.postCollect(t, siteKey, sessionID, visitorID, events[0].page, parts)
}

// postCollect delivers one prepared batch the way a browser does.
func (f fixture) postCollect(t *testing.T, siteKey, sessionID, visitorID, landing string, events []string) {
	t.Helper()
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":%q,"session_id":%q,
		"context":{"page":{"url":%q,"title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
		"events":[%s]}`, siteKey, f.trackingKey, visitorID, sessionID, landing, strings.Join(events, ","))
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("deliver %s: %d %s", sessionID, recorder.Code, recorder.Body.String())
	}
}
