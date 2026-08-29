package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/insight"
	"github.com/hkjang/Momento/internal/segment"
	"github.com/hkjang/Momento/internal/service"
)

// The read paths are covered elsewhere. These tests drive the paths that write:
// alert state, sealed secrets and scheduled delivery. Each one builds SQL by hand
// with an upsert or a dynamic column, so none of it can be trusted until it runs.

func TestAnomalyAlertStateTransitionsThroughTheDatabase(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	reporter := insight.New(pool)
	evaluated := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)

	drop := insight.AnomalyReport{EvaluatedDate: evaluated, Detected: []insight.Anomaly{{
		Metric: "users", Label: "방문자", Severity: "critical", RobustZ: -6,
		Value: 10, Baseline: 900, Evidence: "근거", Action: "확인", Date: evaluated,
	}}}

	// First evaluation: the problem is new, so it is announced and recorded.
	announced, err := reporter.ApplyAnomalyState(ctx, f.siteID, "prd", drop, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(announced) != 1 || announced[0].State != "new" {
		t.Fatalf("first apply announced %+v, want one new anomaly", announced)
	}
	var severity string
	var notified, resolved *time.Time
	if err := pool.QueryRow(ctx, `SELECT severity,notified_on,resolved_on FROM anomaly_alerts WHERE site_id=$1 AND environment='prd' AND metric='users'`, f.siteID).
		Scan(&severity, &notified, &resolved); err != nil {
		t.Fatalf("stored alert: %v", err)
	}
	if severity != "critical" || notified == nil || resolved != nil {
		t.Fatalf("stored alert = %s notified=%v resolved=%v, want an open announced alert", severity, notified, resolved)
	}

	// Same day, same problem: nothing new to say.
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd", drop, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(announced) != 0 {
		t.Fatalf("the same open anomaly was announced again: %+v", announced)
	}

	// The next day it is still open, and the default states exclude "ongoing".
	nextDay := insight.AnomalyReport{EvaluatedDate: evaluated.AddDate(0, 0, 1), Detected: drop.Detected}
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd", nextDay, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("third apply: %v", err)
	}
	if len(announced) != 0 {
		t.Fatalf("an ongoing anomaly was announced by default: %+v", announced)
	}
	states, err := reporter.AnomalyStates(ctx, f.siteID, "prd")
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if open := states["users"]; open.DaysOpen != 2 {
		t.Fatalf("days open = %d, want 2", open.DaysOpen)
	}

	// Opting into ongoing alerts announces it once for the new day.
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd",
		insight.AnomalyReport{EvaluatedDate: evaluated.AddDate(0, 0, 2), Detected: drop.Detected},
		[]string{"new", "ongoing", "recovered"})
	if err != nil {
		t.Fatalf("opt-in apply: %v", err)
	}
	if len(announced) != 1 || announced[0].State != "ongoing" {
		t.Fatalf("opting into ongoing announced %+v", announced)
	}

	// Nothing detected: the alert recovers, once.
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd",
		insight.AnomalyReport{EvaluatedDate: evaluated.AddDate(0, 0, 3)}, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("recovery apply: %v", err)
	}
	if len(announced) != 1 || announced[0].State != "recovered" {
		t.Fatalf("recovery announced %+v", announced)
	}
	if err := pool.QueryRow(ctx, `SELECT resolved_on FROM anomaly_alerts WHERE site_id=$1 AND metric='users'`, f.siteID).Scan(&resolved); err != nil {
		t.Fatalf("resolved alert: %v", err)
	}
	if resolved == nil {
		t.Fatal("the alert was announced as recovered but not marked resolved")
	}
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd",
		insight.AnomalyReport{EvaluatedDate: evaluated.AddDate(0, 0, 4)}, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("second recovery apply: %v", err)
	}
	if len(announced) != 0 {
		t.Fatalf("recovery was announced twice: %+v", announced)
	}

	// The same problem returning is news again.
	announced, err = reporter.ApplyAnomalyState(ctx, f.siteID, "prd",
		insight.AnomalyReport{EvaluatedDate: evaluated.AddDate(0, 0, 5), Detected: drop.Detected}, insight.NotifiableStates())
	if err != nil {
		t.Fatalf("reopen apply: %v", err)
	}
	if len(announced) != 1 || announced[0].State != "new" {
		t.Fatalf("a returning anomaly announced %+v, want new", announced)
	}
}

