#!/usr/bin/env bash
set -euo pipefail

sqlc generate
oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
bun x openapi-ts --file packages/api-client/openapi-ts.config.ts
gofmt -w api/generated/go internal/database/generated
