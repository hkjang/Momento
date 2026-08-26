package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RebuildEnvironmentDateRange reconciles site-local daily aggregates from Raw
// Events. It is used for late-arriving events and administrator backfills while
// keeping Raw Events immutable.
func RebuildEnvironmentDateRange(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, environment string, from, to time.Time) error {
	for _, table := range []string{"daily_site_sessions", "daily_site_visitors", "daily_site_metrics"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date <= $4::date`, siteID, environment, from, to); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, rebuildRangeDailyMetricsSQL, siteID, environment, from, to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildRangeDailyVisitorsSQL, siteID, environment, from, to); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, rebuildRangeDailySessionsSQL, siteID, environment, from, to)
	return err
}

// RebuildSiteDerivedData restores every mutable aggregate from Raw Events.
// Raw Events remain the source of truth for privacy deletion, retention, and
// operational repair workflows.
func RebuildSiteDerivedData(ctx context.Context, tx pgx.Tx, siteID uuid.UUID) error {
	// Every step's plan depends on the planner's statistics, and the rebuild
	// invalidates them as it goes: it empties a table, fills it with hundreds of
	// thousands of rows, and the next step joins it while the planner still
	// believes it holds three. That is how a join of two small tables becomes a
	// nested loop, and how the identity rebuild reads two million rows in visitor
	// order through an index instead of scanning them — the difference between
	// seconds and not finishing. Statistics are refreshed before the first step
	// and after each step a later one reads.
	analyze(ctx, tx, siteID, "raw_events")
	for _, table := range []string{"sessions", "daily_site_sessions", "daily_site_visitors", "daily_site_metrics", "visitor_sessions", "identified_users", "visitor_identities", "visitors"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE site_id=$1`, siteID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, rebuildVisitorsSQL, siteID); err != nil {
		return err
	}
	analyze(ctx, tx, siteID, "visitors")
	if _, err := tx.Exec(ctx, rebuildIdentitiesSQL, siteID); err != nil {
		return err
	}
	analyze(ctx, tx, siteID, "visitor_identities")
	if _, err := tx.Exec(ctx, rebuildIdentifiedUsersSQL, siteID); err != nil {
		return err
	}
	analyze(ctx, tx, siteID, "identified_users")
	if _, err := tx.Exec(ctx, rebuildVisitorSessionsSQL, siteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, rebuildSessionsSQL, siteID); err != nil {
		return err
	}
	analyze(ctx, tx, siteID, "sessions")
	return RebuildSiteDailyAggregates(ctx, tx, siteID)
}

// analyze refreshes one table's statistics. A deployment whose database user may
// not analyse the table still rebuilds, on whatever statistics exist, so the
// failure is logged rather than returned.
func analyze(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, table string) {
	if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
		slog.Warn("could not refresh statistics during the rebuild", "error", err, "site_id", siteID, "table", table)
	}
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

// rebuildIdentitiesSQL derives, for every visitor that was ever identified, the
// user they ended up as, when the visitor was first seen, when that link was
// first made, and when they were last seen.
//
// It used to join raw_events back to a CTE of the latest identity per visitor.
// The planner answered that with a merge join, which reads the whole table in
// visitor order through the index — two million random heap fetches on a
// mid-sized site. The rebuild did not finish in twenty minutes and it holds a
// transaction open while it runs.
//
// Three independent grouped scans and two hash joins over the small results
// produce the same rows in under two seconds.
const rebuildIdentitiesSQL = `WITH bounds AS (
	SELECT site_id,visitor_id,min(event_timestamp) first_seen,max(event_timestamp) last_seen
	FROM raw_events WHERE site_id=$1 GROUP BY site_id,visitor_id
), latest AS (
	SELECT DISTINCT ON(site_id,visitor_id) site_id,visitor_id,user_id
	FROM raw_events WHERE site_id=$1 AND user_id IS NOT NULL
	ORDER BY site_id,visitor_id,event_timestamp DESC,id DESC
), linked AS (
	SELECT site_id,visitor_id,user_id,min(event_timestamp) linked_at
	FROM raw_events WHERE site_id=$1 AND user_id IS NOT NULL
	GROUP BY site_id,visitor_id,user_id
)
INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
SELECT b.site_id,b.visitor_id,l.user_id,b.first_seen,k.linked_at,b.last_seen
FROM bounds b
JOIN latest l ON l.site_id=b.site_id AND l.visitor_id=b.visitor_id
JOIN linked k ON k.site_id=b.site_id AND k.visitor_id=b.visitor_id AND k.user_id=l.user_id`

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

