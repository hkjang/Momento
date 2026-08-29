package privacy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Seven places used to ask what the privacy policy is, each with its own inline
// SQL and its own coalesce default, and the defaults did not agree. Two files
// read visitor_profiles with opposite fallbacks, so a policy missing that field
// left Visitor Explorer's timeline working while its profile list and the MCP
// tool both reported the feature switched off by an administrator. Do Not Track
// fell back to false against a shipped setting of true.
//
// Nothing about a coalesce is wrong on its own. What is wrong is that the
// default lives at the call site, where it is written once per caller and read
// by nobody, so the answers drift apart in the direction each author happened to
// pick. Reading the whole value and handing it to Parse is fine — this only
// forbids picking a field out of the row somewhere else.
func TestOnlyThisPackageDecidesWhatAnAbsentPrivacyFieldMeans(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve source root: %v", err)
	}
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package: %v", err)
	}
	// A named field pulled out of the privacy row: value->>'…' or value->'…'
	// anywhere in a statement that also names the row.
	field := regexp.MustCompile(`value\s*->>?\s*'(\w+)'`)
	checked := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(path, self+string(filepath.Separator)) {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for index, line := range strings.Split(string(source), "\n") {
			at := strings.Index(line, `key='privacy'`)
			if at < 0 {
				continue
			}
			checked++
			// One line can carry several statements. Only the select list that
			// reaches this row is this row's.
			fragment := line[:at]
			if start := strings.LastIndex(strings.ToUpper(fragment), "SELECT"); start >= 0 {
				fragment = fragment[start:]
			}
			for _, match := range field.FindAllStringSubmatch(fragment, -1) {
				t.Errorf("%s:%d reads %s out of the privacy row itself: the fallback written here is this file's own opinion, and the last time there were several they disagreed about whether Visitor Explorer was switched off",
					rel, index+1, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatal("no statement naming the privacy settings row was found, so this proves nothing")
	}
	t.Logf("checked %d statements that name the privacy settings row", checked)
}
