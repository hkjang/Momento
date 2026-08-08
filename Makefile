.PHONY: test build dev docker release-image

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

test:
	go test ./cmd/... ./internal/...
	cd sdk && npm run typecheck
	cd web && npm run build

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

release-image: docker
	docker save momento:$(VERSION) | gzip -9 > momento-image-$(VERSION)-linux-amd64.tar.gz
	sha256sum momento-image-$(VERSION)-linux-amd64.tar.gz > momento-image-$(VERSION)-linux-amd64.tar.gz.sha256
