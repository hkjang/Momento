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

// get_metric_goals is offered to an agent as "목표와 현재 달성 상태" — the goals
// and how they are currently doing. It ran its own SELECT over metric_goals and
// answered the definitions: name, target, comparator, owner. Nothing in it said
// where the metric actually stood, so an agent asked "are we meeting the goal?"
// could only read the goal back.
//
// The goals screen evaluates each one against the period it covers. This holds
// the tool to the same answer, and refuses to run unless the evaluation produced
// a measurement — comparing two absent numbers is how a tool that answers
// nothing passes for a tool that agrees.
func TestTheGoalToolReportsAttainmentAndNotJustTheTarget(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// A visit inside the current period, so the goal's metric has something to
	// measure and "achieved" is a judgement rather than a default.
	f.postCollect(t, f.siteKey, "goal-session", "goal-visitor", "https://portal.internal/goals",
		[]string{fmt.Sprintf(`{"id":%q,"name":"click","timestamp":%d,"properties":{},"contract_version":1}`,
			uuid.NewString(), time.Now().UnixMilli())})
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	shownRows, _ := f.get(t, "/api/v1/sites/"+f.siteKey+"/metric-goals/evaluate")["list"].([]any)
	if len(shownRows) == 0 {
		t.Fatal("the goals screen evaluated nothing, so the tool cannot be held to it")
	}
	byName := map[string]map[string]any{}
	measured := false
	for _, row := range shownRows {
		shown, _ := row.(map[string]any)
		if shown == nil {
			continue
		}
		byName[fmt.Sprint(shown["name"])] = shown
		if value, _ := shown["value"].(float64); value > 0 {
			measured = true
		}
	}
	if !measured {
		t.Fatal("every goal evaluated to 0, so a tool that reported nothing would agree with the screen")
	}

	answered := f.callTool(t, "get_metric_goals", map[string]any{"site_id": f.siteKey})
	if len(answered) != len(shownRows) {
		t.Fatalf("the screen evaluates %d goals and the tool answers %d", len(shownRows), len(answered))
	}
	for _, row := range answered {
		said, _ := row.(map[string]any)
		if said == nil {
			continue
		}
		shown, ok := byName[fmt.Sprint(said["name"])]
		if !ok {
			t.Errorf("the tool reports a goal named %v that the screen does not evaluate", said["name"])
			continue
		}
		// value and achieved are the two the description promises; the rest are
		// what an agent needs to say anything useful about the gap.
		for _, field := range []string{"value", "target_value", "achieved", "progress_percent", "comparator", "period", "metric_name"} {
			value, valueOK := said[field]
			expected, expectedOK := shown[field]
			if !expectedOK {
				t.Errorf("%v: the screen does not report %s, so the comparison is meaningless", said["name"], field)
				continue
			}
			if !valueOK {
				t.Errorf("%v: the tool does not report %s — it was asked for 달성 상태 and answered with the goal", said["name"], field)
				continue
			}
			if fmt.Sprint(value) != fmt.Sprint(expected) {
				t.Errorf("%v %s: the tool says %v and the screen says %v", said["name"], field, value, expected)
			}
		}
	}
}

// analyze_retention carried its own copy of the cohort SQL. A copy agrees with
// the original until one of them is edited, and this one had already drifted in
// the way that matters most: it answered raw (cohort, week, retained) rows with
// no maturity, so a cohort that started days before the window closed reads as a
// cohort that did not come back. The screen pools the curve with cohortMaturity
// applied. This holds the tool to that curve, cell by cell.
func TestTheRetentionToolAnswersTheCohortScreensCurve(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	// Ninety days: the seeded cohorts start further back than a month, and a
	// window with no cohort in it is a window where both sides answer nothing.
	from, today := f.siteDates(t, 90)
	args := map[string]any{"site_id": f.siteKey, "from": from, "to": today, "environment": "prd"}
	cohort := "/api/v1/sites/" + f.siteKey + "/cohort?from=" + from + "&to=" + today + "&granularity=week&periods=12"

	tool := f.toolObject(t, "analyze_retention", args)
	screen := f.get(t, cohort)

	shownGrid, _ := screen["cohorts"].([]any)
	saidGrid, _ := tool["cohorts"].([]any)
	if len(shownGrid) == 0 {
		t.Fatal("the cohort screen has no cohorts in this period, so agreement would prove nothing")
	}
	if len(saidGrid) != len(shownGrid) {
		t.Fatalf("the screen has %d cohorts and the tool %d", len(shownGrid), len(saidGrid))
	}
	retained := 0.0
	for index := range shownGrid {
		shown, _ := shownGrid[index].(map[string]any)
		said, _ := saidGrid[index].(map[string]any)
		if shown == nil || said == nil {
			continue
		}
		for _, field := range []string{"cohort", "size"} {
			if fmt.Sprint(said[field]) != fmt.Sprint(shown[field]) {
				t.Errorf("cohort %d %s: the tool says %v and the screen says %v", index, field, said[field], shown[field])
			}
		}
		shownPeriods, _ := shown["periods"].([]any)
		saidPeriods, _ := said["periods"].([]any)
		if len(saidPeriods) != len(shownPeriods) {
			t.Errorf("cohort %v: the screen reports %d periods and the tool %d — a period the tool omits reads as a period nobody returned in",
				shown["cohort"], len(shownPeriods), len(saidPeriods))
			continue
		}
		for period := range shownPeriods {
			shownCell, _ := shownPeriods[period].(map[string]any)
			saidCell, _ := saidPeriods[period].(map[string]any)
			if shownCell == nil || saidCell == nil {
				continue
			}
			for _, field := range []string{"users", "retention_rate"} {
				if fmt.Sprint(saidCell[field]) != fmt.Sprint(shownCell[field]) {
					t.Errorf("cohort %v week %d %s: the tool says %v and the screen says %v",
						shown["cohort"], period, field, saidCell[field], shownCell[field])
				}
			}
			if period > 0 {
				if users, _ := shownCell["users"].(float64); users > 0 {
					retained += users
				}
			}
		}
	}
	if retained == 0 {
		t.Fatal("nobody returned after week 0 in this period, so the retention numbers being compared are all zero")
	}

	// The pooled curve is what an agent quotes. It is the part that applies
	// maturity, and it did not exist in the tool's answer at all.
	// The screen pools the curve only when it is asked to compare, and the
	// baseline entry is the same population the tool answers for.
	shownCurves, _ := f.get(t, cohort+"&segment_ids="+f.segmentID)["curves"].([]any)
	if len(shownCurves) == 0 {
		t.Fatal("the screen answered no baseline curve, so the tool's curve is held to nothing")
	}
	baseline, _ := shownCurves[0].(map[string]any)
	shownCurve, _ := baseline["periods"].([]any)
	saidCurve, _ := tool["curve"].([]any)
	if len(saidCurve) != len(shownCurve) {
		t.Fatalf("the screen's pooled curve has %d periods and the tool's %d", len(shownCurve), len(saidCurve))
	}
	for period := range shownCurve {
		shownCell, _ := shownCurve[period].(map[string]any)
		saidCell, _ := saidCurve[period].(map[string]any)
		for _, field := range []string{"users", "cohort_users", "retention_rate"} {
			if fmt.Sprint(saidCell[field]) != fmt.Sprint(shownCell[field]) {
				t.Errorf("pooled week %d %s: the tool says %v and the screen says %v", period, field, saidCell[field], shownCell[field])
			}
		}
	}
}
