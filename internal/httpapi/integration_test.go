package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/database"
	"github.com/hkjang/Momento/internal/secret"
	"github.com/hkjang/Momento/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests run the analytical queries against a real PostgreSQL instance. Every
// report added since v0.13 builds SQL by hand, and a mistake there only shows up at
// runtime, so the suite exercises each endpoint end to end and fails on the first
// query error rather than on a wrong number.
//
// Set MOMENTO_TEST_POSTGRES_DSN to run them; without it they skip.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MOMENTO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOMENTO_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

type fixture struct {
	server      *Server
	siteKey     string
	siteID      uuid.UUID
	otherKey    string
	sessionCook string
	trackingKey string
	serverKey   string
	visitorID   string
	userID      string
	segmentID   string
}

// seed builds a small but realistic site: two services in one workspace, an
// anonymous visitor, ten weeks of daily activity, sessions, conversions, web
// vitals and errors.
//
// The two services are not symmetrical, and tests are written against that.
// SITE_MAIN carries the events. SITE_HR carries the identity link for the same
// SSO person and a touch session per day — which is what workspace-scope
// attribution reads, since it credits from sessions — and no events at all.
//
// That is deliberate and load-bearing in both directions. Several tests use the
// second site as a blank canvas, where what they deliver is the whole of what
// the report can see; integration_trend_test.go says so in as many words and its
// comparison of a total against a chart is only meaningful because of it. And a
// test that needs one person on two services in the event tables has to build
// that itself, as integration_rollup_identity_test.go does.
//
// This comment used to say "an SSO user active on both", which read as events on
// both and is how the cross-service reports came to be checked against a
// workspace where nobody was on two services. TestTheFixtureIsWhatItSaysItIs
// holds the description and the data together.
func seed(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %.60s: %v", sql, err)
		}
	}
	// Remove only what this fixture creates. Truncating would also wipe the settings
	// rows the migrations seed, which the privacy and security paths depend on.
	run(`DELETE FROM users WHERE email='admin@test.local'`)
	run(`DELETE FROM organizations WHERE slug='test'`)

	var orgID, workspaceID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Test','test') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id,name) VALUES($1,'Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	const trackingKey = "mom_track_integration"
	// A real hash, so the server-to-server ingestion path can actually be
	// exercised: the column used to hold a literal string, which no request could
	// ever present.
	const serverKey = "mom_server_integration"
	newSite := func(key, name string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
			VALUES($1,$2,$3,$3,$4,'mom_track_x',$5,'mom_server_x',ARRAY['portal.internal'],'Asia/Seoul') RETURNING id`, workspaceID, key, name, auth.HashToken(trackingKey), auth.HashToken(serverKey)).Scan(&id); err != nil {
			t.Fatalf("site: %v", err)
		}
		run(`INSERT INTO site_environments(site_id,name,label) VALUES($1,'prd','Production') ON CONFLICT DO NOTHING`, id)
		return id
	}
	siteID := newSite("SITE_MAIN", "포털")
	otherID := newSite("SITE_HR", "인사")

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
		VALUES('admin@test.local','Admin',$1,'super_admin','Test') RETURNING id`, "hash").Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	token := "mom_sess_integration"
	run(`INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, userID, auth.HashToken(token))

	const person = "EMP001"
	desktop, mobile, anonymous := "visitor-desktop", "visitor-mobile", "visitor-anon"
	for _, visitor := range []string{desktop, mobile} {
		run(`INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
			VALUES($1,$2,$3,now()-interval '70 days',now()-interval '70 days',now())`, siteID, visitor, person)
	}
	run(`INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
		VALUES($1,'hr-visitor',$2,now()-interval '40 days',now()-interval '40 days',now())`, otherID, person)
	for _, site := range []uuid.UUID{siteID, otherID} {
		run(`INSERT INTO identified_users(site_id,user_id,first_seen,last_seen,user_properties)
			VALUES($1,$2,now()-interval '70 days',now(),'{"department":"디지털플랫폼","organization":"기술"}')`, site, person)
	}
	for _, visitor := range []string{desktop, mobile, anonymous} {
		run(`INSERT INTO visitors(site_id,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count)
			VALUES($1,$2,$3,now()-interval '70 days',now(),50,5)`, siteID, visitor,
			map[bool]any{true: person, false: nil}[visitor != anonymous])
	}

	// Ten weeks of daily activity so the anomaly baseline has same weekday history.
	for day := 70; day >= 1; day-- {
		for index, visitor := range []string{desktop, mobile, anonymous} {
			session := fmt.Sprintf("s-%s-%d", visitor, day)
			// Anchored to a site-local calendar date rather than to now() minus N
			// days: with an offset added on top, two different N could land on the
			// same Asia/Seoul date depending on the hour the test ran, which cost the
			// daily series a day and made the anomaly baseline short after 15:00 UTC.
			at := fmt.Sprintf("((((now() AT TIME ZONE 'Asia/Seoul')::date - %d) + time '%02d:00') AT TIME ZONE 'Asia/Seoul')", day, 9+index)
			owner := any(person)
			if visitor == anonymous {
				owner = nil
			}
			run(fmt.Sprintf(`INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type,environment)
				VALUES($1,$2,$3,$4,%s,%s+interval '12 minutes',4,2,1,true,'https://portal.internal/home','https://portal.internal/done',$5,$6,'',$7,'prd')`, at, at),
				siteID, session, visitor, owner,
				[]string{"intranet", "naver", ""}[index], []string{"portal", "organic", ""}[index], []string{"desktop", "mobile", "desktop"}[index])
			run(`INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen) VALUES($1,$2,$3,$4,now()-interval '1 day',now())
				ON CONFLICT DO NOTHING`, siteID, visitor, session, owner)
			if visitor != mobile {
				// Extra fast samples keep mobile under a quarter of the measurements,
				// so the pooled p75 stays fast while the cohort is slow.
				for extra := 0; extra < 3; extra++ {
					run(fmt.Sprintf(`INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,received_at,visitor_id,session_id,user_id,page_url,source,medium,device_type,properties,is_conversion,environment,contract_version)
						VALUES(gen_random_uuid(),$1,'web_vital',%s+interval '%d minutes',now(),$2,$3,$4,'https://portal.internal/home','','',$5,$6,false,'prd',1)`, at, 20+extra),
						siteID, visitor, session, owner, []string{"desktop", "mobile", "desktop"}[index],
						fmt.Sprintf(`{"metric":"LCP","value":%d,"rating":"good"}`, []int{1200, 5200, 1300}[index]))
				}
			}
			for eventIndex, event := range []string{"page_view", "feature_used", "purchase", "web_vital", "error"} {
				conversion := event == "purchase"
				properties := `{}`
				switch event {
				case "web_vital":
					// Mobile is deliberately the slow minority: a cohort that is both
					// slower and small is exactly what a site wide p75 hides.
					properties = fmt.Sprintf(`{"metric":"LCP","value":%d,"rating":"good"}`, []int{1200, 5200, 1300}[index])
				case "error":
					properties = `{"message":"boom"}`
				case "feature_used":
					properties = `{"feature":"document_search"}`
				case "purchase":
					// The product table reads properties.items, so a purchase without one
					// leaves that whole report empty. Price times quantity equals the
					// purchase value, which is what a real payload looks like.
					properties = `{"value":"1000","items":[{"item_id":"sku-1","item_name":"연차 신청서","category":"근태","brand":"사내","quantity":"2","price":"500"}]}`
				}
				run(fmt.Sprintf(`INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,received_at,visitor_id,session_id,user_id,page_url,page_title,referrer,source,medium,device_type,browser,os,network_name,properties,user_properties,is_conversion,environment,contract_version)
					VALUES(gen_random_uuid(),$1,$2,%s+interval '%d minutes',now(),$3,$4,$5,'https://portal.internal/home','홈','',$6,$7,$8,'Chrome','Windows','본사',$9,'{"department":"디지털플랫폼"}',$10,'prd',1)`, at, eventIndex),
					siteID, event, visitor, session, owner,
					[]string{"intranet", "naver", ""}[index], []string{"portal", "organic", ""}[index],
					[]string{"desktop", "mobile", "desktop"}[index], properties, conversion)
			}
		}
		// The other service contributes touch sessions for cross-service credit.
		run(fmt.Sprintf(`INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,source,medium,device_type,environment)
			VALUES($1,$2,'hr-visitor',$3,now()-interval '%d days',now()-interval '%d days'+interval '5 minutes',2,1,0,true,'notice','notice','desktop','prd')`, day, day),
			otherID, fmt.Sprintf("hr-%d", day), person)
	}

	// first_seen and last_seen used to be written as now() on every one of these
	// rows, which said every visitor was first seen today on a day seventy days
	// ago. Nothing read them, so nothing objected; the reports that decide who is
	// new read them now, and a fixture that contradicts its own events cannot show
	// whether those reports are right. They are anchored to the same site-local
	// hour the events for that day are.
	run(`INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,first_seen,last_seen,event_count,conversion_count)
		SELECT $1,(now()-make_interval(days=>d))::date,'prd',v,
			((((now() AT TIME ZONE 'Asia/Seoul')::date - d) + time '09:00') AT TIME ZONE 'Asia/Seoul'),
			((((now() AT TIME ZONE 'Asia/Seoul')::date - d) + time '21:00') AT TIME ZONE 'Asia/Seoul'),
			20,1 FROM generate_series(1,70) d, unnest(ARRAY['visitor-desktop','visitor-mobile','visitor-anon']) v`, siteID)
	run(`INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,user_id,first_seen,last_seen)
		SELECT $1,(now()-make_interval(days=>d))::date,'prd','ds-'||d||'-'||v,v,NULL,
			((((now() AT TIME ZONE 'Asia/Seoul')::date - d) + time '09:00') AT TIME ZONE 'Asia/Seoul'),
			((((now() AT TIME ZONE 'Asia/Seoul')::date - d) + time '21:00') AT TIME ZONE 'Asia/Seoul')
			FROM generate_series(1,70) d, unnest(ARRAY['visitor-desktop','visitor-mobile']) v`, siteID)

	// Events the reports read that nothing was creating, so those paths ran and
	// returned zero and every test passed. Each is seeded with a shape the
	// corresponding report's assertions can be written against.
	//
	//   refund          the ecommerce report's refund and net revenue
	//   user_engagement the engaged-time path, including its numeric guard
	//   resource_error  the experience report reads it alongside error
	//   ai_*            the whole AI operations report
	//   search_*        the search report, and the audiences it offers
	//   friction        the frustration report and its impact comparison
	for day := 3; day >= 1; day-- {
		// Inside the desktop visitor's existing session for that day, not a session
		// of their own: the collector never writes an event without a session row,
		// and inventing one here made the event-derived and table-derived session
		// counts disagree for a reason no deployment would produce.
		at := fmt.Sprintf("((((now() AT TIME ZONE 'Asia/Seoul')::date - %d) + time '09:00') AT TIME ZONE 'Asia/Seoul')", day)
		session := fmt.Sprintf("s-%s-%d", desktop, day)
		event := func(name, properties string, conversion bool, minute int) {
			run(fmt.Sprintf(`INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,received_at,visitor_id,session_id,user_id,page_url,source,medium,device_type,browser,os,properties,is_conversion,environment,contract_version)
				VALUES(gen_random_uuid(),$1,$2,%s+interval '%d minutes',now(),$3,$4,$5,'https://portal.internal/shop','intranet','portal','desktop','Chrome','Windows',$6,$7,'prd',1)`, at, minute),
				siteID, name, desktop, session, person, properties, conversion)
		}
		// A refund of 400 against the 1000 purchases the loop above records.
		// The ecommerce funnel reads four steps and only the last one existed, so the
		// funnel and the cart and checkout counts were never exercised. Each step
		// loses a person, which is what a funnel is for.
		event("view_item", `{"item_id":"sku-1","item_name":"연차 신청서"}`, false, 2)
		event("add_to_cart", `{"item_id":"sku-1","item_name":"연차 신청서"}`, false, 3)
		event("begin_checkout", `{"item_id":"sku-1","item_name":"연차 신청서"}`, false, 4)
		event("refund", `{"value":"400","transaction_id":"tx-refund"}`, false, 6)
		// Engaged time in the shape the metric parses, plus one unparseable value
		// that has to be ignored rather than counted or crashed on.
		event("user_engagement", `{"active_seconds":"15"}`, false, 7)
		event("user_engagement", `{"active_seconds":"not-a-number"}`, false, 8)
		event("resource_error", `{"resource":"https://portal.internal/app.js","resource_type":"script"}`, false, 9)
		event("ai_model_call", `{"model":"claude","provider":"anthropic","success":"true","latency_ms":"820","input_tokens":"1200","output_tokens":"300","cost":"0.02"}`, false, 10)
		event("ai_model_call", `{"model":"claude","provider":"anthropic","success":"false","latency_ms":"1400","input_tokens":"800","output_tokens":"0","cost":"0.01"}`, false, 11)
		// Three searches, one of which finds nothing, and one click on a result.
		// Every rate the search report states is a different fraction of these, so
		// a report that mixed two of them up cannot produce the same numbers.
		event("search", `{"query":"연차","result_count":"7","query_words":1}`, false, 12)
		event("search", `{"query":"출장 정산","result_count":"3","query_words":2}`, false, 13)
		event("search", `{"query":"없는말","result_count":"0","query_words":1}`, false, 14)
		event("search_click", `{"query":"연차","position":"1"}`, false, 15)
		// Friction on somebody who converts. The impact report compares the people
		// who hit a signal against the people who did not, so it needs both, and
		// the anonymous visitor below is the other side.
		event("rage_click", `{"element_text":"제출"}`, false, 16)
		event("dead_click", `{"element_text":"도움말"}`, false, 17)

		// The anonymous visitor hits friction and never converts, in their own
		// session for the day — the same rule as above: an event belongs to a
		// session that exists.
		anonymousSession := fmt.Sprintf("s-%s-%d", anonymous, day)
		anonymousEvent := func(name, properties string, minute int) {
			run(fmt.Sprintf(`INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,received_at,visitor_id,session_id,page_url,source,medium,device_type,browser,os,properties,is_conversion,environment,contract_version)
				VALUES(gen_random_uuid(),$1,$2,%s+interval '%d minutes',now(),$3,$4,'https://portal.internal/help','','','desktop','Chrome','Windows',$5,false,'prd',1)`, at, minute),
				siteID, name, anonymous, anonymousSession, properties)
		}
		anonymousEvent("rage_click", `{"element_text":"신청"}`, 16)
		anonymousEvent("form_retry", `{"form_id":"leave"}`, 17)
		anonymousEvent("search", `{"query":"환불","result_count":"0","query_words":1}`, 18)
	}

	// Daily rollups feed the anomaly baseline, and the landing screen draws its
	// chart from them while counting the total above it from the events. They used
	// to be written as a flat 60/12/3 a day regardless of what the events said, so
	// the two disagreed on this site by construction and no test could compare
	// them here. Derived from the events instead: the fixture inserts the same
	// activity every day, so the baseline is just as flat and now describes
	// something that happened.
	//
	// It runs after the last event is inserted, not next to the other rollups: a
	// rollup derived halfway through the seeding describes half of it, and the
	// difference is exactly the sort of quiet disagreement this is here to remove.
	run(`INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
		SELECT site_id,(event_timestamp AT TIME ZONE 'Asia/Seoul')::date,environment,
			count(*),count(*) FILTER(WHERE event_name='page_view'),count(*) FILTER(WHERE is_conversion),
			coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value','') ~ '^[0-9]+(\.[0-9]+)?$'
				THEN (properties->>'value')::numeric ELSE 0 END),0)
		FROM raw_events WHERE site_id=$1 GROUP BY 1,2,3`, siteID)

	var segmentID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO segments(site_id,name,description,definition,shared,owner_id)
		VALUES($1,'모바일','', $2,true,$3) RETURNING id`, siteID,
		`{"combinator":"and","rules":[{"field":"device.type","operator":"=","value":"mobile"}]}`, userID).Scan(&segmentID); err != nil {
		t.Fatalf("segment: %v", err)
	}
	// A behavioural segment exercises the entity aggregate compiler.
	if _, err := pool.Exec(ctx, `INSERT INTO segments(site_id,name,description,definition,shared,owner_id)
		VALUES($1,'반복 미전환','',$2,true,$3)`, siteID,
		`{"combinator":"and","rules":[{"field":"entity.sessions","operator":">=","value":3},{"field":"entity.conversions","operator":"=","value":0}]}`, userID); err != nil {
		t.Fatalf("behavioural segment: %v", err)
	}
	run(`INSERT INTO semantic_metrics(site_id,name,label,description,definition,format,status)
		VALUES($1,'active_users','Active Users','','{"type":"unique_users"}','number','active') ON CONFLICT DO NOTHING`, siteID)
	run(`INSERT INTO metric_goals(site_id,name,metric_name,target_value,comparator,period,environment)
		VALUES($1,'월간 활성 사용자','active_users',1000,'gte','month','prd')`, siteID)

	cipher, _ := secret.New("integration-test-encryption-key")
	server := New(pool, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), cipher)
	return fixture{
		server: server, siteKey: "SITE_MAIN", siteID: siteID, otherKey: "SITE_HR",
		sessionCook: token, trackingKey: trackingKey, serverKey: serverKey, visitorID: desktop, userID: person, segmentID: segmentID.String(),
	}
}

