package service

import (
	"context"
	"os"
	"testing"

	"github.com/hkjang/Momento/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessPending is named after draining the inbox, and every caller uses it
// that way: the integration guards deliver a batch, call it once, and then read
// the tables as though everything they sent had arrived.
//
// It processed one batch of a hundred. Beyond that the rows stayed pending and
// the test measured a half-processed period — reporting it as a defect in
// whatever it was looking at. That is how a guard fails for a reason that has
// nothing to do with what it guards: it happened in this repository, on a
// session test, on a full-suite run where earlier tests had left a backlog, and
// it passed on its own three times afterwards.
func TestProcessPendingDrainsMoreThanOneBatch(t *testing.T) {
	dsn := os.Getenv("MOMENTO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOMENTO_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// This test owns the site it hangs the rows off. Borrowing an existing one
	// would tie it to whatever another package's fixture is doing to the same
	// database — go test runs packages concurrently, and the first version of
	// this test failed on a foreign key when the httpapi fixture deleted the
	// site out from under it.
	var siteID string
	if err := pool.QueryRow(ctx, `WITH org AS (INSERT INTO organizations(name,slug) VALUES('Inbox Drain','inbox-drain-test') RETURNING id),
		ws AS (INSERT INTO workspaces(organization_id,name) SELECT id,'Workspace' FROM org RETURNING id)
		INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		SELECT id,'SITE_INBOX_DRAIN','Inbox Drain','Inbox Drain','hash','pfx','hash','pfx',ARRAY['portal.internal'],'Asia/Seoul' FROM ws RETURNING id`).Scan(&siteID); err != nil {
		t.Fatalf("site for this test: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE slug='inbox-drain-test'`)
	})
	// Only this site's rows are counted, for the same reason.
	// More than one batch. The payload is deliberately one the worker will
	// refuse — what is being measured is whether every row is looked at, not
	// what happens to it, and a refused row is marked rather than left pending.
	const rows = 250
	for index := 0; index < rows; index++ {
		if _, err := pool.Exec(ctx, `INSERT INTO event_inbox(site_id,payload) VALUES($1,$2)`, siteID, []byte(`{"events":[]}`)); err != nil {
			t.Fatalf("seed the inbox: %v", err)
		}
	}
	if err := (Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}
	var pending int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_inbox WHERE site_id=$1 AND processed_at IS NULL AND available_at<=now()`, siteID).Scan(&pending); err != nil {
		t.Fatalf("count what is left: %v", err)
	}
	if pending != 0 {
		t.Errorf("%d of %d rows are still waiting after one call to ProcessPending: every caller reads the tables next, so what they measure is a period that only partly arrived",
			pending, rows)
	}
}
