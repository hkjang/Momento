# syntax=docker/dockerfile:1.7
FROM node:24-bookworm-slim AS sdk-build
WORKDIR /src/sdk
COPY sdk/package.json sdk/package-lock.json ./
RUN npm ci --ignore-scripts=false
COPY sdk/ ./
RUN npm run build

FROM node:24-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts=false
COPY web/ ./
RUN npm run build
COPY --from=sdk-build /src/sdk/dist/tracker.js /src/web/dist/tracker.js

FROM golang:1.26.7-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# An empty VERSION leaves the version declared in internal/version alone, so an
# image built straight from the source tree still reports the version it was built
# from. It used to default to "dev", and the profile menu showed that to the
# operator as the service version.
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w ${VERSION:+-X github.com/hkjang/Momento/internal/version.Version=${VERSION}} -X github.com/hkjang/Momento/internal/version.Commit=${COMMIT} -X github.com/hkjang/Momento/internal/version.BuildTime=${BUILD_TIME}" -o /out/momento ./cmd/momento

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* && groupadd --system momento && useradd --system --gid momento --home-dir /app momento
WORKDIR /app
COPY --from=go-build /out/momento /app/momento
COPY --from=web-build /src/web/dist /app/web
USER momento
EXPOSE 8080
ENTRYPOINT ["/app/momento"]
