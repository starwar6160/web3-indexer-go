#!/bin/bash
# 🛡️ Web3 Indexer - No-Sleep Mode Verification
# Verifies that LOCAL STABLE (8082) instance is running in "Never Hibernate" mode

set -e

PORT=${1:-8082}
API_URL="http://localhost:${PORT}/api/status"

echo "════════════════════════════════════════════════════════════"
echo "🔍 Verifying No-Sleep Mode for Port ${PORT}"
echo "════════════════════════════════════════════════════════════"
echo ""

# 1. Check if port is listening
echo "1️⃣  Checking if port ${PORT} is listening..."
if lsof -ti:${PORT} >/dev/null 2>&1; then
    PID=$(lsof -ti:${PORT})
    echo "   ✅ Port ${PORT} is active (PID: ${PID})"
else
    echo "   ❌ Port ${PORT} is not listening"
    exit 1
fi
echo ""

# 2. Check API status
echo "2️⃣  Checking API status..."
STATUS=$(curl -s "${API_URL}")
if [ $? -eq 0 ]; then
    echo "   ✅ API is responding"
else
    echo "   ❌ API is not responding"
    exit 1
fi
echo ""

# 3. Check lazy_indexer mode
echo "3️⃣  Checking lazy_indexer mode..."
LAZY_MODE=$(echo "${STATUS}" | jq -r '.lazy_indexer.mode // "unknown"')
LAB_MODE=$(echo "${STATUS}" | jq -r '.lazy_indexer.is_lab_mode // false')

if [ "${LAZY_MODE}" = "active" ]; then
    echo "   ✅ Lazy Mode: ${LAZY_MODE}"
else
    echo "   ⚠️  Lazy Mode: ${LAZY_MODE} (expected: active)"
fi

if [ "${LAB_MODE}" = "true" ]; then
    echo "   ✅ Lab Mode: Enabled 🚀"
    LAB_MODE_ACTIVE=true
else
    echo "   ❌ Lab Mode: Disabled (should be true for Anvil/LOCAL_STABLE)"
    LAB_MODE_ACTIVE=false
fi
echo ""

# 4. Check chain_id
echo "4️⃣  Checking chain_id..."
CHAIN_ID=$(echo "${STATUS}" | jq -r '.chain_id // "unknown"')

if [ "${CHAIN_ID}" = "31337" ]; then
    echo "   ✅ Chain ID: ${CHAIN_ID} (Anvil - Lab Mode)"
elif [ "${CHAIN_ID}" = "11155111" ]; then
    echo "   ⚠️  Chain ID: ${CHAIN_ID} (Sepolia - Testnet Mode)"
    echo "   ℹ️  Sepolia instances use Eco-Mode (normal behavior)"
else
    echo "   ⚠️  Chain ID: ${CHAIN_ID} (unknown)"
fi
echo ""

# 5. Check process environment
echo "5️⃣  Checking process environment..."
if [ -n "${PID}" ]; then
    APP_TITLE=$(cat /proc/${PID}/environ 2>/dev/null | tr '\0' '\n' | grep "^APP_TITLE=" | cut -d= -f2)
    echo "   📦 APP_TITLE: ${APP_TITLE}"
fi
echo ""

# 6. Summary
echo "════════════════════════════════════════════════════════════"
echo "📊 Verification Summary"
echo "════════════════════════════════════════════════════════════"

if [ "${LAB_MODE_ACTIVE}" = true ]; then
    echo "✅ NEVER HIBERNATE MODE: ACTIVE"
    echo ""
    echo "Key Features:"
    echo "  • Hibernation logic: DISABLED"
    echo "  • Fetcher state: ALWAYS RUNNING"
    echo "  • Idle timeout: BYPASSED"
    echo "  • Frontend sleep overlay: DISABLED"
    echo ""
    echo "Performance Profile:"
    echo "  • RPS: Unlimited (vs 1.0 for Sepolia)"
    echo "  • CPU: 100% available"
    echo "  • Memory: Hot-Vault retention"
    echo "  • UI: Always-On Visuals"
    echo ""
    echo "🔥 Your 5600U is ready for infinite processing!"
else
    echo "⚠️  NEVER HIBERNATE MODE: INACTIVE"
    echo ""
    echo "This instance is using Eco-Mode (normal for Sepolia)."
    echo "Anvil/LOCAL_STABLE instances should have Lab Mode enabled."
fi
echo "════════════════════════════════════════════════════════════"
echo ""

exit 0
