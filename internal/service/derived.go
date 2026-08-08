package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RebuildSiteDerivedData restores every mutable aggregate from Raw Events.
// Raw Events remain the source of truth for privacy deletion, retention, and
// operational repair workflows.
func RebuildSiteDerivedData(ctx context.Context, tx pgx.Tx, siteID uuid.UUID) error {
	for _, table := range []string{"sessions", "daily_site_sessions", "daily_site_visitors", "daily_site_metrics", "visitor_sessions", "identified_users", "visitor_identities", "visitors"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE site_id=$1`, siteID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, rebuildVisitorsSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildIdentitiesSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildIdentifiedUsersSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildVisitorSessionsSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildSessionsSQL, siteID); err != nil {
		return err
	}
	return RebuildSiteDailyAggregates(ctx, tx, siteID)
}

// RebuildSiteDailyAggregates is used when a site's timezone changes. No Raw
// Event timestamps are modified; only their site-local calendar buckets move.
func RebuildSiteDailyAggregates(ctx context.Context, tx pgx.Tx, siteID uuid.UUID) error {
	for _, table := range []string{"daily_site_sessions", "daily_site_visitors", "daily_site_metrics"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE site_id=$1`, siteID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, rebuildDailyMetricsSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildDailyVisitorsSQL, siteID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, rebuildDailySessionsSQL, siteID)
	return err
}

const rebuildVisitorsSQL = `INSERT INTO visitors(site_id,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT site_id,visitor_id,
	(array_agg(user_id ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_id IS NOT NULL))[1],
	min(event_timestamp),max(event_timestamp),count(*),count(*) FILTER(WHERE is_conversion),
	coalesce((array_agg(user_properties ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events WHERE site_id=$1 GROUP BY site_id,visitor_id`

const rebuildIdentitiesSQL = `WITH latest_identity AS (
	SELECT DISTINCT ON(site_id,visitor_id) site_id,visitor_id,user_id
	FROM raw_events WHERE site_id=$1 AND user_id IS NOT NULL
	ORDER BY site_id,visitor_id,event_timestamp DESC,id DESC
)
INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
SELECT e.site_id,e.visitor_id,i.user_id,min(e.event_timestamp),
	min(e.event_timestamp) FILTER(WHERE e.user_id=i.user_id),max(e.event_timestamp)
FROM raw_events e JOIN latest_identity i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 GROUP BY e.site_id,e.visitor_id,i.user_id`

const rebuildIdentifiedUsersSQL = `INSERT INTO identified_users(site_id,user_id,first_seen,last_seen,user_properties)
SELECT i.site_id,i.user_id,min(i.first_seen),max(v.last_seen),
	coalesce((array_agg(v.user_properties ORDER BY v.last_seen DESC) FILTER(WHERE v.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM visitor_identities i JOIN visitors v ON v.site_id=i.site_id AND v.visitor_id=i.visitor_id
WHERE i.site_id=$1 GROUP BY i.site_id,i.user_id`

const rebuildVisitorSessionsSQL = `INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen)
SELECT site_id,visitor_id,session_id,
	(array_agg(user_id ORDER BY event_timestamp DESC,id DESC) FILTER(WHERE user_id IS NOT NULL))[1],
	min(event_timestamp),max(event_timestamp)
FROM raw_events WHERE site_id=$1 GROUP BY site_id,visitor_id,session_id`

const rebuildSessionsSQL = `WITH aggregates AS (
	SELECT e.site_id,e.session_id,
		(array_agg(e.visitor_id ORDER BY e.event_timestamp,e.id))[1] visitor_id,
		(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1] user_id,
		min(e.event_timestamp) started_at,max(e.event_timestamp) last_event_at,
		count(*) event_count,count(*) FILTER(WHERE e.event_name='page_view') page_views,
		count(*) FILTER(WHERE e.is_conversion) conversion_count,
		coalesce(sum(CASE WHEN e.event_name='user_engagement' AND coalesce(e.properties->>'active_seconds','') ~ '^[0-9]+(\.[0-9]+)?$' THEN least((e.properties->>'active_seconds')::numeric*1000,3600000) ELSE 0 END),0)::bigint active_engagement_ms,
		count(*) FILTER(WHERE e.event_name='user_engagement') heartbeat_count,
		count(*) FILTER(WHERE e.event_name IN ('click','outbound_click','file_download','search','login','sign_up','form_start','form_submit','conversion','purchase','add_to_cart','begin_checkout','error')) interaction_count,
		(array_agg(e.page_url ORDER BY e.event_timestamp,e.id) FILTER(WHERE e.event_name='page_view' AND e.page_url IS NOT NULL))[1] landing_page,
		(array_agg(e.page_url ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.event_name='page_view' AND e.page_url IS NOT NULL))[1] exit_page,
		(array_agg(e.source ORDER BY e.event_timestamp,e.id) FILTER(WHERE e.source IS NOT NULL))[1] source,
		(array_agg(e.medium ORDER BY e.event_timestamp,e.id) FILTER(WHERE e.medium IS NOT NULL))[1] medium,
		(array_agg(e.campaign ORDER BY e.event_timestamp,e.id) FILTER(WHERE e.campaign IS NOT NULL))[1] campaign,
		(array_agg(e.device_type ORDER BY e.event_timestamp,e.id) FILTER(WHERE e.device_type IS NOT NULL))[1] device_type
	FROM raw_events e WHERE e.site_id=$1 GROUP BY e.site_id,e.session_id
)
INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type,active_engagement_ms,heartbeat_count,interaction_count)
SELECT a.site_id,a.session_id,a.visitor_id,a.user_id,a.started_at,a.last_event_at,a.event_count,a.page_views,a.conversion_count,
	extract(epoch FROM (a.last_event_at-a.started_at)) >= site.engagement_threshold_seconds OR a.conversion_count>0 OR a.page_views>=2 OR a.active_engagement_ms>=site.engagement_threshold_seconds*1000,
	a.landing_page,a.exit_page,a.source,a.medium,a.campaign,a.device_type,a.active_engagement_ms,a.heartbeat_count,a.interaction_count
FROM aggregates a JOIN sites site ON site.id=a.site_id`

const rebuildDailyMetricsSQL = `INSERT INTO daily_site_metrics(site_id,event_date,events,page_views,conversions,revenue)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,count(*),
	count(*) FILTER(WHERE e.event_name='page_view'),count(*) FILTER(WHERE e.is_conversion),
	coalesce(sum(CASE WHEN e.event_name='purchase' AND coalesce(e.properties->>'value',e.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(e.properties->>'value',e.properties->>'revenue')::numeric ELSE 0 END),0)
FROM raw_events e JOIN sites s ON s.id=e.site_id WHERE e.site_id=$1
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date`

const rebuildDailyVisitorsSQL = `INSERT INTO daily_site_visitors(site_id,event_date,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.visitor_id,
	coalesce(i.user_id,(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp),count(*),count(*) FILTER(WHERE e.is_conversion),
	coalesce((array_agg(e.user_properties ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.visitor_id,i.user_id`

const rebuildDailySessionsSQL = `INSERT INTO daily_site_sessions(site_id,event_date,session_id,visitor_id,user_id,first_seen,last_seen)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.session_id,
	(array_agg(e.visitor_id ORDER BY e.event_timestamp,e.id))[1],
	coalesce((array_agg(i.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE i.user_id IS NOT NULL))[1],(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.session_id`
