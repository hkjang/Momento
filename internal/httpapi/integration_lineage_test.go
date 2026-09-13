package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// TestDataLineageFollowsARatioIntoItsSides holds the lineage graph to what a
// definition actually reads. The walk used to stop at the top level of the
// definition, and a ratio keeps its sources one level down: a conversion rate
// built from a purchase count over a session count arrived in the lineage
// table with no source at all, as a metric nobody feeds, while the catalog
// counted the same purchase event as used by it.
func TestDataLineageFollowsARatioIntoItsSides(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	site := "/api/v1/sites/" + f.siteKey

	// Order matters: a metric_ref is refused unless the metric it names exists.
	for _, metric := range []struct{ name, definition string }{
		{"purchases", `{"type":"count","event_name":"purchase"}`},
		{"visits", `{"type":"unique_sessions","event_name":"page_view"}`},
		// The event is one level down, in each side of the ratio.
		{"purchase_rate", `{"type":"ratio","numerator":{"type":"count","event_name":"purchase"},"denominator":{"type":"unique_sessions","event_name":"page_view"}}`},
		// The metrics are one level down, as references.
		{"purchase_share", `{"type":"ratio","numerator":{"type":"metric_ref","metric":"purchases"},"denominator":{"type":"metric_ref","metric":"visits"}}`},
		// The same event twice, two levels down: one edge, not two.
		{"refund_rate", `{"type":"ratio","numerator":{"type":"ratio","numerator":{"type":"count","event_name":"refund"},"denominator":{"type":"count","event_name":"purchase"}},"denominator":{"type":"count","event_name":"purchase"}}`},
	} {
		f.do(t, http.MethodPost, site+"/semantic-metrics",
			fmt.Sprintf(`{"name":%q,"label":%q,"definition":%s}`, metric.name, metric.name, metric.definition))
	}

	lineage := f.get(t, site+"/lineage")
	edges := map[string]int{}
	for _, row := range lineage["edges"].([]any) {
		edge := row.(map[string]any)
		edges[fmt.Sprintf("%s %s %s", edge["from"], edge["relation"], edge["to"])]++
	}
	for _, want := range []string{
		"event:purchase aggregates metric:purchases",
		"event:page_view aggregates metric:visits",
		"event:purchase aggregates metric:purchase_rate",
		"event:page_view aggregates metric:purchase_rate",
		"metric:purchases formula metric:purchase_share",
		"metric:visits formula metric:purchase_share",
		"event:refund aggregates metric:refund_rate",
		"event:purchase aggregates metric:refund_rate",
	} {
		if edges[want] != 1 {
			t.Errorf("the lineage draws %q %d times, the definitions say once", want, edges[want])
		}
	}

	nodes := map[string]bool{}
	for _, row := range lineage["nodes"].([]any) {
		nodes[fmt.Sprint(row.(map[string]any)["id"])] = true
	}
	for _, want := range []string{"event:purchase", "event:page_view", "event:refund"} {
		if !nodes[want] {
			t.Errorf("the lineage has no node %q, though a metric reads it", want)
		}
	}
}
