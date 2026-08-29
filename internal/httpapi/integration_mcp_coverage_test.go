package httpapi

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Half the MCP tools were named in no test at all.
//
// Twenty-two are declared and eleven appeared nowhere: the smoke test iterates
// the list and checks each one answers without an error, so they were covered by
// a rule about answering and by nothing about what they answer. That is the
// arrangement analyze_internal_usage was in when it disagreed with its screen on
// all three counts a grouping can disagree on, and analyze_ai_operations when it
// reported a cost report with no cost in it.
//
// The agreement tests name their pairs, so a tool is covered by being written
// into a list — which is a rule about the list rather than about the tools.
// This one is about the tools: every declared tool has to be named somewhere.
func TestEveryToolIsNamedInATest(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	source, err := os.ReadFile(filepath.Join(root, "internal", "httpapi", "mcp.go"))
	if err != nil {
		t.Fatalf("read mcp.go: %v", err)
	}
	declared := regexp.MustCompile(`"name": "(\w+)"`).FindAllStringSubmatch(string(source), -1)
	if len(declared) < 20 {
		t.Fatalf("found only %d declared tools, so this proves nothing about the rest", len(declared))
	}
	tests, err := filepath.Glob(filepath.Join(root, "internal", "httpapi", "*_test.go"))
	if err != nil {
		t.Fatalf("list the tests: %v", err)
	}
	// This file counts too: it names exactly the tools it exercises, and leaving
	// it out would have meant the tools it covers still reading as uncovered.
	corpus := ""
	for _, file := range tests {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		corpus += string(body)
	}
	for _, tool := range declared {
		if !strings.Contains(corpus, `"`+tool[1]+`"`) {
			t.Errorf("%s is offered to an agent and named in no test: the smoke test checks it answers, and nothing checks what it answers",
				tool[1])
		}
	}
}