// get issues an authenticated request through the real router.
func (f fixture) get(t *testing.T, path string) map[string]any {
	t.Helper()
	return f.do(t, http.MethodGet, path, "")
}

func (f fixture) do(t *testing.T, method, path, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK && recorder.Code != http.StatusCreated && recorder.Code != http.StatusAccepted {
		t.Fatalf("%s %s = %d: %s", method, path, recorder.Code, recorder.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		// Some endpoints answer with a list, which is still a successful response.
		var list []any
		if json.Unmarshal(recorder.Body.Bytes(), &list) == nil {
			return map[string]any{"list": list}
		}
		t.Fatalf("%s %s: decode %v (%s)", method, path, err, recorder.Body.String())
	}
	return decoded
}

func TestAnalyticalEndpointsRunAgainstPostgres(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	// The realtime screen was reached only by the opt-in load harness, so nothing
	// CI runs ever called it — and it is the one screen that reads the last half
	// hour rather than a stored range, which is exactly the read that a change to
	// how its four queries run could break without any other test noticing.
	t.Run("realtime sees an event that just arrived", func(t *testing.T) {
		before, _ := f.get(t, site+"/realtime")["active_users_30m"].(float64)
		f.ingest(t, "visitor-realtime", "", "realtime_check")
		report := f.get(t, site+"/realtime")
		if active, _ := report["active_users_30m"].(float64); active != before+1 {
			t.Errorf("active users in the last 30 minutes = %v, want %v after one arrival", active, before+1)
		}
		events, _ := report["top_events"].([]any)
		found := false
		for _, item := range events {
			if entry, ok := item.(map[string]any); ok && entry["name"] == "realtime_check" {
				found = true
			}
		}
		if !found {
			t.Errorf("the event that just arrived is not among the top events: %v", events)
		}
		if timeline, _ := report["timeline"].([]any); len(timeline) == 0 {
			t.Error("the realtime timeline is empty after an event arrived")
		}
	})

	t.Run("overview and insights", func(t *testing.T) {
		f.get(t, site+"/overview?from="+from+"&to="+today)
		report := f.get(t, site+"/visitor-insights?from="+from+"&to="+today)
		if report["headline"] == nil {
			t.Fatalf("visitor insights has no headline: %v", report)
		}
		kpis, _ := report["kpis"].([]any)
		if len(kpis) != 10 {
			t.Fatalf("visitor insights returned %d KPIs, want 10", len(kpis))
		}
		users, _ := kpis[0].(map[string]any)
		if current, _ := users["current"].(float64); current <= 0 {
			t.Fatalf("the visitor KPI is empty: %v", users)
		}
		if findings, _ := report["findings"].([]any); len(findings) == 0 {
			t.Fatalf("visitor insights produced no findings: %v", report)
		}
		if channels, ok := report["channels"].([]any); !ok || len(channels) == 0 {
			t.Fatalf("visitor insights returned no channels: %v", report["channels"])
		}
	})

	t.Run("anomalies", func(t *testing.T) {
		report := f.get(t, site+"/anomalies")
		checked, _ := report["checked"].([]any)
		if len(checked) != 5 {
			t.Fatalf("anomaly report checked %d metrics, want the five watched ones: %v", len(checked), report["checked"])
		}
		// Ten weeks of rollups mean every metric should have enough history to judge,
		// which also proves the baseline read the rollups rather than giving up.
		for _, entry := range checked {
			metric, _ := entry.(map[string]any)
			if metric["severity"] == "insufficient_history" || metric["severity"] == "unknown" {
				t.Fatalf("metric %v was not judged: %v", metric["metric"], metric)
			}
			if samples, _ := metric["samples"].(float64); samples < 8 {
				t.Fatalf("metric %v used only %v baseline samples", metric["metric"], metric["samples"])
			}
		}
	})

	t.Run("attribution models", func(t *testing.T) {
		for _, model := range []string{"last_non_direct", "first_touch", "last_touch", "linear", "time_decay", "position_based"} {
			for _, scope := range []string{"site", "workspace"} {
				path := fmt.Sprintf("%s/attribution?from=%s&to=%s&model=%s&scope=%s", site, from, today, model, scope)
				response := f.get(t, path)
				report, _ := response["report"].(map[string]any)
				if report == nil {
					t.Fatalf("%s returned no report", path)
				}
				total, _ := report["total_conversions"].(float64)
				if total <= 0 {
					t.Fatalf("%s counted no conversions", path)
				}
				attributed, _ := report["attributed_conversions"].(float64)
				if attributed <= 0 {
					t.Fatalf("%s attributed nothing", path)
				}
				channels, _ := report["channels"].([]any)
				if len(channels) == 0 {
					t.Fatalf("%s credited no channel", path)
				}
				if scope == "workspace" && model == "linear" {
					// The sibling service only ever contributes a touch, never a
					// conversion. A recency model correctly gives it nothing because
					// the conversion site was visited last, so the split model is what
					// proves the cross-service join actually matches the same person.
					credit, _ := report["cross_site_credit"].(float64)
					if credit <= 0 {
						t.Fatalf("%s gave no credit to the other service: %v", path, report["sites"])
					}
					sites, _ := report["sites"].([]any)
					if len(sites) != 2 {
						t.Fatalf("%s credited %d services, want both: %v", path, len(sites), sites)
					}
				}
			}
		}
	})

	t.Run("cohort comparison", func(t *testing.T) {
		// The seeded people first appear ten weeks ago, and a cohort is keyed on first
		// activity, so a thirty day window would legitimately contain no cohort.
		cohortFrom := f.siteDate(t, -90)
		f.get(t, site+"/cohort?from="+cohortFrom+"&to="+today+"&granularity=week&periods=4")
		report := f.get(t, site+"/cohort?from="+cohortFrom+"&to="+today+"&granularity=week&periods=4&segment_ids="+f.segmentID)
		curves, _ := report["curves"].([]any)
		if len(curves) != 2 {
			t.Fatalf("cohort comparison returned %d curves, want baseline plus one segment: %v", len(curves), report["curves"])
		}
		baseline, _ := curves[0].(map[string]any)
		if users, _ := baseline["cohort_users"].(float64); users <= 0 {
			t.Fatalf("baseline cohort is empty: %v", baseline)
		}
	})

	t.Run("experience comparison", func(t *testing.T) {
		f.get(t, site+"/experience?from="+from+"&to="+today)
		report := f.get(t, site+"/experience?from="+from+"&to="+today+"&segment_ids="+f.segmentID)
		cohorts, _ := report["cohorts"].([]any)
		if len(cohorts) != 2 {
			t.Fatalf("experience comparison returned %d cohorts, want two: %v", len(cohorts), report["cohorts"])
		}
		// The mobile segment is seeded with a slower LCP, so the gap must be found.
		gaps, _ := report["gaps"].([]any)
		if len(gaps) == 0 {
			t.Fatalf("the slower mobile cohort produced no gap: %v", report["cohorts"])
		}
	})

	t.Run("visitor trace and search", func(t *testing.T) {
		trace := f.get(t, site+"/visitors/"+f.visitorID+"/timeline?from="+from+"&to="+today)
		if trace["sessions"] == nil || trace["summary"] == nil {
			t.Fatalf("trace is incomplete: %v", trace)
		}
		byUser := f.get(t, site+"/visitors/"+f.userID+"/timeline?from="+from+"&to="+today)
		if byUser["scope"] != "person" {
			t.Fatalf("looking up an SSO user should trace the person, got %v", byUser["scope"])
		}
		ids, _ := byUser["visitor_ids"].([]any)
		if len(ids) != 2 {
			t.Fatalf("person scope merged %d visitor ids, want the desktop and the phone: %v", len(ids), ids)
		}
		sessions, _ := byUser["sessions"].([]any)
		if len(sessions) == 0 {
			t.Fatalf("person trace has no sessions: %v", byUser)
		}
		if others, _ := byUser["other_sites"].([]any); len(others) != 1 {
			t.Fatalf("cross-service activity = %v, want the sibling service", byUser["other_sites"])
		}
		search := f.get(t, site+"/visitor-search?q=EMP&from="+from+"&to="+today)
		if results, ok := search["results"].([]any); !ok || len(results) == 0 {
			t.Fatalf("visitor search found nothing: %v", search)
		}
	})

	t.Run("diagnostics and goals", func(t *testing.T) {
		diagnostics := f.get(t, site+"/install-diagnostics")
		if diagnostics["checks"] == nil {
			t.Fatalf("diagnostics returned no checks: %v", diagnostics)
		}
		f.get(t, site+"/metric-goals/evaluate")
	})

	t.Run("reports", func(t *testing.T) {
		for _, kind := range []string{"events", "pages", "visitors", "sessions", "usage", "identities", "realtime", "ecommerce", "feature-intelligence", "search-analytics", "frustration", "data-quality", "adoption", "workspace-rollup", "ai-analytics", "insights"} {
			f.get(t, fmt.Sprintf("%s/%s?from=%s&to=%s", site, kind, from, today))
		}
	})

	t.Run("funnel comparison", func(t *testing.T) {
		body := fmt.Sprintf(`{"site_id":"%s","environment":"prd","from":"%s","to":"%s","mode":"closed","compare_segment_ids":["%s"],
			"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"},{"name":"구매","event":"purchase"}]}`,
			f.siteKey, from, today, f.segmentID)
		report := f.do(t, http.MethodPost, "/api/v1/funnel", body)
		series, _ := report["series"].([]any)
		comparison, _ := report["comparison"].([]any)
		if len(series) != 2 || len(comparison) != 1 {
			t.Fatalf("funnel comparison returned %d series and %d comparisons, want 2 and 1: %v", len(series), len(comparison), report)
		}
		first, _ := series[0].(map[string]any)
		if entered, _ := first["entered"].(float64); entered <= 0 {
			t.Fatalf("baseline funnel had no entrants: %v", first)
		}
		steps, _ := report["steps"].([]any)
		if len(steps) != 3 {
			t.Fatalf("funnel returned %d steps, want 3", len(steps))
		}
	})

	t.Run("segment aware query", func(t *testing.T) {
		body := fmt.Sprintf(`{"site_id":"%s","environment":"prd","date_range":{"from":"%s","to":"%s"},
			"dimensions":["event.name"],"metrics":["users","events"],"filters":[],"segment_id":"%s","limit":10}`,
			f.siteKey, from, today, f.segmentID)
		f.do(t, http.MethodPost, "/api/v1/query", body)
	})
}

// TestCollectorAndWorkerIngest covers the path every other report depends on: an
// event arriving over HTTP, passing the privacy filter, and reaching raw_events
// through the durable inbox.
func TestCollectorAndWorkerIngest(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	before := f.countEvents(t, "collector_check")
	payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"visitor-collector","session_id":"session-collector",
		"user_id":"EMP002","user_properties":{"department":"기술","email":"leak@example.com"},
		"context":{"page":{"url":"https://portal.internal/apply?token=secret","title":"신청","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
		"events":[{"id":"%s","name":"collector_check","timestamp":%d,"properties":{"feature":"apply","phone":"010-1234-5678"},"contract_version":1}]}`,
		f.siteKey, f.trackingKey, uuid.NewString(), time.Now().UnixMilli())

	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("collect = %d: %s", recorder.Code, recorder.Body.String())
	}

	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}
	if after := f.countEvents(t, "collector_check"); after != before+1 {
		t.Fatalf("stored events = %d, want %d", after, before+1)
	}

	// The privacy policy is applied before the durable write, so the stored row must
	// not contain the blocked property or the query string.
	var properties, userProperties, pageURL string
	if err := pool.QueryRow(ctx, `SELECT properties::text,user_properties::text,coalesce(page_url,'') FROM raw_events
		WHERE site_id=$1 AND event_name='collector_check' ORDER BY received_at DESC LIMIT 1`, f.siteID).
		Scan(&properties, &userProperties, &pageURL); err != nil {
		t.Fatalf("read stored event: %v", err)
	}
	if strings.Contains(properties, "010-1234-5678") || strings.Contains(properties, "phone") {
		t.Fatalf("a blocked property reached storage: %s", properties)
	}
	if strings.Contains(userProperties, "leak@example.com") || strings.Contains(userProperties, "email") {
		t.Fatalf("a blocked user property reached storage: %s", userProperties)
	}
	if strings.Contains(pageURL, "token=secret") {
		t.Fatalf("the query string reached storage: %s", pageURL)
	}
	if !strings.Contains(properties, "apply") {
		t.Fatalf("the allowed property was dropped: %s", properties)
	}

	// A wrong tracking key must be refused rather than silently accepted.
	bad := strings.Replace(payload, f.trackingKey, "mom_track_wrong", 1)
	badRequest := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(bad))
	badRequest.Header.Set("Content-Type", "application/json")
	badRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(badRecorder, badRequest)
	if badRecorder.Code != http.StatusForbidden {
		t.Fatalf("a wrong tracking key returned %d, want 403: %s", badRecorder.Code, badRecorder.Body.String())
	}
}

// siteDates returns the range to ask a report for, in the site's own calendar.
// Every analytical endpoint interprets from and to as dates in the site
// timezone, so a test that ingests an event now and asks for "today" in the
// runner's timezone can land outside the window: with the fixture site on
// Asia/Seoul, a UTC afternoon is already tomorrow in Seoul.
func (f fixture) siteDates(t *testing.T, days int) (string, string) {
	t.Helper()
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the fixture site timezone: %v", err)
	}
	now := time.Now().In(location)
	return now.AddDate(0, 0, -days).Format("2006-01-02"), now.Format("2006-01-02")
}

// siteDate returns one date offset in the site's calendar.
func (f fixture) siteDate(t *testing.T, offsetDays int) string {
	t.Helper()
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the fixture site timezone: %v", err)
	}
	return time.Now().In(location).AddDate(0, 0, offsetDays).Format("2006-01-02")
}

func (f fixture) countEvents(t *testing.T, name string) int64 {
	t.Helper()
	var count int64
	if err := f.server.DB.QueryRow(context.Background(), `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_name=$2`, f.siteID, name).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

// TestMCPToolsRunAgainstPostgres calls every advertised tool. Each one runs its own
// SQL, so a tool that is listed but broken is invisible until an agent calls it.
func TestMCPToolsRunAgainstPostgres(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 60)

	listed := f.rpc(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result, _ := listed["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) < 22 {
		t.Fatalf("tools/list returned %d tools, want at least 22", len(tools))
	}
	arguments := map[string]any{
		"site_id": f.siteKey, "from": from, "to": today, "environment": "prd",
		"dimension": "department", "metric": "active_users", "question": "지난주 사용 현황을 요약해줘",
		"group_by": "model", "model": "linear", "scope": "workspace",
		"cohort_event": "page_view", "return_event": "page_view",
	}
	for _, entry := range tools {
		tool, _ := entry.(map[string]any)
		name, _ := tool["name"].(string)
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 2, "method": "tools/call",
				"params": map[string]any{"name": name, "arguments": arguments},
			})
			if err != nil {
				t.Fatalf("encode call: %v", err)
			}
			response := f.rpc(t, string(body))
			if rpcErr, ok := response["error"]; ok {
				t.Fatalf("%s returned a protocol error: %v", name, rpcErr)
			}
			callResult, _ := response["result"].(map[string]any)
			if callResult == nil {
				t.Fatalf("%s returned no result: %v", name, response)
			}
			if isError, _ := callResult["isError"].(bool); isError {
				t.Fatalf("%s failed: %v", name, callResult["content"])
			}
		})
	}
}

func (f fixture) rpc(t *testing.T, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("mcp = %d: %s", recorder.Code, recorder.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode mcp response: %v (%s)", err, recorder.Body.String())
	}
	return decoded
}

// TestGovernanceEndpointsRunAgainstPostgres covers the reports the earlier suite
// left out, each of which builds its own SQL.
func TestGovernanceEndpointsRunAgainstPostgres(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 60)
	site := "/api/v1/sites/" + f.siteKey
	window := "?from=" + from + "&to=" + today

	for _, path := range []string{
		site + "/path" + window + "&view=all",
		site + "/catalog",
		site + "/lineage",
		site + "/query-audit",
		site + "/aggregate-jobs",
		site + "/annotations" + window,
		site + "/environments",
		site + "/event-contracts",
		site + "/semantic-metrics",
		site + "/metric-goals",
		site + "/journeys",
		site + "/workspace-journeys",
		site + "/adoption-targets",
		site + "/feature-flags",
		site + "/experiments",
		site + "/privacy-requests",
		site + "/delivery-channels",
		site + "/scheduled-reports",
		site + "/delivery-runs",
		site + "/retention",
		site + "/query-policy",
		"/api/v1/tracking-debugger?site_id=" + f.siteKey,
		"/api/v1/audit",
		"/api/v1/segments?site_id=" + f.siteKey,
		"/api/v1/dimensions?site_id=" + f.siteKey,
		"/api/v1/event-definitions?site_id=" + f.siteKey,
		"/api/v1/settings",
		"/api/v1/users",
		"/api/v1/networks",
		"/api/v1/sites",
		"/api/v1/system/encryption",
	} {
		f.get(t, path)
	}

	// The semantic metric evaluation compiles an AST into SQL, which is worth
	// running rather than trusting.
	f.get(t, site+"/semantic-metrics/active_users/query"+window)

	// A journey analysis joins steps across the workspace.
	f.do(t, http.MethodPost, site+"/journeys/analyze"+window,
		`{"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"}],"conversion_window_days":30}`)
	f.do(t, http.MethodPost, site+"/workspace-journeys/analyze"+window,
		`{"steps":[{"name":"진입","event":"page_view"},{"name":"기능","event":"feature_used"}],"conversion_window_days":30}`)

	// Contract validation is what a deployment pipeline calls.
	f.do(t, http.MethodPost, site+"/event-contracts/validate",
		`{"environment":"prd","events":[{"name":"feature_used","contract_version":1,"properties":{"feature":"document_search"}}]}`)

	// The raw export streams CSV and NDJSON rather than JSON.
	for _, format := range []string{"", "&format=json"} {
		request := httptest.NewRequest(http.MethodGet, site+"/export"+window+format, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("export%s = %d: %s", format, recorder.Code, recorder.Body.String())
		}
		if recorder.Body.Len() == 0 {
			t.Fatalf("export%s returned an empty body", format)
		}
	}
}

// TestRejectModeAcceptsAutomaticEventsButNotUnregisteredOnes pins the behaviour
// that lets a site turn on strict contracts. The tracker emits a couple of dozen
// events on its own; if those counted as unregistered, enabling reject mode would
// drop every batch that carried one, and each new automatic signal would break the
// sites that had transcribed the previous list.
//
// session_start was missing from that list for every release that had one, so a
// site with a strict contract lost the first batch of every session: the session
// start, the first page view, and any conversion that shared the flush with them.
// That is why this now sends a real first batch and not only single events.
func TestRejectModeAcceptsAutomaticEventsButNotUnregisteredOnes(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE site_environments SET contract_mode='reject' WHERE site_id=$1 AND name='prd'`, f.siteID); err != nil {
		t.Fatalf("switch to reject mode: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE site_environments SET contract_mode='allow' WHERE site_id=$1 AND name='prd'`, f.siteID)
	})

	send := func(t *testing.T, eventName string) int {
		t.Helper()
		payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"visitor-contract","session_id":"session-contract",
			"context":{"page":{"url":"https://portal.internal/search","title":"검색","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
			"events":[{"id":"%s","name":"%s","timestamp":%d,"properties":{"query_words":2},"contract_version":1}]}`,
			f.siteKey, f.trackingKey, uuid.NewString(), eventName, time.Now().UnixMilli())
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code
	}

	for _, name := range []string{"rage_click", "dead_click", "rapid_back", "form_retry", "repeated_search", "error_after_click", "slow_interaction", "search", "search_click", "search_refine", "page_view", "web_vital", "session_start", "collection_dropped"} {
		if code := send(t, name); code != http.StatusAccepted {
			t.Fatalf("reject mode refused the automatic event %s with %d", name, code)
		}
	}

	// One event refuses the whole batch, so the cost of missing one is everything
	// sent with it. This is the tracker's real first batch: session_start, the page
	// view that follows it, and the web vital reported for the same load. It was
	// rejected in full for every session on a site with a strict contract.
	first := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"visitor-first-batch","session_id":"session-first-batch",
		"context":{"page":{"url":"https://portal.internal/start","title":"시작"},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
		"events":[{"id":"%s","name":"session_start","timestamp":%d,"contract_version":1},
		          {"id":"%s","name":"page_view","timestamp":%d,"contract_version":1},
		          {"id":"%s","name":"web_vital","timestamp":%d,"properties":{"metric":"LCP","value":1800},"contract_version":1}]}`,
		f.siteKey, f.trackingKey,
		uuid.NewString(), time.Now().UnixMilli(),
		uuid.NewString(), time.Now().UnixMilli(),
		uuid.NewString(), time.Now().UnixMilli())
	firstRequest := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(first))
	firstRequest.Header.Set("Content-Type", "application/json")
	firstRequest.Header.Set("Origin", "https://portal.internal")
	firstRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusAccepted {
		t.Fatalf("reject mode refused a session's first batch with %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}
	var storedFirst int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND session_id='session-first-batch'`, f.siteID).Scan(&storedFirst); err != nil {
		t.Fatalf("count the first batch: %v", err)
	}
	if storedFirst != 3 {
		t.Fatalf("%d of the 3 events in a session's first batch were stored, so a strict contract is still losing the start of every session", storedFirst)
	}

	if code := send(t, "definitely_not_registered"); code == http.StatusAccepted {
		t.Fatal("reject mode accepted an unregistered custom event, so the contract is not enforced")
	}

	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}
	if stored := f.countEvents(t, "rage_click"); stored == 0 {
		t.Fatal("the automatic signal was accepted but never stored")
	}
	if stored := f.countEvents(t, "definitely_not_registered"); stored != 0 {
		t.Fatalf("the rejected event reached storage %d times", stored)
	}
}

// TestAutomaticSignalsReachTheReportsThatScoreThem follows the new tracker
// signals from the collector to the two reports that read them. Both reports
// existed before anything produced their events, so the point of this test is
// that ingesting what the tracker now sends actually moves the numbers.
func TestAutomaticSignalsReachTheReportsThatScoreThem(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 7)
	site := "/api/v1/sites/" + f.siteKey

	events := []string{
		fmt.Sprintf(`{"id":"%s","name":"rage_click","timestamp":%d,"properties":{"element_id":"save","clicks":4},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
		fmt.Sprintf(`{"id":"%s","name":"dead_click","timestamp":%d,"properties":{"element_id":"filter"},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
		fmt.Sprintf(`{"id":"%s","name":"error_after_click","timestamp":%d,"properties":{"element_id":"submit","error_kind":"error"},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
		fmt.Sprintf(`{"id":"%s","name":"search","timestamp":%d,"properties":{"query":"연차 신청","result_count":0,"source":"url"},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
		fmt.Sprintf(`{"id":"%s","name":"search","timestamp":%d,"properties":{"query":"출장 정산","result_count":12,"source":"url"},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
		fmt.Sprintf(`{"id":"%s","name":"search_click","timestamp":%d,"properties":{"query":"출장 정산","position":2},"contract_version":1}`, uuid.NewString(), time.Now().UnixMilli()),
	}
	payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"visitor-signals","session_id":"session-signals",
		"context":{"page":{"url":"https://portal.internal/search","title":"검색","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
		"events":[%s]}`, f.siteKey, f.trackingKey, strings.Join(events, ","))

	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("collect = %d: %s", recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}

	frustration := f.get(t, site+"/frustration?from="+from+"&to="+today)
	summary, _ := frustration["summary"].(map[string]any)
	if summary == nil || summary["affected_sessions"] == nil {
		t.Fatalf("frustration report has no summary: %v", frustration)
	}
	if affected, _ := summary["affected_sessions"].(float64); affected < 1 {
		t.Fatalf("the ingested signals left the frustration report empty: %v", summary)
	}
	if score, _ := summary["average_frustration_score"].(float64); score <= 0 {
		t.Fatalf("frustration score was not computed: %v", summary)
	}
	reported := map[string]bool{}
	for _, row := range frustration["signals"].([]any) {
		signal, _ := row.(map[string]any)
		reported[fmt.Sprint(signal["signal"])] = true
	}
	for _, want := range []string{"rage_click", "dead_click", "error_after_click"} {
		if !reported[want] {
			t.Errorf("the frustration report does not list %s", want)
		}
	}

	search := f.get(t, site+"/search-analytics?from="+from+"&to="+today)
	searchSummary, _ := search["summary"].(map[string]any)
	if searchSummary == nil {
		t.Fatalf("search report has no summary: %v", search)
	}
	if searches, _ := searchSummary["searches"].(float64); searches < 2 {
		t.Fatalf("searches were not counted: %v", searchSummary)
	}
	// One of the two searches returned nothing, which is the metric the tracker's
	// result-count hook exists to make possible.
	if zero, _ := searchSummary["zero_results"].(float64); zero < 1 {
		t.Fatalf("a zero-result search was not recognised: %v", searchSummary)
	}
	if clicks, _ := searchSummary["clicks"].(float64); clicks < 1 {
		t.Fatalf("the result click was not counted: %v", searchSummary)
	}
	queries := map[string]bool{}
	for _, row := range search["queries"].([]any) {
		item, _ := row.(map[string]any)
		queries[fmt.Sprint(item["query"])] = true
	}
	if !queries["연차 신청"] || !queries["출장 정산"] {
		t.Errorf("the query table is missing a search term: %v", queries)
	}
}

// TestFrictionAudiencesSelectTheRightPeople checks the whole path from a signal
// to a saved audience: the report counts a group, the segment definition it
// hands over is accepted, and evaluating that segment finds the same person and
// not the person who sailed through.
func TestFrictionAudiencesSelectTheRightPeople(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 7)
	site := "/api/v1/sites/" + f.siteKey

	// One person gets stuck twice and never converts; another searches and finds
	// nothing; a third does neither.
	ingest := func(visitor string, events ...string) {
		t.Helper()
		payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"%s","session_id":"session-%s",
			"context":{"page":{"url":"https://portal.internal/apply","title":"신청","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
			"events":[%s]}`, f.siteKey, f.trackingKey, visitor, visitor, strings.Join(events, ","))
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("collect for %s = %d: %s", visitor, recorder.Code, recorder.Body.String())
		}
	}
	event := func(name, properties string) string {
		return fmt.Sprintf(`{"id":"%s","name":"%s","timestamp":%d,"properties":%s,"contract_version":1}`,
			uuid.NewString(), name, time.Now().UnixMilli(), properties)
	}
	ingest("visitor-stuck", event("rage_click", `{"element_id":"save"}`), event("error_after_click", `{"element_id":"save"}`))
	ingest("visitor-stuck-again", event("dead_click", `{"element_id":"filter"}`))
	ingest("visitor-lost", event("search", `{"result_count":0}`))
	ingest("visitor-fine", event("page_view", `{}`))
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}

	// The stuck visitor has two sessions with friction, which is what the
	// repeatedly-blocked audience is about, so both events need distinct sessions.
	if _, err := pool.Exec(ctx, `UPDATE raw_events SET session_id='session-visitor-stuck-2' WHERE site_id=$1 AND visitor_id='visitor-stuck' AND event_name='error_after_click'`, f.siteID); err != nil {
		t.Fatalf("split the stuck visitor's sessions: %v", err)
	}

	frustration := f.get(t, site+"/frustration?from="+from+"&to="+today)
	audiences, _ := frustration["audiences"].([]any)
	if len(audiences) == 0 {
		t.Fatalf("the frustration report offers no audience: %v", frustration)
	}
	byKey := map[string]map[string]any{}
	for _, item := range audiences {
		audience, _ := item.(map[string]any)
		byKey[fmt.Sprint(audience["key"])] = audience
	}
	blocked := byKey["blocked_no_conversion"]
	if blocked == nil {
		t.Fatalf("the blocked audience is missing: %v", byKey)
	}
	// Two of the four ingested visitors hit a friction signal without converting.
	// The visitor whose search returned nothing is not stuck, and the fourth did
	// nothing but read a page.
	if users, _ := blocked["users"].(float64); users < 2 {
		t.Fatalf("the blocked audience counted %v people, want at least the two who hit friction", blocked["users"])
	}
	if fmt.Sprint(blocked["segment_note"]) == "" {
		t.Error("an audience whose segment is whole-history must say so")
	}

	// The definition the report handed over has to be a definition the segment
	// API accepts, evaluated against the database rather than trusted.
	saved := f.do(t, http.MethodPost, "/api/v1/segments", mustJSON(t, map[string]any{
		"site_id":    f.siteKey,
		"name":       "막힘 후 미전환 (통합 테스트)",
		"definition": blocked["segment"],
		"shared":     false,
	}))
	segmentID := fmt.Sprint(saved["id"])
	if segmentID == "" || segmentID == "<nil>" {
		t.Fatalf("the audience definition was refused by the segment API: %v", saved)
	}

	// Running the saved segment through the query builder is the check that
	// matters: the definition has to select the people who hit friction and leave
	// out the one who did not.
	inSegment := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
		"site_id":    f.siteKey,
		"segment_id": segmentID,
		"date_range": map[string]string{"from": from, "to": today},
		"dimensions": []string{"visitor.id"},
		"metrics":    []string{"events"},
	}))
	selected := map[string]bool{}
	rows, _ := inSegment["rows"].([]any)
	for _, item := range rows {
		row, _ := item.(map[string]any)
		selected[fmt.Sprint(row["visitor.id"])] = true
	}
	if len(rows) == 0 {
		t.Fatalf("the saved audience selected nobody: %v", inSegment)
	}
	for _, want := range []string{"visitor-stuck", "visitor-stuck-again"} {
		if !selected[want] {
			t.Errorf("the audience does not include %s, who hit friction and never converted", want)
		}
	}
	if selected["visitor-fine"] {
		t.Error("the audience includes a visitor who never hit a friction signal")
	}

	searchReport := f.get(t, site+"/search-analytics?from="+from+"&to="+today)
	searchAudiences, _ := searchReport["audiences"].([]any)
	found := false
	for _, item := range searchAudiences {
		audience, _ := item.(map[string]any)
		if fmt.Sprint(audience["key"]) == "zero_result_no_click" {
			found = true
			if users, _ := audience["users"].(float64); users < 1 {
				t.Errorf("the visitor whose search returned nothing is not in the audience: %v", audience)
			}
		}
	}
	if !found {
		t.Errorf("the search report offers no zero-result audience: %v", searchAudiences)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(encoded)
}

