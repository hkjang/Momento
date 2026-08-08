package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/version"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func rpcResult(id, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := decodeJSON(r, &req, 256<<10); err != nil {
		writeJSON(w, 400, rpcError(nil, -32700, "Parse error"))
		return
	}
	switch req.Method {
	case "initialize":
		writeJSON(w, 200, rpcResult(req.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "momento-analytics", "version": version.Version}}))
	case "notifications/initialized":
		w.WriteHeader(202)
	case "tools/list":
		dateProperties := map[string]any{"site_id": map[string]string{"type": "string"}, "from": map[string]string{"type": "string", "description": "YYYY-MM-DD"}, "to": map[string]string{"type": "string", "description": "YYYY-MM-DD"}}
		tools := []any{
			map[string]any{"name": "query_metrics", "description": "Momento 사이트의 기간별 핵심 사용자·세션·이벤트·전환 지표를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_internal_usage", "description": "부서·조직·서비스·기능·버튼·사내망별 사용량을 분석합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "dimension": map[string]any{"type": "string", "enum": []string{"department", "organization", "service", "feature", "button", "network"}}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "dimension", "from", "to"}}},
			map[string]any{"name": "query_ecommerce", "description": "매출·환불·거래·구매자·평균주문금액·구매전환율을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "query_identity_graph", "description": "SSO User ID에 결정적으로 연결된 Visitor ID와 최초·연결·최근 시각을 조회합니다. Visitor Profile 개인정보 정책이 활성화된 경우에만 동작합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "user_id": map[string]string{"type": "string", "description": "선택적 정확 일치 User ID"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "list_segments", "description": "사이트에서 사용할 수 있는 저장 Segment와 중첩 조건 정의를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}}, "required": []string{"site_id"}}},
		}
		writeJSON(w, 200, rpcResult(req.ID, map[string]any{"tools": tools}))
	case "tools/call":
		s.mcpCall(w, r, req)
	default:
		writeJSON(w, 200, rpcError(req.ID, -32601, "Method not found"))
	}
}
func (s *Server) mcpCall(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &call) != nil {
		writeJSON(w, 200, rpcError(req.ID, -32602, "Invalid params"))
		return
	}
	siteKey, _ := call.Arguments["site_id"].(string)
	siteID, siteErr := s.resolveSiteKey(r.Context(), siteKey)
	if siteErr != nil {
		writeJSON(w, 200, rpcResult(req.ID, mcpText("Site not found", true)))
		return
	}
	var from, to time.Time
	if call.Name == "query_metrics" || call.Name == "analyze_internal_usage" || call.Name == "query_ecommerce" {
		var rangeErr error
		from, to, rangeErr = s.explicitDateRange(r.Context(), siteID, stringArg(call.Arguments, "from"), stringArg(call.Arguments, "to"))
		if rangeErr != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(rangeErr.Error(), true)))
			return
		}
	}
	switch call.Name {
	case "query_metrics":
		m, err := s.metrics(r, siteID, from, to)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(metricMap(m), "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_internal_usage":
		dim := stringArg(call.Arguments, "dimension")
		exprs := map[string]string{"department": "coalesce(canonical_user_properties->>'department','(미지정)')", "organization": "coalesce(canonical_user_properties->>'organization','(미지정)')", "service": "coalesce(properties->>'service','(미지정)')", "feature": "coalesce(properties->>'feature','(미지정)')", "button": "coalesce(properties->>'button',properties->>'element_text','(미지정)')", "network": "coalesce(network_name,'External / Unclassified')"}
		expr, ok := exprs[dim]
		if !ok {
			writeJSON(w, 200, rpcResult(req.ID, mcpText("Unsupported dimension", true)))
			return
		}
		sql := `SELECT ` + expr + `,count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3`
		if dim == "button" {
			sql += ` AND event_name='click'`
		}
		sql += ` GROUP BY 1 ORDER BY 2 DESC LIMIT 20`
		rows, err := s.DB.Query(r.Context(), sql, siteID, from, to)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var label string
			var events, users int64
			if rows.Scan(&label, &events, &users) == nil {
				out = append(out, map[string]any{"label": label, "events": events, "users": users})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "query_ecommerce":
		var users, buyers, transactions int64
		var revenue, refunds float64
		err := s.DB.QueryRow(r.Context(), `WITH base AS (SELECT *,entity_id entity FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3) SELECT count(DISTINCT entity),count(DISTINCT entity) FILTER(WHERE event_name='purchase'),count(DISTINCT coalesce(properties->>'transaction_id',properties->>'order_id',event_id::text)) FILTER(WHERE event_name='purchase'),coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision,coalesce(sum(CASE WHEN event_name='refund' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision FROM base`, siteID, from, to).Scan(&users, &buyers, &transactions, &revenue, &refunds)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		aov, rate := float64(0), float64(0)
		if transactions > 0 {
			aov = revenue / float64(transactions)
		}
		if users > 0 {
			rate = float64(buyers) * 100 / float64(users)
		}
		body, _ := json.MarshalIndent(map[string]any{"revenue": revenue, "refunds": refunds, "net_revenue": revenue - refunds, "transactions": transactions, "buyers": buyers, "average_order_value": aov, "purchase_conversion_rate": rate}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "query_identity_graph":
		var profiles bool
		if err := s.DB.QueryRow(r.Context(), `SELECT coalesce((value->>'visitor_profiles')::bool,false) FROM settings WHERE key='privacy'`).Scan(&profiles); err != nil || !profiles {
			writeJSON(w, 200, rpcResult(req.ID, mcpText("Visitor Explorer is disabled by the privacy policy", true)))
			return
		}
		userID := stringArg(call.Arguments, "user_id")
		rows, err := s.DB.Query(r.Context(), `SELECT i.user_id,count(*),array_agg(i.visitor_id ORDER BY i.first_seen),min(i.first_seen),min(i.linked_at),max(i.last_seen),coalesce(sum(v.event_count),0),coalesce(sum(v.conversion_count),0)
			FROM visitor_identities i LEFT JOIN visitors v ON v.site_id=i.site_id AND v.visitor_id=i.visitor_id
			WHERE i.site_id=$1 AND ($2='' OR i.user_id=$2) GROUP BY i.user_id ORDER BY max(i.last_seen) DESC LIMIT 500`, siteID, userID)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var currentUser string
			var visitorIDs []string
			var visitors, events, conversions int64
			var firstSeen, linkedAt, lastSeen time.Time
			if rows.Scan(&currentUser, &visitors, &visitorIDs, &firstSeen, &linkedAt, &lastSeen, &events, &conversions) == nil {
				out = append(out, map[string]any{"user_id": currentUser, "visitor_count": visitors, "visitor_ids": visitorIDs, "first_seen": firstSeen, "linked_at": linkedAt, "last_seen": lastSeen, "events": events, "conversions": conversions, "confidence": 1, "source": "identify"})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "list_segments":
		p, _ := auth.FromContext(r.Context())
		rows, err := s.DB.Query(r.Context(), `SELECT name,description,definition,shared FROM segments WHERE site_id=$1 AND (shared OR owner_id=$2 OR $3 IN ('super_admin','organization_admin','workspace_admin')) ORDER BY name`, siteID, p.ID, p.Role)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var name, description string
			var definition []byte
			var shared bool
			if rows.Scan(&name, &description, &definition, &shared) == nil {
				var value any
				_ = json.Unmarshal(definition, &value)
				out = append(out, map[string]any{"name": name, "description": description, "definition": value, "shared": shared})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	default:
		writeJSON(w, 200, rpcResult(req.ID, mcpText("Unknown tool", true)))
	}
}
func mcpText(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isError}
}
func stringArg(m map[string]any, key string) string { v, _ := m[key].(string); return v }
