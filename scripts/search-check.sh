#!/usr/bin/env bash
# The final package gate calls this inside the Linux gate container.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/search-helper"
go test ./...
go vet ./...
test -z "$(gofmt -l .)"
cd "$root/third-party/clustarr"
go test ./pkg/cardigann ./pkg/torznab
go vet ./pkg/cardigann