func TestSecretLifecycleThroughTheAPI(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)

	created := f.do(t, http.MethodPost, "/api/v1/sites",
		`{"name":"보안 테스트","service_name":"보안","allowed_domains":["portal.internal"],"session_timeout_minutes":30,"timezone":"Asia/Seoul","engagement_threshold_seconds":10}`)
	siteUUID, _ := created["id"].(string)
	trackingKey, _ := created["tracking_key"].(string)
	if siteUUID == "" || trackingKey == "" {
		t.Fatalf("site creation did not return a key: %v", created)
	}
	if recoverable, _ := created["recoverable"].(bool); !recoverable {
		t.Fatalf("the key should be recoverable with an encryption key configured: %v", created)
	}

	// The key is sealed at rest, so the stored value must not be the key itself.
	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT tracking_key_secret FROM sites WHERE id=$1`, siteUUID).Scan(&stored); err != nil {
		t.Fatalf("stored secret: %v", err)
	}
	if stored == trackingKey || !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("the stored secret is not sealed: %q", stored)
	}

	revealed := f.do(t, http.MethodPost, "/api/v1/sites/"+siteUUID+"/reveal-keys", "")
	if revealed["tracking_key"] != trackingKey {
		t.Fatalf("reveal returned %v, want the issued key", revealed["tracking_key"])
	}
	if revealed["server_api_key"] == nil {
		t.Fatalf("reveal did not return the server key: %v", revealed)
	}

	rotated := f.do(t, http.MethodPost, "/api/v1/sites/"+siteUUID+"/rotate-key", "")
	newKey, _ := rotated["tracking_key"].(string)
	if newKey == "" || newKey == trackingKey {
		t.Fatalf("rotation returned %q", newKey)
	}
	afterRotation := f.do(t, http.MethodPost, "/api/v1/sites/"+siteUUID+"/reveal-keys", "")
	if afterRotation["tracking_key"] != newKey {
		t.Fatalf("reveal after rotation returned %v, want the rotated key", afterRotation["tracking_key"])
	}

	// A personal API key follows the same lifecycle.
	issued := f.do(t, http.MethodPost, "/api/v1/me/keys", `{"name":"integration","expires_in_days":30}`)
	keyID, _ := issued["id"].(string)
	personalKey, _ := issued["key"].(string)
	shown := f.do(t, http.MethodPost, "/api/v1/me/keys/"+keyID+"/reveal", "")
	if shown["available"] != true || shown["key"] != personalKey {
		t.Fatalf("personal key reveal = %v, want the issued key", shown)
	}

	// Re-sealing with the same primary key has nothing to do, and must not fail.
	rekeyed := f.do(t, http.MethodPost, "/api/v1/system/encryption/rekey", "")
	if failed, _ := rekeyed["failed"].(float64); failed != 0 {
		t.Fatalf("rekey reported %v failures: %v", failed, rekeyed)
	}
	status := f.get(t, "/api/v1/system/encryption")
	if status["enabled"] != true {
		t.Fatalf("encryption status = %v", status)
	}
	if recoverable, _ := status["recoverable_keys"].(float64); recoverable < 3 {
		t.Fatalf("recoverable keys = %v, want the site and personal keys counted", recoverable)
	}
	if pending, _ := status["pending_reseal"].(float64); pending != 0 {
		t.Fatalf("pending reseal = %v after a rekey, want 0", pending)
	}
}

func TestScheduledDeliveryThroughTheDatabase(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// A local endpoint stands in for the enterprise webhook.
	received := make(chan map[string]any, 4)
	var authorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	if _, err := pool.Exec(ctx, `UPDATE settings SET value=$1 WHERE key='automation'`,
		`{"enabled":true,"allowed_webhook_hosts":["127.0.0.1"],"delivery_timeout_seconds":10,"max_entity_ids":0}`); err != nil {
		t.Fatalf("enable automation: %v", err)
	}

	channel := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/delivery-channels",
		fmt.Sprintf(`{"name":"webhook","channel_type":"webhook","endpoint_url":"%s","headers":{"Authorization":"Bearer integration-token"},"active":true}`, target.URL))
	channelID, _ := channel["id"].(string)
	if channelID == "" {
		t.Fatalf("channel creation failed: %v", channel)
	}
	// The credential is sealed at rest and only the name is listed back.
	var sealed *string
	if err := pool.QueryRow(ctx, `SELECT headers_secret FROM delivery_channels WHERE id=$1`, channelID).Scan(&sealed); err != nil {
		t.Fatalf("stored headers: %v", err)
	}
	if sealed == nil || !strings.HasPrefix(*sealed, "enc:v1:") {
		t.Fatalf("channel headers are not sealed: %v", sealed)
	}
	listed := f.get(t, "/api/v1/sites/"+f.siteKey+"/delivery-channels")
	body, _ := json.Marshal(listed["list"])
	if strings.Contains(string(body), "integration-token") {
		t.Fatalf("the channel list leaked the credential: %s", body)
	}

	automation := service.Automation{DB: pool, Secrets: f.server.Secrets}

	// A visitor insight delivery sends the same report the console shows.
	insightReport := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"주간 인사이트","report_kind":"visitor_insight","interval_minutes":10080,"definition":{"environment":"prd","days":30},"enabled":true}`, channelID))
	insightID, _ := insightReport["id"].(string)
	if err := automation.RunByID(ctx, uuid.MustParse(insightID)); err != nil {
		t.Fatalf("visitor insight delivery: %v", err)
	}
	select {
	case payload := <-received:
		if payload["kind"] != "visitor_insight" {
			t.Fatalf("delivered payload kind = %v", payload["kind"])
		}
		data, _ := payload["data"].(map[string]any)
		if data["headline"] == nil {
			t.Fatalf("the delivered report has no headline: %v", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the webhook never received the insight delivery")
	}
	if authorization != "Bearer integration-token" {
		t.Fatalf("Authorization header = %q, want the sealed credential", authorization)
	}
	f.assertDeliveryStatus(t, insightID, "success")

	// An anomaly alert with nothing detected is a skip, not a failure.
	f.clearAnomalies(t)
	alert := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"이상 감지","report_kind":"anomaly","interval_minutes":60,"definition":{"environment":"prd","notify_on":["new","recovered"]},"enabled":true}`, channelID))
	alertID, _ := alert["id"].(string)
	if err := automation.RunByID(ctx, uuid.MustParse(alertID)); err != nil {
		t.Fatalf("anomaly delivery: %v", err)
	}
	f.assertDeliveryStatus(t, alertID, "skipped")

	// always_send delivers even with nothing detected, which proves the payload path.
	f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"id":"%s","channel_id":"%s","name":"이상 감지","report_kind":"anomaly","interval_minutes":60,"definition":{"environment":"prd","always_send":true},"enabled":true}`, alertID, channelID))
	if err := automation.RunByID(ctx, uuid.MustParse(alertID)); err != nil {
		t.Fatalf("always_send delivery: %v", err)
	}
	select {
	case payload := <-received:
		if payload["kind"] != "anomaly" {
			t.Fatalf("delivered payload kind = %v", payload["kind"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("always_send did not deliver")
	}
	f.assertDeliveryStatus(t, alertID, "success")

	// A host outside the allowlist must fail rather than be attempted.
	blocked := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/delivery-channels",
		`{"name":"blocked","channel_type":"webhook","endpoint_url":"http://example.invalid/hook","headers":{},"active":true}`)
	blockedChannel, _ := blocked["id"].(string)
	blockedReport := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"차단","report_kind":"overview","interval_minutes":1440,"definition":{"environment":"prd","days":7},"enabled":true}`, blockedChannel))
	blockedID, _ := blockedReport["id"].(string)
	if err := automation.RunByID(ctx, uuid.MustParse(blockedID)); err == nil {
		t.Fatal("a host outside the allowlist was delivered to")
	}
	f.assertDeliveryStatus(t, blockedID, "failed")
}

func (f fixture) assertDeliveryStatus(t *testing.T, reportID, want string) {
	t.Helper()
	var status string
	if err := f.server.DB.QueryRow(context.Background(), `SELECT status FROM delivery_runs WHERE report_id=$1 ORDER BY started_at DESC LIMIT 1`, reportID).Scan(&status); err != nil {
		t.Fatalf("delivery run for %s: %v", reportID, err)
	}
	if status != want {
		t.Fatalf("delivery status = %q, want %q", status, want)
	}
	var reportStatus string
	if err := f.server.DB.QueryRow(context.Background(), `SELECT last_status FROM scheduled_reports WHERE id=$1`, reportID).Scan(&reportStatus); err != nil {
		t.Fatalf("schedule status: %v", err)
	}
	if reportStatus != want {
		t.Fatalf("schedule last_status = %q, want %q", reportStatus, want)
	}
}

// clearAnomalies removes any alert rows so a delivery test starts from a known state.
func (f fixture) clearAnomalies(t *testing.T) {
	t.Helper()
	if _, err := f.server.DB.Exec(context.Background(), `DELETE FROM anomaly_alerts WHERE site_id=$1`, f.siteID); err != nil {
		t.Fatalf("clear anomalies: %v", err)
	}
}

// TestBehaviouralSegmentEvaluates runs the entity aggregate compiler against real
// data, which the unit test can only check as generated SQL.
//
// It used to require the segment to match nobody, because everybody in the
// fixture converted. That showed the compiler was not silently matching everyone
// and nothing else — a segment that always matched nobody would have passed it
// just as well. The fixture now has one person who hits friction and converts
// nothing, so the segment can be held to selecting them and excluding the people
// who did convert.
func TestBehaviouralSegmentEvaluates(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	var segmentID string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM segments WHERE site_id=$1 AND name='반복 미전환'`, f.siteID).Scan(&segmentID); err != nil {
		t.Fatalf("behavioural segment: %v", err)
	}
	from, today := f.siteDates(t, 30)
	body := fmt.Sprintf(`{"site_id":"%s","environment":"prd","date_range":{"from":"%s","to":"%s"},
		"dimensions":["event.name"],"metrics":["users","events"],"filters":[],"segment_id":"%s","limit":10}`,
		f.siteKey, from, today, segmentID)
	response := f.do(t, http.MethodPost, "/api/v1/query", body)
	if response["rows"] == nil {
		t.Fatalf("behavioural segment query returned no rows key: %v", response)
	}
	rows, _ := response["rows"].([]any)
	if len(rows) == 0 {
		t.Fatal("the never-converted segment matched nobody: the fixture has somebody who visits repeatedly and converts nothing, so an empty answer means the aggregate is not selecting on what it says it does")
	}
	// The compiler must exclude everyone who converted. The people who do carry
	// the purchases, so a purchase reaching this answer is the segment matching
	// somebody it defines itself by not matching.
	for _, row := range rows {
		item, _ := row.(map[string]any)
		if item == nil {
			continue
		}
		if fmt.Sprint(item["event.name"]) == "purchase" {
			t.Errorf("the never-converted segment answered a purchase row: %v", item)
		}
		if users := toNumber(item["users"]); users != 1 {
			t.Errorf("%v matched %v people, and one person in the fixture converts nothing", item["event.name"], users)
		}
	}
}

