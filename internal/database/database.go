package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// PoolSize is how many connections the service will hold. A report answers by
// running its independent reads together, so the pool has to cover several
// readers at once rather than one at a time: at twenty, two people opening the
// insight report at the same moment queued behind each other with the database
// idle. It stays well inside PostgreSQL's default max_connections of 100, and an
// operator whose database is shared can lower it.
const PoolSize = 32

// analyticalWorkMem is set on every connection. These reports count distinct
// people and sessions, which PostgreSQL answers by sorting; at the 4MB default
// every one of those sorts spilled to disk on a site of any size. This is per
// sort node, so it is deliberately modest rather than generous.
const analyticalWorkMem = "32MB"

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	if size, err := strconv.Atoi(os.Getenv("MOMENTO_POSTGRES_MAX_CONNS")); err == nil && size > 0 {
		cfg.MaxConns = int32(size)
	} else {
		cfg.MaxConns = PoolSize
	}
	cfg.MinConns = 2
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET work_mem = '"+analyticalWorkMem+"'")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// Versions lists the migrations in the order they are applied. Upgrade tests use
// it to reconstruct the schema an older release shipped and then migrate forward,
// which is the path an operator takes and the one that has to keep working.
func Versions() ([]string, error) {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return MigrateThrough(ctx, pool, "")
}

// migrateLock is the advisory lock every migration run takes. The number is
// arbitrary; it only has to be the same one everywhere and unlike any other
// advisory lock this database uses.
const migrateLock = 0x6d6f6d656e746f

// MigrateThrough applies migrations in order and stops after the named one. An
// empty name applies all of them, which is what the service does at startup.
//
// Every process that opens the database migrates it, and more than one can be
// opening it at once: `go test ./...` runs a process per package and each one
// migrates the same database before its first query. Reading schema_migrations
// and then applying what is missing is safe alone and not safe together — two
// processes both find a migration unapplied, both run it, and the second one
// loses on a catalog index rather than on the table it was creating
// (pg_extension_name_index for CREATE EXTENSION, pg_type_typname_nsp_index for
// CREATE TYPE), which reads as a corrupt migration rather than a race. One
// advisory lock around the whole run makes the second process wait and then
// find the work already done.
func MigrateThrough(ctx context.Context, pool *pgxpool.Pool, last string) error {
	// Everything below runs on this one connection: the lock is held by the
	// session that took it, and a pool sized to a single connection would
	// otherwise deadlock against itself.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(migrateLock)); err != nil {
		return err
	}
	defer func() {
		// The caller's context may already be done by the time we unlock, and an
		// advisory lock left behind outlives the statement that took it.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(migrateLock))
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := Versions()
	if err != nil {
		return err
	}
	if last != "" {
		cut := -1
		for i, name := range names {
			if name == last {
				cut = i
				break
			}
		}
		if cut < 0 {
			return fmt.Errorf("unknown migration %s", last)
		}
		names = names[:cut+1]
	}
	for _, name := range names {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, name)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
