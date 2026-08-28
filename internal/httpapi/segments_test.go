package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/segment"
)

func TestCompileNestedSegment(t *testing.T) {
	resolver := segment.ResolverFor(uuid.Nil, "", map[string]segment.CustomDimension{
		"custom.membership": {Name: "membership", PropertyKey: "membership", Scope: "user", DataType: "string"},
		"custom.price":      {Name: "price", PropertyKey: "price", Scope: "event", DataType: "number"},
	})
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
	resolver := scopedResolver()
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
	sql, err := compileSegment(segmentNode{Field: "event.has", Operator: "=", Value: "purchase"}, segment.ResolverFor(uuid.Nil, "", nil), "e", &args, 0)
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

	resolver := scopedResolver()
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
		// A semi-join against one grouped subquery, not a per-row aggregate.
		"e.entity_id IN (SELECT segment_entity.entity_id",
		"GROUP BY segment_entity.entity_id HAVING",
		">= $4",
		"= $7",
		">= $10",
	} {
		if strings.Contains(sql, "segment_entity.entity_id=e.entity_id") {
			t.Fatalf("compiled SQL %q correlates the aggregate to the outer row", sql)
		}
		if !strings.Contains(sql, expected) {
			t.Fatalf("compiled SQL %q does not contain %q", sql, expected)
		}
	}
	// Each rule binds its own site, environment and value.
	if len(args) != 10 {
		t.Fatalf("args = %d, want 4", len(args))
	}
}

func TestBehaviouralSegmentRejectsBadInput(t *testing.T) {
	t.Parallel()

	resolver := scopedResolver()
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

// scopedResolver is what a request builds. Behavioural aggregates are scoped by
// site and environment, so a resolver without them cannot compile one.
func scopedResolver() dimensionResolver {
	return segment.ResolverFor(uuid.MustParse("11111111-1111-1111-1111-111111111111"), "prd", nil)
}

func TestBehaviouralSegmentCountsMissingHistoryAsZero(t *testing.T) {
	t.Parallel()

	resolver := scopedResolver()
	args := []any{"site"}
	sql, err := compileSegment(segmentNode{Field: "entity.conversions", Operator: "=", Value: 0.0}, resolver, "e", &args, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// A person with no matching events still forms a group, and the filtered
	// aggregate is zero for them, so "never converted" keeps selecting them.
	if !strings.Contains(sql, "HAVING coalesce(") {
		t.Fatalf("compiled SQL %q must treat a missing aggregate as zero", sql)
	}
	if !strings.Contains(sql, "e.entity_id IN (SELECT") {
		t.Fatalf("compiled SQL %q must be a semi-join, not a per-row aggregate", sql)
	}
	if strings.Contains(sql, "segment_entity.entity_id=e.entity_id") {
		t.Fatalf("compiled SQL %q still correlates the aggregate to the outer row", sql)
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

// TestCompileFrictionSegment covers the audiences the automatic signals make
// possible: people the product actually blocked, and people who searched and
// found nothing. Both are expressible only because the aggregate names the
// signals itself rather than taking an event name from the request.
func TestCompileFrictionSegment(t *testing.T) {
	t.Parallel()

	resolver := scopedResolver()
	node := segmentNode{Combinator: "and", Rules: []segmentNode{
		{Field: "entity.frustration_sessions", Operator: ">=", Value: 2.0},
		{Field: "entity.conversions", Operator: "=", Value: 0.0},
		{Field: "entity.zero_result_searches", Operator: ">=", Value: 1.0},
		{Field: "entity.search_clicks", Operator: "=", Value: 0.0},
	}}
	args := []any{"site"}
	sql, err := compileSegment(node, resolver, "e", &args, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, expected := range []string{
		"count(DISTINCT segment_entity.session_id) FILTER(WHERE segment_entity.event_name = ANY(",
		"rage_click",
		"error_after_click",
		"segment_entity.event_name='search_no_result'",
		"segment_entity.properties->>'result_count'='0'",
		"count(*) FILTER(WHERE segment_entity.event_name='search_click')",
		"e.entity_id IN (SELECT segment_entity.entity_id",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("compiled SQL %q does not contain %q", sql, expected)
		}
	}
	// The signal list is fixed, so no rule value may reach the SQL text. Each of
	// the four rules binds a site, an environment and its value.
	if strings.Contains(sql, "$14") || len(args) != 13 {
		t.Fatalf("expected four rules bound as twelve values, got %d: %v", len(args)-1, args)
	}

	// A friction field still refuses a non-numeric comparison, the same as every
	// other aggregate.
	for _, bad := range []segmentNode{
		{Field: "entity.frustration_signals", Operator: "contains", Value: "rage"},
		{Field: "entity.searches", Operator: ">=", Value: "많이"},
	} {
		badArgs := []any{"site"}
		if _, err := compileSegment(segmentNode{Combinator: "and", Rules: []segmentNode{bad}}, resolver, "e", &badArgs, 0); err == nil {
			t.Errorf("%s %s %v compiled but should have been refused", bad.Field, bad.Operator, bad.Value)
		}
	}
}
