ALTER TABLE sites
  ADD COLUMN timezone text NOT NULL DEFAULT 'Asia/Seoul',
  ADD COLUMN engagement_threshold_seconds integer NOT NULL DEFAULT 10
    CHECK(engagement_threshold_seconds BETWEEN 1 AND 300);

UPDATE sites
SET timezone = coalesce(
  nullif((SELECT value->>'timezone' FROM settings WHERE key='general'), ''),
  'Asia/Seoul'
);

ALTER TABLE sessions
  ADD COLUMN active_engagement_ms bigint NOT NULL DEFAULT 0,
  ADD COLUMN heartbeat_count bigint NOT NULL DEFAULT 0,
  ADD COLUMN interaction_count bigint NOT NULL DEFAULT 0,
  ADD COLUMN session_properties jsonb NOT NULL DEFAULT '{}';

WITH activity AS (
  SELECT
    site_id,
    session_id,
    coalesce(sum(
      CASE
        WHEN event_name='user_engagement'
          AND coalesce(properties->>'active_seconds','') ~ '^[0-9]+(\.[0-9]+)?$'
        THEN least((properties->>'active_seconds')::numeric * 1000, 3600000)
        ELSE 0
      END
    ),0)::bigint active_engagement_ms,
    count(*) FILTER(WHERE event_name='user_engagement') heartbeat_count,
    count(*) FILTER(WHERE event_name IN (
      'click','outbound_click','file_download','search','login','sign_up',
      'form_start','form_submit','conversion','purchase','add_to_cart',
      'begin_checkout','error'
    )) interaction_count
  FROM raw_events
  GROUP BY site_id,session_id
)
UPDATE sessions s
SET
  active_engagement_ms=a.active_engagement_ms,
  heartbeat_count=a.heartbeat_count,
  interaction_count=a.interaction_count
FROM activity a
WHERE s.site_id=a.site_id AND s.session_id=a.session_id;

UPDATE sessions s
SET engaged =
  extract(epoch FROM (s.last_event_at-s.started_at)) >= site.engagement_threshold_seconds
  OR s.conversion_count > 0
  OR s.page_views >= 2
  OR s.active_engagement_ms >= site.engagement_threshold_seconds * 1000
FROM sites site
WHERE site.id=s.site_id;

UPDATE settings
SET
  value=jsonb_set(value,'{strip_query_string}','true'::jsonb,true),
  updated_at=now()
WHERE key='privacy';
