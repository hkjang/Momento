package database_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/hkjang/Momento/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// `go test ./...` runs one process per package and every one of them migrates
// the same database before its first query. Reading schema_migrations and then
// applying what is missing is safe alone and not safe together: two processes
// both find a migration unapplied, both run it, and the second one fails on a
// catalog index rather than on the table it was creating — CREATE TYPE loses on
// pg_type_typname_nsp_index, CREATE EXTENSION on pg_extension_name_index. That
// reads as a corrupt migration, not as a race, and it is what turned three
// tests red on main the moment enough packages carried database tests to start
// at the same time.
//
// Ten at once against one empty database, which is more than the suite ever
// starts and enough that the old code failed every run.
func TestMigrateIsSafeWhenProcessesStartTogether(t *testing.T) {
	dsn := os.Getenv("MOMENTO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOMENTO_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	const starters = 10
	errs := make([]error, starters)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// A pool each, as separate processes would have.
			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				errs[i] = err
				return
			}
			defer pool.Close()
			errs[i] = database.Migrate(ctx, pool)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("migrate %d of %d: %v", i+1, starters, err)
		}
	}
}
