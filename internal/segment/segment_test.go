package segment

import (
	"testing"

	"github.com/google/uuid"
)

// A text operator's value is bound escaped, so the pattern the database sees
// stands for the text and nothing else; equality is bound as typed, because
// "=" never read the value as a pattern.
func TestTextOperatorsBindTheValueAsALiteral(t *testing.T) {
	resolver := ResolverFor(uuid.New(), "prd", nil)
	for _, probe := range []struct {
		operator string
		value    string
		want     string
	}{
		{"contains", "report_daily", `report\_daily`},
		{"not contains", "%EA%B0%80", `\%EA\%B0\%80`},
		{"startsWith", `C:\temp`, `C:\\temp`},
		{"endsWith", "100%_", `100\%\_`},
		{"contains", "/plain/path", "/plain/path"},
		{"=", "report_daily", "report_daily"},
		{"!=", "%EA%B0%80", "%EA%B0%80"},
	} {
		args := []any{}
		if _, err := Compile(Node{Field: "page.url", Operator: probe.operator, Value: probe.value}, resolver, "e", &args, 0); err != nil {
			t.Fatalf("%s %q: %v", probe.operator, probe.value, err)
		}
		if len(args) != 1 || args[0] != probe.want {
			t.Errorf("%s %q bound %#v, want %q", probe.operator, probe.value, args, probe.want)
		}
	}
}

func TestLikeLiteralQuotesOnlyWhatLikeReads(t *testing.T) {
	for value, want := range map[string]string{
		"":             "",
		"plain":        "plain",
		"a_b":          `a\_b`,
		"50%":          `50\%`,
		`a\b`:          `a\\b`,
		`\%_`:          `\\\%\_`,
		"한글/경로?q=1&x=2": "한글/경로?q=1&x=2",
	} {
		if got := LikeLiteral(value); got != want {
			t.Errorf("LikeLiteral(%q) = %q, want %q", value, got, want)
		}
	}
}