// TestFrictionImpactSeparatesAHarmfulSignalFromAHarmlessOne seeds two signals
// with deliberately different consequences and checks the report tells them
// apart. Ranking is the point of the feature: a report that lists every signal
// as a problem has not helped anyone decide what to fix.
func TestFrictionImpactSeparatesAHarmfulSignalFromAHarmlessOne(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 3)
	site := "/api/v1/sites/" + f.siteKey

	// A clean population large enough to judge: 60 people, 30 of whom hit
	// error_after_click and almost never convert, 30 of whom hit slow_interaction
	// and convert at the same rate as everyone else.
	base := time.Now().Add(-2 * time.Hour)
	insert := func(visitor, event string, converted bool, offset int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd',$3,$4,$5,$6,$6,'{}'::jsonb,$7,'https://portal.internal/apply')`,
			uuid.NewString(), f.siteID, visitor, visitor+"-s", event, base.Add(time.Duration(offset)*time.Second), converted); err != nil {
			t.Fatalf("insert %s for %s: %v", event, visitor, err)
		}
	}
	for i := 0; i < 30; i++ {
		visitor := fmt.Sprintf("impact-harmed-%02d", i)
		insert(visitor, "error_after_click", false, i)
		// Three of the thirty still convert, so the group is not degenerate.
		if i < 3 {
			insert(visitor, "purchase", true, i+1000)
		}
	}
	for i := 0; i < 30; i++ {
		visitor := fmt.Sprintf("impact-slow-%02d", i)
		insert(visitor, "slow_interaction", false, i+2000)
		// Half convert, which is also the rate among everyone who avoided it.
		if i%2 == 0 {
			insert(visitor, "purchase", true, i+3000)
		}
	}
	// A clean group that hits nothing, to be the baseline for both.
	for i := 0; i < 30; i++ {
		visitor := fmt.Sprintf("impact-clean-%02d", i)
		insert(visitor, "page_view", false, i+4000)
		if i%2 == 0 {
			insert(visitor, "purchase", true, i+5000)
		}
	}

	report := f.get(t, site+"/frustration?from="+from+"&to="+today)
	if fmt.Sprint(report["impact_caveat"]) == "" || fmt.Sprint(report["impact_caveat"]) == "<nil>" {
		t.Error("the report compares groups without stating that it is an association")
	}
	rows, _ := report["impact"].([]any)
	if len(rows) == 0 {
		t.Fatalf("no impact rows: %v", report)
	}
	byName := map[string]map[string]any{}
	order := []string{}
	for _, item := range rows {
		row, _ := item.(map[string]any)
		name := fmt.Sprint(row["signal"])
		byName[name] = row
		order = append(order, name)
	}

	harmful := byName["error_after_click"]
	if harmful == nil {
		t.Fatalf("the harmful signal is missing from the impact report: %v", order)
	}
	if verdict := fmt.Sprint(harmful["verdict"]); verdict != "worse" {
		t.Errorf("error_after_click verdict = %q, want worse (its group converts at 10%% against roughly 50%%): %v", verdict, harmful)
	}
	if gap, _ := harmful["gap_points"].(float64); gap > -20 {
		t.Errorf("error_after_click gap = %v points, want a clearly negative gap", gap)
	}
	if lost, _ := harmful["estimated_lost_conversions"].(float64); lost < 5 {
		t.Errorf("error_after_click estimated only %v lost conversions", lost)
	}

	harmless := byName["slow_interaction"]
	if harmless == nil {
		t.Fatalf("the harmless signal is missing from the impact report: %v", order)
	}
	if verdict := fmt.Sprint(harmless["verdict"]); verdict == "worse" {
		t.Errorf("slow_interaction was reported as harmful although its group converts at the same rate: %v", harmless)
	}

	// The harmful signal has to come first, because the ranking is what tells a
	// reader where to start.
	if order[0] != "error_after_click" {
		t.Errorf("impact is ordered %v, want the harmful signal first", order)
	}
}

