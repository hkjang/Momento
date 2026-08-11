package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/insight"
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
		dateProperties := map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]any{"type": "string", "default": "prd"}, "from": map[string]string{"type": "string", "description": "YYYY-MM-DD"}, "to": map[string]string{"type": "string", "description": "YYYY-MM-DD"}}
		tools := []any{
			map[string]any{"name": "query_metrics", "description": "Momento 사이트의 기간별 핵심 사용자·세션·이벤트·전환 지표를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_internal_usage", "description": "부서·조직·서비스·기능·버튼·사내망별 사용량을 분석합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]any{"type": "string", "default": "prd"}, "dimension": map[string]any{"type": "string", "enum": []string{"department", "organization", "service", "feature", "button", "network"}}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "dimension", "from", "to"}}},
			map[string]any{"name": "query_ecommerce", "description": "매출·환불·거래·구매자·평균주문금액·구매전환율을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "query_identity_graph", "description": "SSO User ID에 결정적으로 연결된 Visitor ID와 최초·연결·최근 시각을 조회합니다. Visitor Profile 개인정보 정책이 활성화된 경우에만 동작합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "user_id": map[string]string{"type": "string", "description": "선택적 정확 일치 User ID"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "list_segments", "description": "사이트에서 사용할 수 있는 저장 Segment와 중첩 조건 정의를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "list_semantic_metrics", "description": "버전 관리되는 Semantic Metric Registry를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "query_semantic_metric", "description": "등록된 Semantic Metric을 지정 환경과 기간에 대해 계산합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]string{"type": "string"}, "metric": map[string]string{"type": "string"}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "metric", "from", "to"}}},
			map[string]any{"name": "analyze_retention", "description": "가입·최초 사용 Cohort의 주차별 Retention을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]string{"type": "string"}, "cohort_event": map[string]string{"type": "string"}, "return_event": map[string]string{"type": "string"}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_feature_adoption", "description": "조직·부서·기능별 사용자, 반복 사용과 Adoption을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_experience", "description": "Web Vitals와 오류 영향을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_ai_operations", "description": "AI 모델·Agent·MCP·Tool의 호출, 성공률, 지연, 토큰과 비용을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]string{"type": "string"}, "group_by": map[string]any{"type": "string", "enum": []string{"model", "provider", "agent", "mcp_server", "tool"}}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "inspect_data_quality", "description": "수집 지연, 중복, 계약 경고, PII 차단과 Cardinality 위반을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "get_workspace_rollup", "description": "Workspace의 전사 서비스 사용량과 교차 사이트 고유 SSO 사용자를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "get_feature_scores", "description": "기능별 Adoption, 재사용, 전환, 오류, Dead Feature 후보를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_search", "description": "검색량, Zero Result, CTR, 재검색과 성공률을 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "analyze_frustration", "description": "Rage Click, Dead Click, 오류, 재시도 등 Frustration 신호를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "get_metric_goals", "description": "Semantic Metric에 연결된 목표와 현재 달성 상태를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "get_event_catalog", "description": "이벤트 계약의 소유자, 버전, 최근 사용량과 상태를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]any{"type": "string", "default": "prd"}}, "required": []string{"site_id"}}},
			map[string]any{"name": "get_visitor_insights", "description": "방문자 인사이트를 한 번에 조회합니다: 전기간 대비 KPI, 신규·재방문, 채널 그룹, 진입 페이지, 방문 빈도·최근성, 기기, 실행 대상 Segment와 우선순위 인사이트.", "inputSchema": map[string]any{"type": "object", "properties": dateProperties, "required": []string{"site_id", "from", "to"}}},
			map[string]any{"name": "ask_analytics", "description": "완전 오프라인 Semantic Parser로 한국어·영어 분석 질문에 답합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "environment": map[string]string{"type": "string"}, "question": map[string]string{"type": "string"}}, "required": []string{"site_id", "question"}}},
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
	if call.Name == "query_metrics" || call.Name == "analyze_internal_usage" || call.Name == "query_ecommerce" || call.Name == "query_semantic_metric" || call.Name == "analyze_retention" || call.Name == "analyze_feature_adoption" || call.Name == "analyze_experience" || call.Name == "analyze_ai_operations" || call.Name == "inspect_data_quality" || call.Name == "get_workspace_rollup" || call.Name == "get_feature_scores" || call.Name == "analyze_search" || call.Name == "analyze_frustration" || call.Name == "get_visitor_insights" {
		var rangeErr error
		from, to, rangeErr = s.explicitDateRange(r.Context(), siteID, stringArg(call.Arguments, "from"), stringArg(call.Arguments, "to"))
		if rangeErr != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(rangeErr.Error(), true)))
			return
		}
	}
	switch call.Name {
	case "query_metrics":
		m, err := s.metrics(r, siteID, stringArgDefault(call.Arguments, "environment", "prd"), from, to)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(metricMap(m), "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "get_visitor_insights":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		_, location, tzErr := s.siteTimezone(r.Context(), siteID)
		if tzErr != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(tzErr.Error(), true)))
			return
		}
		previousFrom, previousTo := previousDateRange(from, to, location)
		report, err := insight.New(s.DB).Build(r.Context(), siteID, environment, from, to, previousFrom, previousTo)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(report, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_internal_usage":
		dim := stringArg(call.Arguments, "dimension")
		exprs := map[string]string{"department": "coalesce(canonical_user_properties->>'department','(미지정)')", "organization": "coalesce(canonical_user_properties->>'organization','(미지정)')", "service": "coalesce(properties->>'service','(미지정)')", "feature": "coalesce(properties->>'feature','(미지정)')", "button": "coalesce(properties->>'button',properties->>'element_text','(미지정)')", "network": "coalesce(network_name,'External / Unclassified')"}
		expr, ok := exprs[dim]
		if !ok {
			writeJSON(w, 200, rpcResult(req.ID, mcpText("Unsupported dimension", true)))
			return
		}
		sql := `SELECT ` + expr + `,count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 AND environment=$4`
		if dim == "button" {
			sql += ` AND event_name='click'`
		}
		sql += ` GROUP BY 1 ORDER BY 2 DESC LIMIT 20`
		rows, err := s.DB.Query(r.Context(), sql, siteID, from, to, stringArgDefault(call.Arguments, "environment", "prd"))
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
		err := s.DB.QueryRow(r.Context(), `WITH base AS (SELECT *,entity_id entity FROM analytics_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 AND environment=$4) SELECT count(DISTINCT entity),count(DISTINCT entity) FILTER(WHERE event_name='purchase'),count(DISTINCT coalesce(properties->>'transaction_id',properties->>'order_id',event_id::text)) FILTER(WHERE event_name='purchase'),coalesce(sum(CASE WHEN event_name='purchase' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision,coalesce(sum(CASE WHEN event_name='refund' AND coalesce(properties->>'value',properties->>'revenue','') ~ '^-?[0-9]+(\.[0-9]+)?$' THEN coalesce(properties->>'value',properties->>'revenue')::numeric ELSE 0 END),0)::double precision FROM base`, siteID, from, to, stringArgDefault(call.Arguments, "environment", "prd")).Scan(&users, &buyers, &transactions, &revenue, &refunds)
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
	case "list_semantic_metrics":
		rows, err := s.DB.Query(r.Context(), `SELECT name,label,description,definition,format,unit,definition_version,status FROM semantic_metrics WHERE site_id=$1 ORDER BY name`, siteID)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var name, label, description, format, unit, status string
			var definition []byte
			var version int
			if rows.Scan(&name, &label, &description, &definition, &format, &unit, &version, &status) == nil {
				var value any
				_ = json.Unmarshal(definition, &value)
				out = append(out, map[string]any{"name": name, "label": label, "description": description, "definition": value, "format": format, "unit": unit, "version": version, "status": status})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "query_semantic_metric":
		metric := stringArg(call.Arguments, "metric")
		var raw []byte
		var label, format, unit string
		var version int
		if err := s.DB.QueryRow(r.Context(), `SELECT definition,label,format,unit,definition_version FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active'`, siteID, metric).Scan(&raw, &label, &format, &unit, &version); err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText("Semantic metric not found", true)))
			return
		}
		var definition semanticDefinition
		if err := json.Unmarshal(raw, &definition); err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		value, err := s.evaluateSemanticMetric(r, siteID, stringArgDefault(call.Arguments, "environment", "prd"), from, to, definition, 1)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(map[string]any{"metric": metric, "label": label, "value": value, "format": format, "unit": unit, "version": version}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_retention":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		cohortEvent := stringArg(call.Arguments, "cohort_event")
		returnEvent := stringArg(call.Arguments, "return_event")
		timezone, _, _ := s.siteTimezone(r.Context(), siteID)
		rows, err := s.DB.Query(r.Context(), `WITH candidates AS (SELECT entity_id,min(date_trunc('week',event_timestamp AT TIME ZONE $5)::date) cohort_date FROM analytics_events WHERE site_id=$1 AND environment=$4 AND ($6='' OR event_name=$6) GROUP BY entity_id), cohorts AS (SELECT * FROM candidates WHERE cohort_date>=($2 AT TIME ZONE $5)::date AND cohort_date<($3 AT TIME ZONE $5)::date), activity AS (SELECT DISTINCT entity_id,date_trunc('week',event_timestamp AT TIME ZONE $5)::date activity_date FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND ($7='' OR event_name=$7)) SELECT c.cohort_date,((a.activity_date-c.cohort_date)/7) period,count(DISTINCT c.entity_id),(SELECT count(*) FROM cohorts x WHERE x.cohort_date=c.cohort_date) size FROM cohorts c JOIN activity a ON a.entity_id=c.entity_id AND a.activity_date>=c.cohort_date GROUP BY 1,2 ORDER BY 1,2`, siteID, from, to, environment, timezone, cohortEvent, returnEvent)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var cohort time.Time
			var period int
			var retained, size int64
			if rows.Scan(&cohort, &period, &retained, &size) == nil {
				out = append(out, map[string]any{"cohort": cohort.Format("2006-01-02"), "week": period, "users": retained, "size": size, "retention_rate": 100 * float64(retained) / float64(size)})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_feature_adoption":
		rows, err := s.DB.Query(r.Context(), `SELECT coalesce(canonical_user_properties->>'organization','(미지정)'),coalesce(canonical_user_properties->>'department','(미지정)'),properties->>'feature',count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND coalesce(properties->>'feature','')<>'' GROUP BY 1,2,3 ORDER BY 5 DESC LIMIT 100`, siteID, from, to, stringArgDefault(call.Arguments, "environment", "prd"))
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var org, dept, feature string
			var events, users int64
			if rows.Scan(&org, &dept, &feature, &events, &users) == nil {
				out = append(out, map[string]any{"organization": org, "department": dept, "feature": feature, "events": events, "users": users})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_experience":
		var errors, affected int64
		var lcp, inp, cls float64
		err := s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE event_name=ANY($5)),count(DISTINCT entity_id) FILTER(WHERE event_name=ANY($5)),coalesce(percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric) FILTER(WHERE event_name='web_vital' AND properties->>'metric'='LCP' AND coalesce(properties->>'value','')~'^[0-9]+(\.[0-9]+)?$'),0)::double precision,coalesce(percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric) FILTER(WHERE event_name='web_vital' AND properties->>'metric'='INP' AND coalesce(properties->>'value','')~'^[0-9]+(\.[0-9]+)?$'),0)::double precision,coalesce(percentile_disc(.75) WITHIN GROUP(ORDER BY (properties->>'value')::numeric) FILTER(WHERE event_name='web_vital' AND properties->>'metric'='CLS' AND coalesce(properties->>'value','')~'^[0-9]+(\.[0-9]+)?$'),0)::double precision FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, stringArgDefault(call.Arguments, "environment", "prd"), []string{"error", "resource_error"}).Scan(&errors, &affected, &lcp, &inp, &cls)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(map[string]any{"errors": errors, "affected_users": affected, "p75": map[string]float64{"LCP": lcp, "INP": inp, "CLS": cls}}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_ai_operations":
		group := stringArgDefault(call.Arguments, "group_by", "model")
		if !map[string]bool{"model": true, "provider": true, "agent": true, "mcp_server": true, "tool": true}[group] {
			group = "model"
		}
		rows, err := s.DB.Query(r.Context(), `SELECT coalesce(properties->>$5,'(not set)'),count(*),count(DISTINCT entity_id),coalesce(avg(CASE WHEN coalesce(properties->>'latency_ms','')~'^[0-9]+(\.[0-9]+)?$' THEN (properties->>'latency_ms')::numeric END),0)::double precision,coalesce(sum(CASE WHEN coalesce(properties->>'input_tokens','')~'^[0-9]+$' THEN (properties->>'input_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'output_tokens','')~'^[0-9]+$' THEN (properties->>'output_tokens')::bigint ELSE 0 END),0) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($6) GROUP BY 1 ORDER BY 2 DESC LIMIT 100`, siteID, from, to, stringArgDefault(call.Arguments, "environment", "prd"), group, []string{"ai_prompt", "ai_response", "ai_tool_call", "ai_agent_run", "ai_mcp_call", "ai_model_call"})
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var label string
			var calls, users, input, output int64
			var latency float64
			if rows.Scan(&label, &calls, &users, &latency, &input, &output) == nil {
				out = append(out, map[string]any{"label": label, "calls": calls, "users": users, "average_latency_ms": latency, "input_tokens": input, "output_tokens": output})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "inspect_data_quality":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		var received, accepted, duplicates, warnings, rejected, pii, piiDetected, cardinality int64
		err := s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(received),0),coalesce(sum(accepted),0),coalesce(sum(duplicates),0),coalesce(sum(warnings),0),coalesce(sum(rejected),0),coalesce(sum(pii_blocked),0),coalesce(sum(pii_detected),0),coalesce(sum(cardinality_violations),0) FROM data_quality_daily WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date <= $4::date`, siteID, environment, stringArg(call.Arguments, "from"), stringArg(call.Arguments, "to")).Scan(&received, &accepted, &duplicates, &warnings, &rejected, &pii, &piiDetected, &cardinality)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(map[string]any{"received": received, "accepted": accepted, "duplicates": duplicates, "warnings": warnings, "rejected": rejected, "pii_blocked": pii, "pii_detected": piiDetected, "cardinality_violations": cardinality}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "get_workspace_rollup":
		var workspaceID uuid.UUID
		if err := s.DB.QueryRow(r.Context(), `SELECT workspace_id FROM sites WHERE id=$1`, siteID).Scan(&workspaceID); err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		rows, err := s.DB.Query(r.Context(), `SELECT s.site_key,s.name,s.service_name,count(e.event_id),count(DISTINCT CASE WHEN e.canonical_user_id IS NOT NULL THEN 'u:'||e.canonical_user_id ELSE 's:'||e.site_id::text||':v:'||e.visitor_id END),count(DISTINCT e.session_id),count(DISTINCT e.entity_id) FILTER(WHERE e.is_conversion) FROM sites s LEFT JOIN analytics_events e ON e.site_id=s.id AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3 WHERE s.workspace_id=$1 AND s.active GROUP BY s.id ORDER BY 5 DESC`, workspaceID, from, to, environment)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var key, name, serviceName string
			var events, users, sessions, conversions int64
			if rows.Scan(&key, &name, &serviceName, &events, &users, &sessions, &conversions) == nil {
				out = append(out, map[string]any{"site_id": key, "site_name": name, "service": serviceName, "events": events, "users": users, "sessions": sessions, "conversion_users": conversions})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "get_feature_scores":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		rows, err := s.DB.Query(r.Context(), `WITH u AS (SELECT properties->>'feature' feature,entity_id,count(*) events,bool_or(is_conversion) converted FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND coalesce(properties->>'feature','')<>'' GROUP BY 1,2) SELECT feature,count(*) users,sum(events),count(*) FILTER(WHERE events>=2),count(*) FILTER(WHERE converted) FROM u GROUP BY feature ORDER BY users DESC LIMIT 100`, siteID, from, to, environment)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var feature string
			var users, events, repeats, converted int64
			if rows.Scan(&feature, &users, &events, &repeats, &converted) == nil {
				out = append(out, map[string]any{"feature": feature, "users": users, "events": events, "repeat_users": repeats, "repeat_rate": percent(repeats, users), "conversion_rate": percent(converted, users), "dead_feature_candidate": users < 10 && percent(repeats, users) < 10})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_search":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		var searches, users, zero, clicks, successes int64
		err := s.DB.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE event_name='search'),count(DISTINCT entity_id) FILTER(WHERE event_name='search'),count(*) FILTER(WHERE event_name='search_no_result' OR (event_name='search' AND properties->>'result_count'='0')),count(*) FILTER(WHERE event_name='search_click'),count(*) FILTER(WHERE event_name='search_success') FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, siteID, from, to, environment).Scan(&searches, &users, &zero, &clicks, &successes)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		body, _ := json.MarshalIndent(map[string]any{"searches": searches, "users": users, "zero_results": zero, "zero_result_rate": math.Min(100, percent(zero, searches)), "clicks": clicks, "search_ctr": math.Min(100, percent(clicks, searches)), "successes": successes, "success_rate": math.Min(100, percent(successes, searches))}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "analyze_frustration":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		signals := []string{"rage_click", "dead_click", "rapid_back", "form_retry", "repeated_search", "error_after_click", "slow_interaction", "error", "resource_error"}
		rows, err := s.DB.Query(r.Context(), `SELECT event_name,count(*),count(DISTINCT entity_id),count(DISTINCT session_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5) GROUP BY event_name ORDER BY count(*) DESC`, siteID, from, to, environment, signals)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var signal string
			var count, users, sessions int64
			if rows.Scan(&signal, &count, &users, &sessions) == nil {
				out = append(out, map[string]any{"signal": signal, "count": count, "users": users, "sessions": sessions})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "get_metric_goals":
		rows, err := s.DB.Query(r.Context(), `SELECT g.name,g.metric_name,g.target_value,g.comparator,g.period,g.environment,g.organization,g.department,g.owner,g.active FROM metric_goals g WHERE g.site_id=$1 ORDER BY g.active DESC,g.name`, siteID)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var name, metric, comparator, period, environment, org, dept, owner string
			var target float64
			var active bool
			if rows.Scan(&name, &metric, &target, &comparator, &period, &environment, &org, &dept, &owner, &active) == nil {
				out = append(out, map[string]any{"name": name, "metric": metric, "target": target, "comparator": comparator, "period": period, "environment": environment, "organization": org, "department": dept, "owner": owner, "active": active})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "get_event_catalog":
		environment := stringArgDefault(call.Arguments, "environment", "prd")
		rows, err := s.DB.Query(r.Context(), `SELECT d.name,d.description,d.owner,d.current_version,d.deprecated,count(e.event_id),max(e.event_timestamp) FROM event_definitions d LEFT JOIN raw_events e ON e.site_id=d.site_id AND e.event_name=d.name AND e.environment=$2 WHERE d.site_id=$1 GROUP BY d.name,d.description,d.owner,d.current_version,d.deprecated ORDER BY d.name`, siteID, environment)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var name, description, owner string
			var version int
			var deprecated bool
			var volume int64
			var last *time.Time
			if rows.Scan(&name, &description, &owner, &version, &deprecated, &volume, &last) == nil {
				out = append(out, map[string]any{"event": name, "description": description, "owner": owner, "version": version, "deprecated": deprecated, "volume": volume, "last_seen": last})
			}
		}
		body, _ := json.MarshalIndent(out, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	case "ask_analytics":
		question := strings.ToLower(stringArg(call.Arguments, "question"))
		to := time.Now().UTC()
		from := to.AddDate(0, 0, -7)
		metrics, err := s.platformMetrics(r, siteID, stringArgDefault(call.Arguments, "environment", "prd"), from, to)
		if err != nil {
			writeJSON(w, 200, rpcResult(req.ID, mcpText(err.Error(), true)))
			return
		}
		answer := fmt.Sprintf("최근 7일 사용자는 %d명, 이벤트는 %d건, 전환은 %d건, 오류는 %d건입니다.", metrics.Users, metrics.Events, metrics.Conversions, metrics.Errors)
		if strings.Contains(question, "error") || strings.Contains(question, "오류") {
			answer = fmt.Sprintf("최근 7일 오류는 %d건이며 사용자는 %d명입니다. Experience 분석에서 페이지와 오류 메시지별 영향을 확인하세요.", metrics.Errors, metrics.Users)
		}
		body, _ := json.MarshalIndent(map[string]any{"engine": "offline-semantic-v1", "answer": answer, "metrics": metrics, "confidence": .8}, "", "  ")
		writeJSON(w, 200, rpcResult(req.ID, mcpText(string(body), false)))
	default:
		writeJSON(w, 200, rpcResult(req.ID, mcpText("Unknown tool", true)))
	}
}
func mcpText(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isError}
}
func stringArg(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func stringArgDefault(m map[string]any, key, fallback string) string {
	v := stringArg(m, key)
	if v == "" {
		return fallback
	}
	return v
}