// TestValidatorsAgreeWithDatabaseConstraints closes the gap that let the visitor
// insight and anomaly report kinds be accepted by the API and rejected by the
// database. Any value the service considers valid has to survive the constraint.
func TestValidatorsAgreeWithDatabaseConstraints(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	allowed := func(table, column string) map[string]bool {
		t.Helper()
		var definition string
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
			WHERE c.conrelid=$1::regclass AND c.contype='c' AND c.conname=$2`, table, table+"_"+column+"_check").Scan(&definition); err != nil {
			t.Fatalf("constraint for %s.%s: %v", table, column, err)
		}
		values := map[string]bool{}
		for _, part := range strings.Split(definition, "'") {
			if part != "" && !strings.ContainsAny(part, "(),= ") {
				values[part] = true
			}
		}
		if len(values) == 0 {
			t.Fatalf("could not read the allowed values from %q", definition)
		}
		return values
	}

	reportKinds := allowed("scheduled_reports", "report_kind")
	for _, kind := range []string{"overview", "adoption", "experience", "ai", "segment", "insights", "visitor_insight", "anomaly"} {
		if !validReportKind(kind) {
			t.Fatalf("the service rejects the report kind %q", kind)
		}
		if !reportKinds[kind] {
			t.Fatalf("the database rejects the report kind %q that the service accepts", kind)
		}
	}

	channelTypes := allowed("delivery_channels", "channel_type")
	for _, kind := range []string{"webhook", "confluence", "mail", "internal_message", "ai_agent"} {
		if !validateChannelType(kind) {
			t.Fatalf("the service rejects the channel type %q", kind)
		}
		if !channelTypes[kind] {
			t.Fatalf("the database rejects the channel type %q that the service accepts", kind)
		}
	}

	deliveryStatuses := allowed("delivery_runs", "status")
	// The skipped outcome exists so an alert with nothing to report is not a failure.
	for _, status := range []string{"success", "failed", "skipped"} {
		if !deliveryStatuses[status] {
			t.Fatalf("the database rejects the delivery status %q the service writes", status)
		}
	}
}

// TestBehaviouralSegmentMatchesTheReferenceSemantics pins the rewrite of the
// behavioural aggregates. They used to compile to a correlated subquery
// evaluated once per candidate row, which no site of real size could run:
// analytics_events.entity_id comes from a join with the identity table, so it
// cannot be indexed and every evaluation scanned the site. The compiled form is
// now a semi-join evaluated once, and this test runs both against the same data
// to show they select the same rows.
func TestBehaviouralSegmentMatchesTheReferenceSemantics(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	resolver, err := (&Server{DB: pool}).newDimensionResolver(ctx, f.siteID, "prd")
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	cases := []struct {
		field    string
		operator string
		value    float64
		// reference is the aggregate the correlated form used, written out so the
		// comparison is against the semantics rather than against the new SQL.
		reference string
	}{
		{"entity.sessions", ">=", 2, "count(DISTINCT segment_entity.session_id)"},
		{"entity.events", ">=", 5, "count(*)"},
		{"entity.conversions", "=", 0, "count(*) FILTER(WHERE segment_entity.is_conversion)"},
		{"entity.frustration_signals", ">=", 1, "count(*) FILTER(WHERE segment_entity.event_name = ANY('{rage_click,dead_click,rapid_back,form_retry,repeated_search,error_after_click,slow_interaction,error,resource_error}'))"},
		{"entity.searches", "=", 0, "count(*) FILTER(WHERE segment_entity.event_name='search')"},
	}
	for _, tc := range cases {
		t.Run(tc.field+tc.operator, func(t *testing.T) {
			args := []any{f.siteID, "prd"}
			condition, err := compileSegment(segmentNode{Field: tc.field, Operator: tc.operator, Value: tc.value}, resolver, "e", &args, 0)
			if err != nil {
				t.Fatalf("compile %s: %v", tc.field, err)
			}
			// compileSegment binds its own scope, so the value it compared against
			// is the last argument.
			valuePlaceholder := "$" + strconv.Itoa(len(args))
			compiled := "SELECT e.event_id FROM analytics_events e WHERE e.site_id=$1 AND e.environment=$2 AND " + condition
			operator := tc.operator
			if operator == "!=" {
				operator = "<>"
			}
			reference := "SELECT e.event_id FROM analytics_events e WHERE e.site_id=$1 AND e.environment=$2 AND coalesce((SELECT " +
				tc.reference + " FROM analytics_events segment_entity WHERE segment_entity.site_id=e.site_id" +
				" AND segment_entity.environment=e.environment AND segment_entity.entity_id=e.entity_id),0) " +
				operator + " " + valuePlaceholder

			var compiledCount, referenceCount, onlyCompiled, onlyReference int64
			query := "WITH compiled AS (" + compiled + "), reference AS (" + reference + `)
				SELECT (SELECT count(*) FROM compiled),(SELECT count(*) FROM reference),
					(SELECT count(*) FROM (SELECT * FROM compiled EXCEPT ALL SELECT * FROM reference) a),
					(SELECT count(*) FROM (SELECT * FROM reference EXCEPT ALL SELECT * FROM compiled) b)`
			if err := pool.QueryRow(ctx, query, args...).Scan(&compiledCount, &referenceCount, &onlyCompiled, &onlyReference); err != nil {
				t.Fatalf("compare %s: %v", tc.field, err)
			}
			if onlyCompiled != 0 || onlyReference != 0 {
				t.Fatalf("%s %s %v selects a different set: %d only in the compiled form, %d only in the reference (compiled %d, reference %d)",
					tc.field, tc.operator, tc.value, onlyCompiled, onlyReference, compiledCount, referenceCount)
			}
			if compiledCount == 0 {
				t.Skipf("the fixture has nobody matching %s %s %v, so the comparison proves nothing", tc.field, tc.operator, tc.value)
			}
		})
	}
}

// TestBehaviouralSegmentRefusesToRunWithoutAScope covers the failure mode the
// rewrite introduces: the aggregate is scoped by parameters now, so a resolver
// built without a site would silently measure the wrong population.
func TestBehaviouralSegmentRefusesToRunWithoutAScope(t *testing.T) {
	t.Parallel()
	args := []any{}
	_, err := compileSegment(segmentNode{Field: "entity.sessions", Operator: ">=", Value: 2.0},
		segment.ResolverFor(uuid.Nil, "", nil), "e", &args, 0)
	if err == nil {
		t.Fatal("an unscoped resolver compiled a behavioural aggregate instead of failing")
	}
	if !strings.Contains(err.Error(), "site") {
		t.Fatalf("the error does not say what is missing: %v", err)
	}
}

// TestIdentityRebuildMatchesTheReferenceSemantics pins the rewrite of the
// identity rebuild. The previous form joined raw_events back to a per-visitor
// CTE, which the planner answered with a merge join over the whole table in
// visitor order — two million random heap fetches on a mid-sized site, inside a
// transaction that stays open. Both forms are run here against the same data so
// the faster one is shown to derive the same links.
func TestIdentityRebuildMatchesTheReferenceSemantics(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	const reference = `WITH latest_identity AS (
		SELECT DISTINCT ON(site_id,visitor_id) site_id,visitor_id,user_id
		FROM raw_events WHERE site_id=$1 AND user_id IS NOT NULL
		ORDER BY site_id,visitor_id,event_timestamp DESC,id DESC
	)
	SELECT e.site_id,e.visitor_id,i.user_id,min(e.event_timestamp),
		min(e.event_timestamp) FILTER(WHERE e.user_id=i.user_id),max(e.event_timestamp)
	FROM raw_events e JOIN latest_identity i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
	WHERE e.site_id=$1 GROUP BY e.site_id,e.visitor_id,i.user_id`

	// The shipped statement is an INSERT, so its SELECT is compared by running the
	// rebuild and reading back what it wrote.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := service.RebuildSiteDerivedData(ctx, tx, f.siteID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	type link struct {
		visitor, user       string
		first, linked, last time.Time
	}
	read := func(query string) map[string]link {
		t.Helper()
		rows, err := tx.Query(ctx, query, f.siteID)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		out := map[string]link{}
		for rows.Next() {
			var site uuid.UUID
			var item link
			if err := rows.Scan(&site, &item.visitor, &item.user, &item.first, &item.linked, &item.last); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out[item.visitor] = item
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		return out
	}

	expected := read(reference)
	actual := read(`SELECT site_id,visitor_id,user_id,first_seen,linked_at,last_seen FROM visitor_identities WHERE site_id=$1`)
	if len(expected) == 0 {
		t.Fatal("the fixture produced no identity links, so the comparison proves nothing")
	}
	if len(actual) != len(expected) {
		t.Fatalf("the rebuild wrote %d links, the reference derives %d", len(actual), len(expected))
	}
	for visitor, want := range expected {
		got, ok := actual[visitor]
		if !ok {
			t.Errorf("%s is missing from the rebuild", visitor)
			continue
		}
		if got.user != want.user {
			t.Errorf("%s linked to %q, want %q", visitor, got.user, want.user)
		}
		for name, pair := range map[string][2]time.Time{
			"first_seen": {got.first, want.first},
			"linked_at":  {got.linked, want.linked},
			"last_seen":  {got.last, want.last},
		} {
			if !pair[0].Equal(pair[1]) {
				t.Errorf("%s %s = %s, want %s", visitor, name, pair[0], pair[1])
			}
		}
	}
}

// TestQueryPolicyLimitsEveryReportNotJustTheQueryBuilder covers a governance gap.
// A site's query policy caps how far back an interactive read may go, and the
// administration screen presents that as a limit in force. It was consulted by
// one handler — the query builder — while every report screen read whatever
// range it was asked for, and those are the heavy ones the limit exists for.
// rangedReports is every site report that accepts from and to. The policy refuses
// a range beyond its limit, and a report missing from this list is one nobody
// checks.
var rangedReports = []string{
	"overview", "events", "pages", "sessions", "visitors", "visitor-insights",
	"frustration", "search-analytics", "experience", "ecommerce", "adoption",
	"feature-intelligence", "insights", "ai-analytics", "cohort", "path",
	"attribution", "data-quality", "usage", "workspace-rollup", "annotations",
	"export",
}

func TestQueryPolicyLimitsEveryReportNotJustTheQueryBuilder(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO query_policies(site_id,max_exact_days,max_complexity_score,background_threshold,fast_sample_percent,preview_sample_percent)
		VALUES($1,14,90,60,10,1) ON CONFLICT(site_id) DO UPDATE SET max_exact_days=excluded.max_exact_days`, f.siteID); err != nil {
		t.Fatalf("set the policy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM query_policies WHERE site_id=$1`, f.siteID)
	})

	site := "/api/v1/sites/" + f.siteKey
	within, today := f.siteDates(t, 10)
	beyond := f.siteDate(t, -60)

	// A representative spread was the old approach: seven reports out of the
	// twenty-two that take a range. They all share one helper, so a spread reads
	// like enough — until a new report calls the query directly and nothing here
	// would notice. Every ranged report is listed, and TestEveryRangedReportIsUnderThePolicy
	// fails when the source grows one that is not.
	for _, report := range rangedReports {
		request := httptest.NewRequest(http.MethodGet, site+"/"+report+"?from="+beyond+"&to="+today, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d for a 60 day range under a 14 day policy, want 400", report, recorder.Code)
			continue
		}
		if !strings.Contains(recorder.Body.String(), "RANGE_EXCEEDS_POLICY") {
			t.Errorf("%s refused the range without saying the policy is why: %s", report, truncateBody(recorder.Body.String()))
		}
		// The refusal has to name the limit, or the reader cannot pick a range that
		// will work.
		if !strings.Contains(recorder.Body.String(), "14") {
			t.Errorf("%s did not tell the reader what the limit is: %s", report, truncateBody(recorder.Body.String()))
		}
	}

	// A range inside the limit still answers.
	for _, report := range []string{"overview", "frustration"} {
		f.get(t, site+"/"+report+"?from="+within+"&to="+today)
	}

	// The funnel and the MCP tools build their window from an explicit range
	// rather than the report helper, and they kept reading without a limit when it
	// was added there. Both are checked here because "the limit applies" has to
	// mean everywhere a period is accepted.
	funnel := fmt.Sprintf(`{"site_id":%q,"environment":"prd","from":%q,"to":%q,"mode":"closed",
		"steps":[{"name":"진입","event":"page_view"},{"name":"구매","event":"purchase"}]}`, f.siteKey, beyond, today)
	funnelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/funnel", strings.NewReader(funnel))
	funnelRequest.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	funnelRequest.Header.Set("Content-Type", "application/json")
	funnelRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(funnelRecorder, funnelRequest)
	if funnelRecorder.Code != http.StatusBadRequest || !strings.Contains(funnelRecorder.Body.String(), "RANGE_EXCEEDS_POLICY") {
		t.Errorf("the funnel answered %d for a 60 day range under a 14 day policy: %s", funnelRecorder.Code, truncateBody(funnelRecorder.Body.String()))
	}

	mcp := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_metrics","arguments":{"site_id":%q,"from":%q,"to":%q}}}`, f.siteKey, beyond, today)
	mcpRequest := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(mcp))
	mcpRequest.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	mcpRequest.Header.Set("Content-Type", "application/json")
	mcpRecorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(mcpRecorder, mcpRequest)
	// MCP answers errors inside a successful envelope, so the body is what matters.
	if !strings.Contains(mcpRecorder.Body.String(), "14") {
		t.Errorf("the MCP tool did not refuse a 60 day range under a 14 day policy: %s", truncateBody(mcpRecorder.Body.String()))
	}

	// A deletion still reaches as far back as the data goes: a reporting limit
	// must not block a compliance obligation.
	deletion := f.do(t, http.MethodPost, "/api/v1/privacy/delete", fmt.Sprintf(
		`{"site_id":%q,"mode":"period","from":%q,"to":%q,"confirm":"DELETE"}`, f.siteKey, beyond, today))
	if deletion == nil {
		t.Error("a period deletion wider than the reporting limit was refused")
	}

	// The limit travels to the console with the site, so the period control can
	// offer only what will be accepted.
	sites := f.do(t, http.MethodGet, "/api/v1/sites", "")
	list, _ := sites["list"].([]any)
	found := false
	for _, item := range list {
		row, _ := item.(map[string]any)
		if fmt.Sprint(row["site_id"]) != f.siteKey {
			continue
		}
		found = true
		if limit, _ := row["max_exact_days"].(float64); limit != 14 {
			t.Errorf("the site reports max_exact_days = %v, want the policy's 14", row["max_exact_days"])
		}
	}
	if !found {
		t.Fatalf("the seeded site is missing from the site list: %v", sites)
	}
}