// TestRevenueAgreesBetweenTheOverviewAndTheQueryBuilder pins the fix for a
// silent disagreement. A purchase may carry its amount as `value` or `revenue`;
// the query builder read only the first, so a site using the second saw revenue
// on the overview and zero in the query builder, with nothing to indicate which
// was wrong.
func TestRevenueAgreesBetweenTheOverviewAndTheQueryBuilder(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 3)
	site := "/api/v1/sites/" + f.siteKey

	// Two purchases of the same amount, one under each property name.
	base := time.Now().Add(-2 * time.Hour)
	for index, properties := range []string{`{"value": 1500}`, `{"revenue": 1500}`} {
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd',$3,$4,'purchase',$5,$5,$6::jsonb,true,'https://portal.internal/checkout')`,
			uuid.NewString(), f.siteID, fmt.Sprintf("revenue-visitor-%d", index), fmt.Sprintf("revenue-session-%d", index),
			base.Add(time.Duration(index)*time.Minute), properties); err != nil {
			t.Fatalf("insert purchase %d: %v", index, err)
		}
	}

	overview := f.get(t, site+"/overview?from="+from+"&to="+today)
	current, _ := overview["current"].(map[string]any)
	overviewRevenue, _ := current["revenue"].(float64)
	if overviewRevenue < 3000 {
		t.Fatalf("the overview reports %v revenue, want at least the 3000 just inserted", current["revenue"])
	}

	built := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
		"site_id":    f.siteKey,
		"date_range": map[string]string{"from": from, "to": today},
		"metrics":    []string{"revenue"},
	}))
	rows, _ := built["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the query builder returned no rows: %v", built)
	}
	row, _ := rows[0].(map[string]any)
	builderRevenue, _ := row["revenue"].(float64)
	if builderRevenue != overviewRevenue {
		t.Errorf("the query builder reports %v revenue and the overview %v for the same window: a purchase whose amount is named `revenue` has to count in both",
			row["revenue"], current["revenue"])
	}
}

