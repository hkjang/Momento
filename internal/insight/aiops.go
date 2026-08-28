package insight

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AIOperationEvents are the events an application sends for its model, agent and
// tool calls. None of them is collected automatically.
var AIOperationEvents = []string{"ai_prompt", "ai_response", "ai_tool_call", "ai_agent_run", "ai_mcp_call", "ai_model_call"}

// AIOperationDimension is the cut the caller asked for, or the default.
func AIOperationDimension(group string) string {
	if map[string]bool{"model": true, "provider": true, "agent": true, "mcp_server": true, "tool": true}[group] {
		return group
	}
	return "model"
}

// AIOperations is the AI operations report: one row per model, provider, agent,
// MCP server or tool, with what it was called, by how many people, how often it
// succeeded, how long it took, what it consumed and what it cost.
//
// It lives here because three doors ask for it — the console screen, the MCP
// tool an agent uses, and the scheduled digest — and each one used to carry its
// own query. The tool's stopped at the token counts and answered a cost report
// with no cost in it (v0.34.3). The digest's still does.
func (r Reporter) AIOperations(ctx context.Context, siteID uuid.UUID, environment, group string, from, to time.Time) ([]map[string]any, error) {
	rows, err := r.DB.Query(ctx, `SELECT coalesce(properties->>$5,'(not set)'),count(*),count(DISTINCT entity_id),100.0*count(*) FILTER(WHERE lower(coalesce(properties->>'success','true')) IN ('true','1'))/nullif(count(*),0),coalesce(avg(CASE WHEN coalesce(properties->>'latency_ms','') ~ '^[0-9]+(\.[0-9]+)?$' THEN (properties->>'latency_ms')::numeric END),0)::double precision,coalesce(sum(CASE WHEN coalesce(properties->>'input_tokens','') ~ '^[0-9]+$' THEN (properties->>'input_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'output_tokens','') ~ '^[0-9]+$' THEN (properties->>'output_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'cost','') ~ '^[0-9]+(\.[0-9]+)?$' THEN (properties->>'cost')::numeric ELSE 0 END),0)::double precision,count(*) FILTER(WHERE lower(coalesce(properties->>'fallback_model','')) NOT IN ('','false'))
		FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($6) GROUP BY 1 ORDER BY 2 DESC LIMIT 200`,
		siteID, from, to, environment, group, AIOperationEvents)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var label string
		var calls, users, inputTokens, outputTokens, fallbacks int64
		var successRate, latency, cost float64
		if rows.Scan(&label, &calls, &users, &successRate, &latency, &inputTokens, &outputTokens, &cost, &fallbacks) != nil {
			continue
		}
		out = append(out, map[string]any{"label": label, "calls": calls, "users": users, "success_rate": successRate,
			"average_latency_ms": latency, "input_tokens": inputTokens, "output_tokens": outputTokens, "cost": cost, "fallbacks": fallbacks})
	}
	return out, rows.Err()
}

// AIOperationTotals folds the rows into the figures a digest leads with. A
// weighted success rate and latency, not a mean of means: a model called twice
// must not weigh as much as one called ten thousand times.
func AIOperationTotals(rows []map[string]any) map[string]any {
	var calls, inputTokens, outputTokens, fallbacks, successes int64
	var cost, latencyWeighted float64
	for _, row := range rows {
		rowCalls, _ := row["calls"].(int64)
		calls += rowCalls
		inputTokens += toInt64(row["input_tokens"])
		outputTokens += toInt64(row["output_tokens"])
		fallbacks += toInt64(row["fallbacks"])
		cost += toFloat64(row["cost"])
		successes += int64(toFloat64(row["success_rate"]) * float64(rowCalls) / 100)
		latencyWeighted += toFloat64(row["average_latency_ms"]) * float64(rowCalls)
	}
	totals := map[string]any{"calls": calls, "input_tokens": inputTokens, "output_tokens": outputTokens,
		"fallbacks": fallbacks, "cost": cost, "success_rate": float64(0), "average_latency_ms": float64(0)}
	if calls > 0 {
		totals["success_rate"] = float64(successes) * 100 / float64(calls)
		totals["average_latency_ms"] = latencyWeighted / float64(calls)
	}
	return totals
}

// AIOperationUsers is how many distinct people made any AI call in the window.
// It cannot be folded out of the rows: the same person appears under every model
// they used, so summing the per-row counts overstates the population.
func (r Reporter) AIOperationUsers(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (int64, error) {
	var users int64
	err := r.DB.QueryRow(ctx, `SELECT count(DISTINCT entity_id) FROM analytics_events
		WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5)`,
		siteID, from, to, environment, AIOperationEvents).Scan(&users)
	return users, err
}

func toInt64(value any) int64 {
	if number, ok := value.(int64); ok {
		return number
	}
	return 0
}

func toFloat64(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return 0
}
