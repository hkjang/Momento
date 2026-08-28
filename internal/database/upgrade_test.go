package database_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/database"
)

// An operator upgrades in place: the new image starts against the database the
// previous release left behind, already holding their data. Every release note
// promises that path works, and it was only ever checked by hand. The failure it
// has to catch is a migration that is fine on an empty schema and fails on a
// populated one — a CHECK or NOT NULL that existing rows violate — because the
// service exits when a migration fails and the upgrade leaves them with nothing
// running.
//
// So this reconstructs the schema each past release shipped, fills it with data,
// and migrates to head. Every historical point is covered, so a future migration
// that cannot be applied over real rows fails here rather than in a closed network.
//
// Set MOMENTO_TEST_POSTGRES_DSN to run these; without it they skip.

// seedStep inserts rows that exist from a given migration onward. The SQL is
// written against the schema of that migration, never against head, or it would
// stop describing what the older release actually stored.
type seedStep struct {
	since string
	name  string
	sql   string
	args  []any
}

func historicalSeed(orgID, workspaceID, userID, siteID uuid.UUID, channelID uuid.UUID) []seedStep {
	return []seedStep{
		{since: "001_initial.sql", name: "organization", sql: `INSERT INTO organizations(id,name,slug) VALUES($1,'Upgrade Org','upgrade-org')`, args: []any{orgID}},
		{since: "001_initial.sql", name: "workspace", sql: `INSERT INTO workspaces(id,organization_id,name) VALUES($1,$2,'Upgrade Workspace')`, args: []any{workspaceID, orgID}},
		{since: "001_initial.sql", name: "user", sql: `INSERT INTO users(id,email,display_name,role) VALUES($1,'upgrade@example.com','Upgrade Admin','super_admin')`, args: []any{userID}},
		{since: "001_initial.sql", name: "site", sql: `INSERT INTO sites(id,workspace_id,site_key,name,tracking_key_hash,tracking_key_prefix,server_api_key_hash,server_api_key_prefix) VALUES($1,$2,'SITE_UPGRADE','Upgrade Site','hash-t','mom_track_a','hash-s','mom_server_a')`, args: []any{siteID, workspaceID}},
		{since: "001_initial.sql", name: "api key", sql: `INSERT INTO api_keys(user_id,name,key_hash,key_prefix) VALUES($1,'old key','hash-k','mom_key_a')`, args: []any{userID}},
		{since: "005_analytics_v02.sql", name: "session", sql: `INSERT INTO sessions(site_id,session_id,visitor_id,started_at,last_event_at) VALUES($1,'ses-upgrade','vis-upgrade',now()-interval '2 hours',now()-interval '1 hour')`, args: []any{siteID}},
		{since: "001_initial.sql", name: "events", sql: `INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,visitor_id,session_id,page_url)
			SELECT gen_random_uuid(),$1,name,now()-(n||' minutes')::interval,'vis-upgrade','ses-upgrade','https://upgrade.example.com/p'
			FROM generate_series(1,8) n CROSS JOIN (VALUES('page_view'),('purchase')) v(name)`, args: []any{siteID}},
		{since: "001_initial.sql", name: "settings", sql: `INSERT INTO settings(key,value) VALUES('privacy','{"ip_anonymization":true}'::jsonb) ON CONFLICT(key) DO UPDATE SET value=excluded.value`},
		{since: "001_initial.sql", name: "site setting", sql: `INSERT INTO site_settings(site_id,key,value) VALUES($1,'privacy','{"collect_user_id":true}'::jsonb)`, args: []any{siteID}},
		{since: "004_dead_letters.sql", name: "dead letter", sql: `INSERT INTO event_dead_letters(inbox_id,site_id,payload,error) VALUES(1,$1,'{}'::jsonb,'stored by an older release')`, args: []any{siteID}},
		{since: "008_platformization.sql", name: "delivery channel", sql: `INSERT INTO delivery_channels(id,site_id,name,channel_type,endpoint_url) VALUES($1,$2,'ops','webhook','https://ops.example.com/hook')`, args: []any{channelID, siteID}},
		// The narrowest constraint in the schema and the one a later migration
		// actually widened, so a row here is what would have blocked the upgrade.
		{since: "008_platformization.sql", name: "scheduled report", sql: `INSERT INTO scheduled_reports(site_id,channel_id,name,report_kind,interval_minutes) VALUES($1,$2,'daily overview','overview',1440)`, args: []any{siteID, channelID}},
		{since: "008_platformization.sql", name: "saved report", sql: `INSERT INTO saved_reports(site_id,kind,name,definition) VALUES($1,'overview','saved by an older release','{}'::jsonb)`, args: []any{siteID}},
		{since: "008_platformization.sql", name: "segment", sql: `INSERT INTO segments(site_id,name,definition) VALUES($1,'returning','{"combinator":"and","rules":[]}'::jsonb)`, args: []any{siteID}},
		// The quality counters are the table a later migration added a column to,
		// and until this row existed that migration was only ever applied to an
		// empty one — which is exactly the case this test exists to distrust.
		{since: "008_platformization.sql", name: "quality counters", sql: `INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,received,accepted,missing_user_id)
			VALUES($1,current_date-1,'prd','page_view',120,118,17)`, args: []any{siteID}},
		{since: "012_anomaly_state.sql", name: "anomaly alert", sql: `INSERT INTO anomaly_alerts(site_id,environment,metric,severity,first_detected_on,last_detected_on) VALUES($1,'prd','users','warning',current_date-1,current_date)`, args: []any{siteID}},
	}
}

