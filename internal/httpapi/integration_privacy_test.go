package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hkjang/Momento/internal/service"
)

// Deletion is a compliance promise, so it has to be checked rather than trusted:
// an under-delete that still answers 200 tells an operator the data is gone when it
// is not. These tests count what survives in every table that holds a person.

// personRows counts the rows that still mention a person across the derived tables.
func (f fixture) personRows(t *testing.T, userID string, visitors ...string) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	counts := map[string]int64{}
	count := func(label, query string, args ...any) {
		t.Helper()
		var value int64
		if err := f.server.DB.QueryRow(ctx, query, args...).Scan(&value); err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		counts[label] = value
	}
	count("raw_events", `SELECT count(*) FROM raw_events WHERE site_id=$1 AND (user_id=$2 OR visitor_id=ANY($3))`, f.siteID, userID, visitors)
	count("sessions", `SELECT count(*) FROM sessions WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	count("visitors", `SELECT count(*) FROM visitors WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	count("visitor_sessions", `SELECT count(*) FROM visitor_sessions WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	count("visitor_identities", `SELECT count(*) FROM visitor_identities WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	count("identified_users", `SELECT count(*) FROM identified_users WHERE site_id=$1 AND user_id=$2`, f.siteID, userID)
	count("daily_site_visitors", `SELECT count(*) FROM daily_site_visitors WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	count("daily_site_sessions", `SELECT count(*) FROM daily_site_sessions WHERE site_id=$1 AND visitor_id=ANY($2)`, f.siteID, visitors)
	return counts
}

func TestPrivacyDeletionRemovesEveryTraceOfAPerson(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)

	linked := []string{"visitor-desktop", "visitor-mobile"}
	before := f.personRows(t, f.userID, linked...)
	if before["raw_events"] == 0 || before["sessions"] == 0 || before["visitors"] == 0 {
		t.Fatalf("the fixture has nothing to delete: %v", before)
	}

	response := f.do(t, http.MethodPost, "/api/v1/privacy/delete",
		fmt.Sprintf(`{"site_id":"%s","mode":"user_id","value":"%s","confirm":"DELETE"}`, f.siteKey, f.userID))
	if deleted, _ := response["deleted_or_updated"].(float64); deleted <= 0 {
		t.Fatalf("deletion reported %v rows: %v", deleted, response)
	}

	after := f.personRows(t, f.userID, linked...)
	for table, remaining := range after {
		if remaining != 0 {
			t.Fatalf("%d rows for the deleted person survive in %s (before %d)", remaining, table, before[table])
		}
	}

	// The anonymous visitor is a different person and must be untouched.
	var anonymous int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_events WHERE site_id=$1 AND visitor_id='visitor-anon'`, f.siteID).Scan(&anonymous); err != nil {
		t.Fatalf("count anonymous: %v", err)
	}
	if anonymous == 0 {
		t.Fatal("deleting one person removed another person's events")
	}
}

func TestPrivacyDeletionByVisitorAndPeriod(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	f.do(t, http.MethodPost, "/api/v1/privacy/delete",
		fmt.Sprintf(`{"site_id":"%s","mode":"visitor","value":"visitor-anon","confirm":"DELETE"}`, f.siteKey))
	var remaining int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND visitor_id='visitor-anon'`, f.siteID).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d events survive for the deleted visitor", remaining)
	}

	// A period deletion must remove inside the window and keep everything outside it.
	from := f.siteDate(t, -10)
	to := f.siteDate(t, -5)
	f.do(t, http.MethodPost, "/api/v1/privacy/delete",
		fmt.Sprintf(`{"site_id":"%s","mode":"period","from":"%s","to":"%s","confirm":"DELETE"}`, f.siteKey, from, to))
	var inside, outside int64
	if err := pool.QueryRow(ctx, `SELECT
			count(*) FILTER(WHERE event_timestamp >= (($2::date)::timestamp AT TIME ZONE 'Asia/Seoul') AND event_timestamp < ((($3::date)+1)::timestamp AT TIME ZONE 'Asia/Seoul')),
			count(*) FILTER(WHERE event_timestamp < (($2::date)::timestamp AT TIME ZONE 'Asia/Seoul'))
		FROM raw_events WHERE site_id=$1`, f.siteID, from, to).Scan(&inside, &outside); err != nil {
		t.Fatalf("count period: %v", err)
	}
	if inside != 0 {
		rows, _ := pool.Query(ctx, `SELECT event_timestamp,visitor_id,event_name FROM raw_events WHERE site_id=$1
			AND event_timestamp >= (($2::date)::timestamp AT TIME ZONE 'Asia/Seoul') AND event_timestamp < ((($3::date)+1)::timestamp AT TIME ZONE 'Asia/Seoul') LIMIT 5`, f.siteID, from, to)
		for rows.Next() {
			var at time.Time
			var visitor, event string
			_ = rows.Scan(&at, &visitor, &event)
			t.Logf("survivor %s %s %s", at.Format(time.RFC3339), visitor, event)
		}
		rows.Close()
		t.Fatalf("%d events survive inside the deleted window [%s..%s]", inside, from, to)
	}
	if outside == 0 {
		t.Fatal("a period deletion removed events outside the window")
	}

	// Property deletion strips one key and keeps the rest of the event.
	f.do(t, http.MethodPost, "/api/v1/privacy/delete",
		fmt.Sprintf(`{"site_id":"%s","mode":"property","value":"feature","confirm":"DELETE"}`, f.siteKey))
	var withFeature int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND properties ? 'feature'`, f.siteID).Scan(&withFeature); err != nil {
		t.Fatalf("count property: %v", err)
	}
	if withFeature != 0 {
		t.Fatalf("%d events still carry the deleted property", withFeature)
	}
	var events int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1`, f.siteID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events == 0 {
		t.Fatal("property deletion removed the events instead of the property")
	}
}

func TestPrivacyRequestWorkflow(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	site := "/api/v1/sites/" + f.siteKey

	created := f.do(t, http.MethodPost, site+"/privacy-requests",
		fmt.Sprintf(`{"request_type":"export","identity_type":"user_id","identity_value":"%s","reason":"본인 요청"}`, f.userID))
	requestID, _ := created["id"].(string)
	if requestID == "" {
		t.Fatalf("request creation returned no id: %v", created)
	}

	// The download is only offered after the request has been approved and completed,
	// which is the point of the approval workflow.
	early := httptest.NewRequest(http.MethodGet, site+"/privacy-requests/"+requestID+"/export", nil)
	early.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	earlyRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(earlyRecorder, early)
	if earlyRecorder.Code == http.StatusOK {
		t.Fatal("a pending export request was downloadable before approval")
	}
	f.do(t, http.MethodPost, site+"/privacy-requests/"+requestID+"/decision", `{"decision":"approve"}`)

	request := httptest.NewRequest(http.MethodGet, site+"/privacy-requests/"+requestID+"/export", nil)
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
		t.Fatalf("export = %d body=%d: %s", recorder.Code, recorder.Body.Len(), recorder.Body.String())
	}

	// A delete request needs an explicit decision before anything is removed.
	deleteRequest := f.do(t, http.MethodPost, site+"/privacy-requests",
		fmt.Sprintf(`{"request_type":"delete","identity_type":"visitor_id","identity_value":"visitor-anon","reason":"삭제 요청"}`))
	deleteID, _ := deleteRequest["id"].(string)
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM privacy_requests WHERE id=$1`, deleteID).Scan(&status); err != nil {
		t.Fatalf("request status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("a new delete request is %q, want pending", status)
	}
	var stillThere int64
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_events WHERE site_id=$1 AND visitor_id='visitor-anon'`, f.siteID).Scan(&stillThere)
	if stillThere == 0 {
		t.Fatal("a pending request already deleted the data")
	}

	f.do(t, http.MethodPost, site+"/privacy-requests/"+deleteID+"/decision", `{"decision":"approve"}`)
	if err := pool.QueryRow(context.Background(), `SELECT status FROM privacy_requests WHERE id=$1`, deleteID).Scan(&status); err != nil {
		t.Fatalf("decided status: %v", err)
	}
	if status != "completed" && status != "approved" && status != "running" {
		t.Fatalf("approved request is %q", status)
	}
}

func TestRetentionAndAggregateMaintenance(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// Keep only the last week of raw events for this site.
	if _, err := pool.Exec(ctx, `INSERT INTO retention_policies(site_id,raw_event_months,session_months,realtime_hours,debug_days)
		VALUES($1,1,1,1,1) ON CONFLICT(site_id) DO UPDATE SET raw_event_months=1,session_months=1`, f.siteID); err != nil {
		t.Fatalf("retention policy: %v", err)
	}
	var oldEvents int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_timestamp < now()-interval '40 days'`, f.siteID).Scan(&oldEvents); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if oldEvents == 0 {
		t.Fatal("the fixture has no events old enough to expire")
	}
	if err := (service.Worker{DB: pool}).ApplyRetention(ctx); err != nil {
		t.Fatalf("retention: %v", err)
	}
	var survivors int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_timestamp < now()-interval '40 days'`, f.siteID).Scan(&survivors); err != nil {
		t.Fatalf("count survivors: %v", err)
	}
	if survivors != 0 {
		t.Fatalf("%d events older than the one month policy survived retention", survivors)
	}
	var recent int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_timestamp > now()-interval '7 days'`, f.siteID).Scan(&recent); err != nil {
		t.Fatalf("count recent: %v", err)
	}
	if recent == 0 {
		t.Fatal("retention removed events inside the policy window")
	}

	// The aggregate manager rebuilds the daily rollups on request.
	site := "/api/v1/sites/" + f.siteKey
	f.do(t, http.MethodPost, site+"/aggregate-jobs", `{"job_type":"full_rebuild","environment":"prd"}`)
	maintenance := service.Maintenance{DB: pool}
	for attempt := 0; attempt < 5; attempt++ {
		ran, err := maintenance.RunPending(ctx)
		if err != nil {
			t.Fatalf("aggregate maintenance: %v", err)
		}
		if !ran {
			break
		}
	}
	var pending int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aggregate_jobs WHERE site_id=$1 AND status IN ('pending','running')`, f.siteID).Scan(&pending); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if pending != 0 {
		t.Fatalf("%d aggregate jobs are still unfinished", pending)
	}
	var failed int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aggregate_jobs WHERE site_id=$1 AND status='failed'`, f.siteID).Scan(&failed); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if failed != 0 {
		var reason *string
		_ = pool.QueryRow(ctx, `SELECT error FROM aggregate_jobs WHERE site_id=$1 AND status='failed' LIMIT 1`, f.siteID).Scan(&reason)
		t.Fatalf("%d aggregate jobs failed: %v", failed, reason)
	}
}

