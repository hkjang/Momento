package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The analytical endpoints build SQL by hand and are protected by a 25 second
// deadline. Correctness tests run against a fixture of a few hundred rows, where
// a query that scans the site per row still finishes — which is how behavioural
// segments shipped broken for three minor versions. This harness answers the
// question the fixture cannot: does each report still return inside the deadline
// when the site holds millions of events?
//
// Set MOMENTO_LOAD_POSTGRES_DSN to run it against a throwaway database. It is a
// separate variable from the correctness suite because seeding takes over a
// minute and the run takes several, so CI does not pay for it on every push.
//
// Point it at its own database, not at the one the correctness suite uses: the
// seeded events land on the fixture site, and the correctness tests then run
// their assertions against two million rows and exceed the default test timeout.
//
//	docker run -d --name momento-load -e POSTGRES_PASSWORD=x -e POSTGRES_DB=momento \
//	  -p 55505:5432 postgres:17-alpine -c shared_buffers=256MB -c work_mem=32MB
//	MOMENTO_LOAD_POSTGRES_DSN='postgres://postgres:x@127.0.0.1:55505/momento?sslmode=disable' \
//	  go test ./internal/httpapi/ -run TestEndpointLatency -v -timeout 30m

// loadBudget is the latency a report must stay under. It is deliberately below
// the 25 second deadline: a report that only just fits has no headroom for a
// larger site, a colder cache or a second concurrent reader.
const loadBudget = 15 * time.Second

// loadEventCount is how many events the harness seeds. Two million over ninety
// days is a busy internal service for a year.
const loadEventCount = 2_000_000

func loadPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MOMENTO_LOAD_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOMENTO_LOAD_POSTGRES_DSN is not set")
	}
	t.Setenv("MOMENTO_TEST_POSTGRES_DSN", dsn)
	return testPool(t)
}