// TestDeliveredNumbersMatchTheScreenTheyAreNamedAfter pins the period a
// scheduled report covers. The screens read the site's calendar and end at local
// midnight; a delivery measured from the moment the schedule happened to fire,
// so a digest called the overview covered a different span than the overview and
// the span moved with the send time. It also omitted sessions entirely.
func TestDeliveredNumbersMatchTheScreenTheyAreNamedAfter(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	received := make(chan map[string]any, 2)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	if _, err := pool.Exec(ctx, `UPDATE settings SET value=$1 WHERE key='automation'`,
		`{"enabled":true,"allowed_webhook_hosts":["127.0.0.1"],"delivery_timeout_seconds":10,"max_entity_ids":0}`); err != nil {
		t.Fatalf("enable automation: %v", err)
	}
	channel := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/delivery-channels",
		fmt.Sprintf(`{"name":"period-webhook","channel_type":"webhook","endpoint_url":"%s","active":true}`, target.URL))
	channelID, _ := channel["id"].(string)
	if channelID == "" {
		t.Fatalf("channel creation failed: %v", channel)
	}

	// Thirty days is the overview's own default, so the two windows have to line up.
	const days = 30
	report := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"요약","report_kind":"overview","interval_minutes":1440,"definition":{"environment":"prd","days":%d},"enabled":true}`, channelID, days))
	reportID, _ := report["id"].(string)
	if err := (service.Automation{DB: pool, Secrets: f.server.Secrets}).RunByID(ctx, uuid.MustParse(reportID)); err != nil {
		t.Fatalf("overview delivery: %v", err)
	}

	var delivered map[string]any
	select {
	case payload := <-received:
		delivered, _ = payload["data"].(map[string]any)
	case <-time.After(10 * time.Second):
		t.Fatal("the webhook never received the overview delivery")
	}
	if delivered == nil {
		t.Fatal("the delivered payload carried no data")
	}

	// The overview is asked with no dates so it uses its own default window, which
	// is the rule a thirty day digest is supposed to mirror: thirty local days
	// ending at the coming local midnight. Passing dates here would compare the
	// digest against a window the test invented rather than the one the screen uses.
	overview := f.get(t, "/api/v1/sites/"+f.siteKey+"/overview")
	current, _ := overview["current"].(map[string]any)
	// The digest nests the period's figures under "current" beside "previous" and
	// the change between them, the way the screen does. It used to be a flat bag
	// of totals with nothing to compare them against.
	deliveredCurrent, _ := delivered["current"].(map[string]any)
	if deliveredCurrent == nil {
		t.Fatalf("the delivered digest carries no current period: %v", delivered)
	}

	for _, key := range []string{"users", "sessions", "events", "conversions", "revenue"} {
		want, ok := current[key].(float64)
		if !ok {
			t.Errorf("the overview does not report %s", key)
			continue
		}
		got, ok := deliveredCurrent[key].(float64)
		if !ok {
			t.Errorf("the delivered digest does not report %s: %v", key, delivered)
			continue
		}
		if got != want {
			t.Errorf("%s is %v in the delivered digest and %v on the overview: a digest named after a screen has to cover the same period and count the same way",
				key, got, want)
		}
	}
	// The counts alone are a weak check: whether they differ depends on whether any
	// event happens to fall in the band where the two windows disagree, which for
	// this fixture depends on the hour the test runs. The window itself is
	// compared directly, and it travels with the payload so a reader can tell what
	// was measured without reconstructing it.
	for _, key := range []string{"from", "to"} {
		want := fmt.Sprint(overview[key])
		got := fmt.Sprint(delivered[key])
		if want == "" || want == "<nil>" {
			t.Errorf("the overview does not report its %s", key)
			continue
		}
		if got != want {
			t.Errorf("the digest covered %s=%v while the overview reports %v: a digest named after a screen has to measure the same period",
				key, got, want)
		}
	}
}

// TestAdoptionDigestCarriesTheAdoptionReport pins what a digest named after a
// screen contains. The adoption digest ran its own query and answered with
// feature events and users — the feature intelligence report's content under the
// adoption report's name, with no adoption rate in it, so a schedule called
// "Adoption 요약" delivered no adoption.
func TestAdoptionDigestCarriesTheAdoptionReport(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	received := make(chan map[string]any, 2)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	if _, err := pool.Exec(ctx, `UPDATE settings SET value=$1 WHERE key='automation'`,
		`{"enabled":true,"allowed_webhook_hosts":["127.0.0.1"],"delivery_timeout_seconds":10,"max_entity_ids":0}`); err != nil {
		t.Fatalf("enable automation: %v", err)
	}
	channel := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/delivery-channels",
		fmt.Sprintf(`{"name":"adoption-webhook","channel_type":"webhook","endpoint_url":"%s","active":true}`, target.URL))
	channelID, _ := channel["id"].(string)
	report := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"Adoption 요약","report_kind":"adoption","interval_minutes":1440,"definition":{"environment":"prd","days":30},"enabled":true}`, channelID))
	reportID, _ := report["id"].(string)
	if err := (service.Automation{DB: pool, Secrets: f.server.Secrets}).RunByID(ctx, uuid.MustParse(reportID)); err != nil {
		t.Fatalf("adoption delivery: %v", err)
	}

	var delivered []any
	select {
	case payload := <-received:
		data, _ := payload["data"].(map[string]any)
		delivered, _ = data["features"].([]any)
	case <-time.After(10 * time.Second):
		t.Fatal("the webhook never received the adoption delivery")
	}
	if len(delivered) == 0 {
		t.Fatal("the adoption digest carried no rows")
	}

	screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/adoption")
	rows, _ := screen["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the adoption screen returned no rows: %v", screen)
	}

	// Every field the screen shows for a row has to be in the delivered row, with
	// the same value. Comparing the first row is enough to catch a digest that is
	// computing something else entirely, which is what this test exists for.
	want, _ := rows[0].(map[string]any)
	got, _ := delivered[0].(map[string]any)
	for _, key := range []string{"feature", "department", "organization", "users", "events", "eligible_users", "adoption_rate", "repeat_usage_rate", "dormant_users"} {
		if got[key] == nil {
			t.Errorf("the delivered adoption row has no %s: %v", key, got)
			continue
		}
		if fmt.Sprint(got[key]) != fmt.Sprint(want[key]) {
			t.Errorf("%s is %v in the digest and %v on the screen", key, got[key], want[key])
		}
	}
	if len(delivered) != len(rows) && len(rows) <= 50 {
		t.Errorf("the digest carried %d rows and the screen shows %d", len(delivered), len(rows))
	}
}

