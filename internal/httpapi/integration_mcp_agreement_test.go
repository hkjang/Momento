package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The MCP tools answer the questions the screens answer, for an agent instead of
// a reader. They were checked for answering; nothing checked what they answered.
//
// That is the harder place for a wrong number to live. A screen puts the figure
// next to a chart and a period selector, and somebody eventually compares them.
// An agent reports a number in a sentence, with nothing beside it.
//
// analyze_internal_usage and the usage screen had drifted apart on all three
// counts a grouping can drift: what counts as unset, what an absent value falls
// back to, and which events are included at all.
func TestTheMCPToolsAgreeWithTheScreens(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	screen := f.get(t, site+"/usage?from="+from+"&to="+today)
	compared := 0
	for _, dimension := range usageDimensions {
		rows, _ := screen[dimension.key].([]any)
		tool := f.callTool(t, "analyze_internal_usage", map[string]any{
			"site_id": f.siteKey, "from": from, "to": today, "environment": "prd",
			"dimension": dimension.tool,
		})
		if len(rows) == 0 && len(tool) == 0 {
			continue
		}
		if len(rows) != len(tool) {
			t.Errorf("%s: the screen reports %d rows and the tool %d — an agent and a reader are being told different things",
				dimension.tool, len(rows), len(tool))
			continue
		}
		for index := range rows {
			shown, _ := rows[index].(map[string]any)
			said, _ := tool[index].(map[string]any)
			if shown == nil || said == nil {
				continue
			}
			if shown["label"] != said["label"] {
				t.Errorf("%s row %d: the screen calls it %v and the tool calls it %v",
					dimension.tool, index, shown["label"], said["label"])
				continue
			}
			for _, measure := range []string{"events", "users"} {
				if shown[measure] != said[measure] {
					t.Errorf("%s %v: the screen says %v %s and the tool says %v",
						dimension.tool, shown["label"], shown[measure], measure, said[measure])
				}
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no rows were comparable, so this proves nothing about the two agreeing")
	}
	t.Logf("compared %d rows across %d cuts of the same period", compared, len(usageDimensions))

	// The three list-shaped tools that mirror a screen, row by row. The AI one
	// used to answer a strictly smaller report than its screen — no success rate
	// and no cost, which are the two numbers somebody asks an agent about.
	for _, probe := range []struct {
		tool, screen, list, key string
		arguments               map[string]any
		fields                  []string
	}{
		{
			tool: "get_workspace_rollup", screen: site + "/workspace-rollup?from=" + from + "&to=" + today,
			list: "services", key: "site_id",
			fields: []string{"site_name", "service", "events", "users", "sessions", "conversion_users"},
		},
		{
			tool: "get_feature_scores", screen: site + "/feature-intelligence?from=" + from + "&to=" + today,
			list: "features", key: "feature",
			fields: []string{"events", "users", "repeat_rate", "conversion_rate"},
		},
		{
			tool: "analyze_ai_operations", screen: site + "/ai-analytics?from=" + from + "&to=" + today + "&group_by=model",
			list: "rows", key: "label", arguments: map[string]any{"group_by": "model"},
			fields: []string{"calls", "users", "success_rate", "average_latency_ms", "input_tokens", "output_tokens", "cost", "fallbacks"},
		},
	} {
		arguments := map[string]any{"site_id": f.siteKey, "from": from, "to": today, "environment": "prd"}
		for key, value := range probe.arguments {
			arguments[key] = value
		}
		answered := f.callTool(t, probe.tool, arguments)
		shownRows, _ := f.get(t, probe.screen)[probe.list].([]any)
		if len(shownRows) == 0 {
			t.Errorf("%s: the screen has no %s to compare against, so agreement proves nothing", probe.tool, probe.list)
			continue
		}
		byKey := map[string]map[string]any{}
		for _, row := range shownRows {
			if shown, ok := row.(map[string]any); ok {
				byKey[fmt.Sprint(shown[probe.key])] = shown
			}
		}
		for _, row := range answered {
			said, _ := row.(map[string]any)
			if said == nil {
				continue
			}
			shown, ok := byKey[fmt.Sprint(said[probe.key])]
			if !ok {
				t.Errorf("%s: the tool reports %v and the screen does not", probe.tool, said[probe.key])
				continue
			}
			for _, field := range probe.fields {
				value, valueOK := said[field]
				expected, expectedOK := shown[field]
				if !valueOK || !expectedOK {
					t.Errorf("%s %v: %s is on the screen=%v and in the tool=%v", probe.tool, said[probe.key], field, expectedOK, valueOK)
					continue
				}
				if fmt.Sprint(value) != fmt.Sprint(expected) {
					t.Errorf("%s %v %s: the tool says %v and the screen says %v", probe.tool, said[probe.key], field, value, expected)
				}
			}
		}
	}
}

// callTool runs one MCP tool and decodes the JSON document it answers with.
func (f fixture) callTool(t *testing.T, name string, arguments map[string]any) []any {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if err != nil {
		t.Fatalf("encode the call: %v", err)
	}
	response := f.rpc(t, string(body))
	result, _ := response["result"].(map[string]any)
	if result == nil {
		t.Fatalf("%s answered without a result: %v", name, response)
	}
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("%s failed: %v", name, result["content"])
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("%s answered with no content: %v", name, result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var decoded []any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s answered with something that is not a list: %v (%s)", name, err, truncateBody(text))
	}
	return decoded
}

// toolObject runs a tool that answers with an object rather than a list.
func (f fixture) toolObject(t *testing.T, name string, arguments map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 8, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if err != nil {
		t.Fatalf("encode the call: %v", err)
	}
	response := f.rpc(t, string(body))
	result, _ := response["result"].(map[string]any)
	if result == nil {
		t.Fatalf("%s answered without a result: %v", name, response)
	}
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("%s failed: %v", name, result["content"])
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("%s answered with no content", name)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s answered with something that is not an object: %v (%s)", name, err, truncateBody(text))
	}
	return decoded
}

// Three more tools that carry their own SQL, against the screens they mirror.
//
// The comparison has to be made against numbers that are not zero. This fixture
// has no searches and nothing has passed through the collector, so both sides
// answer nothing and agree however wrong either is — the trap this repository
// has been caught by before. So the activity each tool reports on is delivered
// first, and the test refuses to run on a period it could not discriminate in.
func TestTheMCPToolsAndTheScreensAgreeOnNumbersThatExist(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	site := "/api/v1/sites/" + f.siteKey

	now := time.Now()
	deliver := func(visitor string, events []string, properties []string) {
		t.Helper()
		parts := make([]string, 0, len(events))
		for index, name := range events {
			parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":%s,"contract_version":1}`,
				uuid.NewString(), name, now.Add(time.Duration(index)*time.Second).UnixMilli(), properties[index]))
		}
		f.postCollect(t, f.siteKey, "mcp-"+visitor, visitor, "https://portal.internal/search", parts)
	}
	// Searches that separate every rate the screen reports: three searches, one
	// of them with no results, and one click on a result.
	deliver("mcp-searcher", []string{"search", "search", "search", "search_click"}, []string{
		`{"result_count":"7","query_words":2}`,
		`{"result_count":"3","query_words":1}`,
		`{"result_count":"0","query_words":4}`,
		`{"position":"1"}`,
	})
	// An address in a property, so the privacy counters the data quality screen
	// reports are not all zero either.
	deliver("mcp-quality", []string{"click"}, []string{`{"contact":"person@example.com"}`})
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	from, today := f.siteDates(t, 1)
	args := map[string]any{"site_id": f.siteKey, "from": from, "to": today, "environment": "prd"}

	agree := func(label string, tool, screen map[string]any, fields []string, mustExceedZero string) {
		t.Helper()
		if tool == nil || screen == nil {
			t.Fatalf("%s: one side answered nothing (tool=%v screen=%v)", label, tool != nil, screen != nil)
		}
		if value, _ := screen[mustExceedZero].(float64); value <= 0 {
			t.Fatalf("%s: the screen reports %s=%v, so agreeing with the tool would prove nothing", label, mustExceedZero, value)
		}
		for _, field := range fields {
			said, saidOK := tool[field]
			shown, shownOK := screen[field]
			if !saidOK || !shownOK {
				t.Errorf("%s: %s is on the screen=%v and in the tool=%v", label, field, shownOK, saidOK)
				continue
			}
			if fmt.Sprint(said) != fmt.Sprint(shown) {
				t.Errorf("%s %s: the tool says %v and the screen says %v — an agent quoting this has no chart beside it to be contradicted by",
					label, field, said, shown)
			}
		}
	}

	search := f.get(t, site+"/search-analytics?from="+from+"&to="+today)
	searchSummary, _ := search["summary"].(map[string]any)
	agree("analyze_search", f.toolObject(t, "analyze_search", args), searchSummary,
		[]string{"searches", "users", "clicks", "successes", "search_ctr", "success_rate", "zero_results", "zero_result_rate"},
		"searches")

	quality := f.get(t, site+"/data-quality?from="+from+"&to="+today)
	qualitySection, _ := quality["quality"].(map[string]any)
	agree("inspect_data_quality", f.toolObject(t, "inspect_data_quality", args), qualitySection,
		[]string{"duplicates", "warnings", "rejected", "pii_blocked", "pii_detected", "cardinality_violations"},
		"pii_detected")

	longFrom, longTo := f.siteDates(t, 30)
	ecommerce := f.get(t, site+"/ecommerce?from="+longFrom+"&to="+longTo)
	ecommerceSummary, _ := ecommerce["summary"].(map[string]any)
	agree("query_ecommerce", f.toolObject(t, "query_ecommerce", map[string]any{
		"site_id": f.siteKey, "from": longFrom, "to": longTo, "environment": "prd",
	}), ecommerceSummary,
		[]string{"revenue", "refunds", "net_revenue", "transactions", "buyers", "average_order_value", "purchase_conversion_rate"},
		"revenue")
}
