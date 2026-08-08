package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/jackc/pgx/v5"
)

func dateRange(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	from := to.AddDate(0, 0, -30)
	parse := func(v string) (time.Time, error) {
		if len(v) == 10 {
			return time.Parse("2006-01-02", v)
		}
		return time.Parse(time.RFC3339, v)
	}
	var err error
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
			to = to.Add(24 * time.Hour)
		}
	}
	if !from.Before(to) || to.Sub(from) > 3660*24*time.Hour {
		return from, to, fmt.Errorf("date range must be positive and at most 10 years")
	}
	return from, to, nil
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
	Users, NewUsers, Sessions, PageViews, Events, Conversions   int64
	EngagementRate, AvgSessionDuration, ConversionRate, Revenue float64
}

func (s *Server) metrics(r *http.Request, siteID uuid.UUID, from, to time.Time) (metricSet, error) {
	var m metricSet
	err := s.DB.QueryRow(r.Context(), `WITH period AS (SELECT * FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3), sess AS (SELECT session_id,min(event_timestamp) mn,max(event_timestamp) mx,bool_or(event_name='user_engagement') engaged FROM period GROUP BY session_id), first_seen AS (SELECT visitor_id,min(event_timestamp) first_at FROM raw_events WHERE site_id=$1 GROUP BY visitor_id) SELECT count(DISTINCT coalesce(nullif(p.user_id,''),p.visitor_id)),(SELECT count(*) FROM first_seen WHERE first_at >= $2 AND first_at < $3),count(DISTINCT p.session_id),count(*) FILTER(WHERE p.event_name='page_view'),count(*),count(*) FILTER(WHERE p.is_conversion),coalesce((SELECT 100.0*count(*) FILTER(WHERE engaged)/nullif(count(*),0) FROM sess),0),coalesce((SELECT avg(extract(epoch from(mx-mn))) FROM sess),0),coalesce(100.0*count(*) FILTER(WHERE p.is_conversion)/nullif(count(*),0),0),coalesce(sum(CASE WHEN p.event_name='purchase' AND coalesce(p.properties->>'value',p.properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(p.properties->>'value',p.properties->>'revenue')::numeric ELSE 0 END),0) FROM period p`, siteID, from, to).Scan(&m.Users, &m.NewUsers, &m.Sessions, &m.PageViews, &m.Events, &m.Conversions, &m.EngagementRate, &m.AvgSessionDuration, &m.ConversionRate, &m.Revenue)
	return m, err
}
func metricMap(m metricSet) map[string]any {
	return map[string]any{"users": m.Users, "new_users": m.NewUsers, "sessions": m.Sessions, "page_views": m.PageViews, "events": m.Events, "engagement_rate": m.EngagementRate, "avg_session_duration": m.AvgSessionDuration, "conversions": m.Conversions, "conversion_rate": m.ConversionRate, "revenue": m.Revenue}
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	current, err := s.metrics(r, siteID, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	duration := to.Sub(from)
	previous, _ := s.metrics(r, siteID, from.Add(-duration), from)
	rows, err := s.DB.Query(r.Context(), `SELECT date_trunc('day',event_timestamp) AS bucket,count(DISTINCT coalesce(nullif(user_id,''),visitor_id)),count(DISTINCT session_id),count(*) FILTER(WHERE event_name='page_view'),count(*),count(*) FILTER(WHERE is_conversion) FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 1`, siteID, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	trend := []map[string]any{}
	for rows.Next() {
		var day time.Time
		var users, sessions, views, events, conversions int64
		if rows.Scan(&day, &users, &sessions, &views, &events, &conversions) == nil {
			trend = append(trend, map[string]any{"date": day.Format("2006-01-02"), "users": users, "sessions": sessions, "page_views": views, "events": events, "conversions": conversions})
		}
	}
	writeJSON(w, 200, map[string]any{"from": from, "to": to, "current": metricMap(current), "previous": metricMap(previous), "trend": trend})
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
	var u1, u5, u30, e30, p30 int64
	err = s.DB.QueryRow(r.Context(), `SELECT count(DISTINCT visitor_id) FILTER(WHERE event_timestamp>=now()-interval '1 minute'),count(DISTINCT visitor_id) FILTER(WHERE event_timestamp>=now()-interval '5 minutes'),count(DISTINCT visitor_id),count(*),count(*) FILTER(WHERE event_name='page_view') FROM raw_events WHERE site_id=$1 AND event_timestamp>=now()-interval '30 minutes'`, siteID).Scan(&u1, &u5, &u30, &e30, &p30)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	topEventsRows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT visitor_id) FROM raw_events WHERE site_id=$1 AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8`, siteID)
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
	topPagesRows, err := s.DB.Query(r.Context(), `SELECT coalesce(page_url,'(unknown)'),count(*),count(DISTINCT visitor_id) FROM raw_events WHERE site_id=$1 AND event_name='page_view' AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8`, siteID)
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
	timelineRows, err := s.DB.Query(r.Context(), `SELECT date_trunc('minute',event_timestamp),count(*),count(*) FILTER(WHERE event_name='page_view') FROM raw_events WHERE site_id=$1 AND event_timestamp>=now()-interval '30 minutes' GROUP BY 1 ORDER BY 1`, siteID)
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
	writeJSON(w, 200, map[string]any{"active_users_1m": u1, "active_users_5m": u5, "active_users_30m": u30, "events_30m": e30, "page_views_30m": p30, "events_per_second": float64(e30) / 1800, "page_views_per_second": float64(p30) / 1800, "top_events": topEvents, "top_pages": topPages, "timeline": timeline})
}

func (s *Server) eventReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT coalesce(nullif(user_id,''),visitor_id)),count(*) FILTER(WHERE is_conversion),max(event_timestamp) FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY 1 ORDER BY 2 DESC LIMIT 500`, siteID, from, to)
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
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT coalesce(page_url,'(unknown)'),max(coalesce(page_title,'')),count(*),count(DISTINCT visitor_id),count(DISTINCT session_id),count(*) FILTER(WHERE is_conversion) FROM raw_events WHERE site_id=$1 AND event_name='page_view' AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY page_url ORDER BY 3 DESC LIMIT 500`, siteID, from, to)
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
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	type dimension struct{ key, expr string }
	dims := []dimension{{"networks", "coalesce(network_name,'External / Unclassified')"}, {"departments", "coalesce(nullif(user_properties->>'department',''),'(미지정)')"}, {"organizations", "coalesce(nullif(user_properties->>'organization',''),'(미지정)')"}, {"services", "coalesce(nullif(properties->>'service',''),(SELECT service_name FROM sites WHERE id=$1),'(미지정)')"}, {"features", "coalesce(nullif(properties->>'feature',''),'(미지정)')"}, {"buttons", "coalesce(nullif(properties->>'button',''),nullif(properties->>'element_text',''),nullif(properties->>'element_id',''),'(미지정)')"}}
	result := map[string]any{}
	for _, d := range dims {
		query := `SELECT ` + d.expr + ` label,count(*),count(DISTINCT visitor_id),count(DISTINCT session_id) FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3`
		if d.key == "buttons" {
			query += ` AND event_name='click'`
		}
		query += ` GROUP BY 1 ORDER BY 2 DESC LIMIT 20`
		rows, qerr := s.DB.Query(r.Context(), query, siteID, from, to)
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
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT visitor_id,max(user_id),(array_agg(user_properties ORDER BY event_timestamp DESC))[1],min(event_timestamp),max(event_timestamp),count(*),count(DISTINCT session_id),count(*) FILTER(WHERE is_conversion) FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 GROUP BY visitor_id ORDER BY 5 DESC LIMIT 500`, siteID, from, to)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var visitor string
		var user *string
		var props []byte
		var first, last time.Time
		var events, sessions, conversions int64
		err := rows.Scan(&visitor, &user, &props, &first, &last, &events, &sessions, &conversions)
		var p any
		_ = json.Unmarshal(props, &p)
		return map[string]any{"visitor_id": visitor, "user_id": user, "user_properties": p, "first_seen": first, "last_seen": last, "events": events, "sessions": sessions, "conversions": conversions}, err
	})
	writeJSON(w, 200, out)
}

