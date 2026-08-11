package insight

import "testing"

func TestAttributionOrderIsWhitelisted(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"first_touch", "last_touch", "last_non_direct"} {
		order, ok := AttributionOrder(model)
		if !ok || order == "" {
			t.Fatalf("model %q has no ranking", model)
		}
	}
	// The ranking is interpolated into SQL, so anything outside the map must fail
	// rather than reach the query builder.
	for _, model := range []string{"", "linear", "t.started_at; DROP TABLE sessions"} {
		if _, ok := AttributionOrder(model); ok {
			t.Fatalf("unsupported model %q was accepted", model)
		}
	}
}

func TestLastNonDirectPrefersAKnownChannelThenRecency(t *testing.T) {
	t.Parallel()

	order, _ := AttributionOrder("last_non_direct")
	if order != "(t.source<>'' OR t.medium<>'') DESC,t.started_at DESC,t.session_id" {
		t.Fatalf("ranking = %q, want known channels first and then the most recent visit", order)
	}
}

func TestAttributionModelsAreDescribed(t *testing.T) {
	t.Parallel()

	models := AttributionModels()
	if len(models) != 3 {
		t.Fatalf("models = %d, want 3", len(models))
	}
	if models[0].Key != "last_non_direct" {
		t.Fatalf("first model = %q, want last_non_direct as the default", models[0].Key)
	}
	for _, model := range models {
		if model.Label == "" || model.Description == "" {
			t.Fatalf("model %q is missing a label or description", model.Key)
		}
		if _, ok := AttributionOrder(model.Key); !ok {
			t.Fatalf("model %q is offered but has no ranking", model.Key)
		}
	}
}
