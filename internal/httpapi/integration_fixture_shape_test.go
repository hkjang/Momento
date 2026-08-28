package httpapi

import (
	"context"
	"testing"
)

// Tests are written against the fixture's description, so the description is
// part of the fixture. It said "two services in one workspace, an SSO user
// active on both" and the second service has no events at all — which is how the
// workspace rollup, whose whole purpose is counting a person once across
// services, came to be exercised only against a workspace where nobody was on
// two services.
//
// The asymmetry is deliberate and depended on in both directions: the second
// site is the blank canvas several tests measure against, and workspace-scope
// attribution reads its touch sessions. So this pins the shape rather than
// changing it, and a change to either side has to come with a change to the
// other.
func TestTheFixtureIsWhatItSaysItIs(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	count := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return value
	}

	// Two services, one workspace.
	if sites := count(`SELECT count(*) FROM sites WHERE workspace_id=(SELECT workspace_id FROM sites WHERE site_key=$1)`, f.siteKey); sites != 2 {
		t.Errorf("the workspace holds %d services, and the fixture describes two", sites)
	}

	// The person is linked on both, which is what the identity graph and
	// workspace-scope attribution resolve through.
	if linked := count(`SELECT count(DISTINCT site_id) FROM visitor_identities WHERE user_id=$1`, f.userID); linked != 2 {
		t.Errorf("the SSO person is linked on %d of the two services", linked)
	}

	// The second service has sessions and no events. Both halves matter: the
	// sessions are what attribution credits across services, and the absence of
	// events is what makes the second site usable as a blank canvas.
	otherSessions := count(`SELECT count(*) FROM sessions WHERE site_id=(SELECT id FROM sites WHERE site_key=$1)`, f.otherKey)
	if otherSessions == 0 {
		t.Error("the second service has no touch sessions, so workspace-scope attribution has nothing to credit across services")
	}
	otherEvents := count(`SELECT count(*) FROM raw_events WHERE site_id=(SELECT id FROM sites WHERE site_key=$1)`, f.otherKey)
	if otherEvents != 0 {
		t.Errorf("the second service carries %d events. Several tests measure against it as a blank canvas — integration_trend_test.go compares a total against a chart and says what it delivered is the whole of what both sources see. If this site is meant to carry events now, those tests have to be rewritten first, and the comment on seed has to stop saying otherwise",
			otherEvents)
	}

	// Ten weeks of daily activity on the first service, with the signals the
	// reports read. A fixture missing one of these makes a report answer zero and
	// every assertion about it pass.
	site := `(SELECT id FROM sites WHERE site_key=$1)`
	if days := count(`SELECT count(DISTINCT (event_timestamp AT TIME ZONE 'Asia/Seoul')::date) FROM raw_events WHERE site_id=`+site, f.siteKey); days < 60 {
		t.Errorf("the first service has activity on %d days, and the fixture describes ten weeks", days)
	}
	for _, signal := range []string{
		"page_view", "purchase", "web_vital", "error", "resource_error", "user_engagement", "refund",
		// Search and friction were missing until the report tests had to deliver
		// their own to have anything to compare. Both reports answered nothing
		// against this fixture, so a test comparing two nothings agreed however
		// wrong either side was.
		"search", "search_click", "rage_click", "dead_click", "form_retry",
	} {
		if events := count(`SELECT count(*) FROM raw_events WHERE site_id=`+site+` AND event_name=$2`, f.siteKey, signal); events == 0 {
			t.Errorf("the fixture has no %s events, so every report that reads them answers zero and agrees with itself", signal)
		}
	}

	// Friction has to reach somebody who does not convert, or the impact report
	// compares one population against nothing and every gap it states is the
	// whole population's rate wearing a comparison.
	unconverted := count(`SELECT count(*) FROM (
		SELECT entity_id FROM analytics_events WHERE site_id=`+site+`
		GROUP BY entity_id
		HAVING bool_or(event_name IN ('rage_click','dead_click','form_retry')) AND NOT bool_or(is_conversion)) people`, f.siteKey)
	if unconverted == 0 {
		t.Error("everybody who hits friction in this fixture also converts, so both conversion rates are 100%, every gap the impact report states is zero, and a report that computed the gap backwards would produce the same output")
	}

	// Every visitor the events know about has to be in the daily rollup, because
	// that is where the reports read who is new — the overview, the insight report
	// and the cohort grid all resolve first-seen through insight.FirstSeenCTE. A
	// visitor with events and no rollup row is a person the events count and the
	// screens do not, and the disagreement is reported as the product's.
	missing := count(`SELECT count(DISTINCT e.visitor_id) FROM raw_events e WHERE e.site_id=`+site+`
		AND NOT EXISTS(SELECT 1 FROM daily_site_visitors d WHERE d.site_id=e.site_id AND d.visitor_id=e.visitor_id)`, f.siteKey)
	if missing > 0 {
		t.Errorf("%d visitor(s) have events and no daily_site_visitors row: the events see them and every screen that asks who is new does not", missing)
	}

	// An anonymous visitor, so identity-scoped reads have somebody to exclude.
	if anonymous := count(`SELECT count(*) FROM raw_events WHERE site_id=`+site+` AND visitor_id='visitor-anon' AND user_id IS NULL`, f.siteKey); anonymous == 0 {
		t.Error("the fixture has no anonymous activity, so nothing separates a visitor from an identified person")
	}
}
