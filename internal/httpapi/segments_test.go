package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestCompileNestedSegment(t *testing.T) {
	resolver := dimensionResolver{custom: map[string]customDimension{
		"custom.membership": {Name: "membership", PropertyKey: "membership", Scope: "user", DataType: "string"},
		"custom.price":      {Name: "price", PropertyKey: "price", Scope: "event", DataType: "number"},
	}}
	node := segmentNode{Combinator: "and", Rules: []segmentNode{
		{Field: "device.type", Operator: "=", Value: "mobile"},
		{Combinator: "or", Rules: []segmentNode{
			{Field: "custom.membership", Operator: "=", Value: "premium"},
			{Field: "custom.price", Operator: ">=", Value: 50000.0},
		}},
	}}
	args := []any{"site", "from", "to"}
	sql, err := compileSegment(node, resolver, "e", &args, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"e.device_type = $4", "e.canonical_user_properties->>'membership' = $5", "e.properties->>'price'", ">= $6", " AND ", " OR "} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("compiled SQL %q does not contain %q", sql, expected)
		}
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 arguments, got %d", len(args))
	}
}

func TestCompileSegmentRejectsInvalidInput(t *testing.T) {
	resolver := dimensionResolver{custom: map[string]customDimension{}}
	tests := []segmentNode{
		{Combinator: "xor", Rules: []segmentNode{{Field: "event.name", Operator: "=", Value: "click"}}},
		{Field: "properties->>'unsafe'", Operator: "=", Value: "x"},
		{Field: "event.name", Operator: "in", Value: []any{}},
		{Field: "event.name", Operator: "matches", Value: ".*"},
	}
	for _, node := range tests {
		args := []any{}
		if _, err := compileSegment(node, resolver, "e", &args, 0); err == nil {
			t.Fatalf("expected segment to be rejected: %#v", node)
		}
	}
}

func TestCompileEventExistence(t *testing.T) {
	args := []any{"site", "from", "to"}
	sql, err := compileSegment(segmentNode{Field: "event.has", Operator: "=", Value: "purchase"}, dimensionResolver{custom: map[string]customDimension{}}, "e", &args, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "EXISTS(SELECT 1 FROM analytics_events segment_event") || !strings.Contains(sql, "segment_event.entity_id=e.entity_id") || !strings.Contains(sql, "segment_event.event_name=$4") {
		t.Fatalf("unexpected existence SQL: %s", sql)
	}
	if args[3] != "purchase" {
		t.Fatalf("unexpected event argument: %#v", args[3])
	}
}

func TestCompileBehaviouralSegment(t *testing.T) {
	t.Parallel()

	resolver := dimensionResolver{custom: map[string]customDimension{}}
	// "Visited at least three times, never converted, dormant for 30 days" is the
	// audience an insight points at, so it has to compile to one segment.
	node := segmentNode{Combinator: "and", Rules: []segmentNode{
		{Field: "entity.sessions", Operator: ">=", Value: 3.0},
		{Field: "entity.conversions", Operator: "=", Value: 0.0},
		{Field: "entity.days_since_last_seen", Operator: ">=", Value: 30.0},
	}}
	args := []any{"site"}
	sql, err := compileSegment(node, resolver, "e", &args, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, expected := range []string{
		"count(DISTINCT segment_entity.session_id)",
		"count(*) FILTER(WHERE segment_entity.is_conversion)",
		"segment_entity.entity_id=e.entity_id",
		"segment_entity.environment=e.environment",
		">= $2",
		"= $3",
		">= $4",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("compiled SQL %q does not contain %q", sql, expected)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args = %d, want 4", len(args))
	}
}

func TestBehaviouralSegmentRejectsBadInput(t *testing.T) {
	t.Parallel()

	resolver := dimensionResolver{custom: map[string]customDimension{}}
	for name, node := range map[string]segmentNode{
		"text operator": {Field: "entity.sessions", Operator: "contains", Value: "3"},
		"text value":    {Field: "entity.sessions", Operator: ">=", Value: "many"},
	} {
		t.Run(name, func(t *testing.T) {
			args := []any{"site"}
			if _, err := compileSegment(node, resolver, "e", &args, 0); err == nil {
				t.Fatal("invalid behavioural rule was accepted")
			}
		})
	}
}

func TestBehaviouralSegmentCountsMissingHistoryAsZero(t *testing.T) {
	t.Parallel()

	resolver := dimensionResolver{custom: map[string]customDimension{}}
	args := []any{"site"}
	sql, err := compileSegment(segmentNode{Field: "entity.conversions", Operator: "=", Value: 0.0}, resolver, "e", &args, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.HasPrefix(sql, "coalesce(") {
		t.Fatalf("compiled SQL %q must treat a missing aggregate as zero", sql)
	}
}

func TestGoalForecastProjectsACumulativeGoal(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	// Half way through August with 400 of a 1000 target: the pace lands at 800.
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	got := goalForecast(now, start, "month", "number", 400, 1000, "gte")

	if got["forecast_available"] != true {
		t.Fatalf("forecast_available = %v, want true", got["forecast_available"])
	}
	projected, _ := got["projected_value"].(float64)
	if projected < 780 || projected > 820 {
		t.Fatalf("projected = %.1f, want about 800", projected)
	}
	if got["projected_achieved"] != false || got["forecast_status"] != "behind" {
		t.Fatalf("status = %v achieved = %v, want behind", got["forecast_status"], got["projected_achieved"])
	}
	remaining, _ := got["remaining_to_target"].(float64)
	if remaining != 600 {
		t.Fatalf("remaining = %.0f, want 600", remaining)
	}
	pace, _ := got["required_daily_pace"].(float64)
	if pace < 38 || pace > 40 {
		t.Fatalf("required daily pace = %.2f, want about 39", pace)
	}
}

func TestGoalForecastKeepsRateMetricsAsMeasured(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	got := goalForecast(now, start, "month", "percent", 12, 10, "gte")

	projected, _ := got["projected_value"].(float64)
	if projected != 12 {
		t.Fatalf("projected = %.1f, want the measured 12 for a rate metric", projected)
	}
	if got["projected_achieved"] != true || got["forecast_status"] != "on_track" {
		t.Fatalf("status = %v, want on_track", got["forecast_status"])
	}
	if _, ok := got["required_daily_pace"]; ok {
		t.Fatal("a rate metric has no daily accumulation pace")
	}
}

func TestGoalForecastWaitsForEnoughOfThePeriod(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	got := goalForecast(now, start, "month", "number", 5, 1000, "gte")

	if got["forecast_available"] != false {
		t.Fatalf("forecast_available = %v, want false so early in the period", got["forecast_available"])
	}
	if got["forecast_reason"] == nil {
		t.Fatal("an unavailable forecast must explain itself")
	}
}

func TestGoalPeriodEndCoversEveryPeriod(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	for period, want := range map[string]time.Time{
		"day":     start.AddDate(0, 0, 1),
		"week":    start.AddDate(0, 0, 7),
		"month":   start.AddDate(0, 1, 0),
		"quarter": start.AddDate(0, 3, 0),
	} {
		if got := goalPeriodEnd(start, period); !got.Equal(want) {
			t.Fatalf("goalPeriodEnd(%q) = %v, want %v", period, got, want)
		}
	}
}
