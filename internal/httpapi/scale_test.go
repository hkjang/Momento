package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/Momento/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Two severe defects shipped because every test ran against a fixture of a few
// hundred rows. A query that scans the whole site once per person, and a rebuild
// that reads the table in visitor order through an index, both finish instantly
// on that much data and not at all on a real one. The load harness in
// load_test.go finds them, but it seeds two million events and takes ten
// minutes, so it is opt-in and CI never runs it.
//
// This is the guard in between. Fifty thousand events is enough that quadratic
// work takes minutes while linear work stays under a second, and small enough to
// seed in seconds on every push. The budget is deliberately loose: it is not a
// latency benchmark, it is a shape check. Anything that respects the size of the
// question passes it with room to spare; anything that walks the site per row
// does not come close.
//
// What it catches was measured rather than assumed. Restoring the correlated
// aggregate that shipped before v0.24.1 makes every segment-carrying report here
// answer 504, and the query builder run for nearly eight minutes on these fifty
// thousand events.
//
// What it does not catch was measured too. The rebuild defect fixed in v0.25.0
// was a plan choice, not a query shape: with the old statement restored the
// rebuild finishes in three seconds here and in five at a quarter of a million
// events, because the table still fits in cache and reading it in index order
// costs nothing. That class needs the two million event harness, and the
// statistics refresh in the rebuild removes its main trigger. The test below
// guards that refresh directly.

// scaleEventCount is the smallest size at which the defects this guard exists
// for are unmissable.
const scaleEventCount = 50_000

// scaleBudget is per request. Linear queries answer this dataset in well under a
// second, so a ten second ceiling absorbs slow CI hardware and a cold cache
// while still failing anything quadratic by an order of magnitude.
const scaleBudget = 10 * time.Second

// scaleRebuildBudget covers a full derived-data rebuild. It reads every event
// several times, so it gets more room than a single report.
const scaleRebuildBudget = 60 * time.Second

// scaleRetentionBudget covers one unattended retention pass. It deletes expired
// rows and then anti-joins the identity tables against the events and sessions
// that remain, which is indexed and should not grow with the site.
const scaleRetentionBudget = 30 * time.Second

