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

// The landing screen shows a total and, underneath it, the same measure day by
// day. The total is counted from the events; the daily series is read from the
// rollups the collector maintains as those events arrive. Two sources, one
// number, and nothing compared them — an operator adding up the chart and
// getting something other than the card above it has no way to tell which one
// is wrong, and neither had a test.
//
// Only the additive measures can be compared this way. Summing daily visitors
// or daily sessions is supposed to exceed the period's own count, because a
// person who comes back on three days is three daily visitors and one visitor;
// events, page views and conversions have no such excuse.
func TestTheDailySeriesAddsUpToTheTotalAboveIt(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// The second fixture site has no events and no rollups of its own, so what is
	// delivered here is the whole of what both sources see.
	var siteID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM sites WHERE site_key=$1`, f.otherKey).Scan(&siteID); err != nil {
		t.Fatalf("resolve the second site: %v", err)
	}
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the site timezone: %v", err)
	}

	// Three site-local days, at an hour that cannot drift into a neighbouring one,
	// and a different shape of activity on each so a day that went missing cannot
	// hide behind a day that looks like it.
	type day struct {
		offset            int
		visitors          int
		pageViewsPerVisit int
		otherEventsPerDay int
		conversionsPerDay int
	}
	days := []day{
		{offset: -3, visitors: 2, pageViewsPerVisit: 2, otherEventsPerDay: 1, conversionsPerDay: 1},
		{offset: -2, visitors: 3, pageViewsPerVisit: 1, otherEventsPerDay: 2, conversionsPerDay: 0},
		{offset: -1, visitors: 1, pageViewsPerVisit: 3, otherEventsPerDay: 0, conversionsPerDay: 2},
	}
	wantEvents, wantPageViews, wantConversions := 0, 0, 0
	for _, d := range days {
		at := time.Now().In(location).AddDate(0, 0, d.offset)
		noon := time.Date(at.Year(), at.Month(), at.Day(), 12, 0, 0, 0, location)
		for visitor := 0; visitor < d.visitors; visitor++ {
			events := []string{}
			for view := 0; view < d.pageViewsPerVisit; view++ {
				events = append(events, "page_view")
				wantPageViews++
			}
			for other := 0; other < d.otherEventsPerDay; other++ {
				events = append(events, "click")
			}
			for conversion := 0; conversion < d.conversionsPerDay; conversion++ {
				events = append(events, "purchase")
				wantConversions++
			}
			wantEvents += len(events)
			f.deliver(t, f.otherKey, fmt.Sprintf("trend-v-%d-%d", -d.offset, visitor), noon, events)
		}
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	from, to := f.siteDate(t, -3), f.siteDate(t, -1)
	report := f.get(t, "/api/v1/sites/"+f.otherKey+"/overview?from="+from+"&to="+to)
	current, ok := report["current"].(map[string]any)
	if !ok {
		t.Fatalf("the overview answered without a current period: %v", report)
	}
	trend, ok := report["trend"].([]any)
	if !ok {
		t.Fatalf("the overview answered without a daily series: %v", report)
	}
	if len(trend) != len(days) {
		t.Fatalf("the daily series has %d days for %d days of activity: a day that never reaches the chart is invisible on it", len(trend), len(days))
	}

	// What was delivered is the third opinion: the two sources have to agree with
	// each other and with it, or agreement between them means nothing.
	for _, measure := range []struct {
		name string
		want int
	}{
		{"events", wantEvents},
		{"page_views", wantPageViews},
		{"conversions", wantConversions},
	} {
		total, _ := current[measure.name].(float64)
		daily := float64(0)
		for _, item := range trend {
			entry, _ := item.(map[string]any)
			value, _ := entry[measure.name].(float64)
			daily += value
		}
		if int(total) != measure.want {
			t.Errorf("%s: the total says %v and %d were delivered", measure.name, total, measure.want)
		}
		if daily != total {
			t.Errorf("%s: the chart adds up to %v and the total above it says %v — an operator with both in front of them cannot tell which is wrong",
				measure.name, daily, total)
		}
	}

	// And again on the site with seventy days of history behind it, where the
	// rollups were written by the fixture rather than by the collector. There is
	// no third opinion to hold them to there, but the two sources still have to
	// agree with each other, and a range that long is where a day lost to a
	// timezone boundary would show.
	longFrom, today := f.siteDates(t, 30)
	f.assertChartMatchesTotals(t, "/api/v1/sites/"+f.siteKey+"/overview?from="+longFrom+"&to="+today)
}

func (f fixture) assertChartMatchesTotals(t *testing.T, path string) {
	t.Helper()
	report := f.get(t, path)
	current, _ := report["current"].(map[string]any)
	trend, _ := report["trend"].([]any)
	if current == nil || trend == nil {
		t.Fatalf("%s answered without a period or a series: %v", path, report)
	}
	for _, name := range []string{"events", "page_views", "conversions"} {
		total, _ := current[name].(float64)
		daily := float64(0)
		for _, item := range trend {
			entry, _ := item.(map[string]any)
			value, _ := entry[name].(float64)
			daily += value
		}
		if daily != total {
			t.Errorf("%s over %d days: the chart adds up to %v and the total says %v", name, len(trend), daily, total)
		}
	}
}

// deliver posts one visit through the collector, the way a browser does, with
// every event stamped at the same moment so it falls on one site-local day.
func (f fixture) deliver(t *testing.T, siteKey, visitorID string, at time.Time, events []string) {
	t.Helper()
	parts := make([]string, 0, len(events))
	for index, name := range events {
		properties := "{}"
		if name == "purchase" {
			properties = `{"value":"1000"}`
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":%s,"contract_version":1}`,
			uuid.NewString(), name, at.Add(time.Duration(index)*time.Second).UnixMilli(), properties))
	}
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":%q,"session_id":%q,
		"context":{"page":{"url":"https://portal.internal/home","title":"홈","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
		"events":[%s]}`, siteKey, f.trackingKey, visitorID, "session-"+visitorID, strings.Join(parts, ","))
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("deliver %v: %d %s", events, recorder.Code, recorder.Body.String())
	}
}