// seedLoad fills the fixture site with events. The distribution matters: the
// automatic signals are a minority of traffic, searches are a slice, and most
// people appear a handful of times while a few appear constantly, because a
// uniform spread hides the aggregates that behave badly on skew.
func seedLoad(t *testing.T, pool *pgxpool.Pool, f fixture) {
	t.Helper()
	ctx := context.Background()
	var existing int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1`, f.siteID).Scan(&existing); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if existing >= loadEventCount {
		t.Logf("reusing %d seeded events", existing)
		return
	}
	started := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,event_name,event_timestamp,received_at,visitor_id,session_id,user_id,page_url,device_type,browser,os,properties,is_conversion)
		SELECT gen_random_uuid(),$1,'prd',
			name.value,
			now() - ((i % 7776000) * interval '1 second'),
			now(),
			'load-v-' || (i % 200000),
			'load-s-' || (i % 600000),
			CASE WHEN i % 3 = 0 THEN 'LOADEMP' || (i % 50000) ELSE NULL END,
			'https://portal.internal/page/' || (i % 400),
			(ARRAY['desktop','mobile','tablet'])[1 + (i % 3)],
			'Chrome','Windows',
			CASE WHEN name.value = 'search' THEN jsonb_build_object('result_count', (i % 12), 'query_words', 2)
				WHEN name.value = 'web_vital' THEN jsonb_build_object('metric','LCP','value',(i % 5000))
				ELSE '{}'::jsonb END,
			name.value = 'purchase'
		FROM generate_series(1, $2) i
		CROSS JOIN LATERAL (SELECT (ARRAY['page_view','page_view','page_view','page_view','click','click','click','scroll','user_engagement','web_vital',
			'form_start','form_submit','feature_used','rage_click','dead_click','rapid_back','form_retry','error_after_click',
			'slow_interaction','error','resource_error','search','search_click','search_refine','repeated_search','purchase'])[1 + (i % 26)] AS value) name`,
		f.siteID, loadEventCount); err != nil {
		t.Fatalf("seed events: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,last_seen,linked_at)
		SELECT site_id,visitor_id,min(user_id),min(event_timestamp),max(event_timestamp),min(event_timestamp)
		FROM raw_events WHERE site_id=$1 AND user_id IS NOT NULL GROUP BY site_id,visitor_id
		ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("seed identities: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(site_id,session_id,environment,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,device_type)
		SELECT site_id,session_id,'prd',min(visitor_id),min(user_id),min(event_timestamp),max(event_timestamp),count(*),
			count(*) FILTER(WHERE event_name='page_view'),count(*) FILTER(WHERE is_conversion),count(*) > 2,min(page_url),max(page_url),min(device_type)
		FROM raw_events WHERE site_id=$1 GROUP BY site_id,session_id
		ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	for _, table := range []string{"raw_events", "visitor_identities", "sessions"} {
		if _, err := pool.Exec(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	t.Logf("seeded %d events in %s", loadEventCount, time.Since(started).Round(time.Second))
}

// loadRepeats is how many times each endpoint is called. A single timing is not
// evidence: the same probe varied by three seconds between runs on an otherwise
// idle machine, which is enough to invent a regression or hide one. The median
// is what the budget is checked against; the spread is printed so a noisy
// measurement is visible rather than silently trusted.
const loadRepeats = 3

type loadResult struct {
	name    string
	median  time.Duration
	fastest time.Duration
	slowest time.Duration
	status  int
}

func TestEndpointLatencyUnderLoad(t *testing.T) {
	pool := loadPool(t)
	f := seed(t, pool)
	seedLoad(t, pool, f)

	from, today := f.siteDates(t, 30)
	long := f.siteDate(t, -90)
	site := "/api/v1/sites/" + f.siteKey
	frictionSegment := saveLoadSegment(t, f, "부하 시험 · 막힘",
		`{"combinator":"and","rules":[{"field":"entity.frustration_signals","operator":">=","value":1}]}`)

	type probe struct {
		name   string
		method string
		path   string
		body   string
	}
	probes := []probe{
		{name: "overview", path: site + "/overview?from=" + from + "&to=" + today},
		{name: "visitor-insights", path: site + "/visitor-insights?from=" + from + "&to=" + today},
		{name: "anomalies", path: site + "/anomalies"},
		{name: "attribution", path: site + "/attribution?from=" + from + "&to=" + today + "&model=last_non_direct"},
		{name: "frustration", path: site + "/frustration?from=" + from + "&to=" + today},
		{name: "search-analytics", path: site + "/search-analytics?from=" + from + "&to=" + today},
		{name: "cohort-90d", path: site + "/cohort?from=" + long + "&to=" + today + "&granularity=week&periods=6"},
		{name: "cohort-90d+segment", path: site + "/cohort?from=" + long + "&to=" + today + "&granularity=week&periods=6&segment_ids=" + frictionSegment},
		{name: "experience", path: site + "/experience?from=" + from + "&to=" + today},
		{name: "experience+segment", path: site + "/experience?from=" + from + "&to=" + today + "&segment_ids=" + frictionSegment},
		{name: "pages", path: site + "/pages?from=" + from + "&to=" + today},
		{name: "events", path: site + "/events?from=" + from + "&to=" + today},
		{name: "visitors", path: site + "/visitors?from=" + from + "&to=" + today},
		{name: "sessions", path: site + "/sessions?from=" + from + "&to=" + today},
		{name: "feature-intelligence", path: site + "/feature-intelligence?from=" + from + "&to=" + today},
		{name: "data-quality", path: site + "/data-quality?from=" + from + "&to=" + today},
		{name: "adoption", path: site + "/adoption?from=" + from + "&to=" + today},
		{name: "insights", path: site + "/insights?from=" + from + "&to=" + today},
		{name: "realtime", path: site + "/realtime"},
		{name: "visitor-search", path: site + "/visitor-search?q=load-v-1&from=" + from + "&to=" + today},
		{name: "visitor-timeline", path: site + "/visitors/load-v-1/timeline?from=" + long + "&to=" + today + "&limit=200"},
		{
			name: "query-builder+segment", method: http.MethodPost, path: "/api/v1/query",
			body: fmt.Sprintf(`{"site_id":"%s","segment_id":"%s","date_range":{"from":"%s","to":"%s"},"dimensions":["device.type"],"metrics":["events","users"]}`,
				f.siteKey, frictionSegment, from, today),
		},
		{
			name: "funnel", method: http.MethodPost, path: "/api/v1/funnel",
			body: fmt.Sprintf(`{"site_id":"%s","environment":"prd","from":"%s","to":"%s","mode":"closed",
				"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"},{"name":"구매","event":"purchase"}]}`,
				f.siteKey, from, today),
		},
		{
			name: "funnel+segment", method: http.MethodPost, path: "/api/v1/funnel",
			body: fmt.Sprintf(`{"site_id":"%s","environment":"prd","from":"%s","to":"%s","mode":"closed","compare_segment_ids":["%s"],
				"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"},{"name":"구매","event":"purchase"}]}`,
				f.siteKey, from, today, frictionSegment),
		},
	}

	results := make([]loadResult, 0, len(probes))
	for _, p := range probes {
		method := p.method
		if method == "" {
			method = http.MethodGet
		}
		timings := make([]time.Duration, 0, loadRepeats)
		status := 0
		for attempt := 0; attempt < loadRepeats; attempt++ {
			request := httptest.NewRequest(method, p.path, strings.NewReader(p.body))
			request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
			if p.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			recorder := httptest.NewRecorder()
			started := time.Now()
			f.server.Handler().ServeHTTP(recorder, request)
			timings = append(timings, time.Since(started))
			status = recorder.Code
			if recorder.Code >= 400 {
				t.Errorf("%s answered %d after %s: %s", p.name, recorder.Code, timings[attempt].Round(time.Millisecond), truncateBody(recorder.Body.String()))
				break
			}
		}
		sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
		results = append(results, loadResult{
			name:    p.name,
			median:  timings[len(timings)/2],
			fastest: timings[0],
			slowest: timings[len(timings)-1],
			status:  status,
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].median > results[j].median })
	var report strings.Builder
	report.WriteString(fmt.Sprintf("\nendpoint latency over %d events, median of %d (budget %s)\n", loadEventCount, loadRepeats, loadBudget))
	for _, result := range results {
		flag := "  "
		if result.median > loadBudget {
			flag = "!!"
		}
		report.WriteString(fmt.Sprintf("%s %-24s %9s  [%s .. %s]  http %d\n", flag, result.name,
			result.median.Round(time.Millisecond), result.fastest.Round(time.Millisecond), result.slowest.Round(time.Millisecond), result.status))
	}
	t.Log(report.String())

	for _, result := range results {
		if result.median > loadBudget {
			t.Errorf("%s took %s at the median of %d runs, over the %s budget: it has no headroom under the %s deadline",
				result.name, result.median.Round(time.Millisecond), loadRepeats, loadBudget, analyticalTimeout)
		}
	}
}

func saveLoadSegment(t *testing.T, f fixture, name, definition string) string {
	t.Helper()
	body := fmt.Sprintf(`{"site_id":%s,"name":%s,"definition":%s,"shared":false}`,
		strconv.Quote(f.siteKey), strconv.Quote(name), definition)
	saved := f.do(t, http.MethodPost, "/api/v1/segments", body)
	id := fmt.Sprint(saved["id"])
	if id == "" || id == "<nil>" {
		t.Fatalf("could not save the load segment: %v", saved)
	}
	return id
}

func truncateBody(body string) string {
	if len(body) > 300 {
		return body[:300] + "…"
	}
	return body
}