// TestSessionMetricsAgreeBetweenTheOverviewAndTheInsightReport pins a
// disagreement that a reader could not resolve. Both screens answer "how long
// was the average session in this period"; the overview measured the span of
// events inside the query window and the insight report read the sessions table,
// so the same period was sixteen minutes on one and twelve on the other.
func TestSessionMetricsAgreeBetweenTheOverviewAndTheInsightReport(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	overview := f.get(t, site+"/overview?from="+from+"&to="+today)
	current, _ := overview["current"].(map[string]any)
	insight := f.get(t, site+"/visitor-insights?from="+from+"&to="+today)
	kpis, _ := insight["kpis"].([]any)
	if len(kpis) == 0 {
		t.Fatalf("the insight report has no KPIs: %v", insight)
	}
	byKey := map[string]float64{}
	for _, item := range kpis {
		kpi, _ := item.(map[string]any)
		value, _ := kpi["current"].(float64)
		byKey[fmt.Sprint(kpi["key"])] = value
	}

	for _, pair := range []struct {
		overviewKey string
		insightKey  string
		label       string
	}{
		{"sessions", "sessions", "session count"},
		{"engagement_rate", "engagement_rate", "engagement rate"},
		{"avg_session_duration", "avg_session_duration", "average session duration"},
	} {
		want, ok := byKey[pair.insightKey]
		if !ok {
			t.Errorf("the insight report does not report %s", pair.insightKey)
			continue
		}
		got, _ := current[pair.overviewKey].(float64)
		// A tenth of a unit of slack absorbs float formatting, not a definition.
		if math.Abs(got-want) > 0.1 {
			t.Errorf("%s is %v on the overview and %v in the insight report: one question cannot have two answers",
				pair.label, current[pair.overviewKey], want)
		}
	}

	// Sessions are counted by when they started, so the count cannot exceed the
	// number of sessions the collector recorded for the window.
	var recorded int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE site_id=$1 AND environment='prd' AND started_at >= $2::date AND started_at < ($3::date + 1)`,
		f.siteID, from, today).Scan(&recorded); err != nil {
		t.Fatalf("count recorded sessions: %v", err)
	}
	if sessions, _ := current["sessions"].(float64); recorded > 0 && sessions != float64(recorded) {
		t.Errorf("the overview reports %v sessions while the collector recorded %d starting in the window", current["sessions"], recorded)
	}
}

// TestSessionCountsAreNamedForWhatTheyCount covers the last of the same-name
// disagreements. A dimensional breakdown needs the sessions that were active in
// the range — sessions that saw a page, arrived from a channel. The overview
// reports the sessions that began in it, which is what makes consecutive periods
// add up. Both are useful and they differ by every session open at the boundary,
// so each has a name that says which it is.
func TestSessionCountsAreNamedForWhatTheyCount(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 2)

	// One session that began before the window and continued into it: what happens
	// to every session that is open at midnight.
	started := time.Now().AddDate(0, 0, -3)
	inside := time.Now().Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(site_id,session_id,environment,visitor_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,device_type)
		VALUES($1,'boundary-session','prd','boundary-visitor',$2,$3,2,2,0,true,'desktop')`, f.siteID, started, inside); err != nil {
		t.Fatalf("seed the boundary session: %v", err)
	}
	for _, at := range []time.Time{started, inside} {
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd','boundary-visitor','boundary-session','page_view',$3,$3,'{}'::jsonb,false,'https://portal.internal/boundary')`,
			uuid.NewString(), f.siteID, at); err != nil {
			t.Fatalf("seed the boundary events: %v", err)
		}
	}

	overview := f.get(t, "/api/v1/sites/"+f.siteKey+"/overview?from="+from+"&to="+today)
	current, _ := overview["current"].(map[string]any)
	overviewSessions, _ := current["sessions"].(float64)
	if overviewSessions == 0 {
		t.Fatalf("the overview reports no sessions: %v", current)
	}

	built := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
		"site_id":    f.siteKey,
		"date_range": map[string]string{"from": from, "to": today},
		"metrics":    []string{"sessions", "sessions_started"},
	}))
	rows, _ := built["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the query builder returned no rows: %v", built)
	}
	row, _ := rows[0].(map[string]any)
	active, _ := row["sessions"].(float64)
	startedInRange, _ := row["sessions_started"].(float64)

	if startedInRange != overviewSessions {
		t.Errorf("sessions_started is %v and the overview reports %v: the metric named for sessions that began in the range has to be that number",
			row["sessions_started"], current["sessions"])
	}
	if active != startedInRange+1 {
		t.Errorf("sessions=%v and sessions_started=%v: exactly one seeded session began before the window and continued into it, so the active count has to be one higher",
			row["sessions"], row["sessions_started"])
	}
}

// TestReportsThatHadNoFixtureData checks four reports against known inputs for
// the first time. Nothing in the suite created a refund, an engaged-time event, a
// resource error or an AI call, so those code paths ran, returned zero and passed.
// A report that answers zero because nothing was measuring is indistinguishable
// from one that answers zero because nothing happened.
func TestReportsThatHadNoFixtureData(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 7)
	site := "/api/v1/sites/" + f.siteKey

	t.Run("ecommerce refunds", func(t *testing.T) {
		report := f.get(t, site+"/ecommerce?from="+from+"&to="+today)
		summary, _ := report["summary"].(map[string]any)
		if summary == nil {
			t.Fatalf("the ecommerce report has no summary: %v", report)
		}
		// Three days of refunds at 400 each.
		refunds, _ := summary["refunds"].(float64)
		if refunds != 1200 {
			t.Errorf("refunds = %v, want the 1200 seeded across three days", summary["refunds"])
		}
		revenue, _ := summary["revenue"].(float64)
		if revenue <= 0 {
			t.Fatalf("revenue = %v, so the net figure cannot be checked", summary["revenue"])
		}
		net, _ := summary["net_revenue"].(float64)
		if net != revenue-refunds {
			t.Errorf("net_revenue = %v, want revenue %v minus refunds %v", net, revenue, refunds)
		}
	})

	t.Run("ecommerce funnel and products", func(t *testing.T) {
		report := f.get(t, site+"/ecommerce?from="+from+"&to="+today)
		summary, _ := report["summary"].(map[string]any)
		// One person per step per day, the same person each day.
		if carts, _ := summary["cart_users"].(float64); carts != 1 {
			t.Errorf("cart_users = %v, want the single seeded visitor", summary["cart_users"])
		}
		if checkouts, _ := summary["checkout_users"].(float64); checkouts != 1 {
			t.Errorf("checkout_users = %v, want the single seeded visitor", summary["checkout_users"])
		}

		steps, _ := report["funnel"].([]any)
		if len(steps) != 4 {
			t.Fatalf("the ecommerce funnel has %d steps, want view_item, add_to_cart, begin_checkout and purchase: %v", len(steps), steps)
		}
		byStep := map[string]float64{}
		for _, item := range steps {
			step, _ := item.(map[string]any)
			users, _ := step["users"].(float64)
			byStep[fmt.Sprint(step["event"])] = users
		}
		for _, name := range []string{"view_item", "add_to_cart", "begin_checkout", "purchase"} {
			if byStep[name] == 0 {
				t.Errorf("funnel step %s has no users although it was seeded: %v", name, byStep)
			}
		}

		products, _ := report["products"].([]any)
		if len(products) == 0 {
			t.Fatalf("the product list is empty although every purchase carries an items array: %v", report["products"])
		}
		product, _ := products[0].(map[string]any)
		if fmt.Sprint(product["item_id"]) != "sku-1" || fmt.Sprint(product["item_name"]) == "" {
			t.Errorf("the product row does not carry the seeded identity: %v", product)
		}
		// Two of the item per purchase at 500 each, so these follow from the
		// transaction count whatever the window contains.
		transactions, _ := product["transactions"].(float64)
		quantity, _ := product["quantity"].(float64)
		revenue, _ := product["revenue"].(float64)
		if transactions == 0 {
			t.Fatalf("the product row counts no transactions: %v", product)
		}
		if quantity != transactions*2 {
			t.Errorf("quantity = %v for %v transactions, want two per purchase", quantity, transactions)
		}
		if revenue != quantity*500 {
			t.Errorf("product revenue = %v for quantity %v, want 500 each", revenue, quantity)
		}
	})

	t.Run("engaged time ignores unparseable values", func(t *testing.T) {
		// Two engagement events a day for three days: one parseable at 15 seconds,
		// one that is not a number. The unparseable one must be ignored rather than
		// counted as zero-length engagement or breaking the query.
		usage := f.get(t, site+"/usage?from="+from+"&to="+today)
		if usage == nil {
			t.Fatal("the usage report failed")
		}
		var engagementEvents, active int64
		if err := pool.QueryRow(context.Background(), `SELECT count(*),count(*) FILTER(WHERE coalesce(properties->>'active_seconds','') ~ '^[0-9]+(\.[0-9]+)?$')
			FROM raw_events WHERE site_id=$1 AND event_name='user_engagement'`, f.siteID).Scan(&engagementEvents, &active); err != nil {
			t.Fatalf("count engagement events: %v", err)
		}
		if engagementEvents != 6 || active != 3 {
			t.Fatalf("seeded %d engagement events of which %d are numeric, want 6 and 3", engagementEvents, active)
		}
		overview := f.get(t, site+"/overview?from="+from+"&to="+today)
		current, _ := overview["current"].(map[string]any)
		if rate, ok := current["engagement_rate"].(float64); !ok || rate < 0 || rate > 100 {
			t.Errorf("engagement_rate = %v, want a percentage", current["engagement_rate"])
		}
	})

	t.Run("experience counts resource errors", func(t *testing.T) {
		report := f.get(t, site+"/experience?from="+from+"&to="+today)
		rows, _ := report["errors"].([]any)
		found := false
		for _, item := range rows {
			row, _ := item.(map[string]any)
			if fmt.Sprint(row["event"]) == "resource_error" {
				found = true
				if count, _ := row["count"].(float64); count != 3 {
					t.Errorf("resource_error count = %v, want the 3 seeded", row["count"])
				}
			}
		}
		if !found {
			t.Errorf("the experience report lists no resource_error although three were seeded: %v", rows)
		}
	})

	t.Run("ai operations", func(t *testing.T) {
		report := f.get(t, site+"/ai-analytics?from="+from+"&to="+today+"&group_by=model")
		rows, _ := report["rows"].([]any)
		if len(rows) == 0 {
			t.Fatalf("the AI report returned no rows although six calls were seeded: %v", report)
		}
		row, _ := rows[0].(map[string]any)
		// The grouped dimension is returned as `label`, whatever it was grouped by.
		if fmt.Sprint(row["label"]) != "claude" {
			t.Errorf("the AI report grouped by model returned label %v, want claude: %v", row["label"], row)
		}
		// Six calls over three days, half of them reporting success=false.
		if calls, _ := row["calls"].(float64); calls != 6 {
			t.Errorf("calls = %v, want the 6 seeded", row["calls"])
		}
		if rate, _ := row["success_rate"].(float64); rate < 49 || rate > 51 {
			t.Errorf("success_rate = %v, want about 50 with three of six failing", row["success_rate"])
		}
		if tokens, _ := row["input_tokens"].(float64); tokens != 6000 {
			t.Errorf("input_tokens = %v, want the 6000 seeded (1200+800 over three days)", row["input_tokens"])
		}
		// Latency averages the two seeded calls, and cost sums them.
		if latency, _ := row["average_latency_ms"].(float64); latency < 1109 || latency > 1111 {
			t.Errorf("average_latency_ms = %v, want 1110 from 820 and 1400", row["average_latency_ms"])
		}
		if cost, _ := row["cost"].(float64); cost < 0.089 || cost > 0.091 {
			t.Errorf("cost = %v, want 0.09 from three days of 0.02 and 0.01", row["cost"])
		}
	})
}

