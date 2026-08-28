package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The tracker refuses an identifier that looks like a person, and the collector
// masks one that reaches it anyway. They are two halves of one rule written in
// two languages, and the tracker's own comment says "the two halves now refuse
// the same things" — a claim nothing checked, in either language.
//
// It matters in both directions. A shape the collector detects and the tracker
// does not is a phone number that reaches the server and is blanked there, so
// the person becomes anonymous and their events stop belonging to anybody. A
// shape the tracker refuses and the collector does not is the one that gets
// stored, indexed, joined into the identity graph and put in every export — the
// v0.34.0 defect, which arrived exactly because the two halves were written
// apart.
//
// Both sides read the same file, so an example added for one is an example the
// other has to pass.
func TestTheCollectorDetectsWhatTheTrackerRefuses(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pii_identifiers.json"))
	if err != nil {
		t.Fatalf("read the shared examples: %v", err)
	}
	var examples struct {
		Refused []struct{ Value, Why string } `json:"refused"`
		Allowed []struct{ Value, Why string } `json:"allowed"`
	}
	if err := json.Unmarshal(raw, &examples); err != nil {
		t.Fatalf("parse the shared examples: %v", err)
	}
	if len(examples.Refused) == 0 || len(examples.Allowed) == 0 {
		t.Fatal("the shared examples have nothing on one side, so this proves nothing")
	}

	for _, example := range examples.Refused {
		if found := maskUserID(example.Value); len(found) == 0 {
			t.Errorf("the collector does not detect %q (%s): the tracker refuses it, so this is the shape that reaches the server from an integration that does not use the tracker, and it is stored, indexed and exported",
				example.Value, example.Why)
		}
	}
	for _, example := range examples.Allowed {
		if found := maskUserID(example.Value); len(found) > 0 {
			t.Errorf("the collector refuses %q (%s) as %v: identify() exists to carry values like this, and blanking one makes that person anonymous",
				example.Value, example.Why, found)
		}
	}
}