// TestAggregateRetentionAppliesTheConfiguredLimit covers a setting that was
// accepted and stored and then read by nothing. The retention screen offers a
// limit for the daily rollups, and the sweep deleted raw events, sessions and
// debug rows while keeping every aggregate forever — so a site that asked to
// keep two years of rollups kept all of them.
func TestAggregateRetentionAppliesTheConfiguredLimit(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// Two rollup days: one inside a one month limit, one well outside it.
	recent := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	ancient := time.Now().AddDate(0, -8, 0).Format("2006-01-02")
	for _, day := range []string{recent, ancient} {
		if _, err := pool.Exec(ctx, `INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
			VALUES($1,$2::date,'prd',10,5,1,0) ON CONFLICT(site_id,event_date,environment) DO UPDATE SET events=10`, f.siteID, day); err != nil {
			t.Fatalf("seed rollup %s: %v", day, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,first_seen,last_seen,event_count)
			VALUES($1,$2::date,'prd','retention-visitor',now(),now(),3) ON CONFLICT DO NOTHING`, f.siteID, day); err != nil {
			t.Fatalf("seed visitor rollup %s: %v", day, err)
		}
	}

	countDay := func(day string) int64 {
		t.Helper()
		var count int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_site_metrics WHERE site_id=$1 AND event_date=$2::date`, f.siteID, day).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", day, err)
		}
		return count
	}

	// With no aggregation limit set, nothing is removed: a site that has not asked
	// for a limit keeps its history.
	if _, err := pool.Exec(ctx, `INSERT INTO retention_policies(site_id,raw_event_months,session_months,aggregation_months,realtime_hours,debug_days)
		VALUES($1,13,25,NULL,24,7) ON CONFLICT(site_id) DO UPDATE SET aggregation_months=NULL`, f.siteID); err != nil {
		t.Fatalf("clear the aggregation limit: %v", err)
	}
	if err := (service.Worker{DB: pool}).ApplyRetention(ctx); err != nil {
		t.Fatalf("retention with no limit: %v", err)
	}
	if countDay(ancient) == 0 {
		t.Fatal("an aggregate was deleted although the site set no aggregation limit")
	}

	// With a one month limit the old rollup goes and the recent one stays.
	if _, err := pool.Exec(ctx, `UPDATE retention_policies SET aggregation_months=1 WHERE site_id=$1`, f.siteID); err != nil {
		t.Fatalf("set the aggregation limit: %v", err)
	}
	if err := (service.Worker{DB: pool}).ApplyRetention(ctx); err != nil {
		t.Fatalf("retention with a limit: %v", err)
	}
	if countDay(ancient) != 0 {
		t.Errorf("the rollup from eight months ago survived a one month aggregation limit")
	}
	if countDay(recent) == 0 {
		t.Errorf("the rollup from three days ago was deleted under a one month limit")
	}
	var visitorRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_site_visitors WHERE site_id=$1 AND event_date=$2::date`, f.siteID, ancient).Scan(&visitorRows); err != nil {
		t.Fatalf("count visitor rollups: %v", err)
	}
	if visitorRows != 0 {
		t.Errorf("the per-visitor rollup from eight months ago survived: the limit has to reach every daily table")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE retention_policies SET aggregation_months=NULL WHERE site_id=$1`, f.siteID)
	})
}

