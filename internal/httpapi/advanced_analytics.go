package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hkjang/Momento/internal/auth"
)

type retentionPolicy struct {
	RawEventMonths    int  `json:"raw_event_months"`
	SessionMonths     int  `json:"session_months"`
	AggregationMonths *int `json:"aggregation_months"`
	RealtimeHours     int  `json:"realtime_hours"`
	DebugDays         int  `json:"debug_days"`
}

func (s *Server) getRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var value retentionPolicy
	var updated time.Time
	err = s.DB.QueryRow(r.Context(), `SELECT raw_event_months,session_months,aggregation_months,realtime_hours,debug_days,updated_at FROM retention_policies WHERE site_id=$1`, siteID).Scan(&value.RawEventMonths, &value.SessionMonths, &value.AggregationMonths, &value.RealtimeHours, &value.DebugDays, &updated)
	if err != nil {
		value = retentionPolicy{RawEventMonths: 13, SessionMonths: 25, RealtimeHours: 24, DebugDays: 7}
	}
	writeJSON(w, 200, map[string]any{"policy": value, "updated_at": updated, "last_run": s.lastRetentionRun(r)})
}

// retentionRun is the account of one unattended pass. The policy alone told an
// operator what they had asked for, never whether it was happening: a job that had
// been failing for a month looked exactly like one with nothing to do.
type retentionRun struct {
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Status     string           `json:"status"`
	Removed    map[string]int64 `json:"removed"`
	Error      *string          `json:"error"`
}

// The pass is service-wide rather than per-site, because one sweep applies every
// site's policy. It is reported on the retention screen because that is where an
// operator goes to ask whether retention is working.
func (s *Server) lastRetentionRun(r *http.Request) *retentionRun {
	var run retentionRun
	var removed []byte
	err := s.DB.QueryRow(r.Context(), `SELECT started_at,finished_at,status,removed,error FROM retention_runs ORDER BY started_at DESC LIMIT 1`).
		Scan(&run.StartedAt, &run.FinishedAt, &run.Status, &removed, &run.Error)
	if err != nil {
		return nil
	}
	run.Removed = map[string]int64{}
	_ = json.Unmarshal(removed, &run.Removed)
	return &run
}

func validateRetention(value retentionPolicy) error {
	if value.RawEventMonths < 1 || value.RawEventMonths > 120 {
		return fmt.Errorf("raw_event_months must be between 1 and 120")
	}
	if value.SessionMonths < 1 || value.SessionMonths > 120 {
		return fmt.Errorf("session_months must be between 1 and 120")
	}
	if value.AggregationMonths != nil && (*value.AggregationMonths < 1 || *value.AggregationMonths > 1200) {
		return fmt.Errorf("aggregation_months must be null or between 1 and 1200")
	}
	if value.RealtimeHours < 1 || value.RealtimeHours > 168 {
		return fmt.Errorf("realtime_hours must be between 1 and 168")
	}
	if value.DebugDays < 1 || value.DebugDays > 90 {
		return fmt.Errorf("debug_days must be between 1 and 90")
	}
	return nil
}