const rebuildSessionsSQL = `WITH property_entries AS (
	SELECT DISTINCT ON(e.site_id,e.environment,e.session_id,p.key)
		e.site_id,e.environment,e.session_id,p.key,p.value
	FROM raw_events e CROSS JOIN LATERAL jsonb_each(e.session_properties) p
	WHERE e.site_id=$1
	ORDER BY e.site_id,e.environment,e.session_id,p.key,e.event_timestamp DESC,e.id DESC
), property_sets AS (
	SELECT site_id,environment,session_id,jsonb_object_agg(key,value) session_properties
	FROM property_entries GROUP BY site_id,environment,session_id
), aggregates AS (
	SELECT e.site_id,e.environment,e.session_id,
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
	FROM raw_events e WHERE e.site_id=$1 GROUP BY e.site_id,e.environment,e.session_id
)
INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type,active_engagement_ms,heartbeat_count,interaction_count,environment,session_properties)
SELECT a.site_id,a.session_id,a.visitor_id,a.user_id,a.started_at,a.last_event_at,a.event_count,a.page_views,a.conversion_count,
	extract(epoch FROM (a.last_event_at-a.started_at)) >= site.engagement_threshold_seconds OR a.conversion_count>0 OR a.page_views>=2 OR a.active_engagement_ms>=site.engagement_threshold_seconds*1000,
	a.landing_page,a.exit_page,a.source,a.medium,a.campaign,a.device_type,a.active_engagement_ms,a.heartbeat_count,a.interaction_count,a.environment,coalesce(p.session_properties,'{}'::jsonb)
FROM aggregates a JOIN sites site ON site.id=a.site_id
LEFT JOIN property_sets p ON p.site_id=a.site_id AND p.environment=a.environment AND p.session_id=a.session_id`

const rebuildDailyMetricsSQL = `INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,count(*),
	count(*) FILTER(WHERE e.event_name='page_view'),count(*) FILTER(WHERE e.is_conversion),
	coalesce(sum(CASE WHEN e.event_name='purchase' AND coalesce(e.properties->>'value',e.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(e.properties->>'value',e.properties->>'revenue')::numeric ELSE 0 END),0)
FROM raw_events e JOIN sites s ON s.id=e.site_id WHERE e.site_id=$1
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment`

const rebuildDailyVisitorsSQL = `INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.visitor_id,
	coalesce(i.user_id,(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp),count(*),count(*) FILTER(WHERE e.is_conversion),
	coalesce((array_agg(e.user_properties ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.visitor_id,i.user_id`

const rebuildDailySessionsSQL = `INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,user_id,first_seen,last_seen)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.session_id,
	(array_agg(e.visitor_id ORDER BY e.event_timestamp,e.id))[1],
	coalesce((array_agg(i.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE i.user_id IS NOT NULL))[1],(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.session_id`

const rebuildRangeDailyMetricsSQL = `INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,count(*),
	count(*) FILTER(WHERE e.event_name='page_view'),count(*) FILTER(WHERE e.is_conversion),
	coalesce(sum(CASE WHEN e.event_name='purchase' AND coalesce(e.properties->>'value',e.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(e.properties->>'value',e.properties->>'revenue')::numeric ELSE 0 END),0)
FROM raw_events e JOIN sites s ON s.id=e.site_id
WHERE e.site_id=$1 AND e.environment=$2 AND (e.event_timestamp AT TIME ZONE s.timezone)::date >= $3::date AND (e.event_timestamp AT TIME ZONE s.timezone)::date <= $4::date
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment`

const rebuildRangeDailyVisitorsSQL = `INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.visitor_id,
	coalesce(i.user_id,(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp),count(*),count(*) FILTER(WHERE e.is_conversion),
	coalesce((array_agg(e.user_properties ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_properties <> '{}'::jsonb))[1],'{}'::jsonb)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 AND e.environment=$2 AND (e.event_timestamp AT TIME ZONE s.timezone)::date >= $3::date AND (e.event_timestamp AT TIME ZONE s.timezone)::date <= $4::date
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.visitor_id,i.user_id`

const rebuildRangeDailySessionsSQL = `INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,user_id,first_seen,last_seen)
SELECT e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.session_id,
	(array_agg(e.visitor_id ORDER BY e.event_timestamp,e.id))[1],
	coalesce((array_agg(i.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE i.user_id IS NOT NULL))[1],(array_agg(e.user_id ORDER BY e.event_timestamp DESC,e.id DESC) FILTER(WHERE e.user_id IS NOT NULL))[1]),
	min(e.event_timestamp),max(e.event_timestamp)
FROM raw_events e JOIN sites s ON s.id=e.site_id
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
WHERE e.site_id=$1 AND e.environment=$2 AND (e.event_timestamp AT TIME ZONE s.timezone)::date >= $3::date AND (e.event_timestamp AT TIME ZONE s.timezone)::date <= $4::date
GROUP BY e.site_id,(e.event_timestamp AT TIME ZONE s.timezone)::date,e.environment,e.session_id`
