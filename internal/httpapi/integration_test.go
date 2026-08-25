package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
			at := fmt.Sprintf("now()-interval '%d days'+interval '%d hours'", day, 9+index)
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
					properties = `{"value":"1000"}`
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
