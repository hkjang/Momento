.PHONY: test build dev docker release-image

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
RELEASE_VERSION := $(patsubst v%,%,$(VERSION))
RELEASE_NAME := momento-v$(RELEASE_VERSION)

test:
	go test ./cmd/... ./internal/...
	go vet ./cmd/... ./internal/...
	cd sdk && npm run typecheck && npm test
	cd web && npm run lint && npm run build

build:
	cd sdk && npm run build
	mkdir -p web/public
	cp sdk/dist/tracker.js web/public/tracker.js
	cd web && npm run build
	go build -trimpath -ldflags="-X github.com/hkjang/Momento/internal/version.Version=$(VERSION) -X github.com/hkjang/Momento/internal/version.Commit=$(COMMIT) -X github.com/hkjang/Momento/internal/version.BuildTime=$(BUILD_TIME)" -o bin/momento ./cmd/momento

dev:
	docker compose up --build

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t momento:$(VERSION) .

release-image:
	docker build --build-arg VERSION=$(RELEASE_VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(RELEASE_NAME) .
	docker save $(RELEASE_NAME) | gzip -9 > $(RELEASE_NAME).tar.gz
	sha256sum $(RELEASE_NAME).tar.gz > $(RELEASE_NAME).tar.gz.sha256