// TestAdoptionAgreesAcrossScreenDigestAndTool covers the third place the same
// defect lived. The adoption screen, the scheduled digest and the MCP tool each
// ran their own query; two of them answered with feature events and users, which
// is the feature intelligence report, so a reader and an agent asking about
// adoption got no adoption rate at all.
func TestAdoptionAgreesAcrossScreenDigestAndTool(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)

	screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/adoption?from="+from+"&to="+today)
	rows, _ := screen["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("the adoption screen returned no rows: %v", screen)
	}
	want, _ := rows[0].(map[string]any)

	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"analyze_feature_adoption","arguments":{"site_id":%q,"from":%q,"to":%q}}}`,
		f.siteKey, from, today)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)

	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode the MCP envelope: %v", err)
	}
	result, _ := envelope["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("the tool returned no content: %s", truncateBody(recorder.Body.String()))
	}
	item, _ := content[0].(map[string]any)
	var tool []map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(item["text"])), &tool); err != nil {
		t.Fatalf("decode the tool payload: %v (%v)", err, item["text"])
	}
	if len(tool) == 0 {
		t.Fatal("the tool returned no adoption rows")
	}

	// An agent asking about adoption has to receive the numbers that make it
	// adoption, not a feature event list.
	for _, key := range []string{"feature", "department", "organization", "users", "events", "eligible_users", "adoption_rate", "repeat_usage_rate", "dormant_users"} {
		got, ok := tool[0][key]
		if !ok || got == nil {
			t.Errorf("the tool's adoption row has no %s: %v", key, tool[0])
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want[key]) {
			t.Errorf("%s is %v from the tool and %v on the screen", key, got, want[key])
		}
	}
}

// TestSearchNumbersAreCorrectAndAgreeAcrossPaths does two things the suite could
// not do before: it checks the search report's arithmetic against known inputs,
// and it checks that the screen and the MCP tool answer the same.
//
// Nothing in the suite had ever verified a search number, and the screen and the
// tool each own their own copy of the query — the arrangement that let the same
// defect ship three times in the adoption report. The copies agree today and a
// test is what keeps them agreeing.
//
// The numbers are read as a difference rather than as a total. This test used to
// assert the site's absolute counts, which only worked because the fixture had no
// searches of its own — so it was pinned to an absence, and the absence was the
// reason nothing about search was being exercised anywhere else. Measuring the
// change this test causes says the same thing about the report and stops the
// fixture from having to stay empty for it.
func TestSearchNumbersAreCorrectAndAgreeAcrossPaths(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 7)

	// What the site already reports, so what follows is the difference these
	// searches make rather than the whole of what the report can see.
	before, _ := f.get(t, "/api/v1/sites/"+f.siteKey+"/search-analytics?from="+from+"&to="+today)["summary"].(map[string]any)
	if before == nil {
		t.Fatal("the search report has no summary before anything is seeded")
	}

	// Five people search once and find results, one of them searches again and
	// finds nothing, and one clicks a result. Every number below follows from that.
	base := time.Now().Add(-3 * time.Hour)
	insert := func(visitor, event, properties string, offset int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd',$3,$4,$5,$6,$6,$7::jsonb,false,'https://portal.internal/search')`,
			uuid.NewString(), f.siteID, visitor, visitor+"-session", event, base.Add(time.Duration(offset)*time.Minute), properties); err != nil {
			t.Fatalf("seed %s: %v", event, err)
		}
	}
	for i := 0; i < 5; i++ {
		insert(fmt.Sprintf("search-visitor-%d", i), "search", `{"query":"연차","result_count":4}`, i)
	}
	insert("search-visitor-0", "search", `{"query":"없는말","result_count":0}`, 10)
	insert("search-visitor-1", "search_click", `{"query":"연차","position":1}`, 11)
	// The report also counts refinements, exits and successes, and nothing was
	// producing those event names, so those three figures had never been anything
	// but zero.
	insert("search-visitor-2", "search_refine", `{"query":"연차 신청","previous_query":"연차"}`, 12)
	insert("search-visitor-3", "search_exit", `{"query":"연차"}`, 13)
	insert("search-visitor-4", "search_success", `{"query":"연차"}`, 14)

	screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/search-analytics?from="+from+"&to="+today)
	summary, _ := screen["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("the search report has no summary: %v", screen)
	}
	for _, expected := range []struct {
		key   string
		value float64
	}{
		{"searches", 6},
		{"users", 5},
		{"zero_results", 1},
		{"clicks", 1},
		// These three had never been anything but zero, for want of the event
		// rather than want of the behaviour.
		{"refinements", 1},
		{"exits", 1},
		{"successes", 1},
	} {
		change := toNumber(summary[expected.key]) - toNumber(before[expected.key])
		if change != expected.value {
			t.Errorf("%s went from %v to %v, a change of %v, and the seeded searches add %v",
				expected.key, before[expected.key], summary[expected.key], change, expected.value)
		}
	}

	tool := f.callMCP(t, "analyze_search", fmt.Sprintf(`"from":%q,"to":%q`, from, today))
	var toolSummary map[string]any
	if err := json.Unmarshal([]byte(tool), &toolSummary); err != nil {
		t.Fatalf("decode the tool payload: %v (%s)", err, truncateBody(tool))
	}
	for _, key := range []string{"searches", "users", "zero_results", "clicks", "search_ctr", "zero_result_rate", "successes", "success_rate"} {
		if fmt.Sprint(toolSummary[key]) != fmt.Sprint(summary[key]) {
			t.Errorf("%s is %v from the tool and %v on the screen", key, toolSummary[key], summary[key])
		}
	}
}

