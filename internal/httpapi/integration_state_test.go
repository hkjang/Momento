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
	// Everyone in the fixture converts, so the segment must match nobody rather than
	// silently matching everyone.
	rows, _ := response["rows"].([]any)
	if len(rows) != 0 {
		t.Fatalf("the never-converted segment matched %d rows: %v", len(rows), rows)
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
		dimensionResolver{custom: map[string]customDimension{}}, "e", &args, 0)
	if err == nil {
		t.Fatal("an unscoped resolver compiled a behavioural aggregate instead of failing")
	}
	if !strings.Contains(err.Error(), "site") {
		t.Fatalf("the error does not say what is missing: %v", err)
	}
}
