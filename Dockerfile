# syntax=docker/dockerfile:1.7
# Monorepo: docker build -f services/operations/quality-control/Dockerfile --target monorepo .

FROM golang:1.25-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src/services/operations/quality-control
COPY services/operations/quality-control/go.mod services/operations/quality-control/go.sum ./
RUN go mod download
COPY services/operations/quality-control/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /qc ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build /qc /app/qc
ENV PORT=4004
EXPOSE 4004
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:4004/health || exit 1
USER nobody
ENTRYPOINT ["/app/qc"]
