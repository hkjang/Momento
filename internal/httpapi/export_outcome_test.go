package httpapi

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// An export streams. The header goes out with the first row, so by the time
// anything can go wrong the response is already a 200 and already a download —
// there is no status left to change.
//
// So it ended short and looked whole: a CSV that opens cleanly, with no row
// missing that anybody could point at, standing in for a period that was never
// fully read. The iteration's error was discarded and the row limit was silent,
// which are two different ways to hand somebody a file they will treat as the
// answer.
func TestAnExportSaysWhyItStopped(t *testing.T) {
	t.Parallel()

	// Ran out of events: nothing to say, and a note on a complete file would
	// teach the reader to ignore notes.
	if note := exportOutcome(nil, 42); note != "" {
		t.Errorf("a complete export ends with %q", note)
	}

	// The read failed partway. The count matters as much as the reason: it is how
	// the reader knows which part of the period they are holding.
	failed := exportOutcome(errors.New("connection reset by peer"), 1234)
	if !strings.Contains(failed, "1234") {
		t.Errorf("a failed export does not say how many events it managed: %q", failed)
	}
	if !strings.Contains(failed, "connection reset by peer") {
		t.Errorf("a failed export does not say what went wrong: %q", failed)
	}

	// The limit is not a failure, and saying nothing about it is the same
	// problem: exactly the limit is indistinguishable from a complete export.
	capped := exportOutcome(nil, exportRowLimit)
	if capped == "" {
		t.Fatal("an export cut at the row limit says nothing, so a reader holding exactly the limit cannot tell it from the whole period")
	}
	if !strings.Contains(capped, strconv.Itoa(exportRowLimit)) {
		t.Errorf("the note does not name the limit it hit: %q", capped)
	}
	if !strings.Contains(capped, "Narrow") {
		t.Errorf("the note does not say what to do about it: %q", capped)
	}

	// And the note is a row a reader will see rather than a comment they will not.
	row := exportNote(capped)
	if len(row) < 2 || row[0] == "" {
		t.Errorf("the CSV note is not a visible row: %v", row)
	}
}
