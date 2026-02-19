#!/bin/bash

# --- 🚀 Yokohama Lab AI Autopilot ---
# This script runs the segmented integration tests with progress bar and structured context for AI debugging.

set -e

LOG_FILE="tmp/pipeline_results.log"
mkdir -p tmp

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Progress bar configuration
BAR_WIDTH=40
TOTAL_STEPS=6

# Step counter
CURRENT_STEP=0

# Progress bar function
draw_progress_bar() {
    local percent=$((CURRENT_STEP * 100 / TOTAL_STEPS))
    local filled=$((CURRENT_STEP * BAR_WIDTH / TOTAL_STEPS))
    local empty=$((BAR_WIDTH - filled))
    
    printf "\r${BLUE}[${NC}"
    printf "%0.s█" $(seq 1 $filled)
    printf "%0.s░" $(seq 1 $empty)
    printf "${BLUE}]${NC} ${BOLD}%3d%%${NC} " $percent
}

# Update progress
update_progress() {
    CURRENT_STEP=$((CURRENT_STEP + 1))
    draw_progress_bar
}

# Print step header
print_step() {
    local step_num=$1
    local step_name=$2
    local step_icon=$3
    
    echo ""
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}${step_icon} Step ${step_num}/${TOTAL_STEPS}: ${step_name}${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    update_progress
}

# Print success/failure for each step
print_step_status() {
    local status=$1
    if [ $status -eq 0 ]; then
        echo -e "${GREEN}✅ Step completed successfully${NC}"
    else
        echo -e "${RED}❌ Step failed${NC}"
    fi
}

# Main execution
echo -e "${BOLD}${CYAN}"
echo "╔════════════════════════════════════════════════════════╗"
echo "║     🔍 Yokohama Lab AI Autopilot Pipeline 🔍          ║"
echo "╚════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# Initialize progress bar
draw_progress_bar

# ─────────────────────────────────────────────────────────
# Step 1: Environment Check
# ─────────────────────────────────────────────────────────
print_step 1 "Environment Check" "🔧"
echo -e "${YELLOW}➜${NC} Checking Go installation..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    echo -e "${GREEN}✓${NC} Go version: $GO_VERSION"
else
    echo -e "${RED}✗${NC} Go not found!"
    exit 1
fi

echo -e "${YELLOW}➜${NC} Checking golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    LINT_VERSION=$(golangci-lint --version | head -1)
    echo -e "${GREEN}✓${NC} $LINT_VERSION"
else
    echo -e "${YELLOW}⚠${NC} golangci-lint not found (optional)"
fi

print_step_status 0

# ─────────────────────────────────────────────────────────
# Step 2: Lint Check
# ─────────────────────────────────────────────────────────
print_step 2 "Running Lint Checks" "🔍"
if command -v golangci-lint &> /dev/null; then
    echo -e "${YELLOW}➜${NC} Running golangci-lint..."
    if golangci-lint run ./... > "$LOG_FILE.lint" 2>&1; then
        echo -e "${GREEN}✓${NC} Lint passed"
        print_step_status 0
    else
        echo -e "${RED}✗${NC} Lint issues found"
        cat "$LOG_FILE.lint" | tail -20
        print_step_status 1
    fi
else
    echo -e "${YELLOW}⚠${NC} Skipping lint (golangci-lint not installed)"
    print_step_status 0
fi

# ─────────────────────────────────────────────────────────
# Step 3: Build Check
# ─────────────────────────────────────────────────────────
print_step 3 "Building Application" "🔨"
echo -e "${YELLOW}➜${NC} Building cmd/indexer..."
if go build -o tmp/indexer ./cmd/indexer > "$LOG_FILE.build" 2>&1; then
    echo -e "${GREEN}✓${NC} Build successful"
    print_step_status 0
else
    echo -e "${RED}✗${NC} Build failed"
    cat "$LOG_FILE.build" | tail -20
    print_step_status 1
    exit 1
fi

# ─────────────────────────────────────────────────────────
# Step 4: Unit Tests
# ─────────────────────────────────────────────────────────
print_step 4 "Running Unit Tests" "🧪"
echo -e "${YELLOW}➜${NC} Running go test (short mode)..."
if go test -short ./... > "$LOG_FILE.unit" 2>&1; then
    echo -e "${GREEN}✓${NC} Tests passed"
    print_step_status 0
else
    echo -e "${RED}✗${NC} Some tests failed"
    print_step_status 1
fi

# ─────────────────────────────────────────────────────────
# Step 5: Integration Tests (Pipeline Stages)
# ─────────────────────────────────────────────────────────
print_step 5 "Running Pipeline Integration Tests" "🔄"
echo -e "${YELLOW}➜${NC} Executing data pipeline stages..."
echo -e "${CYAN}   This may take a few minutes...${NC}"
echo ""

# Run integration tests with progress indication
timeout 300 go test -v -tags=integration ./internal/engine -run TestStage > "$LOG_FILE" 2>&1 &
TEST_PID=$!

# Show spinner while tests run
spinner="⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
while kill -0 $TEST_PID 2>/dev/null; do
    for i in $(seq 0 9); do
        printf "\r${CYAN}   ${spinner:$i:1} Running pipeline tests...${NC}"
        sleep 0.1
    done
done

wait $TEST_PID
TEST_EXIT_CODE=$?
printf "\r${GREEN}   ✓ Pipeline tests completed${NC}                \n"

# ─────────────────────────────────────────────────────────
# Step 6: Results Analysis
# ─────────────────────────────────────────────────────────
print_step 6 "Analyzing Results" "📊"

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}${BOLD}  ✅ ALL PIPELINE STAGES PASSED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "${GREEN}✓${NC} Data pipeline is logical and healthy"
    echo -e "${GREEN}✓${NC} All stage transitions working correctly"
    
    FINAL_STATUS=0
else
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}${BOLD}  ❌ PIPELINE BREAKAGE DETECTED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    
    AI_FIX_COUNT=$(grep -c "AI_FIX_REQUIRED" "$LOG_FILE" 2>/dev/null || echo "0")
    if [ "$AI_FIX_COUNT" -gt 0 ]; then
        echo -e "${YELLOW}${BOLD}🚨 Found $AI_FIX_COUNT issue(s):${NC}"
        grep "AI_FIX_REQUIRED" "$LOG_FILE" | head -5
    fi
    
    echo ""
    echo -e "${CYAN}💡 Review: ${YELLOW}cat $LOG_FILE${NC}"
    
    FINAL_STATUS=1
fi

# Final progress bar - 100%
CURRENT_STEP=$TOTAL_STEPS
draw_progress_bar
echo ""
echo ""

exit $FINAL_STATUS
