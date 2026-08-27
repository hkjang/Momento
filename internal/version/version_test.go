package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The version is declared in four places: this package, the console's
// package.json and its lock file, and the SDK's. Nothing compared them, and the
// only thing that ever read the console's copy is the mismatch warning in the
// profile menu — so a release that bumped three of the four would announce a
// version disagreement to every operator who opened that menu, and a release that
// bumped none would report the previous version from a correctly tagged image.
//
// This is the comparison. It fails naming both values, because "versions differ"
// without them costs a search through four files to find which one was missed.
func TestDeclaredVersionsAgree(t *testing.T) {
	root := repoRoot(t)
	for _, file := range []string{
		"web/package.json",
		"web/package-lock.json",
		"sdk/package.json",
		"sdk/package-lock.json",
	} {
		declared := packageVersion(t, filepath.Join(root, file))
		if declared != Version {
			t.Errorf("%s declares %q and internal/version declares %q: the console shows both to an operator and warns that the deployment is inconsistent when they differ",
				file, declared, Version)
		}
	}
}

// TestVersionIsAReleaseVersion pins what an unstamped build reports. It used to
// carry a "-dev" suffix, which meant every build that was not the release
// workflow — `docker build .` as the README gives it, `make build`, the compose
// file — served a console that warned the operator its own version was wrong.
func TestVersionIsAReleaseVersion(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("version is %q, which is not a plain release version: an unstamped build reports this, and the console compares it against the console's own version", Version)
	}
}

// TestEveryBuildStampsTheVersion guards the reason the profile menu showed "dev".
// The version reached the binary only through -ldflags, and only the release
// workflow passed it, so every other way of building the image reported a version
// that was not the service's. Each build path is now required either to pass a
// version through or to leave the declared one alone — never to substitute a
// placeholder for it.
func TestEveryBuildStampsTheVersion(t *testing.T) {
	root := repoRoot(t)
	for file, forbidden := range map[string][]string{
		"Dockerfile":  {"ARG VERSION=dev"},
		"Makefile":    {"VERSION ?= dev"},
		"compose.yml": {"VERSION: dev"},
	} {
		body, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, line := range forbidden {
			if strings.Contains(string(body), line) {
				t.Errorf("%s still contains %q: a build made that way reports \"dev\" as the service version, which is what the profile menu displays", file, line)
			}
		}
	}
}

func packageVersion(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return manifest.Version
}

// repoRoot walks up from the package directory to the tree that holds go.mod, so
// the test does not depend on where it is run from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root from the version package")
	return ""
}