func (s *Server) putRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in retentionPolicy
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if err := validateRetention(in); err != nil {
		writeError(w, 400, "INVALID_RETENTION", err.Error())
		return
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO retention_policies(site_id,raw_event_months,session_months,aggregation_months,realtime_hours,debug_days,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT(site_id) DO UPDATE SET raw_event_months=excluded.raw_event_months,session_months=excluded.session_months,aggregation_months=excluded.aggregation_months,realtime_hours=excluded.realtime_hours,debug_days=excluded.debug_days,updated_by=excluded.updated_by,updated_at=now()`, siteID, in.RawEventMonths, in.SessionMonths, in.AggregationMonths, in.RealtimeHours, in.DebugDays, p.ID)
	if err != nil {
		writeError(w, 500, "RETENTION_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "retention.update", "site", siteID.String(), map[string]any{"raw_event_months": in.RawEventMonths, "session_months": in.SessionMonths}, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) sessionReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeRangeError(w, err)
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT s.session_id,s.visitor_id,coalesce(i.user_id,s.user_id),s.started_at,s.last_event_at,extract(epoch from(s.last_event_at-s.started_at))::double precision,s.event_count,s.page_views,s.conversion_count,s.engaged,s.active_engagement_ms,s.heartbeat_count,s.interaction_count,s.landing_page,s.exit_page,s.source,s.medium,s.campaign,s.device_type FROM sessions s LEFT JOIN visitor_identities i ON i.site_id=s.site_id AND i.visitor_id=s.visitor_id WHERE s.site_id=$1 AND s.environment=$4 AND s.last_event_at >= $2 AND s.last_event_at < $3 ORDER BY s.last_event_at DESC LIMIT 500`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	out := rowsToList(rows, func() (map[string]any, error) {
		var sessionID, visitorID string
		var userID, landing, exitPage, source, medium, campaign, device *string
		var started, last time.Time
		var duration float64
		var events, pageViews, conversions, activeMS, heartbeats, interactions int64
		var engaged bool
		err := rows.Scan(&sessionID, &visitorID, &userID, &started, &last, &duration, &events, &pageViews, &conversions, &engaged, &activeMS, &heartbeats, &interactions, &landing, &exitPage, &source, &medium, &campaign, &device)
		return map[string]any{"session_id": sessionID, "visitor_id": visitorID, "user_id": userID, "started_at": started, "last_event_at": last, "duration_seconds": duration, "events": events, "page_views": pageViews, "conversions": conversions, "engaged": engaged, "active_engagement_ms": activeMS, "heartbeat_count": heartbeats, "interaction_count": interactions, "landing_page": landing, "exit_page": exitPage, "source": source, "medium": medium, "campaign": campaign, "device_type": device}, err
	})
	writeJSON(w, 200, out)
}

func (s *Server) ecommerceReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeRangeError(w, err)
		return
	}
	var users, buyers, transactions, carts, checkouts int64
	var revenue, refunds float64
	err = s.DB.QueryRow(r.Context(), `WITH base AS (SELECT *,entity_id entity FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3) SELECT count(DISTINCT entity),count(DISTINCT entity) FILTER(WHERE event_name='purchase'),count(DISTINCT coalesce(properties->>'transaction_id',properties->>'order_id',event_id::text)) FILTER(WHERE event_name='purchase'),count(DISTINCT entity) FILTER(WHERE event_name='add_to_cart'),count(DISTINCT entity) FILTER(WHERE event_name='begin_checkout'),coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision,coalesce(sum(CASE WHEN event_name='refund' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision FROM base`, siteID, from, to, requestEnvironment(r)).Scan(&users, &buyers, &transactions, &carts, &checkouts, &revenue, &refunds)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	productRows, err := s.DB.Query(r.Context(), `SELECT coalesce(item->>'item_id','(not set)'),coalesce(max(item->>'item_name'),''),coalesce(max(item->>'category'),''),coalesce(max(item->>'brand'),''),sum(CASE WHEN coalesce(item->>'quantity','1') ~ '^[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'quantity','1')::numeric ELSE 1 END)::double precision,sum((CASE WHEN coalesce(item->>'price','0') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'price','0')::numeric ELSE 0 END)*(CASE WHEN coalesce(item->>'quantity','1') ~ '^[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'quantity','1')::numeric ELSE 1 END))::double precision,count(DISTINCT coalesce(e.properties->>'transaction_id',e.properties->>'order_id',e.event_id::text)) FROM raw_events e CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.properties->'items')='array' THEN e.properties->'items' ELSE '[]'::jsonb END) item WHERE e.site_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 AND e.event_name='purchase' GROUP BY 1 ORDER BY 6 DESC LIMIT 200`, siteID, from, to, requestEnvironment(r))
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	products := rowsToList(productRows, func() (map[string]any, error) {
		var itemID, name, category, brand string
		var quantity, itemRevenue float64
		var purchases int64
		err := productRows.Scan(&itemID, &name, &category, &brand, &quantity, &itemRevenue, &purchases)
		return map[string]any{"item_id": itemID, "item_name": name, "category": category, "brand": brand, "quantity": quantity, "revenue": itemRevenue, "transactions": purchases}, err
	})
	steps := []map[string]any{}
	stepRows, _ := s.DB.Query(r.Context(), `SELECT event_name,count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$5 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($4) GROUP BY event_name`, siteID, from, to, []string{"view_item", "add_to_cart", "begin_checkout", "purchase"}, requestEnvironment(r))
	if stepRows != nil {
		defer stepRows.Close()
		counts := map[string]int64{}
		for stepRows.Next() {
			var name string
			var count int64
			_ = stepRows.Scan(&name, &count)
			counts[name] = count
		}
		for _, name := range []string{"view_item", "add_to_cart", "begin_checkout", "purchase"} {
			steps = append(steps, map[string]any{"event": name, "users": counts[name]})
		}
	}
	aov := float64(0)
	if transactions > 0 {
		aov = revenue / float64(transactions)
	}
	purchaseRate := float64(0)
	if users > 0 {
		purchaseRate = float64(buyers) * 100 / float64(users)
	}
	writeJSON(w, 200, map[string]any{"summary": map[string]any{"revenue": revenue, "refunds": refunds, "net_revenue": revenue - refunds, "transactions": transactions, "buyers": buyers, "average_order_value": aov, "purchase_conversion_rate": purchaseRate, "cart_users": carts, "checkout_users": checkouts}, "funnel": steps, "products": products})
}
