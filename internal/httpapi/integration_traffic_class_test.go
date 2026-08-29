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

// The user guide tells an operator how to keep an uptime monitor out of the
// numbers: filter the segment to `traffic.class = normal`. On this product that
// advice removed the entire workforce.
//
// traffic_class carried two unrelated facts. The collector classified the client
// from its user agent — known_bot, monitoring, suspicious, normal — and then
// overwrote the answer with internal_traffic whenever the address matched a
// network an administrator had registered. So on an on-premise deployment,
// where most traffic is internal by construction, `traffic.class = normal`
// dropped every employee, and a crawler running on the intranet was filed as
// internal traffic rather than as a crawler.
//
// The network fact already had its own column and its own segment field:
// is_internal, exposed as traffic.internal. traffic_class describes the client
// now, and only the client.
func TestTheTrafficClassDescribesTheClientAndNotTheNetwork(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'stg','Staging') ON CONFLICT DO NOTHING`, f.siteID); err != nil {
		t.Fatalf("register the staging environment: %v", err)
	}
	// httptest addresses every request from 192.0.2.1, so registering this range
	// makes every delivery below internal traffic.
	if _, err := pool.Exec(ctx, `INSERT INTO network_ranges(name,cidr,description,internal) VALUES('테스트 사내망','192.0.2.0/24','integration test',true) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("register the internal range: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM network_ranges WHERE cidr='192.0.2.0/24'`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM raw_events WHERE site_id=$1 AND environment='stg'`, f.siteID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_environments WHERE site_id=$1 AND name='stg'`, f.siteID)
	})

	deliver := func(t *testing.T, visitor, userAgent string) {
		t.Helper()
		payload := fmt.Sprintf(`{"site_id":%q,"environment":"stg","tracking_key":%q,"visitor_id":%q,"session_id":%q,
			"context":{"page":{"url":"https://portal.internal/app","title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"","medium":""}},
			"events":[{"id":%q,"name":"page_view","timestamp":%d,"properties":{},"contract_version":1}]}`,
			f.siteKey, f.trackingKey, visitor, "session-"+visitor, uuid.NewString(), time.Now().UnixMilli())
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		request.Header.Set("User-Agent", userAgent)
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("deliver %s: %d %s", visitor, recorder.Code, recorder.Body.String())
		}
	}
	deliver(t, "employee-inside", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36")
	deliver(t, "uptime-inside", "Pingdom.com_bot_version_1.4 uptime")
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	classes := map[string]struct {
		class    string
		internal bool
	}{}
	rows, err := pool.Query(ctx, `SELECT visitor_id,traffic_class,is_internal FROM raw_events WHERE site_id=$1 AND environment='stg'`, f.siteID)
	if err != nil {
		t.Fatalf("read the classifications back: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var visitor, class string
		var internal bool
		if err := rows.Scan(&visitor, &class, &internal); err != nil {
			t.Fatalf("scan: %v", err)
		}
		classes[visitor] = struct {
			class    string
			internal bool
		}{class, internal}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the classifications back: %v", err)
	}
	if len(classes) != 2 {
		t.Fatalf("expected both deliveries to be stored, got %v", classes)
	}
	if !classes["employee-inside"].internal || !classes["uptime-inside"].internal {
		t.Fatalf("the collector did not record the deliveries as internal traffic, so this test is not about the class: %v", classes)
	}
	if got := classes["employee-inside"].class; got != "normal" {
		t.Errorf("an employee on the corporate network is filed as %q, so the guide's own advice — filter to traffic.class = normal to drop the uptime monitor — removes the workforce with it", got)
	}
	if got := classes["uptime-inside"].class; got != "monitoring" {
		t.Errorf("an uptime monitor on the corporate network is filed as %q, so no filter on traffic.class can find it: the one fact that identifies it was overwritten by where it ran", got)
	}
	// The network fact is not lost — it has its own column, and its own segment
	// field, which is how "exclude our own staff" is expressed.
	if !classes["employee-inside"].internal {
		t.Error("is_internal no longer records that the visit came from inside")
	}
}
