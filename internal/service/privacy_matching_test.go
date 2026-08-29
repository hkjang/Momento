package service

import (
	"strings"
	"testing"

	"github.com/hkjang/Momento/internal/model"
	privacypolicy "github.com/hkjang/Momento/internal/privacy"
)

// Two lists live on one settings screen — the URL parameters to mask and the
// event properties to block — and they matched differently. A rule of "token"
// masked ?token= and let ?Token= through, while the same rule blocked a property
// named Token. Nothing on the screen said which list was which.
//
// The parameter an enterprise application sends is as often PascalCase as lower:
// ?SessionId=, ?Email=, ?Token=. An operator who types the name they see in
// their own URLs gets a rule that works or one that does nothing, and the
// difference is invisible until somebody reads the stored rows.
func TestAMaskedParameterMatchesWhateverCaseTheURLUses(t *testing.T) {
	t.Parallel()
	policy := privacypolicy.Default()
	policy.MaskedParameters = []string{"token", " Email "}
	for _, raw := range []string{
		"https://portal.internal/app?token=SECRET",
		"https://portal.internal/app?Token=SECRET",
		"https://portal.internal/app?TOKEN=SECRET",
		"https://portal.internal/app?email=someone@example.com",
		"https://portal.internal/app?EMAIL=someone@example.com",
	} {
		got := sanitizeURL(raw, policy)
		if !strings.Contains(got, "MASKED") {
			t.Errorf("%s was stored as %s: the rule names this parameter and the value went through unmasked", raw, got)
		}
		if strings.Contains(got, "SECRET") || strings.Contains(got, "someone@example.com") {
			t.Errorf("%s was stored as %s: the value the rule exists to remove is still in it", raw, got)
		}
	}
	// A parameter nobody named is still carried, because that is what
	// strip_query_string is for and this list is not it.
	if got := sanitizeURL("https://portal.internal/app?tab=orders", policy); !strings.Contains(got, "tab=orders") {
		t.Errorf("a parameter no rule names was removed anyway: %s", got)
	}
}

// The counter on the data quality screen and the filter that removes the
// property are one decision made twice. They disagreed about whitespace: the
// counter trimmed each configured name and the filter did not, so a rule stored
// as " email " — which the API accepts, even though the console trims — was
// reported as blocked and stored anyway.
func TestTheBlockedCounterAgreesWithWhatWasActuallyRemoved(t *testing.T) {
	t.Parallel()
	blocked := []string{" email ", "PHONE"}
	request := model.CollectRequest{
		UserProperties: map[string]any{"email": "someone@example.com", "department": "sales"},
		Events: []model.IncomingEvent{{
			Name:       "page_view",
			Properties: map[string]any{"Phone": "010-0000-0000", "feature": "search"},
		}},
	}
	counted := countPrivacyBlocked(request, blocked)

	removed := 0
	before := len(request.UserProperties)
	after := filteredProperties(request.UserProperties, blocked)
	removed += before - len(after)
	if _, present := after["email"]; present {
		t.Error("a rule stored with spaces around it did not block the property, and the counter said it had")
	}
	before = len(request.Events[0].Properties)
	afterEvent := filteredProperties(request.Events[0].Properties, blocked)
	removed += before - len(afterEvent)
	if _, present := afterEvent["Phone"]; present {
		t.Error("a rule in a different case than the property did not block it")
	}
	if counted != removed {
		t.Fatalf("the data quality screen reports %d blocked properties and %d were actually removed: an operator reading that number is told a rule is working when the value is in the row", counted, removed)
	}
	if _, present := after["department"]; !present {
		t.Error("a property no rule names was removed")
	}
}
