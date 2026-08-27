package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The identities screen tells an administrator how many events a person has
// produced and how many of them converted, summed across every device they have
// been seen on. Visitor search shows the same numbers per device. Both read them
// from the per-visitor aggregate the collector keeps — counters it increments
// one event at a time — and nothing had ever counted the events that went in and
// compared them to what came out.
//
// A counter that drifts is the worst kind of wrong here, because it is
// plausible: nobody can tell 9 from 8 by looking, and both screens read the same
// row so they agree with each other whatever it says.
func TestTheVisitorTotalsCountTheEventsThatProducedThem(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the site timezone: %v", err)
	}
	at := time.Now().In(location).AddDate(0, 0, -100)
	start := time.Date(at.Year(), at.Month(), at.Day(), 9, 0, 0, 0, location)

	const person = "EMP-TOTALS"
	const laptop = "totals-visitor-laptop"
	const phone = "totals-visitor-phone"

	// The laptop is anonymous for two events and then says who it is, which is
	// the ordinary shape of a first visit: the events before the sign-in belong
	// to the same device and have to keep counting.
	f.deliverIdentified(t, f.otherKey, "totals-laptop-1", laptop, "", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/home"},
		{name: "click", after: time.Minute, page: "https://portal.internal/home"},
	}, start)
	f.deliverIdentified(t, f.otherKey, "totals-laptop-2", laptop, person, []timedEvent{
		{name: "page_view", after: 2 * time.Minute, page: "https://portal.internal/apply"},
		{name: "click", after: 3 * time.Minute, page: "https://portal.internal/apply"},
		{name: "purchase", after: 4 * time.Minute, page: "https://portal.internal/apply"},
	}, start)
	// The phone is the same person from its first event.
	f.deliverIdentified(t, f.otherKey, "totals-phone-1", phone, person, []timedEvent{
		{name: "page_view", after: time.Hour, page: "https://portal.internal/home"},
		{name: "click", after: time.Hour + time.Minute, page: "https://portal.internal/home"},
		{name: "click", after: time.Hour + 2*time.Minute, page: "https://portal.internal/home"},
		{name: "purchase", after: time.Hour + 3*time.Minute, page: "https://portal.internal/done"},
	}, start)
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	for _, want := range []struct {
		visitor             string
		events, conversions int64
	}{
		{laptop, 5, 1},
		{phone, 4, 1},
	} {
		var events, conversions int64
		var first, last time.Time
		if err := pool.QueryRow(ctx, `SELECT event_count,conversion_count,first_seen,last_seen FROM visitors WHERE visitor_id=$1`, want.visitor).
			Scan(&events, &conversions, &first, &last); err != nil {
			t.Fatalf("read the aggregate for %s: %v", want.visitor, err)
		}
		if events != want.events || conversions != want.conversions {
			t.Errorf("%s: the aggregate counts %d events and %d conversions; %d and %d were delivered",
				want.visitor, events, conversions, want.events, want.conversions)
		}
		if first.Before(start.Add(-time.Second)) || last.After(start.Add(2*time.Hour)) {
			t.Errorf("%s: the aggregate spans %s to %s, outside the visit it was built from", want.visitor, first, last)
		}
	}

	// The identities screen sums those two rows into one person. Nine events,
	// two conversions, two devices — the numbers an administrator reads when
	// deciding whether an account is one person or several.
	list, _ := f.get(t, "/api/v1/sites/"+f.otherKey+"/identities")["list"].([]any)
	found := false
	for _, item := range list {
		row, _ := item.(map[string]any)
		if row == nil || row["user_id"] != person {
			continue
		}
		found = true
		for _, check := range []struct {
			field string
			want  float64
		}{
			{"visitor_count", 2},
			{"events", 9},
			{"conversions", 2},
		} {
			if got, _ := row[check.field].(float64); got != check.want {
				t.Errorf("the identities screen reports %v %s for a person who produced %v", got, check.field, check.want)
			}
		}
	}
	if !found {
		t.Fatalf("the identities screen does not know %s at all: %v", person, list)
	}
}

// deliverIdentified posts a batch that may or may not carry a user id, so the
// events a device produces before it says who it is can be told apart from the
// ones after.
func (f fixture) deliverIdentified(t *testing.T, siteKey, sessionID, visitorID, userID string, events []timedEvent, start time.Time) {
	t.Helper()
	parts := make([]string, 0, len(events))
	for _, item := range events {
		properties := "{}"
		if item.name == "purchase" {
			properties = `{"value":"1000"}`
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":%s,"contract_version":1,"context":{"page":{"url":%q,"title":"페이지","referrer":""}}}`,
			uuid.NewString(), item.name, start.Add(item.after).UnixMilli(), properties, item.page))
	}
	user := ""
	if userID != "" {
		user = fmt.Sprintf(`"user_id":%q,`, userID)
	}
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":%q,"session_id":%q,%s
		"context":{"page":{"url":%q,"title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
		"events":[%s]}`, siteKey, f.trackingKey, visitorID, sessionID, user, events[0].page, strings.Join(parts, ","))
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("deliver %s: %d %s", sessionID, recorder.Code, recorder.Body.String())
	}
}
