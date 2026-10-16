package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The gates a commit has to pass are declared twice. `.github/workflows/ci.yml`
// is the one that decides whether a push is green, and the `Makefile` is the one
// a person can run before pushing. When the second falls behind the first, the
// local run is green and the push is red — and not hypothetically: the two
// verify-failed runs this tree has seen (an x/text advisory, then ten
// standard-library ones) both failed at `govulncheck`, a step `make test` does
// not contain at all. Nobody saw them coming because nothing local ran them.
//
// So `make verify` exists to run the same gates, and this is the test that keeps
// it honest. What it pins is deliberately not "the gates are up to date", which
// is a number that gets bumped to make a test pass rather than read: it is "the
// two declarations still agree", in the same shape as
// toolchain_test.go's go.mod/Dockerfile pairing, which can only be wrong because
// someone changed one side and not the other.
//
// The leverage is the count. Adding a step to ci.yml without touching this table
// makes the count disagree, and the failure names the step that has no local
// counterpart. That is what catches drift; the per-entry string checks only
// confirm the entries that are already listed.
//
// What this test does NOT establish, because it reads text rather than running
// anything:
//   - that `make verify` passes, or that make picks up the recipe at all. Only
//     actually running it shows that.
//   - that the sdk gate and the web gate each audit their own directory. Each
//     entry's local string carries its own `cd`, so the two lines cannot collapse
//     into one, but containment in the file is still not evidence that make runs
//     them in the directories those `cd`s name.
//
// ci.yml is read as text on purpose. Pulling in a YAML parser to check a
// workflow file would add a dependency to go.mod for one assertion, and
// toolchain_test.go already reads go.mod and the Dockerfile this way.
func TestBuildGatesInCIHaveALocalCounterpart(t *testing.T) {
	// Each entry pairs one `run:` step in ci.yml's `test` job with the Makefile
	// line that reproduces it locally. ciRun is the step's command as ci.yml
	// spells it; local is the recipe line in `make verify`, which differs by the
	// `cd` that ci.yml expresses as `working-directory:`.
	gates := []struct {
		name   string
		ciRun  string
		local  string
		exempt string // non-empty: why the local line is allowed to differ
	}{
		{
			name:  "go test with the race detector",
			ciRun: "go test -race ./cmd/... ./internal/...",
			local: "go test -race ./cmd/... ./internal/...",
		},
		{
			name:  "go vet",
			ciRun: "go vet ./cmd/... ./internal/...",
			local: "go vet ./cmd/... ./internal/...",
		},
		{
			name:  "govulncheck",
			ciRun: "go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/... ./internal/...",
			local: "go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/... ./internal/...",
		},
		{
			name:  "sdk npm gate",
			ciRun: "npm ci && npm audit && npm run typecheck && npm test && npm run build",
			local: "cd sdk && npm ci && npm audit && npm run typecheck && npm test && npm run build",
		},
		{
			name:  "web npm gate",
			ciRun: "npm ci && npm audit && npm run lint && npm test && npm run build",
			local: "cd web && npm ci && npm audit && npm run lint && npm test && npm run build",
		},
		{
			name:  "production image build",
			ciRun: "docker build --build-arg VERSION=ci --build-arg COMMIT=${GITHUB_SHA::12} -t momento:ci .",
			local: "docker build",
			// Exempt from the exact-command check: `make docker` builds the same
			// image from the same Dockerfile, but deliberately stamps different
			// values into it. CI labels the image `ci` and the commit
			// `${GITHUB_SHA::12}`; the Makefile stamps DECLARED_VERSION and
			// `git rev-parse --short`, because a local image names the tree it was
			// built from. Comparing the two strings would fail on that difference
			// forever, so the entry only asks that the Makefile still builds an
			// image at all.
			exempt: "CI and the Makefile pass different --build-arg values on purpose",
		},
	}

	root := repoRoot(t)

	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read .github/workflows/ci.yml: %v", err)
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	// A `run:` step is either a list item (`- run: ...`) or the command of a
	// named step (`  run: ...` under `- name: ...`). Both forms are counted,
	// because both are gates.
	steps := regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]+)?run:[ \t]`).FindAllIndex(ci, -1)
	if len(steps) != len(gates) {
		t.Errorf("ci.yml declares %d `run:` steps but this test's table covers %d.\n"+
			"A gate was added to or removed from CI without a local counterpart. Add the step to the\n"+
			"table above and to the `verify` target in the Makefile (or mark it exempt and say why),\n"+
			"so that `make verify` keeps running what CI runs.", len(steps), len(gates))
	}

	// Non-exempt gates are matched inside the `verify` recipe rather than
	// anywhere in the Makefile. `make test` is the fast loop and shares some of
	// these commands in weaker forms (`go test` without `-race`, npm without
	// `ci`/`audit`), so a whole-file search would find `go vet` in the wrong
	// target and could not say anything about order.
	recipe := verifyRecipe(t, string(makefile))

	previous := -1
	for _, gate := range gates {
		if !strings.Contains(string(ci), gate.ciRun) {
			t.Errorf("%s: ci.yml no longer runs %q.\n"+
				"CI changed its command; `make verify` is now running something else.", gate.name, gate.ciRun)
			continue
		}

		if gate.exempt != "" {
			if !strings.Contains(string(makefile), gate.local) {
				t.Errorf("%s: the Makefile no longer contains %q (exempt from the exact-command check: %s)",
					gate.name, gate.local, gate.exempt)
			}
			continue
		}

		at := strings.Index(recipe, gate.local)
		if at < 0 {
			t.Errorf("%s: the `verify` target does not run %q.\n"+
				"CI gates this and nothing local does, so this failure would first be seen on a push.",
				gate.name, gate.local)
			continue
		}
		// Order is part of the contract: CI stops at the first red step, so a
		// local run that reaches the npm gates before `go vet` reports a
		// different first failure than the push will.
		if at < previous {
			t.Errorf("%s: `make verify` runs %q out of order — CI runs the gates Go, then sdk, then web.",
				gate.name, gate.local)
		}
		previous = at
	}
}

// verifyRecipe returns the recipe body of the Makefile's `verify` target: the
// tab-indented lines that follow it, up to the next line that starts a new
// target. Blank lines and comments inside the recipe are kept, so an index into
// the result orders the commands as make will run them.
func verifyRecipe(t *testing.T, makefile string) string {
	t.Helper()

	lines := strings.Split(makefile, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "verify:") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatal("the Makefile has no `verify` target, so there is no local way to run the gates CI runs " +
			"(`make test` leaves out -race, govulncheck, npm ci and npm audit)")
	}

	var recipe []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			recipe = append(recipe, line)
			continue
		}
		break
	}
	return strings.Join(recipe, "\n")
}
