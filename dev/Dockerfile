# syntax=docker/dockerfile:1.7

FROM oven/bun:1.3.13-alpine@sha256:4de475389889577f346c636f956b42a5c31501b654664e9ae5726f94d7bb5349 AS bun

FROM golang:1.24.3-alpine3.21@sha256:ef18ee7117463ac1055f5a370ed18b8750f01589f13ea0b48642f5792b234044 AS tools
RUN apk add --no-cache bash curl git libgcc libstdc++ make openssh-client postgresql-client
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

FROM tools AS dev-go
RUN go install github.com/air-verse/air@v1.61.7
COPY . .
COPY dev/entrypoint.sh /usr/local/bin/dev-entrypoint
RUN chmod +x /usr/local/bin/dev-entrypoint
ENTRYPOINT ["/usr/local/bin/dev-entrypoint"]
CMD ["air", "-c", "dev/air.toml"]

FROM bun AS dev-web
USER root
RUN apk add --no-cache bash
WORKDIR /workspace
COPY dev/entrypoint.sh /usr/local/bin/dev-entrypoint
RUN chmod +x /usr/local/bin/dev-entrypoint
ENTRYPOINT ["/usr/local/bin/dev-entrypoint"]
CMD ["bun", "run", "--cwd", "web", "dev", "--", "--port", "3001"]

FROM tools AS build
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adenosine ./cmd/adenosine

FROM bun AS web-build
WORKDIR /workspace
COPY package.json bun.lock ./
COPY packages/api-client/package.json packages/api-client/package.json
COPY web/package.json web/package.json
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY packages packages
COPY web web
RUN bun run --cwd web build

FROM tools AS federation-test
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /usr/local/bin/realtime-boundary ./test/realtimeboundary \
    && go build -o /usr/local/bin/realtime-gateway ./test/realtimegateway \
    && go build -o /usr/local/bin/realtime-observer ./test/realtimeobserver \
    && go build -o /usr/local/bin/realtime-producer ./test/realtimeproducer

FROM oven/bun:1.3.13-alpine@sha256:4de475389889577f346c636f956b42a5c31501b654664e9ae5726f94d7bb5349 AS production
RUN apk add --no-cache ca-certificates git openssh-keygen postgresql-client tzdata \
    && addgroup -S adenosine \
    && adduser -S -G adenosine adenosine \
    && mkdir -p /var/lib/adenosine/repos /var/lib/adenosine/state \
    && chown -R adenosine:adenosine /var/lib/adenosine
COPY --from=build /out/adenosine /usr/local/bin/adenosine
COPY --from=web-build /workspace/web/.output /opt/adenosine/web
COPY migrations /opt/adenosine/migrations
USER adenosine
EXPOSE 8080 2222 3000
ENTRYPOINT ["adenosine"]
CMD ["serve"]
