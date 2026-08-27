package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// A session is engaged if any one of four things is true: it lasted at least
// the site's threshold, it converted, it saw two pages, or it reported that
// much active time. That rule is written out in four places — the collector as
// each event arrives, the rebuild that reconstructs sessions from the events,
// the fallback the overview uses when no session row exists, and a migration
// that backfilled it once. They cannot share a string: each reads different
// columns through different aliases.
//
// So they are held to the same answers instead. Engagement decides the bounce
// rate, the engagement rate and every landing page comparison; if the collector
// and the rebuild disagree about it, a privacy deletion or a backfill silently
// changes what a site's history says about itself.
func TestEngagementMeansTheSameThingHoweverASessionWasDerived(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var siteID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM sites WHERE site_key=$1`, f.otherKey).Scan(&siteID); err != nil {
		t.Fatalf("resolve the second site: %v", err)
	}
	var threshold int
	if err := pool.QueryRow(ctx, `SELECT engagement_threshold_seconds FROM sites WHERE id=$1`, siteID).Scan(&threshold); err != nil {
		t.Fatalf("read the site's engagement threshold: %v", err)
	}
	if threshold < 2 {
		t.Fatalf("the site's engagement threshold is %ds, too small to build a visit that misses it", threshold)
	}

	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load the site timezone: %v", err)
	}
	at := time.Now().In(location).AddDate(0, 0, -120)
	start := time.Date(at.Year(), at.Month(), at.Day(), 10, 0, 0, 0, location)
	long := time.Duration(threshold+5) * time.Second

	// One visit per route into engagement, and one that takes none of them.
	visits := []struct {
		session string
		events  []timedEvent
		engaged bool
		why     string
	}{
		{"engaged-by-duration", []timedEvent{
			{name: "page_view", after: 0, page: "https://portal.internal/a"},
			{name: "click", after: long, page: "https://portal.internal/a"},
		}, true, "lasted longer than the threshold"},
		{"engaged-by-conversion", []timedEvent{
			{name: "purchase", after: 0, page: "https://portal.internal/a"},
		}, true, "converted"},
		{"engaged-by-pages", []timedEvent{
			{name: "page_view", after: 0, page: "https://portal.internal/a"},
			{name: "page_view", after: time.Second, page: "https://portal.internal/b"},
		}, true, "saw two pages"},
		{"engaged-by-active-time", []timedEvent{
			{name: "user_engagement", after: 0, page: "https://portal.internal/a"},
		}, true, "reported active time past the threshold"},
		{"not-engaged", []timedEvent{
			{name: "page_view", after: 0, page: "https://portal.internal/a"},
		}, false, "arrived and left"},
	}
	for _, visit := range visits {
		f.deliverEngagement(t, f.otherKey, visit.session, "visitor-"+visit.session, visit.events, start, threshold+5)
	}
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	check := func(stage string) {
		t.Helper()
		for _, visit := range visits {
			var engaged bool
			if err := pool.QueryRow(ctx, `SELECT engaged FROM sessions WHERE session_id=$1`, visit.session).Scan(&engaged); err != nil {
				t.Fatalf("%s: read %s: %v", stage, visit.session, err)
			}
			if engaged != visit.engaged {
				t.Errorf("%s: a session that %s reads engaged=%v, want %v", stage, visit.why, engaged, visit.engaged)
			}
		}
	}
	check("as the collector built it")

	// The rebuild reconstructs every session from the events alone. It runs
	// inside a privacy deletion, so a site that has ever had one is reading
	// numbers this path produced.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := service.RebuildSiteDerivedData(ctx, tx, siteID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("rebuild: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	check("as the rebuild reconstructed it")

	// And the overview's own fallback, which is consulted when no session row
	// exists at all. Four of the five visits are engaged, whichever path counts
	// them.
	day := f.siteDate(t, -120)
	fromSessions := f.get(t, "/api/v1/sites/"+f.otherKey+"/overview?from="+day+"&to="+day)
	if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE site_id=$1`, siteID); err != nil {
		t.Fatalf("remove the session rows: %v", err)
	}
	fromEvents := f.get(t, "/api/v1/sites/"+f.otherKey+"/overview?from="+day+"&to="+day)
	for _, measure := range []string{"sessions", "engagement_rate"} {
		stored, _ := fromSessions["current"].(map[string]any)[measure].(float64)
		derived, _ := fromEvents["current"].(map[string]any)[measure].(float64)
		if stored != derived {
			t.Errorf("%s: the sessions table says %v and the events the overview falls back to say %v — the fallback exists so a reader sees the same screen, not a different one",
				measure, stored, derived)
		}
	}
	if rate, _ := fromSessions["current"].(map[string]any)["engagement_rate"].(float64); rate != 80 {
		t.Errorf("four of five visits are engaged and the engagement rate reads %v", rate)
	}
}

// deliverEngagement posts a visit, giving a user_engagement event the active
// time that is the point of it.
func (f fixture) deliverEngagement(t *testing.T, siteKey, sessionID, visitorID string, events []timedEvent, start time.Time, activeSeconds int) {
	t.Helper()
	parts := make([]string, 0, len(events))
	for _, item := range events {
		properties := "{}"
		switch item.name {
		case "purchase":
			properties = `{"value":"1000"}`
		case "user_engagement":
			properties = fmt.Sprintf(`{"active_seconds":"%d"}`, activeSeconds)
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":%s,"contract_version":1,"context":{"page":{"url":%q,"title":"페이지","referrer":""}}}`,
			uuid.NewString(), item.name, start.Add(item.after).UnixMilli(), properties, item.page))
	}
	f.postCollect(t, siteKey, sessionID, visitorID, events[0].page, parts)
}
