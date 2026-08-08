package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	writeJSON(w, 200, map[string]any{"policy": value, "updated_at": updated})
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
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT s.session_id,s.visitor_id,coalesce(i.user_id,s.user_id),s.started_at,s.last_event_at,extract(epoch from(s.last_event_at-s.started_at))::double precision,s.event_count,s.page_views,s.conversion_count,s.engaged,s.active_engagement_ms,s.heartbeat_count,s.interaction_count,s.landing_page,s.exit_page,s.source,s.medium,s.campaign,s.device_type FROM sessions s LEFT JOIN visitor_identities i ON i.site_id=s.site_id AND i.visitor_id=s.visitor_id WHERE s.site_id=$1 AND s.last_event_at >= $2 AND s.last_event_at < $3 ORDER BY s.last_event_at DESC LIMIT 500`, siteID, from, to)
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

func (s *Server) visitorTimeline(w http.ResponseWriter, r *http.Request) {
	var profiles bool
	if err := s.DB.QueryRow(r.Context(), `SELECT coalesce((value->>'visitor_profiles')::boolean,true) FROM settings WHERE key='privacy'`).Scan(&profiles); err != nil || !profiles {
		writeError(w, 403, "VISITOR_PROFILES_DISABLED", "Visitor Explorer is disabled by the privacy policy")
		return
	}
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	visitorID := strings.TrimSpace(chi.URLParam(r, "visitorID"))
	if visitorID == "" || len(visitorID) > 128 {
		writeError(w, 400, "INVALID_VISITOR", "visitor id is required")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.DB.Query(r.Context(), `SELECT event_id,event_name,event_timestamp,session_id,user_id,page_url,page_title,source,medium,campaign,device_type,browser,network_name,properties,user_properties,is_conversion,traffic_class FROM raw_events WHERE site_id=$1 AND visitor_id=$2 AND event_timestamp >= $3 AND event_timestamp < $4 ORDER BY event_timestamp DESC,id DESC LIMIT $5`, siteID, visitorID, from, to, limit)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	var latestUserID *string
	var latestUserProperties any
	for rows.Next() {
		var eventID uuid.UUID
		var eventName, sessionID string
		var timestamp time.Time
		var userID, pageURL, pageTitle, source, medium, campaign, device, browser, network *string
		var properties, userProperties []byte
		var conversion bool
		var trafficClass string
		if rows.Scan(&eventID, &eventName, &timestamp, &sessionID, &userID, &pageURL, &pageTitle, &source, &medium, &campaign, &device, &browser, &network, &properties, &userProperties, &conversion, &trafficClass) != nil {
			continue
		}
		var props, userProps any
		_ = json.Unmarshal(properties, &props)
		_ = json.Unmarshal(userProperties, &userProps)
		if latestUserID == nil && userID != nil {
			latestUserID = userID
		}
		if latestUserProperties == nil {
			latestUserProperties = userProps
		}
		events = append(events, map[string]any{"event_id": exportUUID(eventID), "event_name": eventName, "timestamp": timestamp, "session_id": sessionID, "user_id": userID, "page_url": pageURL, "page_title": pageTitle, "source": source, "medium": medium, "campaign": campaign, "device_type": device, "browser": browser, "network": network, "properties": props, "is_conversion": conversion, "traffic_class": trafficClass})
	}
	var sessions, conversions int64
	var firstSeen, lastSeen *time.Time
	var canonicalUserID *string
	var linkedVisitors []string
	_ = s.DB.QueryRow(r.Context(), `SELECT v.user_id,v.first_seen,v.last_seen,(SELECT count(*) FROM visitor_sessions vs WHERE vs.site_id=v.site_id AND vs.visitor_id=v.visitor_id),v.conversion_count FROM visitors v WHERE v.site_id=$1 AND v.visitor_id=$2`, siteID, visitorID).Scan(&canonicalUserID, &firstSeen, &lastSeen, &sessions, &conversions)
	if canonicalUserID != nil {
		_ = s.DB.QueryRow(r.Context(), `SELECT array_agg(visitor_id ORDER BY first_seen) FROM visitor_identities WHERE site_id=$1 AND user_id=$2`, siteID, *canonicalUserID).Scan(&linkedVisitors)
		var canonicalProperties []byte
		if s.DB.QueryRow(r.Context(), `SELECT user_properties FROM identified_users WHERE site_id=$1 AND user_id=$2`, siteID, *canonicalUserID).Scan(&canonicalProperties) == nil {
			_ = json.Unmarshal(canonicalProperties, &latestUserProperties)
		}
		latestUserID = canonicalUserID
	}
	writeJSON(w, 200, map[string]any{"visitor_id": visitorID, "user_id": latestUserID, "linked_visitor_ids": linkedVisitors, "user_properties": latestUserProperties, "first_seen": firstSeen, "last_seen": lastSeen, "sessions": sessions, "conversions": conversions, "events": events})
}

func (s *Server) ecommerceReport(w http.ResponseWriter, r *http.Request) {
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
	var users, buyers, transactions, carts, checkouts int64
	var revenue, refunds float64
	err = s.DB.QueryRow(r.Context(), `WITH base AS (SELECT *,entity_id entity FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3) SELECT count(DISTINCT entity),count(DISTINCT entity) FILTER(WHERE event_name='purchase'),count(DISTINCT coalesce(properties->>'transaction_id',properties->>'order_id',event_id::text)) FILTER(WHERE event_name='purchase'),count(DISTINCT entity) FILTER(WHERE event_name='add_to_cart'),count(DISTINCT entity) FILTER(WHERE event_name='begin_checkout'),coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision,coalesce(sum(CASE WHEN event_name='refund' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision FROM base`, siteID, from, to).Scan(&users, &buyers, &transactions, &carts, &checkouts, &revenue, &refunds)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	productRows, err := s.DB.Query(r.Context(), `SELECT coalesce(item->>'item_id','(not set)'),coalesce(max(item->>'item_name'),''),coalesce(max(item->>'category'),''),coalesce(max(item->>'brand'),''),sum(CASE WHEN coalesce(item->>'quantity','1') ~ '^[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'quantity','1')::numeric ELSE 1 END)::double precision,sum((CASE WHEN coalesce(item->>'price','0') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'price','0')::numeric ELSE 0 END)*(CASE WHEN coalesce(item->>'quantity','1') ~ '^[0-9]+(\.[0-9]+)?$' THEN coalesce(item->>'quantity','1')::numeric ELSE 1 END))::double precision,count(DISTINCT coalesce(e.properties->>'transaction_id',e.properties->>'order_id',e.event_id::text)) FROM raw_events e CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.properties->'items')='array' THEN e.properties->'items' ELSE '[]'::jsonb END) item WHERE e.site_id=$1 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 AND e.event_name='purchase' GROUP BY 1 ORDER BY 6 DESC LIMIT 200`, siteID, from, to)
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
	stepRows, _ := s.DB.Query(r.Context(), `SELECT event_name,count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($4) GROUP BY event_name`, siteID, from, to, []string{"view_item", "add_to_cart", "begin_checkout", "purchase"})
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
