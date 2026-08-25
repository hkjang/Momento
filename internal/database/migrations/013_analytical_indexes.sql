-- Indexes for the access patterns the attribution, visitor search and landing page
-- reports introduced. Both tables are keyed by session or by person, not by event,
-- so building these is bounded work; the event table is deliberately left alone
-- because a non-concurrent index build there would block ingestion at startup.
CREATE INDEX IF NOT EXISTS sessions_environment_started_idx
  ON sessions(site_id, environment, started_at);

-- Visitor search matches a fragment anywhere in an identifier, which a btree index
-- cannot serve. pg_trgm can, but a hardened on-premise database may not allow the
-- extension, so the whole block degrades to a sequential scan instead of failing
-- the migration.
DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_trgm;
  CREATE INDEX IF NOT EXISTS identified_users_user_trgm_idx
    ON identified_users USING gin (lower(user_id) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS visitors_visitor_trgm_idx
    ON visitors USING gin (lower(visitor_id) gin_trgm_ops);
EXCEPTION WHEN insufficient_privilege OR feature_not_supported THEN
  RAISE NOTICE 'pg_trgm unavailable; visitor search falls back to a sequential scan';
END $$;