func seedScale(t *testing.T, f fixture) {
	t.Helper()
	ctx := context.Background()
	pool := f.server.DB
	started := time.Now()
	if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,event_name,event_timestamp,received_at,visitor_id,session_id,user_id,page_url,device_type,browser,os,properties,is_conversion)
		SELECT gen_random_uuid(),$1,'prd',
			name.value,
			-- One event per second from now backwards put every one of these fifty
			-- thousand events inside the last fourteen hours, so the thirty and
			-- sixty day ranges the probes below ask for excluded nothing, the
			-- weekly cohort had one bucket instead of six, and every
			-- previous-period comparison read an empty period. i never reaches the
			-- modulus, so the expression that was meant to spread them over sixty
			-- days did nothing at all.
			--
			-- The day comes from the session, and 12000 sessions divide evenly by
			-- 60 days, so a session's events all land on one day; the minute comes
			-- from how far through its session the event is, so a session runs for
			-- about as many minutes as it has events. Visitors recur across days
			-- because 4000 does not divide 60 evenly.
			now() - ((i % 60) * interval '1 day') - (((i / 12000) % 1440) * interval '1 minute'),
			now(),
			'scale-v-' || (i % 4000),
			'scale-s-' || (i % 12000),
			CASE WHEN i % 3 = 0 THEN 'SCALEEMP' || (i % 1200) ELSE NULL END,
			'https://portal.internal/page/' || (i % 60),
			(ARRAY['desktop','mobile','tablet'])[1 + (i % 3)],
			'Chrome','Windows',
			CASE WHEN name.value = 'search' THEN jsonb_build_object('result_count', (i % 12), 'query_words', 2)
				WHEN name.value = 'web_vital' THEN jsonb_build_object('metric','LCP','value',(i % 5000),'rating','good')
				ELSE '{}'::jsonb END,
			name.value = 'purchase'
		FROM generate_series(1, $2) i
		CROSS JOIN LATERAL (SELECT (ARRAY['page_view','page_view','page_view','click','click','scroll','user_engagement','web_vital',
			'form_start','form_submit','feature_used','rage_click','dead_click','rapid_back','form_retry','error_after_click',
			'slow_interaction','error','resource_error','search','search_click','purchase'])[1 + (i % 22)] AS value) name`,
		f.siteID, scaleEventCount); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// VACUUM rather than ANALYZE. Fifty thousand rows have just been inserted, and
	// autovacuum will work through the table on its own schedule — which is to say
	// during the probes below, competing with whichever of them happens to run
	// first. That is not a small effect: measuring immediately after a seed rather
	// than on a settled table read 423ms for a screen that answers in 134ms, which
	// is enough to send a reader looking for a defect that is not there. Doing the
	// work here means the numbers describe the queries.
	if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) raw_events"); err != nil {
		t.Fatalf("settle the event table: %v", err)
	}
	t.Logf("seeded %d events in %s", scaleEventCount, time.Since(started).Round(time.Millisecond))
}

func TestHeavyQueriesStayLinearAtScale(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	seedScale(t, f)
	ctx := context.Background()

	// The rebuild is measured first because the reports read what it derives, and
	// because it is the operation that runs inside a privacy deletion request.
	rebuildStarted := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := service.RebuildSiteDerivedData(ctx, tx, f.siteID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("rebuild: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	rebuildTook := time.Since(rebuildStarted)
	t.Logf("rebuilt derived data for %d events in %s", scaleEventCount, rebuildTook.Round(time.Millisecond))
	if rebuildTook > scaleRebuildBudget {
		t.Errorf("the derived-data rebuild took %s for %d events, over the %s budget: it runs inside privacy deletion, so this is a deletion that does not finish",
			rebuildTook.Round(time.Millisecond), scaleEventCount, scaleRebuildBudget)
	}
	// Same reasoning as the event table: the rebuild has just filled every one of
	// these, and a probe should not be measuring the cleanup of that.
	for _, table := range []string{"visitors", "visitor_identities", "identified_users", "sessions", "daily_site_metrics", "daily_site_visitors", "daily_site_sessions"} {
		if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) "+table); err != nil {
			t.Fatalf("settle %s: %v", table, err)
		}
	}

	from, today := f.siteDates(t, 30)
	long := f.siteDate(t, -60)
	site := "/api/v1/sites/" + f.siteKey
	// A behavioural segment is the shape that took a minute per query before
	// v0.24.1, so every segment-carrying probe below runs through one.
	segment := saveScaleSegment(t, f, `{"combinator":"and","rules":[{"field":"entity.frustration_signals","operator":">=","value":1}]}`)

	probes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "overview", path: site + "/overview?from=" + from + "&to=" + today},
		{name: "anomalies", path: site + "/anomalies?environment=prd"},
		{name: "metric-goals", path: site + "/metric-goals/evaluate"},
		{name: "visitor-insights", path: site + "/visitor-insights?from=" + from + "&to=" + today},
		{name: "usage", path: site + "/usage?from=" + from + "&to=" + today},
		{name: "frustration", path: site + "/frustration?from=" + from + "&to=" + today},
		{name: "search-analytics", path: site + "/search-analytics?from=" + from + "&to=" + today},
		{name: "attribution", path: site + "/attribution?from=" + from + "&to=" + today + "&model=last_non_direct"},
		{name: "experience+segment", path: site + "/experience?from=" + from + "&to=" + today + "&segment_ids=" + segment},
		{name: "cohort+segment", path: site + "/cohort?from=" + long + "&to=" + today + "&granularity=week&periods=6&segment_ids=" + segment},
		{name: "visitors", path: site + "/visitors?from=" + from + "&to=" + today},
		{name: "events", path: site + "/events?from=" + from + "&to=" + today},
		{
			name: "query-builder+segment", method: http.MethodPost, path: "/api/v1/query",
			body: fmt.Sprintf(`{"site_id":"%s","segment_id":"%s","date_range":{"from":"%s","to":"%s"},"dimensions":["device.type"],"metrics":["events","users"]}`,
				f.siteKey, segment, from, today),
		},
		{
			name: "funnel+segment", method: http.MethodPost, path: "/api/v1/funnel",
			body: fmt.Sprintf(`{"site_id":"%s","environment":"prd","from":"%s","to":"%s","mode":"closed","compare_segment_ids":["%s"],
				"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"},{"name":"구매","event":"purchase"}]}`,
				f.siteKey, from, today, segment),
		},
	}

	type timing struct {
		name string
		took time.Duration
	}
	timings := make([]timing, 0, len(probes))
	for _, p := range probes {
		method := p.method
		if method == "" {
			method = http.MethodGet
		}
		request := httptest.NewRequest(method, p.path, strings.NewReader(p.body))
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		if p.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		started := time.Now()
		f.server.Handler().ServeHTTP(recorder, request)
		took := time.Since(started)
		timings = append(timings, timing{p.name, took})
		if recorder.Code >= 400 {
			t.Errorf("%s answered %d: %s", p.name, recorder.Code, truncateBody(recorder.Body.String()))
			continue
		}
		if took > scaleBudget {
			t.Errorf("%s took %s for %d events, over the %s budget: a report that grows with the site rather than with the question does not survive a real one",
				p.name, took.Round(time.Millisecond), scaleEventCount, scaleBudget)
		}
	}
	// Retention runs unattended on a schedule, and it now anti-joins the identity
	// tables against every event and session. Those probes are indexed, but an
	// unindexed one would turn a nightly job into one that never finishes and
	// nobody would be watching when it did.
	retentionStarted := time.Now()
	if err := (service.Worker{DB: pool}).ApplyRetention(ctx); err != nil {
		t.Fatalf("retention at scale: %v", err)
	}
	retentionTook := time.Since(retentionStarted)
	t.Logf("applied retention over %d events in %s", scaleEventCount, retentionTook.Round(time.Millisecond))
	if retentionTook > scaleRetentionBudget {
		t.Errorf("retention took %s for %d events, over the %s budget: it runs unattended, so a scan here is a maintenance job that stops finishing",
			retentionTook.Round(time.Millisecond), scaleEventCount, scaleRetentionBudget)
	}

	// A report that runs its independent reads one after another costs the reader
	// their sum, which no latency budget on its own can distinguish from a site
	// that is simply larger. The usage report is the clearest case — six reads of
	// the same period, one per dimension — so it is compared against a single read
	// of that period rather than against a clock. Serial, it was six times the
	// event report; together, it is under one and a half. The ceiling is set well
	// above that so slow hardware cannot fail it, and well below six.
	took := func(name string) time.Duration {
		for _, item := range timings {
			if item.name == name {
				return item.took
			}
		}
		t.Fatalf("no timing recorded for %s", name)
		return 0
	}
	if single, all := took("events"), took("usage"); single > 0 && all > 4*single {
		t.Errorf("the usage report took %s against %s for one read of the same period: its six dimension reads are running one after another, and the reader waits for their sum",
			all.Round(time.Millisecond), single.Round(time.Millisecond))
	}

	sort.SliceStable(timings, func(i, j int) bool { return timings[i].took > timings[j].took })
	var report strings.Builder
	report.WriteString(fmt.Sprintf("\nlatency over %d events (budget %s)\n", scaleEventCount, scaleBudget))
	for _, item := range timings {
		report.WriteString(fmt.Sprintf("  %-24s %8s\n", item.name, item.took.Round(time.Millisecond)))
	}
	t.Log(report.String())
}

func saveScaleSegment(t *testing.T, f fixture, definition string) string {
	t.Helper()
	body := fmt.Sprintf(`{"site_id":%s,"name":%s,"definition":%s,"shared":false}`,
		strconv.Quote(f.siteKey), strconv.Quote("규모 검사 · 막힘"), definition)
	saved := f.do(t, http.MethodPost, "/api/v1/segments", body)
	id := fmt.Sprint(saved["id"])
	if id == "" || id == "<nil>" {
		t.Fatalf("could not save the scale segment: %v", saved)
	}
	return id
}

// TestRebuildRefreshesStatisticsForTheTablesItFills guards the other half of the
// v0.25.0 fix, which a latency budget cannot see at any size CI can afford.
//
// The rebuild empties a table, fills it with a large number of rows, and the
// next step joins it. If the planner's statistics still describe the table as it
// was, that join is planned as though it were tiny — which is how a join of two
// small tables became a five minute nested loop on a real site. Removing the
// refreshes would restore that behaviour silently, so their effect is asserted
// rather than their presence in the source.
func TestRebuildRefreshesStatisticsForTheTablesItFills(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	seedScale(t, f)
	ctx := context.Background()

	// Statistics start out describing an empty site: the fixture's own rows were
	// inserted without an analyse, and the seeded events land afterwards.
	for _, table := range []string{"visitors", "visitor_identities", "identified_users", "sessions"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE site_id=$1", f.siteID); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	before := map[string]int64{}
	for _, table := range []string{"visitors", "visitor_identities"} {
		before[table] = analyzeCount(t, pool, table)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := service.RebuildSiteDerivedData(ctx, tx, f.siteID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("rebuild: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, table := range []string{"visitors", "visitor_identities"} {
		if analyzeCount(t, pool, table) <= before[table] {
			t.Errorf("the rebuild filled %s and the next step joins it, but its statistics were never refreshed: the join will be planned against the table as it was", table)
		}
	}
}

// analyzeCount reports how many times a table has been analysed. The counter is
// what the planner's statistics being refreshed looks like from outside.
func analyzeCount(t *testing.T, pool *pgxpool.Pool, table string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT coalesce(analyze_count,0)+coalesce(autoanalyze_count,0) FROM pg_stat_user_tables WHERE relname=$1`, table).Scan(&count); err != nil {
		t.Fatalf("read analyze count for %s: %v", table, err)
	}
	return count
}