// Retention removed a person's events and sessions and left their identity behind:
// the visitor_id -> user_id mapping and the per-visitor aggregate had no policy and
// no expiry, so the identities screen went on naming them with an event count taken
// from the aggregate. An operator who set a window to satisfy a retention
// obligation had not met it, and the console said the opposite.
//
// This checks both directions, because a prune that is too eager is the worse
// failure: identities must disappear when nothing they describe is left, and must
// survive as long as anything is.
func TestRetentionExpiresIdentitiesWithTheEventsTheyDescribe(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	site := "/api/v1/sites/" + f.siteKey
	worker := service.Worker{DB: pool}

	count := func(label, query string) int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, query, f.siteID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		return n
	}
	identities := func() int64 {
		return count("visitor_identities", `SELECT count(*) FROM visitor_identities WHERE site_id=$1`)
	}
	users := func() int64 {
		return count("identified_users", `SELECT count(*) FROM identified_users WHERE site_id=$1`)
	}
	visitors := func() int64 { return count("visitors", `SELECT count(*) FROM visitors WHERE site_id=$1`) }
	// The screen an operator would look at to check whether the deletion happened.
	reported := func() int {
		t.Helper()
		body := f.do(t, http.MethodGet, site+"/identities?days=3650", "")
		list, _ := body["list"].([]any)
		return len(list)
	}

	if identities() == 0 || visitors() == 0 || reported() == 0 {
		t.Fatal("the fixture has no identified people, so this proves nothing")
	}

	// A one month window with the fixture untouched: the recent events stay, so
	// every identity is still describing something and none may be removed.
	if _, err := pool.Exec(ctx, `INSERT INTO retention_policies(site_id,raw_event_months,session_months,realtime_hours,debug_days)
		VALUES($1,1,1,1,1) ON CONFLICT(site_id) DO UPDATE SET raw_event_months=1,session_months=1`, f.siteID); err != nil {
		t.Fatalf("retention policy: %v", err)
	}
	beforeIdentities, beforeUsers, beforeVisitors, beforeReported := identities(), users(), visitors(), reported()
	if err := worker.ApplyRetention(ctx); err != nil {
		t.Fatalf("retention: %v", err)
	}
	if count("raw_events", `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_timestamp < now()-interval '40 days'`) != 0 {
		t.Fatal("events outside the window survived, so the rest of this test is measuring the wrong state")
	}
	if got := identities(); got != beforeIdentities {
		t.Fatalf("partial expiry removed identities that still have data: %d -> %d", beforeIdentities, got)
	}
	if got := users(); got != beforeUsers {
		t.Fatalf("partial expiry removed identified users that still have data: %d -> %d", beforeUsers, got)
	}
	if got := visitors(); got != beforeVisitors {
		t.Fatalf("partial expiry removed visitor aggregates that still have data: %d -> %d", beforeVisitors, got)
	}
	if got := reported(); got != beforeReported {
		t.Fatalf("partial expiry changed the identities screen: %d -> %d rows", beforeReported, got)
	}

	// Now age everything past the window: nothing about these people is left to
	// describe, so nothing about them may remain.
	if _, err := pool.Exec(ctx, `UPDATE raw_events SET event_timestamp=event_timestamp-interval '400 days' WHERE site_id=$1`, f.siteID); err != nil {
		t.Fatalf("age events: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET started_at=started_at-interval '400 days',last_event_at=last_event_at-interval '400 days' WHERE site_id=$1`, f.siteID); err != nil {
		t.Fatalf("age sessions: %v", err)
	}
	if err := worker.ApplyRetention(ctx); err != nil {
		t.Fatalf("retention after full expiry: %v", err)
	}
	if got := count("raw_events", `SELECT count(*) FROM raw_events WHERE site_id=$1`); got != 0 {
		t.Fatalf("%d events survived full expiry", got)
	}
	if got := identities(); got != 0 {
		t.Fatalf("%d visitor_id to user_id mappings outlived every event and session they describe", got)
	}
	if got := users(); got != 0 {
		t.Fatalf("%d identified users outlived every event and session they describe", got)
	}
	if got := visitors(); got != 0 {
		t.Fatalf("%d per-visitor aggregates outlived every event and session they describe", got)
	}
	// The count the screen showed came from the per-visitor aggregate, which is why
	// it kept reporting activity for events that had been deleted.
	if got := reported(); got != 0 {
		t.Fatalf("the identities screen still names %d people whose events were all deleted", got)
	}
}
