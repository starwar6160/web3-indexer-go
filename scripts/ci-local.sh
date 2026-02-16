#!/bin/bash
set -euo pipefail

# In containers the repo may be owned by a different UID/GID than the running user.
# This can make git commands fail with exit 128 (safe.directory / dubious ownership),
# which in turn breaks Go VCS stamping during typecheck.
git config --global --add safe.directory /app >/dev/null 2>&1 || true

echo "🔍 1. go vet"
go vet ./...

echo "🧪 2. go test (engine)"
go test -v -count=1 ./internal/engine/...

echo "🧹 3. golangci-lint"
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
golangci-lint run ./...

echo "🛡️ 4. trivy fs"
trivy fs --exit-code 1 --ignore-unfixed --severity CRITICAL,HIGH .

echo "✅ local CI passed"
