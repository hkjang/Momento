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
// The service names itself in two shapes and they are not interchangeable: an
// image is a repository and a tag, an archive is one word. An operator meets
// both — the file they carry into a closed network and the name they run — and
// the offline guide prints them next to each other, so a build path that named
// either differently would be handing out instructions that do not work.
func TestTheImageAndTheArchiveAreNamedTheAgreedWay(t *testing.T) {
	root := repoRoot(t)
	for _, expect := range []struct {
		file, want, why string
	}{
		{"Makefile", "IMAGE := momento:v$(RELEASE_VERSION)", "the image is repository:tag"},
		{"Makefile", "ARCHIVE_NAME := momento-v$(RELEASE_VERSION)", "the archive is one word"},
		{".github/workflows/release.yml", `-t "momento:${RELEASE_TAG}"`, "the released image is repository:tag"},
		{".github/workflows/release.yml", `"momento-${RELEASE_TAG}.tar.gz"`, "the published archive is one word"},
		{"docs/OFFLINE.md", "momento:v<version>", "the guide tells an operator to run the image by its tag"},
		{"compose.yml", "image: momento:v" + Version, "the compose image carries the version it is built from"},
	} {
		body, err := os.ReadFile(filepath.Join(root, expect.file))
		if err != nil {
			t.Fatalf("read %s: %v", expect.file, err)
		}
		if !strings.Contains(string(body), expect.want) {
			t.Errorf("%s no longer contains %q, so %s is no longer true", expect.file, expect.want, expect.why)
		}
	}
}

// TestTheShippedDatabaseHasEnoughSharedMemory pins one line of the compose file
// that is invisible until it is too late.
//
// Docker gives a container 64MB of /dev/shm, and PostgreSQL puts a parallel
// query's shared hash tables and tuple queues there. A report that groups
// millions of rows asks for more, and the query does not slow down — it fails
// with "could not resize shared memory segment", which reaches the reader as a
// 500 on a screen that worked last month. It appears only once a site is large
// enough for the planner to go parallel, which is when nobody is touching the
// deployment. It was hit here on a two million event database.
//
// The guide tells an operator running their own PostgreSQL container the same
// thing, so both are checked: a compose file that quietly loses this line ships
// a deployment that breaks on growth.
func TestTheShippedDatabaseHasEnoughSharedMemory(t *testing.T) {
	root := repoRoot(t)
	compose, err := os.ReadFile(filepath.Join(root, "compose.yml"))
	if err != nil {
		t.Fatalf("read compose.yml: %v", err)
	}
	if !strings.Contains(string(compose), "shm_size:") {
		t.Error("compose.yml no longer sets shm_size on the database, so a parallel query fails once the site is large enough to need one")
	}
	guide, err := os.ReadFile(filepath.Join(root, "docs/OFFLINE.md"))
	if err != nil {
		t.Fatalf("read docs/OFFLINE.md: %v", err)
	}
	if !strings.Contains(string(guide), "--shm-size") {
		t.Error("the offline guide no longer tells an operator running their own PostgreSQL container to raise --shm-size")
	}
}

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