// TestEnvironmentsDoNotLeakIntoEachOther covers a configuration the fixture never
// created. Every analytical query filters by environment, and most of them build
// that predicate by string concatenation, which is exactly where one can be
// dropped by a later edit. With only a production environment ever seeded, a
// missing filter would have shown up as staging traffic silently inflating the
// production numbers, which nobody reads as a bug.
func TestEnvironmentsDoNotLeakIntoEachOther(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 7)
	site := "/api/v1/sites/" + f.siteKey

	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'stg','Staging') ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("create the staging environment: %v", err)
	}
	// Staging traffic named so that a leak is unmistakable in either direction.
	base := time.Now().Add(-2 * time.Hour)
	const stagingEvents = 5
	for i := 0; i < stagingEvents; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'stg','stg-visitor','stg-session','stg_only_event',$3,$3,'{}'::jsonb,false,'https://portal.internal/stg-only')`,
			uuid.NewString(), f.siteID, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("seed staging events: %v", err)
		}
	}

	production := f.get(t, site+"/overview?from="+from+"&to="+today+"&environment=prd")
	staging := f.get(t, site+"/overview?from="+from+"&to="+today+"&environment=stg")
	productionCurrent, _ := production["current"].(map[string]any)
	stagingCurrent, _ := staging["current"].(map[string]any)

	stagingCount, _ := stagingCurrent["events"].(float64)
	if stagingCount != stagingEvents {
		t.Errorf("the staging overview reports %v events, want the %d seeded there", stagingCurrent["events"], stagingEvents)
	}
	productionCount, _ := productionCurrent["events"].(float64)
	if productionCount <= stagingCount {
		t.Fatalf("production reports %v events and staging %v, so the two cannot be told apart", productionCount, stagingCount)
	}

	// A named event and a named page make a leak visible rather than arithmetic.
	prodEvents, _ := json.Marshal(f.get(t, site+"/events?from="+from+"&to="+today+"&environment=prd"))
	stgEvents, _ := json.Marshal(f.get(t, site+"/events?from="+from+"&to="+today+"&environment=stg"))
	if strings.Contains(string(prodEvents), "stg_only_event") {
		t.Errorf("the production event report lists a staging event: %s", truncateBody(string(prodEvents)))
	}
	if !strings.Contains(string(stgEvents), "stg_only_event") {
		t.Errorf("the staging event report does not list its own event: %s", truncateBody(string(stgEvents)))
	}
	prodPages, _ := json.Marshal(f.get(t, site+"/pages?from="+from+"&to="+today+"&environment=prd"))
	if strings.Contains(string(prodPages), "stg-only") {
		t.Errorf("the production page report lists a staging page: %s", truncateBody(string(prodPages)))
	}

	// A spread of the heavier reports has to answer differently for the two, since
	// they describe different traffic.
	for _, report := range []string{"frustration", "search-analytics", "experience", "visitor-insights"} {
		prod, _ := json.Marshal(f.get(t, site+"/"+report+"?from="+from+"&to="+today+"&environment=prd"))
		stg, _ := json.Marshal(f.get(t, site+"/"+report+"?from="+from+"&to="+today+"&environment=stg"))
		if string(prod) == string(stg) {
			t.Errorf("%s returns an identical document for production and staging, so it is not filtering by environment", report)
		}
		if strings.Contains(string(prod), "stg_only_event") || strings.Contains(string(prod), "stg-only") {
			t.Errorf("%s leaked staging data into production: %s", report, truncateBody(string(prod)))
		}
	}
}

// TestAdoptionUsesTheDeclaredTargetPopulation covers a branch that had never run.
// Adoption is a rate, and its denominator is an administrator's declared eligible
// population when one exists and the observed population when it does not. No
// fixture declared a target, so only the fallback was ever exercised — and a rate
// against the wrong denominator is worse than no rate.
func TestAdoptionUsesTheDeclaredTargetPopulation(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	before := f.get(t, site+"/adoption?from="+from+"&to="+today)
	rows, _ := before["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the adoption report returned no rows: %v", before)
	}
	first, _ := rows[0].(map[string]any)
	feature := fmt.Sprint(first["feature"])
	observed, _ := first["eligible_users"].(float64)
	users, _ := first["users"].(float64)
	if observed == 0 || users == 0 {
		t.Fatalf("the adoption row has no population to compare: %v", first)
	}

	// Declare a target ten times the observed population, so the rate has to fall.
	target := observed * 10
	if _, err := pool.Exec(ctx, `INSERT INTO adoption_targets(site_id,organization,department,feature,eligible_users)
		VALUES($1,'','',$2,$3) ON CONFLICT DO NOTHING`, f.siteID, feature, int64(target)); err != nil {
		t.Fatalf("declare the adoption target: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM adoption_targets WHERE site_id=$1`, f.siteID)
	})

	after := f.get(t, site+"/adoption?from="+from+"&to="+today)
	rows, _ = after["rows"].([]any)
	found := false
	for _, item := range rows {
		row, _ := item.(map[string]any)
		if fmt.Sprint(row["feature"]) != feature {
			continue
		}
		found = true
		eligible, _ := row["eligible_users"].(float64)
		if eligible != target {
			t.Errorf("eligible_users is %v after declaring a target of %v, so the declared population is being ignored", eligible, target)
		}
		rate, _ := row["adoption_rate"].(float64)
		rowUsers, _ := row["users"].(float64)
		want := rowUsers * 100 / target
		if rate < want-0.01 || rate > want+0.01 {
			t.Errorf("adoption_rate is %v, want %v users against the declared %v", rate, rowUsers, target)
		}
	}
	if !found {
		t.Errorf("the feature disappeared from the adoption report once a target was declared: %v", rows)
	}
}

// TestAccessControlAcrossWorkspacesAndRoles covers the last configuration the
// fixture only created one way. Every test signs in as a super_admin, which
// short-circuits the workspace membership check entirely, so neither that branch
// nor any administrator refusal had ever been executed. On a shared internal
// deployment those two rules are what keep one team's analytics out of another
// team's console.
func TestAccessControlAcrossWorkspacesAndRoles(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// A second organisation with its own workspace and site, unrelated to the
	// fixture's. Nothing links the two.
	var otherWorkspace, outsideSite uuid.UUID
	if err := pool.QueryRow(ctx, `WITH org AS (
			INSERT INTO organizations(name,slug) VALUES('다른 조직','other-org')
			ON CONFLICT(slug) DO UPDATE SET name=excluded.name RETURNING id
		), ws AS (
			INSERT INTO workspaces(organization_id,name) SELECT id,'다른 Workspace' FROM org RETURNING id,organization_id
		)
		SELECT id FROM ws`).Scan(&otherWorkspace); err != nil {
		t.Fatalf("create the other workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		VALUES($1,'SITE_OUTSIDE','외부','외부','h3','mom_track_z','s3','mom_server_z',ARRAY['portal.internal'],'Asia/Seoul') RETURNING id`, otherWorkspace).Scan(&outsideSite); err != nil {
		t.Fatalf("create the outside site: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'prd','Production') ON CONFLICT DO NOTHING`, outsideSite); err != nil {
		t.Fatalf("create the outside environment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE slug='other-org'`)
	})

	// An analyst whose only workspace role is in the fixture's workspace.
	var analystID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
		VALUES('analyst@test.local','Analyst','hash','analyst','Test')
		ON CONFLICT(email) DO UPDATE SET role='analyst' RETURNING id`).Scan(&analystID); err != nil {
		t.Fatalf("create the analyst: %v", err)
	}
	var homeWorkspace uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM sites WHERE id=$1`, f.siteID).Scan(&homeWorkspace); err != nil {
		t.Fatalf("read the fixture workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_workspace_roles(user_id,workspace_id,role) VALUES($1,$2,'analyst')
		ON CONFLICT DO NOTHING`, analystID, homeWorkspace); err != nil {
		t.Fatalf("grant the analyst their workspace: %v", err)
	}
	analystToken := "mom_sess_analyst"
	if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')
		ON CONFLICT DO NOTHING`, analystID, auth.HashToken(analystToken)); err != nil {
		t.Fatalf("create the analyst session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email='analyst@test.local'`)
	})

	as := func(t *testing.T, token, method, path string) int {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: token})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code
	}

	t.Run("a workspace role reaches its own site", func(t *testing.T) {
		if code := as(t, analystToken, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview"); code != http.StatusOK {
			t.Errorf("the analyst got %d for a site in their own workspace, want 200", code)
		}
	})

	t.Run("a workspace role cannot reach another workspace", func(t *testing.T) {
		// The site exists; the analyst has no role in its workspace. Answering 404
		// rather than 403 also avoids confirming that the site exists.
		if code := as(t, analystToken, http.MethodGet, "/api/v1/sites/SITE_OUTSIDE/overview"); code != http.StatusNotFound {
			t.Errorf("the analyst got %d for a site in another organisation's workspace, want 404", code)
		}
		// And the site list must not mention it.
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: analystToken})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if strings.Contains(recorder.Body.String(), "SITE_OUTSIDE") {
			t.Errorf("the analyst's site list includes another organisation's site: %s", truncateBody(recorder.Body.String()))
		}
	})

	t.Run("administrator endpoints refuse an analyst", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/users",
			"/api/v1/settings",
			"/api/v1/audit",
			"/api/v1/tracking-debugger",
			"/api/v1/sites/" + f.siteKey + "/query-policy",
		} {
			if code := as(t, analystToken, http.MethodGet, path); code != http.StatusForbidden {
				t.Errorf("%s answered %d for an analyst, want 403", path, code)
			}
		}
	})

	t.Run("the super admin still reaches everything", func(t *testing.T) {
		if code := as(t, f.sessionCook, http.MethodGet, "/api/v1/sites/SITE_OUTSIDE/overview"); code != http.StatusOK {
			t.Errorf("the super admin got %d for another organisation's site, want 200", code)
		}
		if code := as(t, f.sessionCook, http.MethodGet, "/api/v1/users"); code != http.StatusOK {
			t.Errorf("the super admin got %d for the user list, want 200", code)
		}
	})

	t.Run("granting a workspace role opens exactly that site", func(t *testing.T) {
		// Proves the membership table is what governs access, rather than the
		// request happening to fail for some other reason: the same request that
		// was refused above succeeds once the role exists, and is refused again
		// when it is removed.
		if _, err := pool.Exec(ctx, `INSERT INTO user_workspace_roles(user_id,workspace_id,role) VALUES($1,$2,'analyst')
			ON CONFLICT DO NOTHING`, analystID, otherWorkspace); err != nil {
			t.Fatalf("grant the analyst the other workspace: %v", err)
		}
		if code := as(t, analystToken, http.MethodGet, "/api/v1/sites/SITE_OUTSIDE/overview"); code != http.StatusOK {
			t.Errorf("the analyst got %d after being granted a role in that workspace, want 200", code)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM user_workspace_roles WHERE user_id=$1 AND workspace_id=$2`, analystID, otherWorkspace); err != nil {
			t.Fatalf("revoke the analyst's other workspace: %v", err)
		}
		if code := as(t, analystToken, http.MethodGet, "/api/v1/sites/SITE_OUTSIDE/overview"); code != http.StatusNotFound {
			t.Errorf("the analyst got %d after the role was revoked, want 404", code)
		}
	})

	t.Run("visitor profiles off blocks person level views", func(t *testing.T) {
		var previous string
		if err := pool.QueryRow(ctx, `SELECT value::text FROM settings WHERE key='privacy'`).Scan(&previous); err != nil {
			t.Fatalf("read the privacy setting: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE settings SET value=jsonb_set(value,'{visitor_profiles}','false') WHERE key='privacy'`); err != nil {
			t.Fatalf("disable visitor profiles: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `UPDATE settings SET value=$1::jsonb WHERE key='privacy'`, previous)
		})
		for _, path := range []string{
			"/api/v1/sites/" + f.siteKey + "/visitors",
			"/api/v1/sites/" + f.siteKey + "/identities",
			"/api/v1/sites/" + f.siteKey + "/visitors/" + f.visitorID + "/timeline",
		} {
			// Even the super admin is refused: this is a privacy policy, not a
			// permission level.
			if code := as(t, f.sessionCook, http.MethodGet, path); code != http.StatusForbidden {
				t.Errorf("%s answered %d with visitor profiles disabled, want 403", path, code)
			}
		}
		// A report that does not identify a person still answers.
		if code := as(t, f.sessionCook, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview"); code != http.StatusOK {
			t.Errorf("the overview answered %d with visitor profiles disabled, want 200: it names nobody", code)
		}
	})
}

