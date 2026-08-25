package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/jackc/pgx/v5"
)

func (s *Server) siteTimezone(ctx context.Context, siteID uuid.UUID) (string, *time.Location, error) {
	var name string
	if err := s.DB.QueryRow(ctx, `SELECT timezone FROM sites WHERE id=$1`, siteID).Scan(&name); err != nil {
		return "", nil, err
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return "", nil, fmt.Errorf("invalid site timezone %q", name)
	}
	return name, location, nil
}

func (s *Server) dateRange(r *http.Request, siteID uuid.UUID) (time.Time, time.Time, error) {
	_, location, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	now := time.Now().In(location)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	from := to.AddDate(0, 0, -30)
	parse := func(v string) (time.Time, error) {
		if len(v) == 10 {
			return time.ParseInLocation("2006-01-02", v, location)
		}
		return time.Parse(time.RFC3339, v)
	}
	if v := r.URL.Query().Get("from"); v != "" {
		from, err = parse(v)
		if err != nil {
			return from, to, err
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = parse(v)
		if err != nil {
			return from, to, err
		}
		if len(v) == 10 {
			to = to.AddDate(0, 0, 1)
		}
	}
	if !from.Before(to) || to.Sub(from) > 3660*24*time.Hour {
		return from, to, fmt.Errorf("date range must be positive and at most 10 years")
	}
	return from.UTC(), to.UTC(), nil
}

func (s *Server) explicitDateRange(ctx context.Context, siteID uuid.UUID, fromValue, toValue string) (time.Time, time.Time, error) {
	_, location, err := s.siteTimezone(ctx, siteID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, err := time.ParseInLocation("2006-01-02", fromValue, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from/to must use YYYY-MM-DD")
	}
	to, err := time.ParseInLocation("2006-01-02", toValue, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from/to must use YYYY-MM-DD")
	}
	to = to.AddDate(0, 0, 1)
	if !from.Before(to) || to.Sub(from) > 3660*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("date range must be positive and at most 10 years")
	}
	return from.UTC(), to.UTC(), nil
}

func previousDateRange(from, to time.Time, location *time.Location) (time.Time, time.Time) {
	localFrom, localTo := from.In(location), to.In(location)
	if localFrom.Hour() == 0 && localFrom.Minute() == 0 && localFrom.Second() == 0 && localFrom.Nanosecond() == 0 &&
		localTo.Hour() == 0 && localTo.Minute() == 0 && localTo.Second() == 0 && localTo.Nanosecond() == 0 {
		fromDate := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), 0, 0, 0, 0, time.UTC)
		toDate := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, time.UTC)
		days := int(toDate.Sub(fromDate) / (24 * time.Hour))
		return localFrom.AddDate(0, 0, -days).UTC(), from
	}
	duration := to.Sub(from)
	return from.Add(-duration), from
}

func localDateBucketRange(from, to time.Time, location *time.Location) (time.Time, time.Time, bool) {
	localFrom, localTo := from.In(location), to.In(location)
	if localFrom.Hour() != 0 || localFrom.Minute() != 0 || localFrom.Second() != 0 || localFrom.Nanosecond() != 0 ||
		localTo.Hour() != 0 || localTo.Minute() != 0 || localTo.Second() != 0 || localTo.Nanosecond() != 0 {
		return time.Time{}, time.Time{}, false
	}
	return time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), 0, 0, 0, 0, time.UTC),
		time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, time.UTC), true
}

