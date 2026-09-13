package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hkjang/Momento/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetryableRebuildErrorNamesOnlyTheTransientClasses(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"deadlock", &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}, true},
		{"serialization", &pgconn.PgError{Code: "40001", Message: "could not serialize access"}, true},
		{"wrapped deadlock", fmt.Errorf("rebuild: %w", &pgconn.PgError{Code: "40P01"}), true},
		{"statement timeout", &pgconn.PgError{Code: "57014", Message: "canceling statement"}, false},
		{"missing table", &pgconn.PgError{Code: "42P01"}, false},
		{"not a database error", errors.New("date range is required for late_event"), false},
		{"nil", nil, false},
	} {
		if got := retryableRebuildError(tc.err); got != tc.want {
			t.Errorf("%s: retryable=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// Seeding seventy days of late events queued seventy late_event rebuilds, and
// seventeen of them finished as failed with "deadlock detected": each rebuild
// deletes and re-inserts a day's rows in one transaction while the event worker
// upserts the same rows from its own batch, and the two take the rows in a
// different order. Nothing was wrong with the rows — running the same job again
// once the batch had committed succeeded — but a failed job is not run again,
// so the day stayed wrong until an operator noticed.
//
// The test builds that deadlock deliberately. It holds a metrics row for the
// day, waits for the rebuild to block on it, then asks for a sessions row the
// rebuild already deleted. The rebuild has been waiting longer, so it is the
// one to detect the cycle and be rolled back.
func TestMaintenanceRequeuesARebuildThatLostADeadlock(t *testing.T) {
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
	var siteID string
	if err := pool.QueryRow(ctx, `WITH org AS (INSERT INTO organizations(name,slug) VALUES('Rebuild Deadlock','rebuild-deadlock-test') RETURNING id),
		ws AS (INSERT INTO workspaces(organization_id,name) SELECT id,'Workspace' FROM org RETURNING id)
		INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		SELECT id,'SITE_REBUILD_DEADLOCK','Rebuild Deadlock','Rebuild Deadlock','hash','pfx','hash','pfx',ARRAY['portal.internal'],'Asia/Seoul' FROM ws RETURNING id`).Scan(&siteID); err != nil {
		t.Fatalf("site for this test: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE slug='rebuild-deadlock-test'`)
	})
	const day = "2026-01-15"
	if _, err := pool.Exec(ctx, `INSERT INTO daily_site_metrics(site_id,event_date,environment,events) VALUES($1,$2,'prd',1)`, siteID, day); err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,first_seen,last_seen) VALUES($1,$2,'prd','s1','v1',now(),now())`, siteID, day); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	var jobID string
	if err := pool.QueryRow(ctx, `INSERT INTO aggregate_jobs(site_id,environment,job_type,date_from,date_to,reason) VALUES($1,'prd','late_event',$2::date,$2::date,'test') RETURNING id`, siteID, day).Scan(&jobID); err != nil {
		t.Fatalf("queue the rebuild: %v", err)
	}

	// The other party: a transaction that already holds the day's metrics row,
	// the last table the rebuild clears.
	other, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer other.Rollback(ctx)
	if _, err := other.Exec(ctx, `SELECT 1 FROM daily_site_metrics WHERE site_id=$1 AND event_date=$2 FOR UPDATE`, siteID, day); err != nil {
		t.Fatalf("hold the metrics row: %v", err)
	}

	type outcome struct {
		ran bool
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		ran, err := Maintenance{DB: pool}.RunPending(ctx)
		done <- outcome{ran, err}
	}()

	// Wait until the rebuild is the one waiting, so that it — not this test —
	// is the transaction that has waited longest when the cycle closes.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'DELETE FROM daily_site_metrics%'`).Scan(&waiting); err != nil {
			t.Fatalf("watch for the rebuild to block: %v", err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the rebuild never blocked on the held metrics row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Closing the cycle: the rebuild holds the sessions rows it deleted.
	_, lockErr := other.Exec(ctx, `SELECT 1 FROM daily_site_sessions WHERE site_id=$1 AND event_date=$2 FOR UPDATE`, siteID, day)

	var result outcome
	select {
	case result = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the rebuild did not return after the deadlock")
	}
	if lockErr != nil {
		t.Fatalf("the test's transaction was chosen as the deadlock victim, not the rebuild: %v", lockErr)
	}
	if result.err != nil {
		t.Fatalf("a lost deadlock is not a failure to report: %v", result.err)
	}
	if result.ran {
		t.Fatal("a job put back on the queue must be reported as not run, or a draining caller spins on it")
	}
	var status string
	var attempts int
	var reason *string
	if err := pool.QueryRow(ctx, `SELECT status,attempts,error FROM aggregate_jobs WHERE id=$1`, jobID).Scan(&status, &attempts, &reason); err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("job is %s after %d attempts, want pending after 1", status, attempts)
	}
	if reason == nil || *reason == "" {
		t.Fatal("the reason the job was put back is not recorded")
	}

	// With the other transaction gone the same job goes through.
	if err := other.Rollback(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	ran, err := Maintenance{DB: pool}.RunPending(ctx)
	if err != nil || !ran {
		t.Fatalf("retry: ran=%v err=%v", ran, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status,attempts FROM aggregate_jobs WHERE id=$1`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	if status != "success" || attempts != 2 {
		t.Fatalf("job is %s after %d attempts, want success after 2", status, attempts)
	}
}

// The bound is the difference between a retry and a job that hides in the
// queue forever. A job that has already used its attempts fails like any other.
func TestMaintenanceFailsARebuildOutOfAttempts(t *testing.T) {
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
	var siteID string
	if err := pool.QueryRow(ctx, `WITH org AS (INSERT INTO organizations(name,slug) VALUES('Rebuild Attempts','rebuild-attempts-test') RETURNING id),
		ws AS (INSERT INTO workspaces(organization_id,name) SELECT id,'Workspace' FROM org RETURNING id)
		INSERT INTO sites(workspace_id,site_key,name,service_name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix,allowed_domains,timezone)
		SELECT id,'SITE_REBUILD_ATTEMPTS','Rebuild Attempts','Rebuild Attempts','hash','pfx','hash','pfx',ARRAY['portal.internal'],'Asia/Seoul' FROM ws RETURNING id`).Scan(&siteID); err != nil {
		t.Fatalf("site for this test: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE slug='rebuild-attempts-test'`)
	})
	const day = "2026-01-16"
	if _, err := pool.Exec(ctx, `INSERT INTO daily_site_metrics(site_id,event_date,environment,events) VALUES($1,$2,'prd',1)`, siteID, day); err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,first_seen,last_seen) VALUES($1,$2,'prd','s1','v1',now(),now())`, siteID, day); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	var jobID string
	if err := pool.QueryRow(ctx, `INSERT INTO aggregate_jobs(site_id,environment,job_type,date_from,date_to,reason,attempts) VALUES($1,'prd','late_event',$2::date,$2::date,'test',$3) RETURNING id`, siteID, day, maxRebuildAttempts-1).Scan(&jobID); err != nil {
		t.Fatalf("queue the rebuild: %v", err)
	}
	other, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer other.Rollback(ctx)
	if _, err := other.Exec(ctx, `SELECT 1 FROM daily_site_metrics WHERE site_id=$1 AND event_date=$2 FOR UPDATE`, siteID, day); err != nil {
		t.Fatalf("hold the metrics row: %v", err)
	}
	type outcome struct {
		ran bool
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		ran, err := Maintenance{DB: pool}.RunPending(ctx)
		done <- outcome{ran, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'DELETE FROM daily_site_metrics%'`).Scan(&waiting); err != nil {
			t.Fatalf("watch for the rebuild to block: %v", err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the rebuild never blocked on the held metrics row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, lockErr := other.Exec(ctx, `SELECT 1 FROM daily_site_sessions WHERE site_id=$1 AND event_date=$2 FOR UPDATE`, siteID, day)
	var result outcome
	select {
	case result = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the rebuild did not return after the deadlock")
	}
	if lockErr != nil {
		t.Fatalf("the test's transaction was chosen as the deadlock victim, not the rebuild: %v", lockErr)
	}
	if !retryableRebuildError(result.err) || !result.ran {
		t.Fatalf("the last attempt reports its deadlock as a failure: ran=%v err=%v", result.ran, result.err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM aggregate_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	if status != "failed" {
		t.Fatalf("job is %s, want failed once its attempts are used up", status)
	}
}
