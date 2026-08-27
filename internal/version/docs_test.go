package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The guides carry a version in their header, and both said v0.21.0 while their
// own text described behaviour introduced in v0.33.1 — twelve releases later,
// on a correction made the same week. A guide is the support channel for an
// on-premise product: an operator running v0.33 who opens a document stamped
// v0.21 either distrusts what it says or assumes the parts they need are
// missing from it, and both are wrong.
//
// The rule is self-consistency rather than freshness, because a guide is not
// rewritten every release and a stamp that had to be bumped every time would be
// bumped without being read. A document that cites a version has to admit to
// being at least that recent.
//
// It applies to the two documents that describe how the running product
// behaves. A roadmap is deliberately excluded: it names versions that do not
// exist yet, so a plan stamped v0.1.0 that describes v2.0.0 is telling the
// truth, and a rule that called that a defect would be wrong rather than
// strict.
func TestAGuideIsAtLeastAsRecentAsWhatItDescribes(t *testing.T) {
	root := repoRoot(t)
	stamp := regexp.MustCompile(`\*\*문서 버전\*\*: v(\d+)\.(\d+)\.(\d+)`)
	cited := regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)`)

	checked := 0
	for _, name := range []string{"ADMIN_GUIDE.md", "USER_GUIDE.md"} {
		guide := filepath.Join(root, "docs", name)
		body, err := os.ReadFile(guide)
		if err != nil {
			t.Fatalf("read %s: %v", guide, err)
		}
		text := string(body)
		header := stamp.FindStringSubmatch(text)
		if header == nil {
			t.Errorf("%s no longer carries a version header, so a reader has no way to tell how old it is", name)
			continue
		}
		checked++
		stamped := versionOrder(t, header[1:])
		newest, newestText := 0, ""
		for _, match := range cited.FindAllStringSubmatch(text, -1) {
			if order := versionOrder(t, match[1:]); order > newest {
				newest, newestText = order, match[0]
			}
		}
		if newest > stamped {
			t.Errorf("%s is stamped %s and describes %s: a reader running a recent release cannot tell whether this document covers it",
				name, "v"+strings.Join(header[1:], "."), newestText)
		}
	}
	if checked != 2 {
		t.Fatalf("checked %d guides, want both of them", checked)
	}
	t.Logf("checked the version header of %d guide(s)", checked)
}

// versionOrder turns a semantic version into one comparable number. The parts
// are bounded by the release history, not by the format, so the multiplier is
// generous.
func versionOrder(t *testing.T, parts []string) int {
	t.Helper()
	order := 0
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("version part %q: %v", part, err)
		}
		order = order*1000 + value
	}
	return order
}
