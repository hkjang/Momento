package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The tracking debugger read network_name into a string. That column is null for
// everything outside a named internal network — most events on most sites, and
// every event on a site with no networks configured. The scan failed on all of
// them, the failure was dropped, and the screen an operator opens to watch
// events arrive showed nothing at all: exactly what "nothing is arriving" looks
// like, which is the one conclusion that screen exists to rule out.
//
// That was one loop. Fifty-one others dropped their failures the same way, and
// the shape is always the same: a read that stops partway becomes a short list,
// and a short list is indistinguishable from a quiet site. Nothing about the
// column types makes this safe — a migration that widens a column, a view that
// starts returning null, a reordered SELECT all turn a working scan into a
// failing one, and the loop keeps returning 200.
//
// So the rule is about what a loop does with its failures, not about which
// columns it happens to read today.
func TestEveryRowLoopReportsItsFailures(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve source root: %v", err)
	}
	loopStart := regexp.MustCompile(`for (\w+)\.Next\(\)`)
	// Both ways of throwing the error away: comparing it inline without binding
	// it, and binding it only to take the success path.
	discarded := regexp.MustCompile(`\.Scan\([^;\n]*\)\s*(==|!=)\s*nil`)
	successOnly := regexp.MustCompile(`if err := \w+\.Scan\([^;\n]*\); err == nil \{`)

	checked := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		lines := strings.Split(string(source), "\n")
		for index, line := range lines {
			match := loopStart.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			rows := match[1]
			depth, end := 0, index
			for scan := index; scan < len(lines); scan++ {
				depth += strings.Count(lines[scan], "{") - strings.Count(lines[scan], "}")
				if depth <= 0 && scan > index {
					end = scan
					break
				}
			}
			body := strings.Join(lines[index:end+1], "\n")
			checked++

			if discarded.MatchString(body) || successOnly.MatchString(body) {
				t.Errorf("%s:%d loops over %s and throws the scan error away: a column that stops scanning fails on every row, so the caller is handed an empty list and no reason for it",
					rel, index+1, rows)
			}
			// The failure has to leave the loop. A bare continue is the same
			// silence written differently: the row is gone and nobody is told.
			for _, fragment := range strings.Split(body, "; err != nil {")[1:] {
				block := fragment
				if close := strings.Index(block, "\n\t\t}"); close > 0 {
					block = block[:close]
				}
				if strings.TrimSpace(strings.ReplaceAll(block, "\n", "")) == "continue" {
					t.Errorf("%s:%d loops over %s and answers a scan failure with continue, which drops the row as quietly as ignoring the error would",
						rel, index+1, rows)
				}
			}
			// A read can also stop between rows — a lost connection, a canceled
			// query, a timeout partway through. Scan never sees that one.
			tail := lines[end:min(end+12, len(lines))]
			if !strings.Contains(strings.Join(tail, "\n"), rows+".Err()") {
				t.Errorf("%s:%d loops over %s and never checks %s.Err(): a read that stops partway ends the loop normally and the short list is returned as the answer",
					rel, index+1, rows, rows)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked < 80 {
		t.Fatalf("only %d row loops were examined, which is too few for this to mean anything", checked)
	}
	t.Logf("checked %d row loops", checked)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
