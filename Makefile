.PHONY: test build dev docker release-image

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
