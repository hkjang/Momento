package insight

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SessionMetrics is what a period's sessions amount to. Sessions have their own
// table, maintained by the collector as events arrive, and it is the only place
// that knows a session's real span and whether it was engaged — the events in a
// query window are a truncated view of both.
//
// The overview derived these from the events in the window while the insight
// report read the sessions table, so the same period was reported as a sixteen
// minute average session on one screen and twelve on the other. Two screens, one
// question, two answers, and nothing to say which was right.
type SessionMetrics struct {
	Sessions       int64
	Engaged        int64
	Converting     int64
	PageViews      int64
	AverageSeconds float64
	// Source says where the numbers came from. The sessions table is authoritative,
	// but a site whose derived data has not been built yet would otherwise see
	// zero sessions next to a page view count, so the events answer the question
	// until the table can.
	Source string
}

// EngagementRate is the share of sessions that were engaged.
func (m SessionMetrics) EngagementRate() float64 {
	return percent(m.Engaged, m.Sessions)
}

// ConversionRate is the share of sessions that converted.
func (m SessionMetrics) ConversionRate() float64 {
	return percent(m.Converting, m.Sessions)
}

// readSessionMetrics counts the sessions that started inside the period. Counting
// by start is what makes consecutive periods add up: a session that spans
// midnight belongs to the day it began, not to both.
func (r Reporter) SessionMetrics(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (SessionMetrics, error) {
	var m SessionMetrics
	err := r.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE engaged),count(*) FILTER(WHERE conversion_count > 0),
			coalesce(sum(page_views),0),coalesce(avg(extract(epoch FROM (last_event_at-started_at))),0)::double precision
		FROM sessions WHERE site_id=$1 AND environment=$2 AND started_at >= $3 AND started_at < $4`,
		siteID, environment, from, to).Scan(&m.Sessions, &m.Engaged, &m.Converting, &m.PageViews, &m.AverageSeconds)
	if err != nil {
		return m, err
	}
	m.Source = "sessions"
	if m.Sessions > 0 {
		return m, nil
	}
	// No session rows for this period. Either nothing happened, or the derived
	// data is behind; the events tell the two apart.
	fallback := SessionMetrics{Source: "events"}
	err = r.DB.QueryRow(ctx, `WITH period AS (
			SELECT session_id,event_name,is_conversion,event_timestamp,properties
			FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp >= $3 AND event_timestamp < $4
		), per_session AS (
			SELECT session_id,min(event_timestamp) started_at,max(event_timestamp) last_event_at,
				count(*) FILTER(WHERE event_name='page_view') page_views,
				count(*) FILTER(WHERE is_conversion) conversions,
				coalesce(sum(CASE WHEN event_name='user_engagement' AND coalesce(properties->>'active_seconds','') ~ '^[0-9]+(\.[0-9]+)?$'
					THEN least((properties->>'active_seconds')::numeric*1000,3600000) ELSE 0 END),0) active_ms
			FROM period GROUP BY session_id
		), threshold AS (SELECT engagement_threshold_seconds AS seconds FROM sites WHERE id=$1)
		SELECT count(*),
			count(*) FILTER(WHERE extract(epoch FROM (last_event_at-started_at)) >= (SELECT seconds FROM threshold)
				OR conversions > 0 OR page_views >= 2 OR active_ms >= (SELECT seconds FROM threshold)*1000),
			count(*) FILTER(WHERE conversions > 0),
			coalesce(sum(page_views),0),
			coalesce(avg(extract(epoch FROM (last_event_at-started_at))),0)::double precision
		FROM per_session`, siteID, environment, from, to).
		Scan(&fallback.Sessions, &fallback.Engaged, &fallback.Converting, &fallback.PageViews, &fallback.AverageSeconds)
	if err != nil {
		return m, err
	}
	return fallback, nil
}
