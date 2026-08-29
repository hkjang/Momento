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

// The overview splits direct traffic into "Direct" and "Direct (사내망)", which
// on an on-premise analytics product is the distinction the screen exists to
// draw: someone who came from the corporate network, and someone who did not.
//
// It decided by asking whether the event carried a network name at all. The
// collector writes one for every event — "External / Unclassified" when the
// address matches no configured range — so the test was true for everything
// that ever arrived through the tracker, and every direct visit was reported as
// having come from inside.
//
// The column that actually records it is is_internal, set from the same lookup,
// and it was sitting in the same row.
func TestDirectTrafficFromOutsideIsNotReportedAsInternal(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'stg','Staging') ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("register the staging environment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM raw_events WHERE site_id=$1 AND environment='stg'`, f.siteID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_environments WHERE site_id=$1 AND name='stg'`, f.siteID)
	})

	// Direct: no source, no medium, no referrer. Delivered through the collector
	// so the row carries whatever the collector actually writes.
	now := time.Now()
	for index, visitor := range []string{"direct-one", "direct-two"} {
		payload := fmt.Sprintf(`{"site_id":%q,"environment":"stg","tracking_key":%q,"visitor_id":%q,"session_id":%q,
			"context":{"page":{"url":"https://portal.internal/app","title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"","medium":"","campaign":""}},
			"events":[{"id":%q,"name":"page_view","timestamp":%d,"properties":{},"contract_version":1}]}`,
			f.siteKey, f.trackingKey, visitor, "session-"+visitor,
			uuid.NewString(), now.Add(time.Duration(index)*time.Second).UnixMilli())
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("deliver %s: %d %s", visitor, recorder.Code, recorder.Body.String())
		}
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	// What the collector wrote, so the assertion below is about the report and
	// not about the ingestion.
	var internal int64
	var networks []string
	rows, err := pool.Query(ctx, `SELECT count(*) FILTER(WHERE is_internal),array_agg(DISTINCT coalesce(network_name,'<null>')) FROM raw_events WHERE site_id=$1 AND environment='stg'`, f.siteID)
	if err != nil {
		t.Fatalf("read back the delivered events: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no events were delivered")
	}
	if err := rows.Scan(&internal, &networks); err != nil {
		t.Fatalf("scan: %v", err)
	}
	rows.Close()
	if internal != 0 {
		t.Fatalf("the collector marked %d of the delivered events as internal traffic, so this test is not about the report", internal)
	}
	if len(networks) == 0 {
		t.Fatal("no network name was recorded at all, so the old expression would have been false and this proves nothing")
	}
	t.Logf("the collector recorded network_name %v with is_internal false", networks)

	from, today := f.siteDates(t, 1)
	overview := f.get(t, "/api/v1/sites/"+f.siteKey+"/visitor-insights?from="+from+"&to="+today+"&environment=stg")
	channels, _ := overview["channels"].([]any)
	if len(channels) == 0 {
		t.Fatal("the overview reports no channels for the delivered traffic")
	}
	for _, entry := range channels {
		row, _ := entry.(map[string]any)
		name := fmt.Sprint(row["channel"])
		users, _ := row["users"].(float64)
		if strings.Contains(name, "사내망") && users > 0 {
			t.Fatalf("%.0f people who arrived from outside any configured network are reported under %q: the report asks whether the event carries a network name, and the collector gives every event one",
				users, name)
		}
	}

	// And the other direction, so the fix is not simply "never internal": the
	// same delivery from an address inside a configured range has to land under
	// the internal split.
	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'qa','QA') ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("register the qa environment: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO network_ranges(name,cidr,description,internal) VALUES('테스트 사내망','192.0.2.0/24','integration test',true) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("register the internal range: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM network_ranges WHERE cidr='192.0.2.0/24'`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM raw_events WHERE site_id=$1 AND environment='qa'`, f.siteID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_environments WHERE site_id=$1 AND name='qa'`, f.siteID)
	})
	payload := fmt.Sprintf(`{"site_id":%q,"environment":"qa","tracking_key":%q,"visitor_id":"direct-inside","session_id":"session-direct-inside",
		"context":{"page":{"url":"https://portal.internal/app","title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"","medium":"","campaign":""}},
		"events":[{"id":%q,"name":"page_view","timestamp":%d,"properties":{},"contract_version":1}]}`,
		f.siteKey, f.trackingKey, uuid.NewString(), now.UnixMilli())
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("deliver the internal visit: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}
	var markedInternal int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE is_internal) FROM raw_events WHERE site_id=$1 AND environment='qa'`, f.siteID).Scan(&markedInternal); err != nil {
		t.Fatalf("read back the internal visit: %v", err)
	}
	if markedInternal == 0 {
		t.Fatal("the collector did not mark the visit as internal traffic, so the report cannot be expected to")
	}
	insideReport := f.get(t, "/api/v1/sites/"+f.siteKey+"/visitor-insights?from="+from+"&to="+today+"&environment=qa")
	insideChannels, _ := insideReport["channels"].([]any)
	found := false
	for _, entry := range insideChannels {
		row, _ := entry.(map[string]any)
		if users, _ := row["users"].(float64); strings.Contains(fmt.Sprint(row["channel"]), "사내망") && users > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("a direct visit from inside a configured internal range is not reported under the internal split: %v", insideChannels)
	}
}
