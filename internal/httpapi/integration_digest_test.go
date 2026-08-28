package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// A scheduled digest is the answer nobody can check. A screen puts a number next
// to a chart and a period selector; a mailed or published report arrives on its
// own, and whoever reads it has no way to notice that it is smaller than the
// screen it is named after.
//
// Two of them were. The AI digest sent calls, users and two token sums — no
// cost, no success rate, no latency, no breakdown — which is exactly where the
// AI screen's query stopped before v0.34.3 and where the MCP tool stopped until
// it was fixed; the digest was the third copy and the last one to be looked at.
// The experience digest sent an error count and an affected-user count, under
// the name of the screen whose subject is Web Vitals, with no vitals in it and
// nothing about what the errors did to conversion.
//
// This delivers each one for real, through the webhook path, and holds the
// payload to the screen.
func TestTheScheduledDigestsCarryTheWholeReport(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// AI activity with every measure separated: two models, one failed call, a
	// fallback, latency, tokens and cost. Without it both sides answer zero and
	// agree however wrong either is.
	now := time.Now()
	deliverAI := func(visitor string, events []string) {
		t.Helper()
		f.postCollect(t, f.siteKey, "digest-"+visitor, visitor, "https://portal.internal/ai", events)
	}
	call := func(index int, model, success, latency, cost, fallback string) string {
		return fmt.Sprintf(`{"id":%q,"name":"ai_model_call","timestamp":%d,"properties":{"model":%q,"success":%q,"latency_ms":%q,"input_tokens":"120","output_tokens":"340","cost":%q,"fallback_model":%q},"contract_version":1}`,
			uuid.NewString(), now.Add(time.Duration(index)*time.Second).UnixMilli(), model, success, latency, cost, fallback)
	}
	deliverAI("ai-one", []string{
		call(0, "claude", "true", "820", "0.014", ""),
		call(1, "claude", "false", "1400", "0.002", ""),
		call(2, "claude", "true", "700", "0.011", "haiku"),
	})
	deliverAI("ai-two", []string{call(0, "gpt", "true", "410", "0.008", "")})
	// An error and a web vital, so the experience digest has both halves.
	f.postCollect(t, f.siteKey, "digest-broken", "digest-broken", "https://portal.internal/report", []string{
		fmt.Sprintf(`{"id":%q,"name":"error","timestamp":%d,"properties":{"message":"boom"},"contract_version":1}`, uuid.NewString(), now.UnixMilli()),
		fmt.Sprintf(`{"id":%q,"name":"web_vital","timestamp":%d,"properties":{"metric":"LCP","value":"2400","rating":"needs-improvement"},"contract_version":1}`, uuid.NewString(), now.Add(time.Second).UnixMilli()),
		fmt.Sprintf(`{"id":%q,"name":"web_vital","timestamp":%d,"properties":{"metric":"INP","value":"180","rating":"good"},"contract_version":1}`, uuid.NewString(), now.Add(2*time.Second).UnixMilli()),
	})
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	received := make(chan map[string]any, 4)
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
		fmt.Sprintf(`{"name":"digest","channel_type":"webhook","endpoint_url":"%s","headers":{},"active":true}`, target.URL))
	channelID, _ := channel["id"].(string)
	automation := service.Automation{DB: pool, Secrets: f.server.Secrets}

	// The digest reads the last whole day in the site's calendar, so the screen
	// has to be asked for the window the payload says it used.
	deliver := func(kind string) map[string]any {
		t.Helper()
		report := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
			fmt.Sprintf(`{"channel_id":"%s","name":"%s 다이제스트","report_kind":"%s","interval_minutes":1440,"definition":{"environment":"prd","days":1},"enabled":true}`, channelID, kind, kind))
		id, _ := report["id"].(string)
		if err := automation.RunByID(ctx, uuid.MustParse(id)); err != nil {
			t.Fatalf("%s delivery: %v", kind, err)
		}
		select {
		case payload := <-received:
			if payload["kind"] != kind {
				t.Fatalf("delivered kind = %v, want %s", payload["kind"], kind)
			}
			return payload
		case <-time.After(10 * time.Second):
			t.Fatalf("the %s digest never arrived", kind)
			return nil
		}
	}
	// The window the digest reports on, in the form the screen's query accepts.
	// The payload states it as UTC instants and the screen reads dates in the
	// site's calendar, so the conversion has to go through the site's location —
	// in a UTC+9 site the two boundaries fall on different UTC dates, and asking
	// the screen for the UTC ones compares a one day digest with a two day
	// screen. That is a difference in period reported as a difference in numbers.
	var timezone string
	if err := pool.QueryRow(ctx, `SELECT timezone FROM sites WHERE id=$1`, f.siteID).Scan(&timezone); err != nil {
		t.Fatalf("site timezone: %v", err)
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		t.Fatalf("site timezone %q: %v", timezone, err)
	}
	window := func(payload map[string]any) string {
		t.Helper()
		from, fromErr := time.Parse(time.RFC3339Nano, fmt.Sprint(payload["from"]))
		to, toErr := time.Parse(time.RFC3339Nano, fmt.Sprint(payload["to"]))
		if fromErr != nil || toErr != nil {
			t.Fatalf("the payload does not say what period it covers: from=%v to=%v", payload["from"], payload["to"])
		}
		// The screen's range is inclusive of the end date; the digest's is an
		// exclusive instant, so the last covered day is the one before it.
		return "from=" + from.In(location).Format("2006-01-02") +
			"&to=" + to.In(location).Add(-time.Second).Format("2006-01-02")
	}

	t.Run("the AI digest says what it cost and whether it worked", func(t *testing.T) {
		payload := deliver("ai")
		data, _ := payload["data"].(map[string]any)
		rows, _ := data["rows"].([]any)
		totals, _ := data["totals"].(map[string]any)
		if len(rows) == 0 || totals == nil {
			t.Fatalf("the AI digest carries no rows or no totals: %v", data)
		}
		if cost, _ := totals["cost"].(float64); cost <= 0 {
			t.Fatalf("the delivered cost is %v, so agreeing with the screen would prove nothing", totals["cost"])
		}
		if rate, _ := totals["success_rate"].(float64); rate <= 0 || rate >= 100 {
			t.Fatalf("the delivered success rate is %v; the fixture has both a success and a failure, so a rate at either end means the measure is not discriminating", totals["success_rate"])
		}

		screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/ai-analytics?"+window(payload)+"&group_by=model")
		shownRows, _ := screen["rows"].([]any)
		byLabel := map[string]map[string]any{}
		for _, row := range shownRows {
			if shown, ok := row.(map[string]any); ok {
				byLabel[fmt.Sprint(shown["label"])] = shown
			}
		}
		if len(byLabel) != len(rows) {
			t.Fatalf("the screen shows %d models and the digest delivered %d", len(byLabel), len(rows))
		}
		for _, row := range rows {
			said, _ := row.(map[string]any)
			shown, ok := byLabel[fmt.Sprint(said["label"])]
			if !ok {
				t.Errorf("the digest delivered a model %v the screen does not show", said["label"])
				continue
			}
			for _, field := range []string{"calls", "users", "success_rate", "average_latency_ms", "input_tokens", "output_tokens", "cost", "fallbacks"} {
				if fmt.Sprint(said[field]) != fmt.Sprint(shown[field]) {
					t.Errorf("%v %s: the digest says %v and the screen says %v", said["label"], field, said[field], shown[field])
				}
			}
		}

		// The totals are folded from the rows, and a mean of means would let a
		// model called once weigh as much as one called a thousand times.
		var calls, weighted float64
		for _, row := range shownRows {
			shown, _ := row.(map[string]any)
			rowCalls, _ := shown["calls"].(float64)
			rate, _ := shown["success_rate"].(float64)
			calls += rowCalls
			weighted += rate * rowCalls
		}
		if calls == 0 {
			t.Fatal("the screen shows no calls")
		}
		if rate, _ := totals["success_rate"].(float64); rate < weighted/calls-1 || rate > weighted/calls+1 {
			t.Errorf("the delivered success rate is %v and the call-weighted rate across the screen's rows is %v", rate, weighted/calls)
		}
	})

	t.Run("the experience digest carries the vitals it is named after", func(t *testing.T) {
		payload := deliver("experience")
		data, _ := payload["data"].(map[string]any)
		p75, _ := data["p75"].(map[string]any)
		if p75 == nil {
			t.Fatal("the experience digest carries no Web Vitals, which is the subject of the screen it is named after")
		}
		if lcp, _ := p75["LCP"].(float64); lcp <= 0 {
			t.Fatalf("the delivered LCP is %v, so it cannot be told apart from a digest with no vitals in it", p75["LCP"])
		}
		if errors, _ := data["errors"].(float64); errors <= 0 {
			t.Fatalf("the delivered error count is %v, so agreeing with the screen would prove nothing", data["errors"])
		}

		// The conversion impact is why the digest is worth sending: an error count
		// alone does not say whether it mattered.
		screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/experience?"+window(payload))
		impact, _ := screen["impact"].(map[string]any)
		if impact == nil {
			t.Fatal("the experience screen answered no impact block")
		}
		for _, field := range []string{"users", "error_users", "error_user_conversion_rate", "clean_user_conversion_rate", "conversion_rate_delta"} {
			said, ok := data[field]
			if !ok {
				t.Errorf("the digest does not carry %s — the error count it does carry cannot say whether the errors mattered", field)
				continue
			}
			if fmt.Sprint(said) != fmt.Sprint(impact[field]) {
				t.Errorf("%s: the digest says %v and the screen says %v", field, said, impact[field])
			}
		}
		if users, _ := impact["error_users"].(float64); users <= 0 {
			t.Fatal("nobody in this window had an error, so the impact figures being compared are all zero")
		}
	})
}
