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
	"strings"
	"testing"
	"time"

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
	visitorID   string
	userID      string
	segmentID   string
}

// seed builds a small but realistic site: two services in one workspace, an SSO user
// active on both, an anonymous visitor, ten weeks of daily activity, sessions,
// conversions, web vitals and errors.
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
	newSite := func(key, name string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
			VALUES($1,$2,$3,$3,$4,'mom_track_x',$5,'mom_server_x',ARRAY['portal.internal'],'Asia/Seoul') RETURNING id`, workspaceID, key, name, auth.HashToken(trackingKey), "server-"+key).Scan(&id); err != nil {
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

	// Daily rollups feed the anomaly baseline.
	run(`INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
		SELECT $1,(now()-make_interval(days=>d))::date,'prd',60,12,3,1000 FROM generate_series(1,70) d`, siteID)
	run(`INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,first_seen,last_seen,event_count,conversion_count)
		SELECT $1,(now()-make_interval(days=>d))::date,'prd',v,now(),now(),20,1 FROM generate_series(1,70) d, unnest(ARRAY['visitor-desktop','visitor-mobile','visitor-anon']) v`, siteID)
	run(`INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,user_id,first_seen,last_seen)
		SELECT $1,(now()-make_interval(days=>d))::date,'prd','ds-'||d||'-'||v,v,NULL,now(),now() FROM generate_series(1,70) d, unnest(ARRAY['visitor-desktop','visitor-mobile']) v`, siteID)

	// Events the reports read that nothing was creating, so those paths ran and
	// returned zero and every test passed. Each is seeded with a shape the
	// corresponding report's assertions can be written against.
	//
	//   refund          the ecommerce report's refund and net revenue
	//   user_engagement the engaged-time path, including its numeric guard
	//   resource_error  the experience report reads it alongside error
	//   ai_*            the whole AI operations report
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
	}

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
		sessionCook: token, trackingKey: trackingKey, visitorID: desktop, userID: person, segmentID: segmentID.String(),
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
// that lets a site turn on strict contracts. The tracker emits twenty-one events
// on its own; if those counted as unregistered, enabling reject mode would drop
// every batch that carried one, and each new automatic signal would break the
// sites that had transcribed the previous list.
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

	for _, name := range []string{"rage_click", "dead_click", "rapid_back", "form_retry", "repeated_search", "error_after_click", "slow_interaction", "search", "search_click", "search_refine", "page_view", "web_vital"} {
		if code := send(t, name); code != http.StatusAccepted {
			t.Fatalf("reject mode refused the automatic event %s with %d", name, code)
		}
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