// TestAPIKeyAuthentication covers the other way in. Every other test arrives with
// a session cookie, so the personal API key path — the one a BI job or another
// service actually uses — had never been exercised, including the refusals that
// exist to stop a key being used as an administrator.
func TestAPIKeyAuthentication(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var owner uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email='admin@test.local'`).Scan(&owner); err != nil {
		t.Fatalf("read the fixture user: %v", err)
	}
	issue := func(t *testing.T, name, token string, expires any, revoked bool) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO api_keys(user_id,name,key_hash,key_prefix,scopes,expires_at,revoked_at)
			VALUES($1,$2,$3,'mom_key_','{}',$4,CASE WHEN $5 THEN now() ELSE NULL END)`,
			owner, name, auth.HashToken(token), expires, revoked); err != nil {
			t.Fatalf("issue key %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE user_id=$1`, owner)
	})

	const good = "mom_key_integration_good"
	issue(t, "good", good, nil, false)
	issue(t, "revoked", "mom_key_integration_revoked", nil, true)
	issue(t, "expired", "mom_key_integration_expired", time.Now().Add(-time.Hour), false)

	call := func(t *testing.T, key, method, path, body string) (int, string) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer "+key)
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	t.Run("a key reads the analytics it was issued for", func(t *testing.T) {
		if code, body := call(t, good, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview", ""); code != http.StatusOK {
			t.Errorf("a valid key got %d for the overview, want 200: %s", code, truncateBody(body))
		}
		if code, _ := call(t, good, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/export?format=json", ""); code != http.StatusOK {
			t.Errorf("a valid key got %d for the export, which is the reason keys exist", code)
		}
	})

	t.Run("a key is never an administrator", func(t *testing.T) {
		// The key's owner is a super administrator. Keys are refused on these
		// endpoints regardless, because a long-lived credential in a script should
		// not be able to change the deployment.
		for _, path := range []string{"/api/v1/users", "/api/v1/settings", "/api/v1/audit"} {
			code, body := call(t, good, http.MethodGet, path, "")
			if code != http.StatusForbidden {
				t.Errorf("%s answered %d for a key owned by a super administrator, want 403: %s", path, code, truncateBody(body))
			}
		}
	})

	t.Run("a key cannot perform interactive writes", func(t *testing.T) {
		body := fmt.Sprintf(`{"site_id":%q,"name":"키로 만든 Segment","definition":{"field":"device.type","operator":"=","value":"mobile"}}`, f.siteKey)
		code, response := call(t, good, http.MethodPost, "/api/v1/segments", body)
		if code != http.StatusForbidden {
			t.Errorf("creating a segment with a key answered %d, want 403", code)
		}
		if !strings.Contains(response, "SESSION_REQUIRED") {
			t.Errorf("the refusal does not say an interactive session is required: %s", truncateBody(response))
		}
	})

	t.Run("revoked and expired keys are refused", func(t *testing.T) {
		for name, key := range map[string]string{
			"revoked": "mom_key_integration_revoked",
			"expired": "mom_key_integration_expired",
			"unknown": "mom_key_never_issued",
		} {
			if code, _ := call(t, key, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview", ""); code != http.StatusUnauthorized {
				t.Errorf("a %s key got %d, want 401", name, code)
			}
		}
	})

	t.Run("a key stops working when its owner is deactivated", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE users SET active=false WHERE id=$1`, owner); err != nil {
			t.Fatalf("deactivate the owner: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), `UPDATE users SET active=true WHERE id=$1`, owner)
		}()
		if code, _ := call(t, good, http.MethodGet, "/api/v1/sites/"+f.siteKey+"/overview", ""); code != http.StatusUnauthorized {
			t.Errorf("a key belonging to a deactivated user got %d, want 401", code)
		}
	})
}

// TestServerSideIngestionRequiresTheServerKey covers the collector rule that
// separates a backend from a browser, which no test had reached. A request with
// no Origin is server to server and must present the site's server API key; the
// tracking key is not enough there, and that matters because the tracking key is
// visible in the HTML of every page the site serves.
func TestServerSideIngestionRequiresTheServerKey(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	send := func(t *testing.T, key, origin string) (int, string) {
		t.Helper()
		payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"server-visitor","session_id":"server-session",
			"context":{"page":{"url":"https://portal.internal/api","title":"","referrer":""},"device":{},"traffic":{}},
			"events":[{"id":"%s","name":"server_side_event","timestamp":%d,"properties":{},"contract_version":1}]}`,
			f.siteKey, key, uuid.NewString(), time.Now().UnixMilli())
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	t.Run("no origin accepts the server key", func(t *testing.T) {
		if code, body := send(t, f.serverKey, ""); code != http.StatusAccepted {
			t.Errorf("server-side ingestion with the server key answered %d, want 202: %s", code, truncateBody(body))
		}
	})

	t.Run("no origin refuses the tracking key", func(t *testing.T) {
		// The tracking key is published in every page's HTML. If it were accepted
		// here, anyone who viewed the site could inject events attributed to it.
		code, body := send(t, f.trackingKey, "")
		if code == http.StatusAccepted {
			t.Errorf("server-side ingestion accepted the public tracking key, which anyone can read from the page source")
		}
		if code != http.StatusForbidden {
			t.Errorf("answered %d, want 403: %s", code, truncateBody(body))
		}
	})

	t.Run("no origin refuses a missing key", func(t *testing.T) {
		if code, _ := send(t, "", ""); code != http.StatusForbidden {
			t.Errorf("server-side ingestion without a key answered %d, want 403", code)
		}
	})

	t.Run("a browser request accepts either key from an allowed origin", func(t *testing.T) {
		if code, body := send(t, f.trackingKey, "https://portal.internal"); code != http.StatusAccepted {
			t.Errorf("a browser request with the tracking key answered %d, want 202: %s", code, truncateBody(body))
		}
		if code, body := send(t, f.serverKey, "https://portal.internal"); code != http.StatusAccepted {
			t.Errorf("a browser request with the server key answered %d, want 202: %s", code, truncateBody(body))
		}
	})

	t.Run("an unlisted origin is refused whichever key it carries", func(t *testing.T) {
		for name, key := range map[string]string{"tracking": f.trackingKey, "server": f.serverKey} {
			if code, _ := send(t, key, "https://evil.example"); code != http.StatusForbidden {
				t.Errorf("an unlisted origin with the %s key answered %d, want 403", name, code)
			}
		}
	})

	// The accepted events are the ones that reached storage.
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("worker: %v", err)
	}
	if stored := f.countEvents(t, "server_side_event"); stored != 3 {
		t.Errorf("%d server_side_event rows reached storage, want the 3 accepted requests", stored)
	}
}

// TestLoginIsRateLimited covers the wiring rather than the limiter. The limiter
// has its own unit tests; what had never been checked is that the login endpoint
// consults it, which is the only thing standing between a reachable console and
// an unlimited password guessing loop.
func TestLoginIsRateLimited(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)

	attempt := func() (int, string) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"email":"admin@test.local","password":"wrong"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	limited := false
	for i := 0; i < 40 && !limited; i++ {
		code, body := attempt()
		if code == http.StatusTooManyRequests {
			limited = true
			if !strings.Contains(body, "RATE_LIMITED") {
				t.Errorf("the refusal does not identify itself as rate limiting: %s", truncateBody(body))
			}
			continue
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d answered %d, want 401 for a wrong password: %s", i+1, code, truncateBody(body))
		}
	}
	if !limited {
		t.Error("forty wrong passwords from one address were all answered, so nothing is limiting password guessing")
	}
}