func (s *Server) resolveSite(r *http.Request, param string) (uuid.UUID, error) {
	key := chi.URLParam(r, param)
	p, _ := auth.FromContext(r.Context())
	var id uuid.UUID
	err := s.DB.QueryRow(r.Context(), `SELECT s.id FROM sites s WHERE s.site_key=$1 AND s.active AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3))`, key, p.Role, p.ID).Scan(&id)
	return id, err
}
func (s *Server) resolveSiteKey(ctx context.Context, key string) (uuid.UUID, error) {
	p, _ := auth.FromContext(ctx)
	var id uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT s.id FROM sites s WHERE s.site_key=$1 AND s.active AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3))`, key, p.Role, p.ID).Scan(&id)
	return id, err
}

type metricSet struct {
	Users, NewUsers, Sessions, PageViews, Events, Conversions int64
	ConversionUsers, ConversionSessions                       int64
	EngagementRate, AvgSessionDuration, UserConversionRate    float64
	SessionConversionRate, Revenue                            float64
}

func (s *Server) metrics(r *http.Request, siteID uuid.UUID, environment string, from, to time.Time) (metricSet, error) {
	var m metricSet
	err := s.DB.QueryRow(r.Context(), `WITH
		cfg AS (SELECT engagement_threshold_seconds threshold FROM sites WHERE id=$1),
		period AS (SELECT *,entity_id entity FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3),
		sess AS (
			SELECT session_id,min(event_timestamp) mn,max(event_timestamp) mx,
				count(*) FILTER(WHERE event_name='page_view') page_views,
				count(*) FILTER(WHERE is_conversion) conversions,
				coalesce(sum(CASE WHEN event_name='user_engagement' AND coalesce(properties->>'active_seconds','') ~ '^[0-9]+(\.[0-9]+)?$' THEN least((properties->>'active_seconds')::numeric*1000,3600000) ELSE 0 END),0) active_ms
			FROM period GROUP BY session_id
		),
		first_seen AS (
			SELECT entity_id entity,min(event_timestamp) first_at
			FROM analytics_events
			WHERE site_id=$1 AND environment=$4 GROUP BY entity_id
		)
		SELECT
			count(DISTINCT p.entity),
			(SELECT count(*) FROM first_seen WHERE first_at >= $2 AND first_at < $3),
			count(DISTINCT p.session_id),
			count(*) FILTER(WHERE p.event_name='page_view'),
			count(*),
			count(*) FILTER(WHERE p.is_conversion),
			count(DISTINCT p.entity) FILTER(WHERE p.is_conversion),
			count(DISTINCT p.session_id) FILTER(WHERE p.is_conversion),
			coalesce((SELECT 100.0*count(*) FILTER(WHERE extract(epoch FROM (mx-mn)) >= cfg.threshold OR conversions>0 OR page_views>=2 OR active_ms>=cfg.threshold*1000)/nullif(count(*),0) FROM sess,cfg),0),
			coalesce((SELECT avg(extract(epoch FROM (mx-mn))) FROM sess),0),
			coalesce(100.0*count(DISTINCT p.entity) FILTER(WHERE p.is_conversion)/nullif(count(DISTINCT p.entity),0),0),
			coalesce(100.0*count(DISTINCT p.session_id) FILTER(WHERE p.is_conversion)/nullif(count(DISTINCT p.session_id),0),0),
			coalesce(sum(CASE WHEN p.event_name='purchase' AND coalesce(p.properties->>'value',p.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(p.properties->>'value',p.properties->>'revenue')::numeric ELSE 0 END),0)
		FROM period p`, siteID, from, to, environment).Scan(&m.Users, &m.NewUsers, &m.Sessions, &m.PageViews, &m.Events, &m.Conversions, &m.ConversionUsers, &m.ConversionSessions, &m.EngagementRate, &m.AvgSessionDuration, &m.UserConversionRate, &m.SessionConversionRate, &m.Revenue)
	return m, err
}
func metricMap(m metricSet) map[string]any {
	return map[string]any{"users": m.Users, "new_users": m.NewUsers, "sessions": m.Sessions, "page_views": m.PageViews, "events": m.Events, "engagement_rate": m.EngagementRate, "avg_session_duration": m.AvgSessionDuration, "conversions": m.Conversions, "conversion_users": m.ConversionUsers, "conversion_sessions": m.ConversionSessions, "conversion_rate": m.UserConversionRate, "user_conversion_rate": m.UserConversionRate, "session_conversion_rate": m.SessionConversionRate, "revenue": m.Revenue}
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	current, err := s.metrics(r, siteID, environment, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	timezone, location, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		writeError(w, 500, "INVALID_TIMEZONE", err.Error())
		return
	}
	previousFrom, previousTo := previousDateRange(from, to, location)
	previous, _ := s.metrics(r, siteID, environment, previousFrom, previousTo)
	var rows pgx.Rows
	if dateFrom, dateTo, daily := localDateBucketRange(from, to, location); daily {
		rows, err = s.DB.Query(r.Context(), `WITH visitor_counts AS (
			SELECT d.event_date,count(DISTINCT coalesce('u:'||i.user_id,'v:'||d.visitor_id)) users
			FROM daily_site_visitors d LEFT JOIN visitor_identities i ON i.site_id=d.site_id AND i.visitor_id=d.visitor_id
			WHERE d.site_id=$1 AND d.environment=$4 AND d.event_date >= $2::date AND d.event_date < $3::date GROUP BY d.event_date
		), session_counts AS (
			SELECT event_date,count(DISTINCT session_id) sessions FROM daily_site_sessions
			WHERE site_id=$1 AND environment=$4 AND event_date >= $2::date AND event_date < $3::date GROUP BY event_date
		)
		SELECT to_char(m.event_date,'YYYY-MM-DD'),coalesce(v.users,0),coalesce(ss.sessions,0),m.page_views,m.events,m.conversions
		FROM daily_site_metrics m LEFT JOIN visitor_counts v USING(event_date) LEFT JOIN session_counts ss USING(event_date)
		WHERE m.site_id=$1 AND m.environment=$4 AND m.event_date >= $2::date AND m.event_date < $3::date ORDER BY m.event_date`, siteID, dateFrom, dateTo, environment)
	} else {
		rows, err = s.DB.Query(r.Context(), `SELECT to_char(event_timestamp AT TIME ZONE $4,'YYYY-MM-DD') AS bucket,count(DISTINCT entity_id),count(DISTINCT session_id),count(*) FILTER(WHERE event_name='page_view'),count(*),count(*) FILTER(WHERE is_conversion) FROM analytics_events WHERE site_id=$1 AND environment=$5 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 1`, siteID, from, to, timezone, environment)
	}
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	trend := []map[string]any{}
	for rows.Next() {
		var day string
		var users, sessions, views, events, conversions int64
		if rows.Scan(&day, &users, &sessions, &views, &events, &conversions) == nil {
			trend = append(trend, map[string]any{"date": day, "users": users, "sessions": sessions, "page_views": views, "events": events, "conversions": conversions})
		}
	}
	writeJSON(w, 200, map[string]any{"from": from, "to": to, "timezone": timezone, "environment": environment, "current": metricMap(current), "previous": metricMap(previous), "trend": trend})
}

func rowsToList(rows pgx.Rows, scan func() (map[string]any, error)) []map[string]any {
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		v, err := scan()
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *Server) realtime(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	environment := requestEnvironment(r)
	var u1, u5, u30, e30, p30 int64
	err = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT entity_id) FILTER(WHERE event_timestamp>=now()-interval '1 minute'),count(DISTINCT entity_id) FILTER(WHERE event_timestamp>=now()-interval '5 minutes'),count(DISTINCT entity_id),count(*),count(*) FILTER(WHERE event_name='page_view') FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp>=now()-interval '30 minutes'`, siteID, environment).Scan(&u1, &u5, &u30, &e30, &p30)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	topEventsRows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8`, siteID, environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	topEvents := rowsToList(topEventsRows, func() (map[string]any, error) {
		var n string
		var c, u int64
		err := topEventsRows.Scan(&n, &c, &u)
		return map[string]any{"name": n, "count": c, "users": u}, err
	})
	topPagesRows, err := s.DB.Query(r.Context(), `SELECT coalesce(page_url,'(unknown)'),count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$2 AND event_name='page_view' AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8`, siteID, environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	topPages := rowsToList(topPagesRows, func() (map[string]any, error) {
		var n string
		var c, u int64
		err := topPagesRows.Scan(&n, &c, &u)
		return map[string]any{"name": n, "count": c, "users": u}, err
	})
	timelineRows, err := s.DB.Query(r.Context(), `SELECT date_trunc('minute',event_timestamp),count(*),count(*) FILTER(WHERE event_name='page_view') FROM raw_events WHERE site_id=$1 AND environment=$2 AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 1`, siteID, environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	timeline := rowsToList(timelineRows, func() (map[string]any, error) {
		var t time.Time
		var e, p int64
		err := timelineRows.Scan(&t, &e, &p)
		return map[string]any{"time": t, "events": e, "page_views": p}, err
	})
	writeJSON(w, 200, map[string]any{"environment": environment, "active_users_1m": u1, "active_users_5m": u5, "active_users_30m": u30, "events_30m": e30, "page_views_30m": p30, "events_per_second": float64(e30) / 1800, "page_views_per_second": float64(p30) / 1800, "top_events": topEvents, "top_pages": topPages, "timeline": timeline})
}

func (s *Server) eventReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT entity_id),count(*) FILTER(WHERE is_conversion),max(event_timestamp) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 2 DESC LIMIT 500`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var name string
		var count, users, conversions int64
		var last time.Time
		err := rows.Scan(&name, &count, &users, &conversions, &last)
		return map[string]any{"event": name, "count": count, "users": users, "conversions": conversions, "last_seen": last}, err
	})
	writeJSON(w, 200, out)
}
func (s *Server) pageReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT coalesce(page_url,'(unknown)'),max(coalesce(page_title,'')),count(*),count(DISTINCT entity_id),count(DISTINCT session_id),count(*) FILTER(WHERE is_conversion) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_name='page_view' AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY page_url ORDER BY 3 DESC LIMIT 500`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var page, title string
		var views, users, sessions, conversions int64
		err := rows.Scan(&page, &title, &views, &users, &sessions, &conversions)
		return map[string]any{"page": page, "title": title, "views": views, "users": users, "sessions": sessions, "conversions": conversions}, err
	})
	writeJSON(w, 200, out)
}

func (s *Server) usageReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	type dimension struct{ key, expr string }
	dims := []dimension{{"networks", "coalesce(network_name,'External / Unclassified')"}, {"departments", "coalesce(nullif(canonical_user_properties->>'department',''),'(미지정)')"}, {"organizations", "coalesce(nullif(canonical_user_properties->>'organization',''),'(미지정)')"}, {"services", "coalesce(nullif(properties->>'service',''),(SELECT service_name FROM sites WHERE id=$1),'(미지정)')"}, {"features", "coalesce(nullif(properties->>'feature',''),'(미지정)')"}, {"buttons", "coalesce(nullif(properties->>'button',''),nullif(properties->>'element_text',''),nullif(properties->>'element_id',''),'(미지정)')"}}
	result := map[string]any{}
	for _, d := range dims {
		query := `SELECT ` + d.expr + ` label,count(*),count(DISTINCT entity_id),count(DISTINCT session_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`
		if d.key == "buttons" {
			query += ` AND event_name='click'`
		}
		query += ` GROUP BY 1 ORDER BY 2 DESC LIMIT 20`
		rows, qerr := s.DB.Query(r.Context(), query, siteID, from, to, requestEnvironment(r))
		if qerr != nil {
			writeError(w, 500, "QUERY_FAILED", qerr.Error())
			return
		}
		result[d.key] = rowsToList(rows, func() (map[string]any, error) {
			var label string
			var events, users, sessions int64
			err := rows.Scan(&label, &events, &users, &sessions)
			return map[string]any{"label": label, "events": events, "users": users, "sessions": sessions}, err
		})
	}
	writeJSON(w, 200, result)
}

func (s *Server) visitorReport(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	if s.DB.QueryRow(r.Context(), `SELECT coalesce((value->>'visitor_profiles')::bool,false) FROM settings WHERE key='privacy'`).Scan(&enabled) != nil || !enabled {
		writeError(w, 403, "VISITOR_PROFILES_DISABLED", "Visitor Explorer is disabled by the privacy policy")
		return
	}
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `WITH grouped AS (
		SELECT visitor_id,max(canonical_user_id) user_id,(array_agg(canonical_user_properties ORDER BY event_timestamp DESC))[1] user_properties,min(event_timestamp) first_seen,max(event_timestamp) last_seen,count(*) events,count(DISTINCT session_id) sessions,count(*) FILTER(WHERE is_conversion) conversions
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY visitor_id
	) SELECT g.*,(SELECT count(*) FROM visitor_identities linked WHERE linked.site_id=$1 AND linked.user_id=g.user_id) FROM grouped g ORDER BY g.last_seen DESC LIMIT 500`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var visitor string
		var user *string
		var props []byte
		var first, last time.Time
		var events, sessions, conversions, linkedVisitors int64
		err := rows.Scan(&visitor, &user, &props, &first, &last, &events, &sessions, &conversions, &linkedVisitors)
		var p any
		_ = json.Unmarshal(props, &p)
		return map[string]any{"visitor_id": visitor, "user_id": user, "user_properties": p, "first_seen": first, "last_seen": last, "events": events, "sessions": sessions, "conversions": conversions, "linked_visitors": linkedVisitors}, err
	})
	writeJSON(w, 200, out)
}

func (s *Server) identityReport(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	if s.DB.QueryRow(r.Context(), `SELECT coalesce((value->>'visitor_profiles')::bool,false) FROM settings WHERE key='privacy'`).Scan(&enabled) != nil || !enabled {
		writeError(w, 403, "VISITOR_PROFILES_DISABLED", "Visitor Explorer is disabled by the privacy policy")
		return
	}
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT i.user_id,count(*),array_agg(i.visitor_id ORDER BY i.first_seen),min(i.first_seen),min(i.linked_at),max(i.last_seen),coalesce(sum(v.event_count),0),coalesce(sum(v.conversion_count),0)
		FROM visitor_identities i LEFT JOIN visitors v ON v.site_id=i.site_id AND v.visitor_id=i.visitor_id
		WHERE i.site_id=$1 GROUP BY i.user_id ORDER BY max(i.last_seen) DESC LIMIT 500`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var userID string
		var visitorIDs []string
		var visitors, events, conversions int64
		var firstSeen, linkedAt, lastSeen time.Time
		err := rows.Scan(&userID, &visitors, &visitorIDs, &firstSeen, &linkedAt, &lastSeen, &events, &conversions)
		return map[string]any{"user_id": userID, "visitor_count": visitors, "visitor_ids": visitorIDs, "first_seen": firstSeen, "linked_at": linkedAt, "last_seen": lastSeen, "events": events, "conversions": conversions, "confidence": 1, "source": "identify"}, err
	})
	writeJSON(w, 200, out)
}

