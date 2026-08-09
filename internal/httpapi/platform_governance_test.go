package httpapi

import "testing"

func TestValidateSemanticDefinition(t *testing.T) {
	conversion := true
	valid := []semanticDefinition{
		{Type: "count"},
		{Type: "unique_users", EventName: "purchase", Conversion: &conversion},
		{Type: "sum", EventName: "purchase", Property: "value", FallbackProperty: "revenue"},
		{Type: "ratio", Numerator: &semanticDefinition{Type: "unique_users", Conversion: &conversion}, Denominator: &semanticDefinition{Type: "unique_users"}},
	}
	for _, definition := range valid {
		if err := validateSemanticDefinition(definition, 1); err != nil {
			t.Fatalf("valid definition rejected: %#v: %v", definition, err)
		}
	}
	invalid := []semanticDefinition{
		{Type: "sql", Property: "events"},
		{Type: "sum"},
		{Type: "count", EventName: "purchase' OR true--"},
		{Type: "ratio", Numerator: &semanticDefinition{Type: "count"}},
		{Type: "average", Property: "price) FROM users--"},
	}
	for _, definition := range invalid {
		if err := validateSemanticDefinition(definition, 1); err == nil {
			t.Fatalf("invalid definition accepted: %#v", definition)
		}
	}
}
