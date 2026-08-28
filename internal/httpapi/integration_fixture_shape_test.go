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
	for _, signal := range []string{"page_view", "purchase", "web_vital", "error", "resource_error", "user_engagement", "refund"} {
		if events := count(`SELECT count(*) FROM raw_events WHERE site_id=`+site+` AND event_name=$2`, f.siteKey, signal); events == 0 {
			t.Errorf("the fixture has no %s events, so every report that reads them answers zero and agrees with itself", signal)
		}
	}

	// What the fixture does not carry, said out loud rather than discovered.
	//
	// The seed already fixed this for one set of signals — its own comment says
	// they were "events the reports read that nothing was creating, so those paths
	// ran and returned zero and every test passed" — and search and friction were
	// not in that set. Both reports exist, and against this fixture both answer
	// nothing, so a test that compares two nothings agrees however wrong either
	// side is. integration_mcp_agreement_test.go hit exactly that and works around
	// it by delivering its own; so does the frustration agreement test.
	//
	// This is reported rather than asserted because adding them to the shared
	// fixture changes the numbers those tests already pin, which is a change worth
	// making deliberately and not as a side effect of writing this one.
	for _, absent := range []string{"search", "rage_click", "dead_click", "form_retry"} {
		if events := count(`SELECT count(*) FROM raw_events WHERE site_id=`+site+` AND event_name=$2`, f.siteKey, absent); events == 0 {
			t.Logf("the fixture has no %s events: a test about that report has to deliver its own, or it compares nothing with nothing", absent)
		}
	}

	// An anonymous visitor, so identity-scoped reads have somebody to exclude.
	if anonymous := count(`SELECT count(*) FROM raw_events WHERE site_id=`+site+` AND visitor_id='visitor-anon' AND user_id IS NULL`, f.siteKey); anonymous == 0 {
		t.Error("the fixture has no anonymous activity, so nothing separates a visitor from an identified person")
	}
}