type queryRequest struct {
	SiteID      string       `json:"site_id"`
	Environment string       `json:"environment,omitempty"`
	SegmentID   string       `json:"segment_id,omitempty"`
	Segment     *segmentNode `json:"segment,omitempty"`
	Mode        string       `json:"mode,omitempty"`
	DateRange   struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"date_range"`
	Dimensions []string `json:"dimensions"`
	Metrics    []string `json:"metrics"`
	Filters    []struct {
		Field    string `json:"field"`
		Operator string `json:"operator"`
		Value    any    `json:"value"`
	} `json:"filters"`
	Limit int `json:"limit"`
}

var metricSQL = map[string]string{"events": "count(*)", "users": "count(DISTINCT e.entity_id)", "sessions": "count(DISTINCT e.session_id)", "page_views": "count(*) FILTER(WHERE e.event_name='page_view')", "conversions": "count(*) FILTER(WHERE e.is_conversion)", "conversion_users": "count(DISTINCT e.entity_id) FILTER(WHERE e.is_conversion)", "conversion_sessions": "count(DISTINCT e.session_id) FILTER(WHERE e.is_conversion)", "user_conversion_rate": "coalesce(100.0*count(DISTINCT e.entity_id) FILTER(WHERE e.is_conversion)/nullif(count(DISTINCT e.entity_id),0),0)", "session_conversion_rate": "coalesce(100.0*count(DISTINCT e.session_id) FILTER(WHERE e.is_conversion)/nullif(count(DISTINCT e.session_id),0),0)", "revenue": "coalesce(sum(CASE WHEN e.event_name='purchase' AND coalesce(e.properties->>'value','') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (e.properties->>'value')::numeric ELSE 0 END),0)"}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	var in queryRequest
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	// The environment is settled before the resolver is built: behavioural segment
	// fields are scoped by it, so it cannot still be empty when they compile.
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	resolver, err := s.newDimensionResolver(r.Context(), siteID, in.Environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	from, to, err := s.explicitDateRange(r.Context(), siteID, in.DateRange.From, in.DateRange.To)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	plan, planError := s.planAnalyticsQuery(r.Context(), siteID, in, from, to)
	if planError != "" {
		s.createQueryAudit(r.Context(), siteID, in.Environment, in, from, to, plan, "rejected", planError)
		writeError(w, 422, "QUERY_COST_LIMIT", planError)
		return
	}
	started := time.Now()
	auditID := s.createQueryAudit(r.Context(), siteID, in.Environment, in, from, to, plan, "running", "")
	auditComplete := false
	defer func() {
		if !auditComplete {
			s.finishQueryAudit(r.Context(), auditID, started, 0, "failed", "query validation or execution stopped")
		}
	}()
	semanticCount := 0
	for _, metric := range in.Metrics {
		if strings.HasPrefix(metric, "semantic.") {
			semanticCount++
		}
	}
	semanticOnly := semanticCount > 0
	if semanticCount > 0 && semanticCount != len(in.Metrics) {
		writeError(w, 400, "INVALID_QUERY", "semantic metrics cannot be mixed with built-in metrics")
		return
	}
	if semanticOnly {
		if len(in.Dimensions) > 0 {
			writeError(w, 400, "INVALID_QUERY", "semantic metric preview currently requires no dimensions")
			return
		}
		row := map[string]any{}
		columns := []string{}
		for _, metric := range in.Metrics {
			name := strings.TrimPrefix(metric, "semantic.")
			var raw []byte
			if err := s.DB.QueryRow(r.Context(), `SELECT definition FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active'`, siteID, name).Scan(&raw); err != nil {
				writeError(w, 400, "INVALID_METRIC", "semantic metric not found: "+name)
				return
			}
			var definition semanticDefinition
			if err := json.Unmarshal(raw, &definition); err != nil {
				writeError(w, 500, "INVALID_METRIC_DEFINITION", err.Error())
				return
			}
			value, err := s.evaluateSemanticMetric(r, siteID, in.Environment, from, to, definition, 1)
			if err != nil {
				s.finishQueryAudit(r.Context(), auditID, started, 0, "failed", err.Error())
				auditComplete = true
				writeError(w, 500, "QUERY_FAILED", err.Error())
				return
			}
			row[metric] = value
			columns = append(columns, metric)
		}
		s.finishQueryAudit(r.Context(), auditID, started, 1, "success", "")
		auditComplete = true
		writeJSON(w, 200, map[string]any{"columns": columns, "rows": []map[string]any{row}, "environment": in.Environment, "query": plan})
		return
	}
	selects := []string{}
	groups := []string{}
	columns := []string{}
	for _, d := range in.Dimensions {
		expr, err := resolver.expression(d, "e")
		if err != nil {
			writeError(w, 400, "INVALID_DIMENSION", err.Error())
			return
		}
		selects = append(selects, "coalesce("+expr+",'(not set)') AS d"+strconv.Itoa(len(groups)+1))
		groups = append(groups, strconv.Itoa(len(selects)))
		columns = append(columns, d)
	}
	for _, m := range in.Metrics {
		expr, ok := metricSQL[m]
		if !ok {
			writeError(w, 400, "INVALID_METRIC", "unsupported metric: "+m)
			return
		}
		selects = append(selects, expr+" AS m"+strconv.Itoa(len(columns)+1))
		columns = append(columns, m)
	}
	if len(in.Metrics) == 0 {
		writeError(w, 400, "INVALID_QUERY", "at least one metric is required")
		return
	}
	args := []any{siteID, from, to, in.Environment}
	where := []string{"e.site_id=$1", "e.event_timestamp >= $2", "e.event_timestamp < $3", "e.environment=$4"}
	if plan.SamplePercent < 100 {
		threshold := int(math.Round(plan.SamplePercent * 100))
		args = append(args, threshold)
		where = append(where, "mod((hashtextextended(e.event_id::text,0) & 9223372036854775807),10000) < $"+strconv.Itoa(len(args)))
	}
	for _, f := range in.Filters {
		part, err := compileSegment(segmentNode{Field: f.Field, Operator: f.Operator, Value: f.Value}, resolver, "e", &args, 0)
		if err != nil {
			writeError(w, 400, "INVALID_FILTER", err.Error())
			return
		}
		where = append(where, part)
	}
	if in.SegmentID != "" {
		definition, err := s.loadSegment(r.Context(), siteID, in.SegmentID)
		if err != nil {
			writeError(w, 400, "INVALID_SEGMENT", err.Error())
			return
		}
		part, err := compileSegment(definition, resolver, "e", &args, 0)
		if err != nil {
			writeError(w, 400, "INVALID_SEGMENT", err.Error())
			return
		}
		where = append(where, part)
	}
	if in.Segment != nil {
		part, err := compileSegment(*in.Segment, resolver, "e", &args, 0)
		if err != nil {
			writeError(w, 400, "INVALID_SEGMENT", err.Error())
			return
		}
		where = append(where, part)
	}
	limit := in.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	sql := `SELECT ` + strings.Join(selects, ",") + ` FROM analytics_events e WHERE ` + strings.Join(where, " AND ")
	if len(groups) > 0 {
		sql += ` GROUP BY ` + strings.Join(groups, ",")
	}
	sql += ` ORDER BY ` + strconv.Itoa(len(in.Dimensions)+1) + ` DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := s.DB.Query(r.Context(), sql, args...)
	if err != nil {
		s.finishQueryAudit(r.Context(), auditID, started, 0, "failed", err.Error())
		auditComplete = true
		writeError(w, 400, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		item := map[string]any{}
		for i, c := range columns {
			item[c] = vals[i]
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		s.finishQueryAudit(r.Context(), auditID, started, len(result), "failed", err.Error())
		auditComplete = true
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	s.finishQueryAudit(r.Context(), auditID, started, len(result), "success", "")
	auditComplete = true
	writeJSON(w, 200, map[string]any{"columns": columns, "rows": result, "environment": in.Environment, "query": plan})
}

func (s *Server) exportEvents(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_id,event_timestamp,event_name,visitor_id,session_id,user_id,page_url,source,medium,campaign,device_type,browser,network_name,properties,environment,contract_version FROM raw_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 ORDER BY event_timestamp LIMIT 100000`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "EXPORT_FAILED", err.Error())
		return
	}
	defer rows.Close()
	format := r.URL.Query().Get("format")
	if format == "json" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="momento-events.ndjson"`)
		enc := json.NewEncoder(w)
		for rows.Next() {
			vals, _ := rows.Values()
			properties := vals[13]
			if raw, ok := properties.([]byte); ok {
				properties = json.RawMessage(raw)
			}
			_ = enc.Encode(map[string]any{"event_id": exportUUID(vals[0]), "timestamp": vals[1], "event_name": vals[2], "visitor_id": vals[3], "session_id": vals[4], "user_id": vals[5], "page_url": vals[6], "source": vals[7], "medium": vals[8], "campaign": vals[9], "device_type": vals[10], "browser": vals[11], "network": vals[12], "properties": properties, "environment": vals[14], "contract_version": vals[15]})
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="momento-events.csv"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"event_id", "timestamp", "event_name", "visitor_id", "session_id", "user_id", "page_url", "source", "medium", "campaign", "device_type", "browser", "network", "properties", "environment", "contract_version"})
	for rows.Next() {
		vals, _ := rows.Values()
		record := make([]string, len(vals))
		for i, v := range vals {
			if v != nil {
				switch x := v.(type) {
				case []byte:
					record[i] = string(x)
				case [16]byte:
					record[i] = uuid.UUID(x).String()
				case uuid.UUID:
					record[i] = x.String()
				default:
					record[i] = fmt.Sprint(x)
				}
			}
		}
		_ = cw.Write(record)
	}
	cw.Flush()
}
func exportUUID(value any) any {
	switch current := value.(type) {
	case [16]byte:
		return uuid.UUID(current).String()
	case uuid.UUID:
		return current.String()
	default:
		return value
	}
}

type funnelRequest struct {
	SiteID        string       `json:"site_id"`
	Environment   string       `json:"environment,omitempty"`
	From          string       `json:"from"`
	To            string       `json:"to"`
	Mode          string       `json:"mode,omitempty"`
	WithinMinutes int          `json:"within_minutes,omitempty"`
	SegmentID     string       `json:"segment_id,omitempty"`
	Segment       *segmentNode `json:"segment,omitempty"`
	// CompareSegmentIDs runs the same funnel for each segment next to the baseline,
	// which is how a flat overall rate becomes "which group is stuck, and where".
	CompareSegmentIDs []string `json:"compare_segment_ids,omitempty"`
	Steps             []struct {
		Name    string        `json:"name"`
		Event   string        `json:"event"`
		Filters []segmentNode `json:"filters,omitempty"`
	} `json:"steps"`
}

func (s *Server) funnel(w http.ResponseWriter, r *http.Request) {
	var in funnelRequest
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if len(in.Steps) < 2 || len(in.Steps) > 10 {
		writeError(w, 400, "INVALID_STEPS", "funnel requires 2 to 10 steps")
		return
	}
	siteID, siteErr := s.resolveSiteKey(r.Context(), in.SiteID)
	if siteErr != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	if !environmentNamePattern.MatchString(in.Environment) {
		in.Environment = "prd"
	}
	resolver, err := s.newDimensionResolver(r.Context(), siteID, in.Environment)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	from, to, err := s.explicitDateRange(r.Context(), siteID, in.From, in.To)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	if in.Mode == "" {
		in.Mode = "closed"
	}
	if in.Mode != "closed" && in.Mode != "open" {
		writeError(w, 400, "INVALID_MODE", "funnel mode must be closed or open")
		return
	}
	if in.WithinMinutes < 0 || in.WithinMinutes > 525600 {
		writeError(w, 400, "INVALID_WINDOW", "within_minutes must be between 0 and 525600")
		return
	}
	if len(in.CompareSegmentIDs) > 3 {
		writeError(w, 400, "TOO_MANY_SEGMENTS", "compare_segment_ids accepts at most 3 segments")
		return
	}
	cohorts := []funnelCohort{{Key: "baseline", Label: "전체"}}
	for _, id := range in.CompareSegmentIDs {
		definition, err := s.loadSegment(r.Context(), siteID, id)
		if err != nil {
			writeError(w, 400, "INVALID_SEGMENT", err.Error())
			return
		}
		name, _ := s.segmentName(r.Context(), siteID, id)
		cohorts = append(cohorts, funnelCohort{Key: id, Label: name, Definition: &definition})
	}
	// Comparing three cohorts runs three funnels, so the whole comparison shares
	// one deadline instead of multiplying the cost of a wide range.
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	series := []map[string]any{}
	var baseline []map[string]any
	for _, cohort := range cohorts {
		steps, err := s.runFunnel(ctx, r, siteID, in, resolver, cohort.Definition)
		if err != nil {
			writeQueryError(w, err)
			return
		}
		if cohort.Key == "baseline" {
			baseline = steps
		}
		series = append(series, map[string]any{"key": cohort.Key, "label": cohort.Label, "steps": steps, "entered": funnelEntered(steps), "completion_rate": funnelCompletion(steps)})
	}
	response := map[string]any{"steps": baseline, "from": from, "to": to, "mode": in.Mode, "within_minutes": in.WithinMinutes}
	if len(cohorts) > 1 {
		response["series"] = series
		response["comparison"] = compareFunnelSeries(series)
	}
	writeJSON(w, 200, response)
}

// funnelCohort is one column of a comparison: the baseline, or one segment.
type funnelCohort struct {
	Key        string
	Label      string
	Definition *segmentNode
}

func (s *Server) segmentName(ctx context.Context, siteID uuid.UUID, id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return id, err
	}
	var name string
	if err := s.DB.QueryRow(ctx, `SELECT name FROM segments WHERE id=$1 AND site_id=$2`, parsed, siteID).Scan(&name); err != nil {
		return id, err
	}
	return name, nil
}

// runFunnel evaluates the funnel for one cohort. Every cohort shares the same steps,
// window and mode so the columns stay comparable.
func (s *Server) runFunnel(ctx context.Context, r *http.Request, siteID uuid.UUID, in funnelRequest, resolver dimensionResolver, cohort *segmentNode) ([]map[string]any, error) {
	from, to, err := s.explicitDateRange(ctx, siteID, in.From, in.To)
	if err != nil {
		return nil, err
	}
	args := []any{siteID, from, to, in.Environment}
	baseWhere := []string{"e.site_id=$1", "e.event_timestamp >= $2", "e.event_timestamp < $3", "e.environment=$4"}
	if in.SegmentID != "" {
		definition, err := s.loadSegment(ctx, siteID, in.SegmentID)
		if err != nil {
			return nil, err
		}
		part, err := compileSegment(definition, resolver, "e", &args, 0)
		if err != nil {
			return nil, err
		}
		baseWhere = append(baseWhere, part)
	}
	if in.Segment != nil {
		part, err := compileSegment(*in.Segment, resolver, "e", &args, 0)
		if err != nil {
			return nil, err
		}
		baseWhere = append(baseWhere, part)
	}
	if cohort != nil {
		part, err := compileSegment(*cohort, resolver, "e", &args, 0)
		if err != nil {
			return nil, err
		}
		baseWhere = append(baseWhere, part)
	}
	ctes := []string{`base AS (SELECT e.*,e.entity_id entity FROM analytics_events e WHERE ` + strings.Join(baseWhere, " AND ") + `)`}
	for i, step := range in.Steps {
		if strings.TrimSpace(step.Event) == "" {
			return nil, fmt.Errorf("every funnel step requires an event")
		}
		args = append(args, step.Event)
		param := len(args)
		name := fmt.Sprintf("s%d", i+1)
		conditions := []string{fmt.Sprintf("b.event_name=$%d", param)}
		for _, filter := range step.Filters {
			part, err := compileSegment(filter, resolver, "b", &args, 0)
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, part)
		}
		conditionSQL := strings.Join(conditions, " AND ")
		if i == 0 || in.Mode == "open" {
			ctes = append(ctes, fmt.Sprintf(`%s AS (SELECT b.entity,min(b.event_timestamp) t FROM base b WHERE %s GROUP BY b.entity)`, name, conditionSQL))
		} else {
			prev := fmt.Sprintf("s%d", i)
			windowSQL := ""
			if in.WithinMinutes > 0 {
				args = append(args, in.WithinMinutes)
				windowSQL = fmt.Sprintf(" AND b.event_timestamp<=p.t+make_interval(mins=>$%d)", len(args))
			}
			ctes = append(ctes, fmt.Sprintf(`%s AS (SELECT p.entity,min(b.event_timestamp) t FROM %s p LEFT JOIN base b ON b.entity=p.entity AND %s AND b.event_timestamp>=p.t%s WHERE p.t IS NOT NULL GROUP BY p.entity)`, name, prev, conditionSQL, windowSQL))
		}
	}
	parts := []string{}
	for i := range in.Steps {
		name := fmt.Sprintf("s%d", i+1)
		if i == 0 || in.Mode == "open" {
			parts = append(parts, fmt.Sprintf(`SELECT %d step,count(t) users,0::double precision avg_seconds FROM %s`, i+1, name))
		} else {
			prev := fmt.Sprintf("s%d", i)
			parts = append(parts, fmt.Sprintf(`SELECT %d step,count(c.t) users,coalesce(avg(extract(epoch from(c.t-p.t))) FILTER(WHERE c.t IS NOT NULL),0)::double precision avg_seconds FROM %s c JOIN %s p USING(entity)`, i+1, name, prev))
		}
	}
	sql := `WITH ` + strings.Join(ctes, ",") + " " + strings.Join(parts, " UNION ALL ") + ` ORDER BY step`
	rows, err := s.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	var first int64
	for rows.Next() {
		var index int
		var users int64
		var seconds float64
		if rows.Scan(&index, &users, &seconds) == nil {
			if index == 1 {
				first = users
			}
			prevUsers := first
			if len(out) > 0 {
				prevUsers = out[len(out)-1]["users"].(int64)
			}
			rate := float64(0)
			if prevUsers > 0 {
				rate = float64(users) * 100 / float64(prevUsers)
			}
			out = append(out, map[string]any{"step": index, "name": in.Steps[index-1].Name, "event": in.Steps[index-1].Event, "users": users, "step_conversion_rate": rate, "overall_conversion_rate": func() float64 {
				if first == 0 {
					return 0
				}
				return float64(users) * 100 / float64(first)
			}(), "avg_seconds": seconds})
		}
	}
	return out, rows.Err()
}

func (s *Server) pathReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	view, err := normalizePathView(r.URL.Query().Get("view"))
	if err != nil {
		writeError(w, 400, "INVALID_PATH_VIEW", err.Error())
		return
	}
	includeSystem := r.URL.Query().Get("include_system") == "true"
	rows, err := s.DB.Query(r.Context(), `WITH seq AS (
		SELECT session_id,
			CASE WHEN event_name='page_view' THEN coalesce(nullif(btrim(page_url),''),'(unknown page)') ELSE event_name END node,
			lead(CASE WHEN event_name='page_view' THEN coalesce(nullif(btrim(page_url),''),'(unknown page)') ELSE event_name END) OVER(PARTITION BY session_id ORDER BY event_timestamp,id) next_node
		FROM raw_events
		WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
			AND ($5='all' OR ($5='pages' AND event_name='page_view') OR ($5='events' AND event_name<>'page_view'))
			AND ($6 OR event_name NOT IN ('user_engagement','web_vital'))
	)
	SELECT node,next_node,count(*)
	FROM seq
	WHERE next_node IS NOT NULL AND node<>next_node
	GROUP BY 1,2 ORDER BY 3 DESC LIMIT 80`, siteID, from, to, requestEnvironment(r), view, includeSystem)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var source, target string
		var count int64
		err := rows.Scan(&source, &target, &count)
		return map[string]any{"source": source, "target": target, "count": count}, err
	})
	writeJSON(w, 200, out)
}

func normalizePathView(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "all", nil
	}
	switch value {
	case "all", "pages", "events":
		return value, nil
	default:
		return "", fmt.Errorf("view must be one of all, pages, or events")
	}
}
