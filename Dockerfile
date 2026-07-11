# syntax=docker/dockerfile:1.7
#
# Targets:
#   standalone (default) — iag-quality-control repo root on Railway
#   monorepo             — IAG_multi_backend root context (deploy/docker-compose)
#
# Monorepo:  docker build -f services/operations/quality-control/Dockerfile --target monorepo .
# Standalone: docker build -f Dockerfile --target standalone .

FROM golang:1.25-alpine AS base
RUN apk add --no-cache git ca-certificates
ENV PLATFORM_GO_DEP=/deps/platform-go

FROM base AS platform-go-copy
COPY shared/platform-go ${PLATFORM_GO_DEP}

FROM base AS build-standalone
# Standalone (iag-quality-control repo root): the meta-repo is private, so
# Railway can't clone it at build time. Instead the standalone repo carries a
# committed snapshot at third_party/platform-go (refreshed via
# scripts/sync-platform-go.sh). Copy that into /deps/platform-go and point the
# replace directive at it.
WORKDIR /src
COPY third_party/platform-go ${PLATFORM_GO_DEP}
COPY go.mod go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY . .
# `COPY . .` restored go.mod from the build context, which still carries the
# meta-repo-only `replace => ../../../shared/platform-go`. That path does not
# exist inside the build container, so re-apply the vendored replace first.
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /qc ./cmd/server

FROM base AS build-monorepo
COPY --from=platform-go-copy ${PLATFORM_GO_DEP} ${PLATFORM_GO_DEP}
WORKDIR /src/services/operations/quality-control
COPY services/operations/quality-control/go.mod services/operations/quality-control/go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY services/operations/quality-control/ .
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /qc ./cmd/server

FROM alpine:3.21 AS monorepo
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build-monorepo /qc /app/qc
ENV PORT=4004
EXPOSE 4004
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:4004/health || exit 1
USER nobody
ENTRYPOINT ["/app/qc"]

FROM alpine:3.21 AS standalone
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build-standalone /qc /app/qc
ENV PORT=4004
EXPOSE 4004
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:4004/health || exit 1
USER nobody
ENTRYPOINT ["/app/qc"]
