package privacy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// An absent field is the shipped answer, not the zero value of its Go type. For
// every protective setting here those are opposites, so a policy written before
// a field existed — or a write that named only some fields — used to turn the
// rest off without anybody choosing it.
func TestAFieldTheStoredPolicyDoesNotNameKeepsTheShippedAnswer(t *testing.T) {
	t.Parallel()
	policy, err := Parse([]byte(`{"ip_anonymization":false}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if policy.IPAnonymization {
		t.Error("an explicit false was not honoured, so an administrator cannot turn anything off")
	}
	shipped := Default()
	if policy.PIIDetectionMode != shipped.PIIDetectionMode {
		t.Errorf("pii_detection_mode fell to %q for a policy that does not name it; the shipped answer is %q, and %q means no PII detection at all",
			policy.PIIDetectionMode, shipped.PIIDetectionMode, policy.PIIDetectionMode)
	}
	if !reflect.DeepEqual(policy.MaskedParameters, shipped.MaskedParameters) {
		t.Errorf("masked_parameters fell to %v, so tokens and passwords in query strings would be stored whole", policy.MaskedParameters)
	}
	if !reflect.DeepEqual(policy.BlockedProperties, shipped.BlockedProperties) {
		t.Errorf("blocked_properties fell to %v, so properties the policy names would be stored", policy.BlockedProperties)
	}
	if !policy.DoNotTrack {
		t.Error("do_not_track fell to false, which is the difference between honouring the header and ignoring it")
	}
	if !policy.VisitorProfiles {
		t.Error("visitor_profiles fell to false, which reports the feature as switched off by an administrator")
	}
	if !policy.CollectUserID || !policy.CollectUserAgent {
		t.Error("a collection flag fell to false, which stops collecting something nobody asked to stop collecting")
	}
}

// An empty policy and a policy that will not parse are different answers, and
// only one of them is an answer.
func TestAPolicyThatWillNotParseIsAFailureNotAnEmptyPolicy(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"ip_anonymization":`)); err == nil {
		t.Fatal("a malformed policy parsed cleanly, so the collector would store events with every protection off")
	}
	policy, err := Parse(nil)
	if err != nil {
		t.Fatalf("an absent policy is the shipped policy, not an error: %v", err)
	}
	if !reflect.DeepEqual(policy, Default()) {
		t.Error("an absent policy did not come back as the shipped policy")
	}
}

// The defaults in this package and the row the migrations seed are the same
// statement written twice. If they drift, a fresh install and an upgraded one
// behave differently, and the difference is invisible until somebody compares
// two deployments.
func TestTheDefaultsMatchWhatTheMigrationsSeed(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	seeded := map[string]any{}
	insert := regexp.MustCompile(`\('privacy',\s*'(\{[^']*\})'`)
	amend := regexp.MustCompile(`value \|\| '(\{[^']*\})'::jsonb WHERE key='privacy'`)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		source, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}
		for _, pattern := range []*regexp.Regexp{insert, amend} {
			for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
				var fields map[string]any
				if err := json.Unmarshal([]byte(match[1]), &fields); err != nil {
					t.Fatalf("%s seeds a privacy policy this test cannot read: %v", entry.Name(), err)
				}
				for name, value := range fields {
					seeded[name] = value
				}
			}
		}
	}
	if len(seeded) == 0 {
		t.Fatal("no seeded privacy policy was found, so this proves nothing")
	}
	shipped, err := json.Marshal(Default())
	if err != nil {
		t.Fatalf("marshal defaults: %v", err)
	}
	var defaults map[string]any
	if err := json.Unmarshal(shipped, &defaults); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}
	for name, value := range seeded {
		got, ok := defaults[name]
		if !ok {
			t.Errorf("the migrations seed %s but Default() does not carry it, so a policy that omits it falls to a zero value nobody chose", name)
			continue
		}
		if !reflect.DeepEqual(fmt.Sprint(got), fmt.Sprint(value)) {
			t.Errorf("%s is %v in the migrations and %v in Default(): a fresh install and an upgraded one would disagree", name, value, got)
		}
	}
}
