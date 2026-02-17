#!/bin/bash
# scripts/ci-local.sh - Local CI simulation script for Go Indexer
# Optimized for high-speed feedback and industrial-grade quality gates.

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

log_step() {
    echo -e "
${BLUE}========================================${NC}"
    echo -e "${BLUE}STEP: $1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

echo -e "${BLUE}🚀 Starting local CI simulation...${NC}"

# 1. Linting
log_step "Running golangci-lint"
golangci-lint run ./... --timeout=5m
echo -e "${GREEN}✅ Linting passed!${NC}"

# 2. Testing
log_step "Running Go tests"
go test -v -race -cover ./...
echo -e "${GREEN}✅ Tests passed!${NC}"

# 3. Security Scanning
log_step "Running Trivy Vulnerability Scan"
trivy fs . --severity HIGH,CRITICAL --scanners vuln,secret,config --exit-code 1
echo -e "${GREEN}✅ Security scan passed!${NC}"

echo -e "
${GREEN}🎊 All CI quality gates passed! Ready for deployment.${NC}"
