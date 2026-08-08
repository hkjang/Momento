CREATE TABLE visitor_identities (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  visitor_id text NOT NULL,
  user_id text NOT NULL,
  first_seen timestamptz NOT NULL,
  linked_at timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  confidence numeric(4,3) NOT NULL DEFAULT 1.000 CHECK(confidence >= 0 AND confidence <= 1),
  source text NOT NULL DEFAULT 'identify',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,visitor_id)
);
CREATE INDEX visitor_identities_user_idx ON visitor_identities(site_id,user_id);

CREATE TABLE visitors (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  visitor_id text NOT NULL,
  user_id text,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  event_count bigint NOT NULL DEFAULT 0,
  conversion_count bigint NOT NULL DEFAULT 0,
  user_properties jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,visitor_id)
);
CREATE INDEX visitors_site_first_seen_idx ON visitors(site_id,first_seen);
CREATE INDEX visitors_site_last_seen_idx ON visitors(site_id,last_seen DESC);
CREATE INDEX visitors_site_user_idx ON visitors(site_id,user_id) WHERE user_id IS NOT NULL;

CREATE TABLE identified_users (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  user_id text NOT NULL,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  user_properties jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,user_id)
);
CREATE INDEX identified_users_site_last_seen_idx ON identified_users(site_id,last_seen DESC);

CREATE TABLE visitor_sessions (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  visitor_id text NOT NULL,
  session_id text NOT NULL,
  user_id text,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,visitor_id,session_id)
);
CREATE INDEX visitor_sessions_site_time_idx ON visitor_sessions(site_id,last_seen DESC);
CREATE INDEX visitor_sessions_site_session_idx ON visitor_sessions(site_id,session_id);

CREATE TABLE daily_site_metrics (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_date date NOT NULL,
  events bigint NOT NULL DEFAULT 0,
  page_views bigint NOT NULL DEFAULT 0,
  conversions bigint NOT NULL DEFAULT 0,
  revenue numeric(24,6) NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,event_date)
);

CREATE TABLE daily_site_visitors (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_date date NOT NULL,
  visitor_id text NOT NULL,
  user_id text,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  event_count bigint NOT NULL DEFAULT 0,
  conversion_count bigint NOT NULL DEFAULT 0,
  user_properties jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,event_date,visitor_id)
);
CREATE INDEX daily_site_visitors_user_idx ON daily_site_visitors(site_id,event_date,user_id) WHERE user_id IS NOT NULL;

CREATE TABLE daily_site_sessions (
  site_id uuid NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  event_date date NOT NULL,
  session_id text NOT NULL,
  visitor_id text NOT NULL,
  user_id text,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(site_id,event_date,session_id)
);
CREATE INDEX daily_site_sessions_visitor_idx ON daily_site_sessions(site_id,event_date,visitor_id);

INSERT INTO visitors(site_id,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT
  site_id,
  visitor_id,
  (array_agg(user_id ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_id IS NOT NULL))[1],
  min(event_timestamp),
  max(event_timestamp),
  count(*),
  count(*) FILTER(WHERE is_conversion),
  coalesce((array_agg(user_properties ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events
GROUP BY site_id,visitor_id;

WITH latest_identity AS (
  SELECT DISTINCT ON(site_id,visitor_id) site_id,visitor_id,user_id
  FROM raw_events
  WHERE user_id IS NOT NULL
  ORDER BY site_id,visitor_id,event_timestamp DESC,id DESC
)
INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
SELECT
  e.site_id,
  e.visitor_id,
  i.user_id,
  min(e.event_timestamp),
  min(e.event_timestamp) FILTER(WHERE e.user_id=i.user_id),
  max(e.event_timestamp)
FROM raw_events e
JOIN latest_identity i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
GROUP BY e.site_id,e.visitor_id,i.user_id;

INSERT INTO identified_users(site_id,user_id,first_seen,last_seen,user_properties)
SELECT
  i.site_id,
  i.user_id,
  min(i.first_seen),
  max(v.last_seen),
  coalesce((array_agg(v.user_properties ORDER BY v.last_seen DESC) FILTER(WHERE v.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM visitor_identities i
JOIN visitors v ON v.site_id=i.site_id AND v.visitor_id=i.visitor_id
GROUP BY i.site_id,i.user_id;

INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen)
SELECT
  site_id,
  visitor_id,
  session_id,
  (array_agg(user_id ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_id IS NOT NULL))[1],
  min(event_timestamp),
  max(event_timestamp)
FROM raw_events
GROUP BY site_id,visitor_id,session_id;

INSERT INTO daily_site_metrics(site_id,event_date,events,page_views,conversions,revenue)
SELECT
  e.site_id,
  (e.event_timestamp AT TIME ZONE s.timezone)::date,
  count(*),
  count(*) FILTER(WHERE e.event_name='page_view'),
  count(*) FILTER(WHERE e.is_conversion),
  coalesce(sum(CASE WHEN e.event_name='purchase' AND coalesce(e.properties->>'value',e.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$'
    THEN coalesce(e.properties->>'value',e.properties->>'revenue')::numeric ELSE 0 END),0)
FROM raw_events e
JOIN sites s ON s.id=e.site_id
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date;

INSERT INTO daily_site_visitors(site_id,event_date,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT
  e.site_id,
  (e.event_timestamp AT TIME ZONE s.timezone)::date,
  e.visitor_id,
  (array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1],
  min(e.event_timestamp),
  max(e.event_timestamp),
  count(*),
  count(*) FILTER(WHERE e.is_conversion),
  coalesce((array_agg(e.user_properties ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events e
JOIN sites s ON s.id=e.site_id
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.visitor_id;

INSERT INTO daily_site_sessions(site_id,event_date,session_id,visitor_id,user_id,first_seen,last_seen)
SELECT
  e.site_id,
  (e.event_timestamp AT TIME ZONE s.timezone)::date,
  e.session_id,
  (array_agg(e.visitor_id ORDER BY e.event_timestamp,e.id))[1],
  (array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1],
  min(e.event_timestamp),
  max(e.event_timestamp)
FROM raw_events e
JOIN sites s ON s.id=e.site_id
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.session_id;

CREATE VIEW analytics_events AS
SELECT
  e.*,
  coalesce(i.user_id,e.user_id) AS canonical_user_id,
  CASE WHEN u.user_properties IS NOT NULL AND u.user_properties <> '{}'::jsonb THEN u.user_properties ELSE e.user_properties END AS canonical_user_properties,
  CASE
    WHEN coalesce(i.user_id,e.user_id) IS NOT NULL THEN 'u:' || coalesce(i.user_id,e.user_id)
    ELSE 'v:' || e.visitor_id
  END AS entity_id
FROM raw_events e
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
LEFT JOIN identified_users u ON u.site_id=e.site_id AND u.user_id=coalesce(i.user_id,e.user_id);