// The tracking debugger is the screen an operator opens to watch events arrive.
//
// Its scan read network_name into a string, and network_name is null for
// anything from outside a named internal network — which is most events. The
// scan failed on every one of those rows, and the failure was dropped by the
// helper that built the list, so the screen omitted them. With no internal
// networks configured it showed nothing at all, which is exactly what "nothing
// is arriving" looks like: the one conclusion this screen exists to rule out.
func TestTheTrackingDebuggerShowsEventsWithNoNetwork(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var withoutNetwork int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND network_name IS NULL`, f.siteID).Scan(&withoutNetwork); err != nil {
		t.Fatalf("count events with no network: %v", err)
	}
	if withoutNetwork == 0 {
		t.Fatal("every seeded event carries a network, so this cannot tell a screen that drops them from one that does not")
	}

	rows, _ := f.get(t, "/api/v1/tracking-debugger?site_id="+f.siteKey)["events"].([]any)
	if len(rows) == 0 {
		t.Fatal("the debugger shows nothing, which is what an operator reads as nothing arriving")
	}
	seen := 0
	for _, row := range rows {
		event, _ := row.(map[string]any)
		if event == nil {
			continue
		}
		if event["network"] == nil {
			seen++
		}
	}
	if seen == 0 {
		t.Errorf("the debugger listed %d events and none of them has a null network, while the site holds %d such events: they are being dropped",
			len(rows), withoutNetwork)
	}
}

// callMCP invokes one tool and returns its text payload, which is where every
// tool puts its answer.
func (f fixture) callMCP(t *testing.T, tool, arguments string) string {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{"site_id":%q,%s}}}`,
		tool, f.siteKey, arguments)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("%s returned an undecodable envelope: %s", tool, truncateBody(recorder.Body.String()))
	}
	result, _ := envelope["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("%s returned no content: %s", tool, truncateBody(recorder.Body.String()))
	}
	item, _ := content[0].(map[string]any)
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("%s answered with an error: %v", tool, item["text"])
	}
	return fmt.Sprint(item["text"])
}

