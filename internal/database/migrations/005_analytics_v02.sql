CREATE TABLE retention_policies (
  site_id uuid PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  raw_event_months integer NOT NULL DEFAULT 13 CHECK(raw_event_months BETWEEN 1 AND 120),
  session_months integer NOT NULL DEFAULT 25 CHECK(session_months BETWEEN 1 AND 120),
  aggregation_months integer CHECK(aggregation_months IS NULL OR aggregation_months BETWEEN 1 AND 1200),
  realtime_hours integer NOT NULL DEFAULT 24 CHECK(realtime_hours BETWEEN 1 AND 168),
  debug_days integer NOT NULL DEFAULT 7 CHECK(debug_days BETWEEN 1 AND 90),
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO retention_policies(site_id)
SELECT id FROM sites
ON CONFLICT(site_id) DO NOTHING;

CREATE TABLE sessions (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  session_id text NOT NULL,
  visitor_id text NOT NULL,
  user_id text,
  started_at timestamptz NOT NULL,
  last_event_at timestamptz NOT NULL,
  event_count bigint NOT NULL DEFAULT 0,
  page_views bigint NOT NULL DEFAULT 0,
  conversion_count bigint NOT NULL DEFAULT 0,
  engaged boolean NOT NULL DEFAULT false,
  landing_page text,
  exit_page text,
  source text,
  medium text,
  campaign text,
  device_type text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id, session_id)
);

INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type)
SELECT
  site_id,
  session_id,
  (array_agg(visitor_id ORDER BY event_timestamp,id))[1],
  max(user_id),
  min(event_timestamp),
  max(event_timestamp),
  count(*),
  count(*) FILTER(WHERE event_name='page_view'),
  count(*) FILTER(WHERE is_conversion),
  bool_or(event_name='user_engagement'),
  (array_agg(page_url ORDER BY event_timestamp,id) FILTER(WHERE event_name='page_view' AND page_url IS NOT NULL))[1],
  (array_agg(page_url ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE event_name='page_view' AND page_url IS NOT NULL))[1],
  (array_agg(source ORDER BY event_timestamp,id) FILTER(WHERE source IS NOT NULL))[1],
  (array_agg(medium ORDER BY event_timestamp,id) FILTER(WHERE medium IS NOT NULL))[1],
  (array_agg(campaign ORDER BY event_timestamp,id) FILTER(WHERE campaign IS NOT NULL))[1],
  (array_agg(device_type ORDER BY event_timestamp,id) FILTER(WHERE device_type IS NOT NULL))[1]
FROM raw_events
GROUP BY site_id,session_id
ON CONFLICT(site_id,session_id) DO NOTHING;

CREATE INDEX sessions_site_last_event_idx ON sessions(site_id,last_event_at DESC);
CREATE INDEX sessions_visitor_idx ON sessions(site_id,visitor_id,last_event_at DESC);

CREATE TABLE segments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  owner_id uuid REFERENCES users(id) ON DELETE SET NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  definition jsonb NOT NULL,
  shared boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(site_id, owner_id, name)
);
CREATE INDEX segments_site_idx ON segments(site_id,updated_at DESC);
CREATE INDEX raw_events_identity_event_idx ON raw_events(site_id,(coalesce(nullif(user_id,''),visitor_id)),event_name,event_timestamp DESC);

ALTER TABLE dimensions ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
ALTER TABLE dimensions ADD COLUMN IF NOT EXISTS data_type text NOT NULL DEFAULT 'string';
ALTER TABLE dimensions ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE dimensions DROP CONSTRAINT IF EXISTS dimensions_data_type_check;
ALTER TABLE dimensions ADD CONSTRAINT dimensions_data_type_check CHECK(data_type IN ('string','number','boolean','date'));

ALTER TABLE saved_reports ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
ALTER TABLE saved_reports ADD COLUMN IF NOT EXISTS shared boolean NOT NULL DEFAULT false;
ALTER TABLE saved_reports ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS saved_reports_site_kind_idx ON saved_reports(site_id,kind,updated_at DESC);
