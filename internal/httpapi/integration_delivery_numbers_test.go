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

// A scheduled digest is the landing screen sent to somebody who is not looking
// at it. It is built by its own queries, in a different package from the screen
// it is named after, and what had been checked was its shape: that a payload
// arrived, that it carried the right kind, that it had a headline.
//
// Nobody had ever compared the numbers. A digest that says four thousand users
// where the screen says three thousand is not a broken delivery — it arrives, it
// looks right, and the person reading it has no screen in front of them to
// notice. Two of the comments in that code exist because this went wrong twice
// already: once measuring from the moment the schedule fired instead of the
// period the screen shows, and once sending one screen's content under another
// screen's name.
func TestTheDigestCarriesTheNumbersTheScreenShows(t *testing.T) {
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
		fmt.Sprintf(`{"name":"digest","channel_type":"webhook","endpoint_url":"%s","active":true}`, target.URL))
	channelID, _ := channel["id"].(string)
	if channelID == "" {
		t.Fatalf("could not create the delivery channel: %v", channel)
	}

	const days = 7
	report := f.do(t, http.MethodPost, "/api/v1/sites/"+f.siteKey+"/scheduled-reports",
		fmt.Sprintf(`{"channel_id":"%s","name":"주간 요약","report_kind":"overview","interval_minutes":10080,"definition":{"environment":"prd","days":%d},"enabled":true}`, channelID, days))
	reportID, _ := report["id"].(string)
	if reportID == "" {
		t.Fatalf("could not schedule the report: %v", report)
	}
	automation := service.Automation{DB: pool, Secrets: f.server.Secrets}
	if err := service.Automation(automation).RunByID(ctx, uuid.MustParse(reportID)); err != nil {
		t.Fatalf("run the scheduled digest: %v", err)
	}

	var delivered map[string]any
	select {
	case payload := <-received:
		delivered, _ = payload["data"].(map[string]any)
	case <-time.After(10 * time.Second):
		t.Fatal("the digest never arrived")
	}
	if delivered == nil {
		t.Fatal("the digest arrived with no data")
	}

	// The window the digest reports on is the last seven site-local days ending
	// at local midnight, which is what the screen shows when a reader asks for
	// the same seven days.
	from, to := f.siteDate(t, -(days-1)), f.siteDate(t, 0)
	screen := f.get(t, "/api/v1/sites/"+f.siteKey+"/overview?from="+from+"&to="+to)
	current, _ := screen["current"].(map[string]any)
	if current == nil {
		t.Fatalf("the overview answered without a period: %v", screen)
	}

	compared := 0
	for _, measure := range []string{"users", "sessions", "events", "conversions", "revenue"} {
		sent, sentOK := delivered[measure].(float64)
		shown, shownOK := current[measure].(float64)
		if !sentOK {
			t.Errorf("the digest carries no %s at all: %v", measure, delivered)
			continue
		}
		if !shownOK {
			t.Errorf("the overview reports no %s: %v", measure, current)
			continue
		}
		if sent != shown {
			t.Errorf("%s: the digest says %v and the screen it is named after says %v — the person reading the digest has no screen in front of them to notice",
				measure, sent, shown)
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("nothing was comparable between the digest and the screen")
	}
	// A digest of an empty period would agree with an empty screen, so the
	// comparison has to be made against numbers that exist.
	if users, _ := delivered["users"].(float64); users == 0 {
		t.Error("the digest reports no visitors at all, so agreeing with the screen proves nothing")
	}
	t.Logf("%d measures compared between the digest and the screen", compared)
}
