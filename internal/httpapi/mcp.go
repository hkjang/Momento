package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

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
		writeJSON(w, 200, rpcResult(req.ID, map[string]any{"tools": []any{map[string]any{"name": "query_metrics", "description": "Momento 사이트의 기간별 핵심 사용자·세션·이벤트·전환 지표를 조회합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "from": map[string]string{"type": "string", "description": "YYYY-MM-DD"}, "to": map[string]string{"type": "string", "description": "YYYY-MM-DD"}}, "required": []string{"site_id", "from", "to"}}}, map[string]any{"name": "analyze_internal_usage", "description": "부서·조직·서비스·기능·버튼·사내망별 사용량을 분석합니다.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"site_id": map[string]string{"type": "string"}, "dimension": map[string]any{"type": "string", "enum": []string{"department", "organization", "service", "feature", "button", "network"}}, "from": map[string]string{"type": "string"}, "to": map[string]string{"type": "string"}}, "required": []string{"site_id", "dimension", "from", "to"}}}}}))
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
	from, err1 := time.Parse("2006-01-02", stringArg(call.Arguments, "from"))
	to, err2 := time.Parse("2006-01-02", stringArg(call.Arguments, "to"))
	to = to.Add(24 * time.Hour)
	if err1 != nil || err2 != nil {
		writeJSON(w, 200, rpcResult(req.ID, mcpText("from/to must use YYYY-MM-DD", true)))
		return
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
		exprs := map[string]string{"department": "coalesce(user_properties->>'department','(미지정)')", "organization": "coalesce(user_properties->>'organization','(미지정)')", "service": "coalesce(properties->>'service','(미지정)')", "feature": "coalesce(properties->>'feature','(미지정)')", "button": "coalesce(properties->>'button',properties->>'element_text','(미지정)')", "network": "coalesce(network_name,'External / Unclassified')"}
		expr, ok := exprs[dim]
		if !ok {
			writeJSON(w, 200, rpcResult(req.ID, mcpText("Unsupported dimension", true)))
			return
		}
		sql := `SELECT ` + expr + `,count(*),count(DISTINCT visitor_id) FROM raw_events WHERE site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3`
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
	default:
		writeJSON(w, 200, rpcResult(req.ID, mcpText("Unknown tool", true)))
	}
}
func mcpText(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isError}
}
func stringArg(m map[string]any, key string) string { v, _ := m[key].(string); return v }
