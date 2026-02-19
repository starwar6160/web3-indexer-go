#!/bin/bash
# Reality Collapse Configuration Audit Script
# Scans all .env files and database checkpoints for misconfigurations

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🔍 REALITY COLLISION CONFIGURATION AUDIT"
echo "======================================="
echo ""

# Function to check .env files
audit_env_file() {
	local env_file=$1
	echo "📄 Auditing: $env_file"

	if [ ! -f "$env_file" ]; then
		echo "  ${YELLOW}⚠️  File not found (skipping)${NC}"
		return
	fi

	# Check for START_BLOCK hardcoding
	if grep -q "^START_BLOCK=" "$env_file"; then
		start_block=$(grep "^START_BLOCK=" "$env_file" | cut -d'=' -f2)
		if [ "$start_block" != "latest" ] && [ "$start_block" != "0" ]; then
			echo "  ${YELLOW}⚠️  START_BLOCK=$start_block (hardcoded)${NC}"
			echo "     → Consider using START_BLOCK=latest for Anvil environments"
		else
			echo "  ${GREEN}✓ START_BLOCK=$start_block${NC}"
		fi
	else
		echo "  ${YELLOW}⚠️  START_BLOCK not set (will use default)${NC}"
	fi

	# Check for CHAIN_ID consistency
	if grep -q "^CHAIN_ID=" "$env_file"; then
		chain_id=$(grep "^CHAIN_ID=" "$env_file" | cut -d'=' -f2)
		echo "  ${GREEN}✓ CHAIN_ID=$chain_id${NC}"

		# Warn if Anvil but not using latest
		if [ "$chain_id" = "31337" ] && [ "$start_block" != "latest" ]; then
			echo "  ${RED}🚨 ANVIL_DETECTED: Should use START_BLOCK=latest${NC}"
		fi
	fi

	echo ""
}

# Main audit execution
main() {
	# Audit all .env files
	for env_file in .env .env.testnet .env.testnet.local .env.demo2; do
		audit_env_file "$env_file"
	done

	echo "✅ Audit Complete"
	echo ""
	echo "Recommendations:"
	echo "1. Use START_BLOCK=latest for Anvil environments"
	echo "2. Use explicit START_BLOCK for testnet/mainnet"
	echo "3. Ensure .env.testnet.local is in .gitignore"
	echo "4. Run this script after any Anvil restarts"
}

main "$@"
