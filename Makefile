.PHONY: test verify build dev docker release-image

# internal/version holds the one declaration of what this tree is; a build that
# names no version reports that rather than a placeholder.
DECLARED_VERSION := $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"$$/\1/p' internal/version/version.go)
VERSION ?= $(DECLARED_VERSION)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
RELEASE_VERSION := $(patsubst v%,%,$(VERSION))

# The service names itself the same way in both places it is named, and they are
# not the same shape because they are not the same kind of name: an image is a
# repository and a tag, a file is one word. VERSION may be given either as
# 0.34.2 or v0.34.2, so the v is added here rather than assumed to be there.
#   image    momento:v0.34.2
#   archive  momento-v0.34.2.tar.gz
IMAGE := momento:v$(RELEASE_VERSION)
ARCHIVE_NAME := momento-v$(RELEASE_VERSION)

test:
	go test ./cmd/... ./internal/...
	go vet ./cmd/... ./internal/...
	cd sdk && npm run typecheck && npm test
	cd web && npm run lint && npm test && npm run build

# `test` is the fast loop; `verify` is what to run before pushing. It runs the
# same commands as the `test` job in .github/workflows/ci.yml, in the same order,
# because the gates `test` leaves out are exactly the ones that have failed on a
# push: both verify-failed runs this tree has seen were `govulncheck`, which
# `test` does not run at all. internal/version/buildgates_test.go fails if a gate
# is added to ci.yml and not to this target.
#
# Two things this does not promise:
#   - govulncheck and npm audit read the network and a live advisory database, so
#     offline this target fails. That failure is left to surface rather than
#     being skipped around: a gate that passes because it could not run is worse
#     than one that stops you.
#   - green here is not green in CI. The database integration tests skip without
#     MOMENTO_TEST_POSTGRES_DSN, and CI sets it to a postgres:17-alpine service
#     it starts for the job.
#
# Each recipe line runs in its own shell, so `cd sdk` does not leak into the line
# after it.
verify:
	go test -race ./cmd/... ./internal/...
	go vet ./cmd/... ./internal/...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./cmd/... ./internal/...
	cd sdk && npm ci && npm audit && npm run typecheck && npm test && npm run build
	cd web && npm ci && npm audit && npm run lint && npm test && npm run build

build:
	cd sdk && npm run build
	mkdir -p web/public
	cp sdk/dist/tracker.js web/public/tracker.js
	cd web && npm run build
	go build -trimpath -ldflags="-X github.com/hkjang/Momento/internal/version.Version=$(VERSION) -X github.com/hkjang/Momento/internal/version.Commit=$(COMMIT) -X github.com/hkjang/Momento/internal/version.BuildTime=$(BUILD_TIME)" -o bin/momento ./cmd/momento

dev:
	docker compose up --build

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(IMAGE) .

release-image:
	docker build --build-arg VERSION=$(RELEASE_VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(IMAGE) .
	docker save $(IMAGE) | gzip -9 > $(ARCHIVE_NAME).tar.gz
	sha256sum $(ARCHIVE_NAME).tar.gz > $(ARCHIVE_NAME).tar.gz.sha256
