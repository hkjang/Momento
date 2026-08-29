package httpapi

import (
	"context"
	"testing"

	"github.com/hkjang/Momento/internal/service"
)

// Retention already learned this lesson once. The comment it left says it: the
// identity tables were unbounded, so once a person's events and sessions had
// been removed their visitor_id -> user_id mapping stayed behind with no policy
// and no expiry, and an operator who set a window to satisfy an obligation had
// not met it.
//
// The fix covered visitor_identities, visitors and identified_users. It did not
// cover visitor_sessions, which carries the same mapping — site, visitor,
// session, and the SSO user id the collector back-fills onto it — plus the first
// and last time that person was seen in that session. Nothing ever deleted a row
// from it. Erasure of a single person is safe, because that path rebuilds the
// derived tables from what survives; the retention window is the path that
// deletes incrementally, and it walked past this table.
//
// It describes a session, so it expires with one: the same rule the sessions
// table itself uses, read from the same policy column.
func TestRetentionExpiresTheVisitorSessionIndex(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO retention_policies(site_id,raw_event_months,session_months,realtime_hours,debug_days)
		VALUES($1,1,1,1,1) ON CONFLICT(site_id) DO UPDATE SET raw_event_months=1,session_months=1`, f.siteID); err != nil {
		t.Fatalf("retention policy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM retention_policies WHERE site_id=$1`, f.siteID)
	})

	// One row as the collector writes them: a visitor, a session, and the user id
	// that identifies the person, last seen well outside the window.
	if _, err := pool.Exec(ctx, `INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen)
		VALUES($1,'expired-visitor','expired-session','EMP_EXPIRED',now()-interval '400 days',now()-interval '400 days')
		ON CONFLICT(site_id,visitor_id,session_id) DO UPDATE SET user_id=excluded.user_id,last_seen=excluded.last_seen`, f.siteID); err != nil {
		t.Fatalf("store the expired row: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen)
		VALUES($1,'current-visitor','current-session','EMP_CURRENT',now()-interval '1 hour',now()-interval '1 hour')
		ON CONFLICT(site_id,visitor_id,session_id) DO UPDATE SET user_id=excluded.user_id,last_seen=excluded.last_seen`, f.siteID); err != nil {
		t.Fatalf("store the current row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM visitor_sessions WHERE site_id=$1 AND visitor_id IN ('expired-visitor','current-visitor')`, f.siteID)
	})

	if err := (service.Worker{DB: pool, RetentionBatchSize: 3}).ApplyRetention(ctx); err != nil {
		t.Fatalf("retention pass: %v", err)
	}

	survives := func(visitor string) bool {
		t.Helper()
		var present bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM visitor_sessions WHERE site_id=$1 AND visitor_id=$2)`, f.siteID, visitor).Scan(&present); err != nil {
			t.Fatalf("read %s back: %v", visitor, err)
		}
		return present
	}
	if survives("expired-visitor") {
		t.Error("a visitor session last seen 400 days ago survived a retention pass with a one-month window: the row still names the visitor and the SSO user it belongs to, so the window an operator set to satisfy an obligation was not met")
	}
	if !survives("current-visitor") {
		t.Error("a visitor session from an hour ago was removed: the pass is deleting inside the policy window")
	}
}