// TestCollectorClassifiesAndLimitsTraffic covers the collector's security
// settings and its traffic classification, neither of which any test had reached.
//
// The classification matters for a different reason than the limit: an uptime
// monitor hitting a page every minute adds fourteen hundred page views a day, and
// Momento records the class but does not exclude anything from the reports. The
// assertions below state that as the behaviour it is, so a future change to it is
// a deliberate one.
func TestCollectorClassifiesAndLimitsTraffic(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	send := func(t *testing.T, userAgent string, events int) (int, string) {
		t.Helper()
		items := make([]string, 0, events)
		for i := 0; i < events; i++ {
			items = append(items, fmt.Sprintf(`{"id":"%s","name":"classified_event","timestamp":%d,"properties":{},"contract_version":1}`,
				uuid.NewString(), time.Now().UnixMilli()))
		}
		payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"class-visitor","session_id":"class-session",
			"context":{"page":{"url":"https://portal.internal/home","title":"","referrer":""},"device":{},"traffic":{}},
			"events":[%s]}`, f.siteKey, f.trackingKey, strings.Join(items, ","))
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		if userAgent != "" {
			request.Header.Set("User-Agent", userAgent)
		}
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	t.Run("a batch over the configured limit is refused", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE settings SET value=jsonb_set(value,'{max_events_per_request}','3') WHERE key='security'`); err != nil {
			t.Fatalf("set the batch limit: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `UPDATE settings SET value=jsonb_set(value,'{max_events_per_request}','100') WHERE key='security'`)
		})
		if code, _ := send(t, "Mozilla/5.0 Chrome/140.0", 3); code != http.StatusAccepted {
			t.Errorf("a batch at the limit answered %d, want 202", code)
		}
		code, body := send(t, "Mozilla/5.0 Chrome/140.0", 4)
		if code == http.StatusAccepted {
			t.Errorf("a batch of 4 was accepted under a limit of 3")
		}
		if !strings.Contains(body, "3") {
			t.Errorf("the refusal does not say what the limit is: %s", truncateBody(body))
		}
	})

	t.Run("traffic is classified by user agent", func(t *testing.T) {
		cases := map[string]string{
			"Mozilla/5.0 (compatible; Googlebot/2.1)": "known_bot",
			"Pingdom.com_bot_version_1.4":             "monitoring",
			"Mozilla/5.0 Chrome/140.0":                "normal",
		}
		for agent, want := range cases {
			if code, body := send(t, agent, 1); code != http.StatusAccepted {
				t.Fatalf("%s answered %d: %s", agent, code, truncateBody(body))
			}
			if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
				t.Fatalf("worker: %v", err)
			}
			var class string
			if err := pool.QueryRow(ctx, `SELECT traffic_class FROM raw_events WHERE site_id=$1 AND event_name='classified_event' AND user_agent=$2 ORDER BY received_at DESC LIMIT 1`,
				f.siteID, agent).Scan(&class); err != nil {
				t.Fatalf("read the stored class for %s: %v", agent, err)
			}
			if class != want {
				t.Errorf("%s was classified %q, want %q", agent, class, want)
			}
		}
	})

	t.Run("reports include every traffic class", func(t *testing.T) {
		// Not an endorsement, a statement: nothing filters by class, so a crawler
		// and a monitor are counted like a person. The way to exclude them is a
		// segment on traffic.class, which the next case shows works.
		from, today := f.siteDates(t, 1)
		report := f.get(t, "/api/v1/sites/"+f.siteKey+"/events?from="+from+"&to="+today)
		body, _ := json.Marshal(report)
		if !strings.Contains(string(body), "classified_event") {
			t.Fatalf("the event report does not show the ingested events at all: %s", truncateBody(string(body)))
		}
		var stored int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND event_name='classified_event' AND traffic_class IN ('known_bot','monitoring')`, f.siteID).Scan(&stored); err != nil {
			t.Fatalf("count classified events: %v", err)
		}
		if stored == 0 {
			t.Fatal("no bot or monitoring events were stored, so this case proves nothing")
		}
	})

	t.Run("a segment can exclude a traffic class", func(t *testing.T) {
		from, today := f.siteDates(t, 1)
		built := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
			"site_id":    f.siteKey,
			"date_range": map[string]string{"from": from, "to": today},
			"metrics":    []string{"events"},
			"segment":    map[string]any{"field": "traffic.class", "operator": "=", "value": "normal"},
		}))
		rows, _ := built["rows"].([]any)
		if len(rows) == 0 {
			t.Fatalf("the segmented query returned no rows: %v", built)
		}
		row, _ := rows[0].(map[string]any)
		normalOnly, _ := row["events"].(float64)

		all := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
			"site_id":    f.siteKey,
			"date_range": map[string]string{"from": from, "to": today},
			"metrics":    []string{"events"},
		}))
		allRows, _ := all["rows"].([]any)
		allRow, _ := allRows[0].(map[string]any)
		everything, _ := allRow["events"].(float64)
		if normalOnly >= everything {
			t.Errorf("filtering to normal traffic gave %v events against %v unfiltered, so the class is not usable as a filter", normalOnly, everything)
		}
	})

	t.Run("internal traffic is filterable", func(t *testing.T) {
		// The collector has always recorded whether an event came from a network an
		// administrator marked internal, and nothing could read it until now.
		from, today := f.siteDates(t, 1)
		built := f.do(t, http.MethodPost, "/api/v1/query", mustJSON(t, map[string]any{
			"site_id":    f.siteKey,
			"date_range": map[string]string{"from": from, "to": today},
			"metrics":    []string{"events"},
			"segment":    map[string]any{"field": "traffic.internal", "operator": "=", "value": false},
		}))
		if built["rows"] == nil {
			t.Fatalf("filtering on traffic.internal failed: %v", built)
		}
	})
}

// The administrative routes on /api/v1/sites/{id} take the site's uuid from the
// path. Their middleware checks that the caller is at least a workspace_admin; it
// does not check which workspace, and four of the six handlers never checked
// either — they parsed the uuid and acted on it.
//
// Measured before the fix, as a workspace_admin whose only membership was one
// workspace: renaming, rotating the tracking key, rotating the server key and
// deleting a site in another workspace all succeeded. Rotating a key stops that
// site collecting anything until someone redeploys its tracker; the delete takes
// every event, session and aggregate with it and cannot be undone. On a shared
// internal deployment, keeping one team out of another team's site is the whole
// point of workspaces.
//
// Two handlers had the membership predicate written inline and were correct,
// which is how this survived: the routes looked guarded because some of them
// were. Both directions are checked here, because a fix that locks the owner out
// of their own site is not a fix.
func TestSiteAdministrationIsConfinedToTheOwningWorkspace(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var otherWorkspace, outsideSite uuid.UUID
	if err := pool.QueryRow(ctx, `WITH org AS (
			INSERT INTO organizations(name,slug) VALUES('이웃 조직','neighbour-org')
			ON CONFLICT(slug) DO UPDATE SET name=excluded.name RETURNING id
		), ws AS (INSERT INTO workspaces(organization_id,name) SELECT id,'이웃 Workspace' FROM org RETURNING id)
		SELECT id FROM ws`).Scan(&otherWorkspace); err != nil {
		t.Fatalf("create the neighbouring workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE slug='neighbour-org'`)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		VALUES($1,'SITE_NEIGHBOUR','이웃','이웃','h9','mom_track_q','s9','mom_server_q',ARRAY['portal.internal'],'Asia/Seoul') RETURNING id`, otherWorkspace).Scan(&outsideSite); err != nil {
		t.Fatalf("create the neighbouring site: %v", err)
	}

	var adminID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
		VALUES('wsadmin@test.local','WS Admin','hash','workspace_admin','Test')
		ON CONFLICT(email) DO UPDATE SET role='workspace_admin' RETURNING id`).Scan(&adminID); err != nil {
		t.Fatalf("create the workspace admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email='wsadmin@test.local'`)
	})
	var homeWorkspace uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM sites WHERE id=$1`, f.siteID).Scan(&homeWorkspace); err != nil {
		t.Fatalf("read the fixture workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_workspace_roles(user_id,workspace_id,role) VALUES($1,$2,'workspace_admin') ON CONFLICT DO NOTHING`, adminID, homeWorkspace); err != nil {
		t.Fatalf("grant the admin their workspace: %v", err)
	}
	token := "mom_sess_wsadmin"
	if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')
		ON CONFLICT(token_hash) DO UPDATE SET user_id=excluded.user_id,expires_at=excluded.expires_at`, adminID, auth.HashToken(token)); err != nil {
		t.Fatalf("create the admin session: %v", err)
	}

	call := func(method, path, body string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: token})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	neighbour := outsideSite.String()
	valid := `{"timezone":"UTC","session_timeout_minutes":45,"engagement_threshold_seconds":15,"name":"hijacked"}`
	for _, attempt := range []struct{ label, method, path, body string }{
		{"read the reports", http.MethodGet, "/api/v1/sites/SITE_NEIGHBOUR/overview", ""},
		{"read the tracking code", http.MethodGet, "/api/v1/sites/" + neighbour + "/tracking-code", ""},
		{"reveal the collection keys", http.MethodPost, "/api/v1/sites/" + neighbour + "/reveal-keys", ""},
		{"change the settings", http.MethodPatch, "/api/v1/sites/" + neighbour, valid},
		{"rotate the tracking key", http.MethodPost, "/api/v1/sites/" + neighbour + "/rotate-key", ""},
		{"rotate the server key", http.MethodPost, "/api/v1/sites/" + neighbour + "/rotate-server-key", ""},
		{"delete the site", http.MethodDelete, "/api/v1/sites/" + neighbour + "?confirm=SITE_NEIGHBOUR", ""},
	} {
		code, body := call(attempt.method, attempt.path, attempt.body)
		if code < 400 {
			t.Errorf("a workspace_admin of another workspace could %s: %d %s", attempt.label, code, truncateBody(body))
		}
	}
	// Refusing is not enough if the work happened anyway.
	var name, prefix string
	if err := pool.QueryRow(ctx, `SELECT name,tracking_key_prefix FROM sites WHERE id=$1`, outsideSite).Scan(&name, &prefix); err != nil {
		t.Fatalf("the neighbouring site is gone after refused requests: %v", err)
	}
	if name != "이웃" || prefix != "mom_track_q" {
		t.Errorf("the refused requests changed the neighbouring site anyway: name=%q tracking_key_prefix=%q", name, prefix)
	}

	// And the same admin keeps full control of their own site.
	own := f.siteID.String()
	for _, allowed := range []struct{ label, method, path, body string }{
		{"read the reports", http.MethodGet, "/api/v1/sites/" + f.siteKey + "/overview", ""},
		{"read the tracking code", http.MethodGet, "/api/v1/sites/" + own + "/tracking-code", ""},
		{"reveal the collection keys", http.MethodPost, "/api/v1/sites/" + own + "/reveal-keys", ""},
		{"change the settings", http.MethodPatch, "/api/v1/sites/" + own, `{"timezone":"Asia/Seoul","session_timeout_minutes":30,"engagement_threshold_seconds":10,"name":"포털"}`},
		{"rotate the tracking key", http.MethodPost, "/api/v1/sites/" + own + "/rotate-key", ""},
		{"rotate the server key", http.MethodPost, "/api/v1/sites/" + own + "/rotate-server-key", ""},
	} {
		if code, body := call(allowed.method, allowed.path, allowed.body); code >= 400 {
			t.Errorf("a workspace_admin cannot %s on their own site: %d %s", allowed.label, code, truncateBody(body))
		}
	}
}

// Instance administration — the settings that apply to every site, the network
// ranges that decide what counts as internal traffic, and the user accounts —
// sat behind the admin middleware, which means "at least workspace_admin". The
// lowest administrative role could therefore reach all of it, and nothing bounded
// the role it handed out.
//
// Measured before the fix, as a workspace_admin of one workspace: PATCH on
// another user set their role to super_admin, PATCH on itself set its own role to
// super_admin, and a network range that classifies every site's traffic was
// deleted. super_admin satisfies every workspace check in the service, so one
// request turned the smallest administrative role into the largest.
//
// Both halves are checked: that the escalation is refused, and that each role can
// still do the administration that belongs to it. A permission fix that stops the
// product working is not a fix.
func TestRoleGrantsCannotExceedTheCallersOwnAuthority(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	user := func(email, role string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role,organization_name)
			VALUES($1,$1,'hash',$2,'Test') ON CONFLICT(email) DO UPDATE SET role=excluded.role RETURNING id`, email, role).Scan(&id); err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, email) })
		return id
	}
	session := func(id uuid.UUID, token string) string {
		t.Helper()
		// The token is a fixed string, so its hash is the same on every run and
		// the row can already exist from a previous one. DO NOTHING left that row
		// in place: an hour later it is expired, this helper reports success, and
		// the test fails with "invalid session" on a request that has nothing to
		// do with sessions. Refreshing it is what a function called session means.
		if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')
			ON CONFLICT(token_hash) DO UPDATE SET user_id=excluded.user_id,expires_at=excluded.expires_at`, id, auth.HashToken(token)); err != nil {
			t.Fatalf("create a session: %v", err)
		}
		return token
	}
	call := func(token, method, path, body string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: token})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	roleOf := func(id uuid.UUID) string {
		t.Helper()
		var role string
		if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, id).Scan(&role); err != nil {
			t.Fatalf("read a role: %v", err)
		}
		return role
	}

	// The instance settings are shared by every test in this package, so this
	// writes back exactly what is already there: the point is who may write, not
	// what is written.
	var privacy []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='privacy'`).Scan(&privacy); err != nil {
		t.Fatalf("read the privacy setting: %v", err)
	}
	privacyBody := fmt.Sprintf(`{"value":%s}`, privacy)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE settings SET value=$1 WHERE key='privacy'`, privacy)
	})

	wsAdmin := user("grant-ws@test.local", "workspace_admin")
	orgAdmin := user("grant-org@test.local", "organization_admin")
	target := user("grant-target@test.local", "analyst")
	wsToken := session(wsAdmin, "mom_sess_grant_ws")
	orgToken := session(orgAdmin, "mom_sess_grant_org")
	var homeWorkspace uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM sites WHERE id=$1`, f.siteID).Scan(&homeWorkspace); err != nil {
		t.Fatalf("read the fixture workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_workspace_roles(user_id,workspace_id,role) VALUES($1,$2,'workspace_admin') ON CONFLICT DO NOTHING`, wsAdmin, homeWorkspace); err != nil {
		t.Fatalf("grant the workspace admin their workspace: %v", err)
	}

	promote := func(id uuid.UUID, role string) string {
		return fmt.Sprintf(`{"display_name":"x","department":"","organization_name":"Test","role":%q}`, role)
	}

	t.Run("a workspace admin cannot reach instance administration", func(t *testing.T) {
		for _, attempt := range []struct{ label, method, path, body string }{
			{"promote another user", http.MethodPatch, "/api/v1/users/" + target.String(), promote(target, "super_admin")},
			{"promote itself", http.MethodPatch, "/api/v1/users/" + wsAdmin.String(), promote(wsAdmin, "super_admin")},
			{"create a super administrator", http.MethodPost, "/api/v1/users", `{"email":"smuggled@test.local","display_name":"x","role":"super_admin","password":"a-long-enough-password"}`},
			{"change an instance setting", http.MethodPut, "/api/v1/settings/privacy", privacyBody},
			{"add a network range", http.MethodPost, "/api/v1/networks", `{"name":"probe","cidr":"10.98.0.0/16"}`},
		} {
			if code, body := call(wsToken, attempt.method, attempt.path, attempt.body); code != http.StatusForbidden {
				t.Errorf("a workspace_admin could %s: %d %s", attempt.label, code, truncateBody(body))
			}
		}
		if role := roleOf(target); role != "analyst" {
			t.Errorf("the refused requests changed a role anyway: %s is now %q", "grant-target", role)
		}
		if role := roleOf(wsAdmin); role != "workspace_admin" {
			t.Errorf("a workspace_admin promoted itself to %q", role)
		}
		var smuggled int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email='smuggled@test.local'`).Scan(&smuggled); err != nil {
			t.Fatalf("count: %v", err)
		}
		if smuggled > 0 {
			t.Error("a refused request created the account anyway")
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE email='smuggled@test.local'`)
		}
	})

	t.Run("an organization admin administers up to its own authority", func(t *testing.T) {
		if code, body := call(orgToken, http.MethodPatch, "/api/v1/users/"+target.String(), promote(target, "workspace_admin")); code != http.StatusOK {
			t.Fatalf("an organization_admin cannot grant a role below its own: %d %s", code, truncateBody(body))
		}
		if role := roleOf(target); role != "workspace_admin" {
			t.Fatalf("the grant answered 200 and the role is %q", role)
		}
		if code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+target.String(), promote(target, "super_admin")); code != http.StatusForbidden {
			t.Errorf("an organization_admin granted super_admin, which is above its own authority: %d", code)
		}
		if code, _ := call(orgToken, http.MethodPatch, "/api/v1/users/"+orgAdmin.String(), promote(orgAdmin, "super_admin")); code == http.StatusOK {
			t.Error("an organization_admin raised its own role")
		}
		if role := roleOf(orgAdmin); role != "organization_admin" {
			t.Errorf("the organization_admin is now %q", role)
		}
		// The administration that does belong to it still works.
		if code, body := call(orgToken, http.MethodPut, "/api/v1/settings/privacy", privacyBody); code != http.StatusOK {
			t.Errorf("an organization_admin cannot change an instance setting: %d %s", code, truncateBody(body))
		}
	})

	t.Run("a super administrator keeps every grant but its own role", func(t *testing.T) {
		if code, body := call(f.sessionCook, http.MethodPatch, "/api/v1/users/"+target.String(), promote(target, "super_admin")); code != http.StatusOK {
			t.Errorf("a super_admin cannot grant super_admin: %d %s", code, truncateBody(body))
		}
		if role := roleOf(target); role != "super_admin" {
			t.Errorf("the grant answered 200 and the role is %q", role)
		}
		var self uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN user_sessions s ON s.user_id=u.id WHERE s.token_hash=$1`, auth.HashToken(f.sessionCook)).Scan(&self); err != nil {
			t.Fatalf("find the fixture user: %v", err)
		}
		if code, _ := call(f.sessionCook, http.MethodPatch, "/api/v1/users/"+self.String(), promote(self, "viewer")); code == http.StatusOK {
			t.Error("a super_admin demoted itself, which can leave a deployment with no administrator")
		}
		if role := roleOf(self); role != "super_admin" {
			t.Errorf("the fixture administrator is now %q", role)
		}
	})
}

// A personal API key is for reading analytics from a script or a BI tool. It is
// never an administrator and never makes an interactive change, and that rests
// entirely on each route being wrapped in the middleware that refuses one. A route
// added without the wrapper would hand a key whatever its owner's role allows, and
// nothing would say so — the key would simply start working where it should not.
//
// Measured across every mutating route: 51 of 57 refuse a key outright, and the
// six that reach a handler are reads that use POST only because the question does
// not fit in a query string. That is the shape this holds in place.
func TestAPersonalAPIKeyCannotChangeAnything(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// The key belongs to the fixture's super administrator, so nothing here is
	// explained by a weak owner: if the confinement leaks, it leaks at full
	// authority.
	var owner uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN user_sessions s ON s.user_id=u.id WHERE s.token_hash=$1`, auth.HashToken(f.sessionCook)).Scan(&owner); err != nil {
		t.Fatalf("find the key owner: %v", err)
	}
	plain, hash, prefix, err := auth.NewToken("mom_key_", 32)
	if err != nil {
		t.Fatalf("mint a key: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO api_keys(user_id,name,key_hash,key_prefix) VALUES($1,'confinement probe',$2,$3)`, owner, hash, prefix); err != nil {
		t.Fatalf("store the key: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE key_hash=$1`, hash)
	})

	// Reads that have to be POSTed because the question is a document, not a query
	// string. Adding to this list is a decision to let keys reach a new handler,
	// which is the moment to check it changes nothing.
	readsSentAsPosts := map[string]bool{
		"POST /api/v1/query":                                     true,
		"POST /api/v1/funnel":                                    true,
		"POST /api/v1/sites/{siteID}/natural-query":              true,
		"POST /api/v1/sites/{siteID}/journeys/analyze":           true,
		"POST /api/v1/sites/{siteID}/workspace-journeys/analyze": true,
		"POST /api/v1/sites/{siteID}/event-contracts/validate":   true,
	}

	mux, ok := f.server.Handler().(*chi.Mux)
	if !ok {
		t.Fatalf("the handler is %T, not a chi router", f.server.Handler())
	}
	type route struct{ method, pattern string }
	routes := []route{}
	if err := chi.Walk(mux, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		pattern = strings.TrimSuffix(pattern, "/")
		switch method {
		case http.MethodGet, http.MethodOptions, http.MethodHead:
			return nil
		}
		// Signing in is how a session is obtained; it is not a change to the data.
		if !strings.HasPrefix(pattern, "/api/v1") || strings.HasPrefix(pattern, "/api/v1/auth") {
			return nil
		}
		routes = append(routes, route{method, pattern})
		return nil
	}); err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	if len(routes) < 40 {
		t.Fatalf("found only %d mutating routes, so this is no longer walking the router it thinks it is", len(routes))
	}

	unexpected := []string{}
	for _, r := range routes {
		path := strings.ReplaceAll(r.pattern, "{siteID}", f.siteKey)
		path = strings.ReplaceAll(path, "{id}", uuid.NewString())
		path = strings.ReplaceAll(path, "{key}", "privacy")
		path = strings.ReplaceAll(path, "{name}", "probe")
		request := httptest.NewRequest(r.method, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+plain)
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		refused := recorder.Code == http.StatusUnauthorized || recorder.Code == http.StatusForbidden
		operation := r.method + " " + r.pattern
		if refused || readsSentAsPosts[operation] {
			continue
		}
		unexpected = append(unexpected, fmt.Sprintf("%s answered %d", operation, recorder.Code))
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("a personal API key reached %d handlers that change something:\n  %s\n\nA key is for reading. If one of these is a read that has to be POSTed, add it to readsSentAsPosts after checking it writes nothing.",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}
	// The other direction: an entry that no longer exists is a stale exemption.
	served := map[string]bool{}
	for _, r := range routes {
		served[r.method+" "+r.pattern] = true
	}
	for operation := range readsSentAsPosts {
		if !served[operation] {
			t.Errorf("readsSentAsPosts exempts %q, which this server no longer serves", operation)
		}
	}
}
