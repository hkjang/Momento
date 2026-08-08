CREATE TABLE IF NOT EXISTS event_dead_letters (
  id bigserial PRIMARY KEY,
  inbox_id bigint NOT NULL,
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  payload jsonb NOT NULL,
  error text NOT NULL,
  failed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS event_dead_letters_site_time_idx ON event_dead_letters(site_id,failed_at DESC);
