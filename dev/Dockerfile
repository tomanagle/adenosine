# syntax=docker/dockerfile:1.7

FROM oven/bun:1.3.13-alpine AS bun

FROM golang:1.24.3-alpine3.21 AS tools
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

FROM alpine:3.21 AS production
RUN apk add --no-cache ca-certificates git tzdata \
    && addgroup -S adenosine \
    && adduser -S -G adenosine adenosine \
    && mkdir -p /var/lib/adenosine/repos /var/lib/adenosine/state \
    && chown -R adenosine:adenosine /var/lib/adenosine
COPY --from=build /out/adenosine /usr/local/bin/adenosine
USER adenosine
EXPOSE 8080 2222
ENTRYPOINT ["adenosine"]
CMD ["serve"]