// The tools that mirror a screen, against the screens they mirror.
//
// Each one is a second copy of a question the console already answers, and an
// agent quoting a number has no chart beside it to be contradicted by. Every
// comparison here refuses to run on a measure that is zero on both sides, since
// two nothings agree however wrong either is.
func TestTheRemainingToolsAgreeWithTheirScreens(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey
	args := map[string]any{"site_id": f.siteKey, "from": from, "to": today, "environment": "prd"}

	t.Run("analyze_attribution", func(t *testing.T) {
		tool := f.toolObject(t, "analyze_attribution", args)
		// The screen wraps the same report under "report" and adds the model list
		// beside it; the tool answers the report itself.
		screen, _ := f.get(t, site+"/attribution?from="+from+"&to="+today+"&model=last_non_direct")["report"].(map[string]any)
		if screen == nil {
			t.Fatal("the attribution screen answered no report")
		}
		for _, field := range []string{"total_conversions", "attributed_conversions", "unattributed_conversions", "model"} {
			if fmt.Sprint(tool[field]) != fmt.Sprint(screen[field]) {
				t.Errorf("%s: the tool says %v and the screen says %v", field, tool[field], screen[field])
			}
		}
		if toNumber(screen["total_conversions"]) == 0 {
			t.Fatal("the screen attributes no conversions, so agreeing with the tool proves nothing")
		}
		toolChannels, _ := tool["channels"].([]any)
		screenChannels, _ := screen["channels"].([]any)
		if len(toolChannels) != len(screenChannels) {
			t.Fatalf("the tool credits %d channels and the screen %d", len(toolChannels), len(screenChannels))
		}
		for index := range screenChannels {
			said, _ := toolChannels[index].(map[string]any)
			shown, _ := screenChannels[index].(map[string]any)
			for _, field := range []string{"channel", "credited_conversions", "credited_users", "credit_share_percent"} {
				if fmt.Sprint(said[field]) != fmt.Sprint(shown[field]) {
					t.Errorf("channel %v %s: the tool says %v and the screen says %v", shown["channel"], field, said[field], shown[field])
				}
			}
		}
	})

	t.Run("analyze_frustration", func(t *testing.T) {
		tool := f.toolObject(t, "analyze_frustration", args)
		screen := f.get(t, site+"/frustration?from="+from+"&to="+today)
		toolImpact, _ := tool["impact"].([]any)
		screenImpact, _ := screen["impact"].([]any)
		if len(screenImpact) == 0 {
			t.Fatal("the screen reports no friction impact, so there is nothing to hold the tool to")
		}
		if len(toolImpact) != len(screenImpact) {
			t.Fatalf("the tool reports %d signals and the screen %d", len(toolImpact), len(screenImpact))
		}
		for index := range screenImpact {
			said, _ := toolImpact[index].(map[string]any)
			shown, _ := screenImpact[index].(map[string]any)
			for _, field := range []string{"signal", "affected_people", "unaffected_people", "gap_points", "verdict"} {
				if fmt.Sprint(said[field]) != fmt.Sprint(shown[field]) {
					t.Errorf("%v %s: the tool says %v and the screen says %v", shown["signal"], field, said[field], shown[field])
				}
			}
		}
	})

	t.Run("detect_anomalies", func(t *testing.T) {
		tool := f.toolObject(t, "detect_anomalies", map[string]any{"site_id": f.siteKey, "environment": "prd"})
		screen := f.get(t, site+"/anomalies")
		for _, field := range []string{"evaluated_date", "timezone", "baseline_weeks"} {
			if fmt.Sprint(tool[field]) != fmt.Sprint(screen[field]) {
				t.Errorf("%s: the tool says %v and the screen says %v", field, tool[field], screen[field])
			}
		}
		toolChecked, _ := tool["checked"].([]any)
		screenChecked, _ := screen["checked"].([]any)
		if len(screenChecked) == 0 {
			t.Fatal("the screen checked no metric, so agreement proves nothing")
		}
		if len(toolChecked) != len(screenChecked) {
			t.Errorf("the tool checked %d metrics and the screen %d", len(toolChecked), len(screenChecked))
		}
	})

	t.Run("list_segments", func(t *testing.T) {
		tool := f.callTool(t, "list_segments", map[string]any{"site_id": f.siteKey})
		screen, _ := f.get(t, "/api/v1/segments?site_id="+f.siteKey)["list"].([]any)
		if len(screen) == 0 {
			t.Fatal("the site has no segments, so agreement proves nothing")
		}
		if len(tool) != len(screen) {
			t.Fatalf("the tool lists %d segments and the screen %d", len(tool), len(screen))
		}
		names := map[string]bool{}
		for _, row := range screen {
			if item, ok := row.(map[string]any); ok {
				names[fmt.Sprint(item["name"])] = true
			}
		}
		for _, row := range tool {
			item, _ := row.(map[string]any)
			if !names[fmt.Sprint(item["name"])] {
				t.Errorf("the tool offers a segment %q the screen does not list", item["name"])
			}
		}
	})

	t.Run("analyze_experience", func(t *testing.T) {
		tool := f.toolObject(t, "analyze_experience", args)
		screen := f.get(t, site+"/experience?from="+from+"&to="+today)
		impact, _ := screen["impact"].(map[string]any)
		if impact == nil {
			t.Fatal("the experience screen answered no impact block")
		}
		if toNumber(impact["error_users"]) == 0 {
			t.Fatal("nobody hit an error in this period, so agreeing about the impact proves nothing")
		}
		for _, field := range []string{"users", "error_users", "error_user_conversion_rate", "clean_user_conversion_rate", "conversion_rate_delta"} {
			if fmt.Sprint(tool[field]) != fmt.Sprint(impact[field]) {
				t.Errorf("%s: the tool says %v and the screen says %v", field, tool[field], impact[field])
			}
		}
		// And the vitals, which are the subject of the screen the tool is named
		// after. The screen reports them per page, so the comparison is that the
		// tool measured the same metrics rather than the same rows.
		p75, _ := tool["p75"].(map[string]any)
		if p75 == nil {
			t.Fatal("the tool reports no Web Vitals")
		}
		vitals, _ := screen["vitals"].([]any)
		seen := map[string]bool{}
		for _, row := range vitals {
			if item, ok := row.(map[string]any); ok {
				seen[fmt.Sprint(item["metric"])] = true
			}
		}
		measured := false
		for metric, value := range p75 {
			if toNumber(value) > 0 {
				measured = true
				if !seen[metric] {
					t.Errorf("the tool reports a %s of %v and the screen measures no %s at all", metric, value, metric)
				}
			}
		}
		if !measured {
			t.Fatal("every vital the tool reports is zero, so agreeing with the screen proves nothing")
		}
	})

	t.Run("get_visitor_insights", func(t *testing.T) {
		tool := f.toolObject(t, "get_visitor_insights", args)
		screen := f.get(t, site+"/visitor-insights?from="+from+"&to="+today)
		if fmt.Sprint(tool["headline"]) != fmt.Sprint(screen["headline"]) {
			t.Errorf("the tool and the screen lead with different sentences:\n  tool   %v\n  screen %v", tool["headline"], screen["headline"])
		}
		toolKPIs, _ := tool["kpis"].([]any)
		screenKPIs, _ := screen["kpis"].([]any)
		if len(screenKPIs) == 0 {
			t.Fatal("the screen reports no KPIs, so agreement proves nothing")
		}
		if len(toolKPIs) != len(screenKPIs) {
			t.Fatalf("the tool answers %d KPIs and the screen %d", len(toolKPIs), len(screenKPIs))
		}
		moved := false
		for index := range screenKPIs {
			said, _ := toolKPIs[index].(map[string]any)
			shown, _ := screenKPIs[index].(map[string]any)
			for _, field := range []string{"key", "current", "previous", "change_percent"} {
				if fmt.Sprint(said[field]) != fmt.Sprint(shown[field]) {
					t.Errorf("%v %s: the tool says %v and the screen says %v", shown["key"], field, said[field], shown[field])
				}
			}
			if toNumber(shown["current"]) != 0 {
				moved = true
			}
		}
		if !moved {
			t.Fatal("every KPI is zero on both sides, so this compares nothing")
		}
	})

	t.Run("list_semantic_metrics and query_semantic_metric", func(t *testing.T) {
		listed := f.callTool(t, "list_semantic_metrics", map[string]any{"site_id": f.siteKey})
		screen, _ := f.get(t, site+"/semantic-metrics")["list"].([]any)
		if len(screen) == 0 {
			t.Fatal("the site has no semantic metrics, so agreement proves nothing")
		}
		if len(listed) != len(screen) {
			t.Fatalf("the tool lists %d metrics and the screen %d", len(listed), len(screen))
		}
		first, _ := screen[0].(map[string]any)
		name := fmt.Sprint(first["name"])

		// And the value it computes for one of them, against the screen that
		// computes the same metric. Two doors onto one definition.
		computed := f.toolObject(t, "query_semantic_metric", map[string]any{
			"site_id": f.siteKey, "environment": "prd", "metric": name, "from": from, "to": today,
		})
		evaluated := f.get(t, site+"/semantic-metrics/"+name+"/query?from="+from+"&to="+today)
		if toNumber(evaluated["value"]) == 0 {
			t.Fatalf("%s evaluates to 0 on the screen, so agreeing with the tool proves nothing", name)
		}
		if fmt.Sprint(computed["value"]) != fmt.Sprint(evaluated["value"]) {
			t.Errorf("%s: the tool computes %v and the screen computes %v", name, computed["value"], evaluated["value"])
		}
	})

	t.Run("get_event_catalog", func(t *testing.T) {
		tool := f.callTool(t, "get_event_catalog", map[string]any{"site_id": f.siteKey, "environment": "prd"})
		screen, _ := f.get(t, site+"/event-contracts")["list"].([]any)
		if len(tool) != len(screen) {
			t.Errorf("the tool lists %d event contracts and the screen %d", len(tool), len(screen))
		}
		// The fixture defines no contracts, so both are empty. Said out loud
		// rather than passed over: this pair agrees on nothing, and a defect in
		// either would look exactly like this.
		if len(screen) == 0 {
			t.Log("the fixture defines no event contracts, so this compares two empty lists and proves only that neither invented rows")
		}
	})

	t.Run("ask_analytics", func(t *testing.T) {
		// The only tool with no screen behind it: it answers a question in a
		// sentence rather than mirroring a report. So it is held to the numbers it
		// quotes instead — the sentence names the last seven days, and the site's
		// own overview for those days has to say the same.
		answered := f.toolObject(t, "ask_analytics", map[string]any{
			"site_id": f.siteKey, "environment": "prd", "question": "지난 주 사용자",
		})
		sentence := fmt.Sprint(answered["answer"])
		if sentence == "" || sentence == "<nil>" {
			t.Fatalf("the tool answered no sentence: %v", answered)
		}
		week, weekToday := f.siteDates(t, 7)
		current, _ := f.get(t, site+"/overview?from="+week+"&to="+weekToday)["current"].(map[string]any)
		if current == nil {
			t.Fatal("the overview answered no period")
		}
		users := int64(toNumber(current["users"]))
		if users == 0 {
			t.Fatal("the site had no visitors in the last seven days, so a sentence quoting zero would agree with anything")
		}
		if !strings.Contains(sentence, fmt.Sprint(users)) {
			t.Errorf("the tool told an agent %q and the overview reports %d users for the same seven days: an agent repeating this sentence has no screen beside it",
				sentence, users)
		}
	})

	t.Run("query_identity_graph", func(t *testing.T) {
		tool := f.callTool(t, "query_identity_graph", map[string]any{"site_id": f.siteKey})
		screen, _ := f.get(t, site+"/identities")["list"].([]any)
		if len(screen) == 0 {
			t.Fatal("the site has no identified people, so agreement proves nothing")
		}
		linked := map[string]bool{}
		for _, row := range screen {
			if item, ok := row.(map[string]any); ok {
				linked[fmt.Sprint(item["user_id"])] = true
			}
		}
		if len(tool) == 0 {
			t.Fatal("the tool resolves no identities while the screen shows some")
		}
		for _, row := range tool {
			item, _ := row.(map[string]any)
			if !linked[fmt.Sprint(item["user_id"])] {
				t.Errorf("the tool links a user %q the identities screen does not show", item["user_id"])
			}
		}
	})
}
