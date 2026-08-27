package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/service"
)

// Every test in this suite runs against a site in Asia/Seoul, nine hours ahead
// of UTC. A site west of UTC converts the other way, and the whole product is
// built on that conversion: the collector files an event under its site-local
// date, the landing screen's chart reads those daily rows, and the range a
// reader asks for is parsed as local midnights. A sign error in any of them
// would put a visit on the wrong day, and every test would still pass.
//
// This is the same measurement from the other side. Two page views half an hour
// either side of a site's local midnight have to land on the two days they
// happened on — not on the one UTC day that contains them both.
func TestASiteWestOfUTCFilesEventsOnItsOwnDays(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var workspaceID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM sites WHERE site_key=$1`, f.siteKey).Scan(&workspaceID); err != nil {
		t.Fatalf("read the fixture workspace: %v", err)
	}
	const siteKey = "SITE_NY"
	const zone = "America/New_York"
	if _, err := pool.Exec(ctx, `INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		VALUES($1,$2,'뉴욕','뉴욕',$3,'mom_track_x',$4,'mom_server_x',ARRAY['portal.internal'],$5)
		ON CONFLICT(site_key) DO UPDATE SET timezone=excluded.timezone`,
		workspaceID, siteKey, auth.HashToken(f.trackingKey), auth.HashToken(f.serverKey), zone); err != nil {
		t.Fatalf("create a site west of UTC: %v", err)
	}
	var siteID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM sites WHERE site_key=$1`, siteKey).Scan(&siteID); err != nil {
		t.Fatalf("resolve the new site: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO site_environments(site_id,name,label) VALUES($1,'prd','Production') ON CONFLICT DO NOTHING`, siteID); err != nil {
		t.Fatalf("give the new site an environment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sites WHERE site_key=$1`, siteKey)
	})

	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Skipf("the runner has no timezone database for %s: %v", zone, err)
	}
	// Late enough to be well inside the seventy days the fixture fills for the
	// other sites, and far enough back that "today" cannot move underneath it.
	anchor := time.Now().In(location).AddDate(0, 0, -20)
	midnight := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, location)
	before := midnight.Add(-30 * time.Minute) // 23:30 on the previous local day
	after := midnight.Add(30 * time.Minute)   // 00:30 on this local day

	f.deliverAt(t, siteKey, "ny-before", "ny-visitor-before", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/before"},
	}, before)
	f.deliverAt(t, siteKey, "ny-after", "ny-visitor-after", []timedEvent{
		{name: "page_view", after: 0, page: "https://portal.internal/after"},
	}, after)
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	earlier := before.Format("2006-01-02")
	later := after.Format("2006-01-02")
	if earlier == later {
		t.Fatalf("the two events were meant to fall on different local days and both read %s", earlier)
	}

	// Both days together: one event on each, and the chart says which.
	report := f.get(t, "/api/v1/sites/"+siteKey+"/overview?from="+earlier+"&to="+later)
	trend, _ := report["trend"].([]any)
	byDay := map[string]float64{}
	for _, item := range trend {
		entry, _ := item.(map[string]any)
		date, _ := entry["date"].(string)
		events, _ := entry["events"].(float64)
		byDay[date] = events
	}
	if byDay[earlier] != 1 || byDay[later] != 1 {
		t.Errorf("a visit at 23:30 and one at 00:30 the next day read as %v on %s and %v on %s: both fall in one UTC day, and only the site's own timezone tells them apart",
			byDay[earlier], earlier, byDay[later], later)
	}

	// And the earlier day on its own, which is the range a reader asks for when
	// they click one day in that chart.
	single := f.get(t, "/api/v1/sites/"+siteKey+"/overview?from="+earlier+"&to="+earlier)
	current, _ := single["current"].(map[string]any)
	if current == nil {
		t.Fatalf("the overview answered without a period: %v", single)
	}
	if events, _ := current["events"].(float64); events != 1 {
		t.Errorf("asking for %s alone reports %v events; one happened on that local day", earlier, events)
	}
}