// Every report builds its own SQL. When two of them answer the same question
// differently, an operator with both screens open stops trusting either, and
// there is no error anywhere to find — that is how the average session duration
// came to read 960 on one screen and 720 on another.
//
// This asks one question through every screen that can answer it and compares.
// The fixture alone cannot do that: it reports 90 sessions, 90 page views and 90
// conversions, so a report returning the wrong one of those would look like
// agreement. A batch is ingested first to pull them apart — 7 page views, 2
// purchases and one add to cart in a single new session — so each number is
// distinct before anything is compared.
func TestTheScreensAgreeOnTheSameQuestion(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey
	rng := fmt.Sprintf("?from=%s&to=%s", from, today)

	now := time.Now().UnixMilli()
	parts := []string{}
	for index := 0; index < 7; index++ {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":"page_view","timestamp":%d,"contract_version":1}`, uuid.NewString(), now-int64(index*1000)))
	}
	for index := 0; index < 2; index++ {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":"purchase","timestamp":%d,"properties":{"value":1500},"contract_version":1}`, uuid.NewString(), now-int64(index*500)))
	}
	parts = append(parts, fmt.Sprintf(`{"id":%q,"name":"add_to_cart","timestamp":%d,"contract_version":1}`, uuid.NewString(), now))
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":"agreement-visitor","session_id":"agreement-session",
		"context":{"page":{"url":"https://portal.internal/agree","title":"Agree"},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"google","medium":"organic"}},
		"events":[%s]}`, f.siteKey, f.trackingKey, strings.Join(parts, ","))
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("ingest the distinguishing batch: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("process the batch: %v", err)
	}

	number := func(v any) float64 { n, _ := v.(float64); return n }
	overview := f.get(t, site+"/overview"+rng)
	current, _ := overview["current"].(map[string]any)
	rowsOf := func(path string) []any {
		body := f.get(t, path)
		if list, ok := body["list"].([]any); ok {
			return list
		}
		return nil
	}
	sessions := rowsOf(site + "/sessions" + rng)
	pages := rowsOf(site + "/pages" + rng)
	events := rowsOf(site + "/events" + rng)
	sum := func(rows []any, key string) float64 {
		total := 0.0
		for _, row := range rows {
			r, _ := row.(map[string]any)
			total += number(r[key])
		}
		return total
	}
	eventRow := func(name string) map[string]any {
		for _, row := range events {
			r, _ := row.(map[string]any)
			if fmt.Sprint(r["event"]) == name {
				return r
			}
		}
		return map[string]any{}
	}
	compared := 0
	agree := func(question string, left float64, leftName string, right float64, rightName string) {
		t.Helper()
		compared++
		if left != right {
			t.Errorf("%s: %s says %.0f, %s says %.0f — an operator with both screens open cannot tell which is wrong",
				question, leftName, left, rightName, right)
		}
	}

	// The fixture's own numbers have to differ from each other, or agreeing proves
	// nothing about which number each screen returned.
	distinct := map[float64]string{}
	for name, value := range map[string]float64{
		"sessions":    number(current["sessions"]),
		"page_views":  number(current["page_views"]),
		"conversions": number(current["conversions"]),
		"events":      number(current["events"]),
	} {
		if other, clash := distinct[value]; clash {
			t.Fatalf("%s and %s are both %.0f, so this test cannot tell them apart", name, other, value)
		}
		distinct[value] = name
	}

	agree("sessions", number(current["sessions"]), "the overview", float64(len(sessions)), "the sessions report")
	agree("page views", number(current["page_views"]), "the overview", sum(pages, "views"), "the pages report")
	agree("events", number(current["events"]), "the overview", sum(events, "count"), "the events report")
	agree("conversions", number(current["conversions"]), "the overview", sum(events, "conversions"), "the events report")

	built := f.do(t, http.MethodPost, "/api/v1/query", fmt.Sprintf(
		`{"site_id":%q,"environment":"prd","date_range":{"from":%q,"to":%q},"dimensions":[],"metrics":["users","sessions","page_views","events","conversions"],"filters":[],"limit":10}`,
		f.siteKey, from, today))
	queried, _ := built["rows"].([]any)
	if len(queried) == 0 {
		t.Fatalf("the query builder answered no rows: %v", built)
	}
	row, _ := queried[0].(map[string]any)
	for _, metric := range []string{"users", "sessions", "page_views", "events", "conversions"} {
		agree("query builder "+metric, number(current[metric]), "the overview", number(row[metric]), "the query builder")
	}

	ecommerce := f.get(t, site+"/ecommerce"+rng)
	summary, _ := ecommerce["summary"].(map[string]any)
	agree("revenue", number(current["revenue"]), "the overview", number(summary["revenue"]), "the ecommerce report")
	agree("transactions", number(summary["transactions"]), "the ecommerce report", number(eventRow("purchase")["count"]), "the events report")
	agree("buyers", number(summary["buyers"]), "the ecommerce report", number(eventRow("purchase")["users"]), "the events report")
	steps, _ := ecommerce["funnel"].([]any)
	for _, step := range steps {
		s, _ := step.(map[string]any)
		name := fmt.Sprint(s["event"])
		agree("people who did "+name, number(s["users"]), "the ecommerce funnel", number(eventRow(name)["users"]), "the events report")
	}

	funnel := f.do(t, http.MethodPost, "/api/v1/funnel", fmt.Sprintf(
		`{"site_id":%q,"environment":"prd","from":%q,"to":%q,"steps":[{"event":"page_view"},{"event":"purchase"}]}`,
		f.siteKey, from, today))
	funnelSteps, _ := funnel["steps"].([]any)
	if len(funnelSteps) != 2 {
		t.Fatalf("the funnel answered %d steps for a two step funnel", len(funnelSteps))
	}
	for _, step := range funnelSteps {
		s, _ := step.(map[string]any)
		name := fmt.Sprint(s["event"])
		agree("people who did "+name, number(s["users"]), "the funnel", number(eventRow(name)["users"]), "the events report")
	}

	rollup := f.get(t, site+"/workspace-rollup"+rng)
	services, _ := rollup["services"].([]any)
	found := false
	for _, service := range services {
		r, _ := service.(map[string]any)
		if fmt.Sprint(r["site_id"]) != f.siteKey {
			continue
		}
		found = true
		for _, metric := range []string{"users", "sessions", "events"} {
			agree("workspace rollup "+metric, number(current[metric]), "the overview", number(r[metric]), "the workspace rollup")
		}
	}
	if !found {
		t.Errorf("the workspace rollup does not list this site at all: %v", services)
	}
	if compared < 18 {
		t.Fatalf("only %d comparisons ran, so a screen stopped answering rather than started disagreeing", compared)
	}
	t.Logf("%d numbers compared across seven screens", compared)
}

// A segment narrows a report by picking people rather than events, and it is
// applied by a semi-join that was rewritten from a correlated subquery for speed.
// The rewrite was checked for being fast and for one pair of numbers; whether it
// still answers the same question on every screen that takes a segment was not.
//
// Two identities settle that without needing a second opinion on any number. A
// segment matching everybody has to answer exactly what no segment answers, and
// two segments that partition the population have to add back up to it. Grouping
// is the same idea: splitting a total by a dimension cannot change it.
func TestSegmentsAndGroupingPreserveTheTotals(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	// Everyone in the fixture converts, so a converted/not-converted partition
	// would be the total plus zero and would hold however the predicate behaved.
	// This adds somebody who never converts, so the two halves are both real.
	now := time.Now().UnixMilli()
	parts := []string{}
	for index := 0; index < 4; index++ {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":"page_view","timestamp":%d,"contract_version":1}`, uuid.NewString(), now-int64(index*1000)))
	}
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":"never-converts","session_id":"never-converts-session",
		"context":{"page":{"url":"https://portal.internal/browse","title":"Browse"},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
		"events":[%s]}`, f.siteKey, f.trackingKey, strings.Join(parts, ","))
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("ingest somebody who never converts: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	save := func(name, definition string) string {
		t.Helper()
		saved := f.do(t, http.MethodPost, "/api/v1/segments", fmt.Sprintf(`{"site_id":%s,"name":%s,"definition":%s,"shared":false}`,
			strconv.Quote(f.siteKey), strconv.Quote(name), definition))
		id, _ := saved["id"].(string)
		if id == "" {
			t.Fatalf("save the %s segment: %v", name, saved)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM segments WHERE id=$1`, id)
		})
		return id
	}
	everyone := save("모두", `{"combinator":"and","rules":[{"field":"entity.events","operator":">=","value":1}]}`)
	nobody := save("아무도", `{"combinator":"and","rules":[{"field":"entity.events","operator":">=","value":9999999}]}`)
	converted := save("전환함", `{"combinator":"and","rules":[{"field":"entity.conversions","operator":">=","value":1}]}`)
	notConverted := save("전환 안 함", `{"combinator":"and","rules":[{"field":"entity.conversions","operator":"=","value":0}]}`)

	number := func(v any) float64 { n, _ := v.(float64); return n }
	additive := []string{"sessions", "page_views", "events", "conversions"}
	ask := func(segment string, dimensions string) []any {
		t.Helper()
		clause := ""
		if segment != "" {
			clause = fmt.Sprintf(`,"segment_id":%q`, segment)
		}
		out := f.do(t, http.MethodPost, "/api/v1/query", fmt.Sprintf(
			`{"site_id":%q,"environment":"prd","date_range":{"from":%q,"to":%q},"dimensions":[%s],"metrics":["users","sessions","page_views","events","conversions"],"filters":[],"limit":500%s}`,
			f.siteKey, from, today, dimensions, clause))
		rows, _ := out["rows"].([]any)
		return rows
	}
	single := func(segment string) map[string]any {
		t.Helper()
		rows := ask(segment, "")
		if len(rows) == 0 {
			return map[string]any{}
		}
		row, _ := rows[0].(map[string]any)
		return row
	}

	base := single("")
	if number(base["users"]) < 3 || number(base["conversions"]) == 0 {
		t.Fatalf("the fixture has %v users and %v conversions, too little to tell a segment from no segment", base["users"], base["conversions"])
	}

	all := single(everyone)
	for _, metric := range append([]string{"users"}, additive...) {
		if number(all[metric]) != number(base[metric]) {
			t.Errorf("a segment matching everybody changed %s: no segment says %.0f, the segment says %.0f",
				metric, number(base[metric]), number(all[metric]))
		}
	}
	none := single(nobody)
	for _, metric := range append([]string{"users"}, additive...) {
		if got := number(none[metric]); got != 0 {
			t.Errorf("a segment matching nobody reports %s = %.0f", metric, got)
		}
	}

	yes, no := single(converted), single(notConverted)
	if number(yes["users"]) == 0 || number(no["users"]) == 0 {
		t.Fatalf("the partition is one sided — converted has %.0f users and not converted has %.0f — so it would hold whatever the predicate did",
			number(yes["users"]), number(no["users"]))
	}
	for _, metric := range append([]string{"users"}, additive...) {
		if sum := number(yes[metric]) + number(no[metric]); sum != number(base[metric]) {
			t.Errorf("the two halves of the population do not add back up for %s: %.0f converted + %.0f not converted = %.0f, but the total is %.0f",
				metric, number(yes[metric]), number(no[metric]), sum, number(base[metric]))
		}
	}

	// Splitting a total by a dimension cannot change it. Users are left out: one
	// person appears in more than one group and counting them once per group is
	// correct, so the sum is legitimately larger.
	for _, dimension := range []string{"device.type", "traffic.source", "page.url"} {
		rows := ask("", strconv.Quote(dimension))
		if len(rows) < 2 {
			t.Errorf("grouping by %s produced %d groups, too few to test that splitting preserves a total", dimension, len(rows))
			continue
		}
		sums := map[string]float64{}
		for _, row := range rows {
			r, _ := row.(map[string]any)
			for _, metric := range additive {
				sums[metric] += number(r[metric])
			}
		}
		for _, metric := range []string{"page_views", "events", "conversions"} {
			if sums[metric] != number(base[metric]) {
				t.Errorf("splitting by %s changed %s: ungrouped %.0f, the %d groups add to %.0f",
					dimension, metric, number(base[metric]), len(rows), sums[metric])
			}
		}
	}

	// The other screens that take a segment have to honour the same identity.
	funnelUsers := func(segment string) []float64 {
		t.Helper()
		clause := ""
		if segment != "" {
			clause = fmt.Sprintf(`,"segment_id":%q`, segment)
		}
		out := f.do(t, http.MethodPost, "/api/v1/funnel", fmt.Sprintf(
			`{"site_id":%q,"environment":"prd","from":%q,"to":%q,"steps":[{"event":"page_view"},{"event":"purchase"}]%s}`,
			f.siteKey, from, today, clause))
		steps, _ := out["steps"].([]any)
		users := []float64{}
		for _, step := range steps {
			s, _ := step.(map[string]any)
			users = append(users, number(s["users"]))
		}
		return users
	}
	plain, segmented, empty := funnelUsers(""), funnelUsers(everyone), funnelUsers(nobody)
	if len(plain) != 2 {
		t.Fatalf("the funnel answered %d steps", len(plain))
	}
	for index := range plain {
		if plain[index] == 0 {
			t.Fatalf("funnel step %d has nobody in it, so a segment cannot change it", index+1)
		}
		if segmented[index] != plain[index] {
			t.Errorf("a segment matching everybody changed funnel step %d: %.0f without, %.0f with", index+1, plain[index], segmented[index])
		}
		if empty[index] != 0 {
			t.Errorf("a segment matching nobody left %.0f people in funnel step %d", empty[index], index+1)
		}
	}

	experienceUsers := func(segment string) float64 {
		t.Helper()
		path := site + "/experience?from=" + from + "&to=" + today
		if segment != "" {
			path += "&segment_ids=" + segment
		}
		out := f.get(t, path)
		impact, _ := out["impact"].(map[string]any)
		return number(impact["users"])
	}
	if with, without := experienceUsers(everyone), experienceUsers(""); with != without {
		t.Errorf("a segment matching everybody changed the experience report: %.0f without, %.0f with", without, with)
	}
}
