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

// The data quality screen counts events that arrive without a user id, so an
// operator can tell an integration to start identifying people. The privacy
// filter now inspects the user id and blanks one it will not store — which
// leaves the event looking exactly like one that never carried an identifier.
//
// Those need opposite actions. One team is not calling identify() at all; the
// other is calling it with a phone number, and telling them to start doing what
// they are already doing is worse than saying nothing. So the two are counted
// apart, all the way from the request to the screen.
func TestARefusedIdentifierIsCountedApartFromAMissingOne(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	deliver := func(name, userID string) {
		t.Helper()
		user := ""
		if userID != "" {
			user = fmt.Sprintf(`"user_id":%q,`, userID)
		}
		payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":"refused-visitor-%s","session_id":"refused-session-%s",%s
			"context":{"page":{"url":"https://portal.internal/home","title":"홈","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{}},
			"events":[{"id":%q,"name":%q,"timestamp":%d,"properties":{},"contract_version":1}]}`,
			f.siteKey, f.trackingKey, name, name, user, uuid.NewString(), name, time.Now().UnixMilli())
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("deliver %s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}

	// One integration identifies nobody. One identifies people with a phone
	// number. Both events reach storage with an empty user id.
	deliver("identifier_absent", "")
	deliver("identifier_refused", "010-1234-5678")
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	for _, want := range []struct {
		event   string
		missing int64
		refused int64
	}{
		{"identifier_absent", 1, 0},
		{"identifier_refused", 0, 1},
	} {
		var missing, refused int64
		if err := pool.QueryRow(ctx, `SELECT coalesce(sum(missing_user_id),0),coalesce(sum(refused_user_id),0)
			FROM data_quality_daily WHERE site_id=$1 AND event_name=$2`, f.siteID, want.event).Scan(&missing, &refused); err != nil {
			t.Fatalf("read the quality counters for %s: %v", want.event, err)
		}
		if missing != want.missing || refused != want.refused {
			t.Errorf("%s: missing=%d refused=%d, want missing=%d refused=%d — the screen would send the reader after the wrong problem",
				want.event, missing, refused, want.missing, want.refused)
		}
	}

	// The phone number is not in the stored event either, which is the point of
	// refusing it.
	var stored int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM raw_events WHERE site_id=$1 AND user_id LIKE '%1234%'`, f.siteID).Scan(&stored); err != nil {
		t.Fatalf("look for the refused identifier: %v", err)
	}
	if stored != 0 {
		t.Errorf("%d events kept the refused identifier", stored)
	}

	// And the screen is told, so an operator can act on it.
	report := f.get(t, "/api/v1/sites/"+f.siteKey+"/data-quality?from="+mustDate(t, f, -1)+"&to="+mustDate(t, f, 0))
	quality, _ := report["quality"].(map[string]any)
	if quality == nil {
		t.Fatalf("the data quality report has no quality section: %v", report)
	}
	if value, _ := quality["refused_user_id"].(float64); value < 1 {
		t.Errorf("the data quality screen reports %v refused identifiers, want at least one", value)
	}
}

func mustDate(t *testing.T, f fixture, offset int) string {
	t.Helper()
	return f.siteDate(t, offset)
}
