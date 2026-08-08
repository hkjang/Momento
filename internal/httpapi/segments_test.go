package httpapi

import (
	"strings"
	"testing"
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
