package insight

import (
	"strings"
	"testing"
)

func TestEveryOfferedModelHasAWeight(t *testing.T) {
	t.Parallel()

	models := AttributionModels()
	if len(models) != 6 {
		t.Fatalf("models = %d, want 6", len(models))
	}
	if models[0].Key != "last_non_direct" {
		t.Fatalf("first model = %q, want last_non_direct as the default", models[0].Key)
	}
	multiTouch := 0
	for _, model := range models {
		if model.Label == "" || model.Description == "" {
			t.Fatalf("model %q is missing a label or description", model.Key)
		}
		if _, ok := AttributionWeight(model.Key, 7); !ok {
			t.Fatalf("model %q is offered but has no weight", model.Key)
		}
		if model.MultiTouch {
			multiTouch++
		}
	}
	if multiTouch != 3 {
		t.Fatalf("multi touch models = %d, want 3", multiTouch)
	}
}

func TestUnsupportedModelIsRejected(t *testing.T) {
	t.Parallel()

	// The weight is interpolated into SQL, so anything outside the map must fail
	// rather than reach the query builder.
	for _, model := range []string{"", "u_shaped", "1.0 FROM sessions; DROP TABLE sites"} {
		if _, ok := AttributionWeight(model, 7); ok {
			t.Fatalf("unsupported model %q was accepted", model)
		}
		if _, ok := AttributionOrder(model); ok {
			t.Fatalf("unsupported model %q passed the compatibility check", model)
		}
	}
}

func TestTimeDecayHalfLifeIsClampedIntoTheQuery(t *testing.T) {
	t.Parallel()

	expression, ok := AttributionWeight("time_decay", 14)
	if !ok {
		t.Fatal("time_decay has no weight")
	}
	if !strings.Contains(expression, "pow(0.5,p.days_before/14.0)") {
		t.Fatalf("expression %q does not use the requested half life", expression)
	}
	if !strings.Contains(expression, "OVER (PARTITION BY p.event_id)") {
		t.Fatalf("expression %q must normalise per conversion so weights sum to one", expression)
	}
	// Out of range values fall back to the default instead of reaching SQL.
	for _, invalid := range []int{0, -3, 500} {
		expression, _ := AttributionWeight("time_decay", invalid)
		if !strings.Contains(expression, "/7.0") {
			t.Fatalf("half life %d produced %q, want the 7 day default", invalid, expression)
		}
	}
}

func TestSingleTouchWeightsPickExactlyOneVisit(t *testing.T) {
	t.Parallel()

	first, _ := AttributionWeight("first_touch", 7)
	if !strings.Contains(first, "p.position=1 THEN 1.0") || !strings.Contains(first, "ELSE 0.0") {
		t.Fatalf("first_touch = %q, want all credit on the first visit", first)
	}
	last, _ := AttributionWeight("last_touch", 7)
	if !strings.Contains(last, "p.position=p.touches THEN 1.0") {
		t.Fatalf("last_touch = %q, want all credit on the last visit", last)
	}
	nonDirect, _ := AttributionWeight("last_non_direct", 7)
	if !strings.Contains(nonDirect, "p.non_direct_touches>0") || !strings.Contains(nonDirect, "p.non_direct_rank=1") {
		t.Fatalf("last_non_direct = %q, want the latest known channel", nonDirect)
	}
	if !strings.Contains(nonDirect, "p.position=p.touches THEN 1.0") {
		t.Fatalf("last_non_direct = %q, want a direct fallback when no channel is known", nonDirect)
	}
}

func TestPositionBasedSplitsFortyTwentyForty(t *testing.T) {
	t.Parallel()

	expression, _ := AttributionWeight("position_based", 7)
	for _, want := range []string{"p.touches=1 THEN 1.0", "p.touches=2 THEN 0.5", "THEN 0.4", "0.2/(p.touches-2)"} {
		if !strings.Contains(expression, want) {
			t.Fatalf("position_based = %q is missing %q", expression, want)
		}
	}
}

func TestLinearSplitsEvenly(t *testing.T) {
	t.Parallel()

	expression, _ := AttributionWeight("linear", 7)
	if expression != "1.0/p.touches" {
		t.Fatalf("linear = %q, want an even split", expression)
	}
}

// positionWeight mirrors the SQL position rule so the split can be asserted in Go.
func positionWeight(position, touches int) float64 {
	switch {
	case touches == 1:
		return 1
	case touches == 2:
		return 0.5
	case position == 1 || position == touches:
		return 0.4
	default:
		return 0.2 / float64(touches-2)
	}
}

func TestPositionWeightsSumToOnePerConversion(t *testing.T) {
	t.Parallel()

	for touches := 1; touches <= 8; touches++ {
		total := 0.0
		for position := 1; position <= touches; position++ {
			total += positionWeight(position, touches)
		}
		if total < 0.9999 || total > 1.0001 {
			t.Fatalf("weights for %d touches sum to %.4f, want 1", touches, total)
		}
	}
}

func TestPathSpansTheAllowedSitesOnly(t *testing.T) {
	t.Parallel()

	// The touch CTE must be parameterised by the allowed site list rather than
	// pinned to the conversion site, otherwise workspace scope silently does nothing.
	if !strings.Contains(attributionPathCTE, "s.site_id = ANY($6)") {
		t.Fatalf("touch CTE does not scope by the allowed sites:\n%s", attributionPathCTE)
	}
	// Conversions always come from the site being asked about.
	if !strings.Contains(attributionPathCTE, "WHERE site_id=$1 AND environment=$2") {
		t.Fatal("conversions must stay scoped to the requested site")
	}
	// The originating service is carried through so credit can be split by service.
	if !strings.Contains(attributionPathCTE, "t.site_id touch_site") {
		t.Fatal("the touch site must be carried into the path")
	}
	// Cross-site matching relies on the SSO identity, never on a raw visitor id.
	if !strings.Contains(attributionPathCTE, "'u:'||coalesce(i.user_id,s.user_id)") {
		t.Fatal("the touch entity must use the canonical user identity")
	}
}
