package httpapi

import (
	"encoding/json"
	"fmt"
	"testing"
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

var _ = fmt.Sprint