type queryRequest struct {
	SiteID    string `json:"site_id"`
	DateRange struct {
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

var dimensionSQL = map[string]string{"event.name": "event_name", "page.url": "page_url", "device.type": "device_type", "browser": "browser", "os": "os", "country": "country", "traffic.source": "source", "traffic.medium": "medium", "traffic.campaign": "campaign", "network": "network_name", "user.department": "user_properties->>'department'", "user.organization": "user_properties->>'organization'", "feature": "properties->>'feature'", "button": "coalesce(properties->>'button',properties->>'element_text')"}
var metricSQL = map[string]string{"events": "count(*)", "users": "count(DISTINCT coalesce(nullif(user_id,''),visitor_id))", "sessions": "count(DISTINCT session_id)", "page_views": "count(*) FILTER(WHERE event_name='page_view')", "conversions": "count(*) FILTER(WHERE is_conversion)", "revenue": "coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value','') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (properties->>'value')::numeric ELSE 0 END),0)"}

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
	from, err := time.Parse("2006-01-02", in.DateRange.From)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", "from must use YYYY-MM-DD")
		return
	}
	to, err := time.Parse("2006-01-02", in.DateRange.To)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", "to must use YYYY-MM-DD")
		return
	}
	to = to.Add(24 * time.Hour)
	selects := []string{}
	groups := []string{}
	columns := []string{}
	for _, d := range in.Dimensions {
		expr, ok := dimensionSQL[d]
		if !ok {
			writeError(w, 400, "INVALID_DIMENSION", "unsupported dimension: "+d)
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
	args := []any{siteID, from, to}
	where := []string{"site_id=$1", "event_timestamp >= $2", "event_timestamp < $3"}
	for _, f := range in.Filters {
		expr, ok := dimensionSQL[f.Field]
		if !ok {
			writeError(w, 400, "INVALID_FILTER", "unsupported filter: "+f.Field)
			return
		}
		filterValue := f.Value
		if f.Operator == "in" || f.Operator == "not in" {
			raw, ok := f.Value.([]any)
			if !ok || len(raw) == 0 {
				writeError(w, 400, "INVALID_FILTER", "in operator requires a non-empty array")
				return
			}
			values := make([]string, 0, len(raw))
			for _, item := range raw {
				values = append(values, fmt.Sprint(item))
			}
			filterValue = values
		}
		args = append(args, filterValue)
		n := len(args)
		switch f.Operator {
		case "=", "!=", ">", ">=", "<", "<=":
			where = append(where, fmt.Sprintf("%s %s $%d", expr, f.Operator, n))
		case "contains":
			where = append(where, fmt.Sprintf("%s ILIKE '%%'||$%d||'%%'", expr, n))
		case "not contains":
			where = append(where, fmt.Sprintf("%s NOT ILIKE '%%'||$%d||'%%'", expr, n))
		case "startsWith":
			where = append(where, fmt.Sprintf("%s ILIKE $%d||'%%'", expr, n))
		case "endsWith":
			where = append(where, fmt.Sprintf("%s ILIKE '%%'||$%d", expr, n))
		case "in":
			where = append(where, fmt.Sprintf("%s = ANY($%d)", expr, n))
		case "not in":
			where = append(where, fmt.Sprintf("%s <> ALL($%d)", expr, n))
		case "exists":
			args = args[:len(args)-1]
			where = append(where, expr+" IS NOT NULL")
		case "not exists":
			args = args[:len(args)-1]
			where = append(where, expr+" IS NULL")
		default:
			writeError(w, 400, "INVALID_OPERATOR", "unsupported operator: "+f.Operator)
			return
		}
	}
	limit := in.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	sql := `SELECT ` + strings.Join(selects, ",") + ` FROM raw_events WHERE ` + strings.Join(where, " AND ")
	if len(groups) > 0 {
		sql += ` GROUP BY ` + strings.Join(groups, ",")
	}
	sql += ` ORDER BY ` + strconv.Itoa(len(in.Dimensions)+1) + ` DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := s.DB.Query(r.Context(), sql, args...)
	if err != nil {
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
	writeJSON(w, 200, map[string]any{"columns": columns, "rows": result})
}

func (s *Server) exportEvents(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_id,event_timestamp,event_name,visitor_id,session_id,user_id,page_url,source,medium,campaign,device_type,browser,network_name,properties FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 ORDER BY event_timestamp LIMIT 100000`, siteID, from, to)
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
			_ = enc.Encode(map[string]any{"event_id": exportUUID(vals[0]), "timestamp": vals[1], "event_name": vals[2], "visitor_id": vals[3], "session_id": vals[4], "user_id": vals[5], "page_url": vals[6], "source": vals[7], "medium": vals[8], "campaign": vals[9], "device_type": vals[10], "browser": vals[11], "network": vals[12], "properties": properties})
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="momento-events.csv"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"event_id", "timestamp", "event_name", "visitor_id", "session_id", "user_id", "page_url", "source", "medium", "campaign", "device_type", "browser", "network", "properties"})
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
	SiteID string `json:"site_id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Steps  []struct {
		Name  string `json:"name"`
		Event string `json:"event"`
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
	from, err1 := time.Parse("2006-01-02", in.From)
	to, err2 := time.Parse("2006-01-02", in.To)
	to = to.Add(24 * time.Hour)
	if err1 != nil || err2 != nil {
		writeError(w, 400, "INVALID_RANGE", "from/to must use YYYY-MM-DD")
		return
	}
	args := []any{siteID, from, to}
	ctes := []string{`base AS (SELECT coalesce(nullif(user_id,''),visitor_id) entity,event_name,event_timestamp FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3)`}
	for i, step := range in.Steps {
		args = append(args, step.Event)
		param := len(args)
		name := fmt.Sprintf("s%d", i+1)
		if i == 0 {
			ctes = append(ctes, fmt.Sprintf(`%s AS (SELECT entity,min(event_timestamp) t FROM base WHERE event_name=$%d GROUP BY entity)`, name, param))
		} else {
			prev := fmt.Sprintf("s%d", i)
			ctes = append(ctes, fmt.Sprintf(`%s AS (SELECT p.entity,min(b.event_timestamp) t FROM %s p LEFT JOIN base b ON b.entity=p.entity AND b.event_name=$%d AND b.event_timestamp>=p.t WHERE p.t IS NOT NULL GROUP BY p.entity)`, name, prev, param))
		}
	}
	parts := []string{}
	for i := range in.Steps {
		name := fmt.Sprintf("s%d", i+1)
		if i == 0 {
			parts = append(parts, fmt.Sprintf(`SELECT %d step,count(t) users,0::double precision avg_seconds FROM %s`, i+1, name))
		} else {
			prev := fmt.Sprintf("s%d", i)
			parts = append(parts, fmt.Sprintf(`SELECT %d step,count(c.t) users,coalesce(avg(extract(epoch from(c.t-p.t))) FILTER(WHERE c.t IS NOT NULL),0)::double precision avg_seconds FROM %s c JOIN %s p USING(entity)`, i+1, name, prev))
		}
	}
	sql := `WITH ` + strings.Join(ctes, ",") + " " + strings.Join(parts, " UNION ALL ") + ` ORDER BY step`
	rows, err := s.DB.Query(r.Context(), sql, args...)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
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
	writeJSON(w, 200, map[string]any{"steps": out, "from": from, "to": to})
}

func (s *Server) pathReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := dateRange(r)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `WITH seq AS (SELECT session_id,CASE WHEN event_name='page_view' THEN coalesce(page_url,'(unknown)') ELSE event_name END node,lead(CASE WHEN event_name='page_view' THEN coalesce(page_url,'(unknown)') ELSE event_name END) OVER(PARTITION BY session_id ORDER BY event_timestamp,id) next_node FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3) SELECT node,next_node,count(*) FROM seq WHERE next_node IS NOT NULL GROUP BY 1,2 ORDER BY 3 DESC LIMIT 80`, siteID, from, to)
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
