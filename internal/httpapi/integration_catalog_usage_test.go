package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// TestEventCatalogCountsTheMetricsThatNameTheEvent holds the catalog's used_by
// to what the metric definitions actually say. The count used to be a LIKE over
// the definition's text, which read the event name as a pattern: "purchase" was
// a substring of "purchase_completed" and matched every metric on that event,
// and each "_" in a name stood for any one character. A reader deciding whether
// an event can be deprecated was told it was still in use when it was not.
func TestEventCatalogCountsTheMetricsThatNameTheEvent(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	site := "/api/v1/sites/" + f.siteKey

	// Three events whose names contain one another as text: "purchase" is a
	// substring of "purchase_completed", and "purchaseXcompleted" is what the
	// "_" wildcard would let "purchase_completed" match.
	for _, event := range []string{"purchase", "purchase_completed", "purchaseXcompleted"} {
		f.do(t, http.MethodPost, site+"/event-contracts",
			fmt.Sprintf(`{"event_name":%q,"schema":{"type":"object"},"validation_mode":"allow","activate":true}`, event))
	}
	// checkout_revenue names purchase_completed at the top level. purchase_rate
	// names purchase only inside its numerator, which is where a ratio keeps the
	// event. wildcard_revenue names purchaseXcompleted, which the pattern for
	// purchase_completed would also have read as its own.
	for name, definition := range map[string]string{
		"checkout_revenue": `{"type":"sum","event_name":"purchase_completed","property":"value"}`,
		"purchase_rate":    `{"type":"ratio","numerator":{"type":"count","event_name":"purchase"},"denominator":{"type":"unique_sessions"}}`,
		"wildcard_revenue": `{"type":"sum","event_name":"purchaseXcompleted","property":"value"}`,
	} {
		f.do(t, http.MethodPost, site+"/semantic-metrics",
			fmt.Sprintf(`{"name":%q,"label":%q,"definition":%s}`, name, name, definition))
	}
	// One goal, on the metric that names purchase_completed. The goal count for
	// purchase used to follow the same pattern and claimed this goal as well.
	f.do(t, http.MethodPost, site+"/metric-goals",
		`{"name":"분기 매출","metric_name":"checkout_revenue","target_value":1000,"comparator":"gte","period":"quarter","environment":"prd"}`)

	want := map[string][2]float64{
		"purchase":           {1, 0},
		"purchase_completed": {1, 1},
		"purchaseXcompleted": {1, 0},
	}
	catalog, _ := f.get(t, site+"/catalog")["list"].([]any)
	seen := 0
	for _, row := range catalog {
		entry, _ := row.(map[string]any)
		name := fmt.Sprint(entry["name"])
		expected, ok := want[name]
		if !ok {
			continue
		}
		seen++
		usedBy, _ := entry["used_by"].(map[string]any)
		if got := toNumber(usedBy["metrics"]); got != expected[0] {
			t.Errorf("%s: the catalog counts %v metrics, the definitions name it in %v", name, got, expected[0])
		}
		if got := toNumber(usedBy["goals"]); got != expected[1] {
			t.Errorf("%s: the catalog counts %v goals, the definitions name it in %v", name, got, expected[1])
		}
	}
	if seen != len(want) {
		t.Fatalf("the catalog listed %d of the %d events this test defined", seen, len(want))
	}
}