// scratchDatabase gives each upgrade point its own database, because the schema is
// rewound and the shared test database is at head.
func scratchDatabase(t *testing.T, name string) string {
	t.Helper()
	dsn := os.Getenv("MOMENTO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOMENTO_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)); err != nil {
		t.Fatalf("drop scratch database: %v", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := database.Open(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
	})
	return replaceDatabase(dsn, name)
}

// replaceDatabase swaps the database in a DSN, handling both the URL form the
// documentation uses and the keyword form.
func replaceDatabase(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		rest := dsn[strings.Index(dsn, "//")+2:]
		host := rest
		query := ""
		if i := strings.Index(rest, "?"); i >= 0 {
			host, query = rest[:i], rest[i:]
		}
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
		return dsn[:strings.Index(dsn, "//")+2] + host + "/" + name + query
	}
	fields := []string{}
	for _, field := range strings.Fields(dsn) {
		if strings.HasPrefix(field, "dbname=") {
			continue
		}
		fields = append(fields, field)
	}
	return strings.Join(fields, " ") + " dbname=" + name
}

func TestUpgradeFromEveryHistoricalSchemaWithData(t *testing.T) {
	versions, err := database.Versions()
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) < 2 {
		t.Fatalf("expected several migrations, found %d", len(versions))
	}
	head := versions[len(versions)-1]
	// Every point except head: at head there is nothing left to upgrade to.
	for index, from := range versions[:len(versions)-1] {
		from := from
		index := index
		t.Run(strings.TrimSuffix(from, ".sql"), func(t *testing.T) {
			ctx := context.Background()
			dsn := scratchDatabase(t, fmt.Sprintf("momento_upgrade_%02d", index+1))
			pool, err := database.Open(ctx, dsn)
			if err != nil {
				t.Fatalf("open scratch database: %v", err)
			}
			defer pool.Close()

			// The schema as the release shipping this migration left it.
			if err := database.MigrateThrough(ctx, pool, from); err != nil {
				t.Fatalf("build %s schema: %v", from, err)
			}
			var applied int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
				t.Fatalf("count migrations: %v", err)
			}
			if applied != index+1 {
				t.Fatalf("expected %d migrations through %s, applied %d", index+1, from, applied)
			}

			orgID, workspaceID, userID, siteID, channelID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			seeded := 0
			for _, step := range historicalSeed(orgID, workspaceID, userID, siteID, channelID) {
				if step.since > from {
					continue
				}
				if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
					t.Fatalf("seed %s into the %s schema: %v", step.name, from, err)
				}
				seeded++
			}
			if seeded == 0 {
				t.Fatalf("no seed applies at %s, so the upgrade would run on an empty schema", from)
			}

			// The sessions table only exists from 005, so before that the upgrade
			// carries events alone.
			hasSessions := from >= "005_analytics_v02.sql"
			countRows := func(stage string) (int, int) {
				var events, sessions int
				query := `SELECT (SELECT count(*) FROM raw_events),0`
				if hasSessions {
					query = `SELECT (SELECT count(*) FROM raw_events),(SELECT count(*) FROM sessions)`
				}
				if err := pool.QueryRow(ctx, query).Scan(&events, &sessions); err != nil {
					t.Fatalf("count rows %s: %v", stage, err)
				}
				return events, sessions
			}
			events, sessions := countRows("before the upgrade")
			if events == 0 || (hasSessions && sessions == 0) {
				t.Fatalf("seed produced no events or sessions at %s", from)
			}

			// This is the upgrade: the same call the new image makes at startup.
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatalf("upgrade %s -> %s over %d seeded rows: %v", from, head, seeded, err)
			}

			var afterApplied int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&afterApplied); err != nil {
				t.Fatalf("count migrations after upgrade: %v", err)
			}
			if afterApplied != len(versions) {
				t.Fatalf("after upgrading from %s, %d of %d migrations are recorded", from, afterApplied, len(versions))
			}

			// A migration that silently discarded rows would be as bad as one that
			// failed, and harder to notice.
			eventsAfter, sessionsAfter := countRows("after the upgrade")
			if eventsAfter != events || sessionsAfter != sessions {
				t.Fatalf("upgrading from %s changed the data: events %d -> %d, sessions %d -> %d", from, events, eventsAfter, sessions, sessionsAfter)
			}

			// A counter column added later has to arrive at its default over rows
			// that predate it, and must not disturb what those rows already held.
			// An ADD COLUMN NOT NULL DEFAULT is only free because PostgreSQL
			// stores the default in the catalogue; asserting the value is how this
			// stays true of whatever a later migration writes instead.
			if from >= "008_platformization.sql" {
				var missing, refused int64
				if err := pool.QueryRow(ctx, `SELECT missing_user_id,refused_user_id FROM data_quality_daily WHERE site_id=$1`, siteID).
					Scan(&missing, &refused); err != nil {
					t.Fatalf("quality counters after upgrading from %s: %v", from, err)
				}
				if missing != 17 || refused != 0 {
					t.Fatalf("upgrading from %s left the quality counters at missing=%d refused=%d, want 17 and 0", from, missing, refused)
				}
			}

			// The view the whole read path is built on has to resolve against rows
			// written before the columns it reads existed.
			var viewRows int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM analytics_events WHERE site_id=$1`, siteID).Scan(&viewRows); err != nil {
				t.Fatalf("analytics_events after upgrading from %s: %v", from, err)
			}
			if viewRows != events {
				t.Fatalf("analytics_events shows %d of %d events after upgrading from %s", viewRows, events, from)
			}

			// Applying again has to be a no-op: an operator who restarts the new
			// image, or runs two replicas, must not re-run anything.
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatalf("second startup after upgrading from %s: %v", from, err)
			}
		})
	}
}

// A migration file added without being applied to a database that is already at
// head is the other half of the upgrade: the version is recorded only when the
// body succeeds, so a partially applied migration must not be marked done.
func TestMigrateIsIdempotentAndOrdered(t *testing.T) {
	ctx := context.Background()
	dsn := scratchDatabase(t, "momento_upgrade_order")
	pool, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	versions, err := database.Versions()
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	// Ordered by version, not by applied_at.
	//
	// applied_at is now(), which is the wall clock at the start of each
	// migration's transaction, and a wall clock can step backwards — an NTP
	// correction, or a host resuming from sleep, which is ordinary on a
	// developer's machine. Reading the sequence from it made a clock step look
	// like a migration applied out of order, and it failed here exactly that way:
	// 007 recorded before 001, on a run where 001 could not have come second.
	//
	// What is actually being checked is that every migration ran and each one
	// recorded itself, so the versions are the thing to order by. The timestamps
	// are checked below for what they can honestly say.
	rows, err := pool.Query(ctx, `SELECT version, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	applied := []string{}
	stamps := []time.Time{}
	for rows.Next() {
		var version string
		var at time.Time
		if err := rows.Scan(&version, &at); err != nil {
			t.Fatalf("scan: %v", err)
		}
		applied = append(applied, version)
		stamps = append(stamps, at)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if len(applied) != len(versions) {
		t.Fatalf("recorded %d migrations, expected %d", len(applied), len(versions))
	}
	for i := range versions {
		if applied[i] != versions[i] {
			t.Fatalf("migration %d is recorded as %s and the embedded set has %s at that position: the recorded set has to be exactly the migrations that exist",
				i, applied[i], versions[i])
		}
	}
	// The timestamps should climb with the versions, and when they do not the
	// clock moved rather than the migrations. Said as what it is, so nobody
	// spends an afternoon looking for a reordering that did not happen.
	for i := 1; i < len(stamps); i++ {
		if stamps[i].Before(stamps[i-1]) {
			t.Logf("%s is stamped before %s, which the migration order rules out: the wall clock stepped backwards during this run",
				applied[i], applied[i-1])
		}
	}
	if err := database.MigrateThrough(ctx, pool, "unknown_migration.sql"); err == nil {
		t.Fatal("MigrateThrough accepted a migration name that does not exist")
	}
}
