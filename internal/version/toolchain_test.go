package version

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The Go version this product is built with is declared in two places, and only
// one of them is what CI checks. `go.mod` carries a `toolchain` directive, which
// is what actions/setup-go reads and therefore what
// `govulncheck ./cmd/... ./internal/...` measures; the Dockerfile pins its own
// `golang:` base image, which is what the binary shipped to an operator is
// actually compiled by.
//
// A standard-library advisory is cleared by raising the toolchain, so the
// tempting fix is to edit `go.mod` alone — CI turns green, because CI never
// compiles with the Dockerfile's toolchain before the image build step. The
// image then keeps building the released binary with the version the advisory
// named, and the thing the advisory was about is still shipped. That is the
// failure this pins: not "the toolchain is recent", which would need bumping
// every time it is read and so would be bumped without being read, but "the two
// declarations agree", which is only ever wrong because someone updated one of
// them.
func TestTheDockerfileBuildsWithTheToolchainGoModDeclares(t *testing.T) {
	root := repoRoot(t)

	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	declared := regexp.MustCompile(`(?m)^toolchain go(\d+\.\d+\.\d+)$`).FindSubmatch(gomod)
	if declared == nil {
		t.Fatal("go.mod no longer carries a `toolchain goX.Y.Z` directive, so nothing says which Go version CI measures")
	}

	dockerfile, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	pinned := regexp.MustCompile(`(?m)^FROM golang:(\d+\.\d+\.\d+)-`).FindSubmatch(dockerfile)
	if pinned == nil {
		t.Fatal("the Dockerfile no longer pins a `FROM golang:X.Y.Z-` base image, so the shipped binary's Go version is whatever the tag floats to")
	}

	if string(declared[1]) != string(pinned[1]) {
		t.Errorf("go.mod declares toolchain go%s but the Dockerfile builds the shipped binary with golang:%s — a standard-library advisory cleared in one is still shipped by the other", declared[1], pinned[1])
	}
}
