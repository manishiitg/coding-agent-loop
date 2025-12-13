#!/bin/bash

# Test script to check for unused functions/variables/types
# Simulates what the pre-commit hook would do

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 Testing unused code detection (pre-commit hook simulation)...${NC}"
echo ""

# Check if golangci-lint is available
GOLANGCI_LINT_CMD=""
if command -v golangci-lint &> /dev/null; then
    GOLANGCI_LINT_CMD="golangci-lint"
elif [ -f "$(go env GOPATH)/bin/golangci-lint" ]; then
    GOLANGCI_LINT_CMD="$(go env GOPATH)/bin/golangci-lint"
else
    echo -e "${YELLOW}⚠️  golangci-lint not found.${NC}"
    echo "Run 'cd agent_go && make install-linter' to install golangci-lint."
    exit 1
fi

# Store original directory
ORIGINAL_DIR=$(pwd)
COMBINED_LINT_OUTPUT=""
COMBINED_LINT_EXIT=0

# Function to run lint check on a directory
run_lint_check() {
    local DIR=$1
    local DIR_NAME=$2
    
    echo -e "${BLUE}🔍 Running golangci-lint on ${DIR_NAME}...${NC}"
    
    cd "$DIR" 2>/dev/null || {
        echo -e "${YELLOW}⚠️  ${DIR_NAME} directory not found. Skipping.${NC}"
        return 0
    }
    
    set +e
    # Run golangci-lint (uses .golangci.yml config which includes unused in fast preset)
    # The pre-commit hook runs without --enable flags, so we match that behavior
    LINT_OUTPUT_FULL=$($GOLANGCI_LINT_CMD run ./... 2>&1)
    LINT_OUTPUT=$(echo "$LINT_OUTPUT_FULL" | grep -v -E "(tool_output_folder|tool_output/|cache/|bin/)")
    echo "$LINT_OUTPUT"
    
    # Check for any linting issues (including unused)
    # Exit code 1 means issues found, 0 means clean
    if echo "$LINT_OUTPUT" | grep -qE "(^[^:]+:[0-9]+:[0-9]+:.*)|issues found|issues:"; then
        LINT_EXIT=1
    else
        # Also check golangci-lint exit code
        echo "$LINT_OUTPUT_FULL" | tail -1 | grep -q "issues:" && LINT_EXIT=1 || LINT_EXIT=0
    fi
    set -e
    
    cd "$ORIGINAL_DIR" > /dev/null
    
    if [ $LINT_EXIT -ne 0 ]; then
        COMBINED_LINT_EXIT=1
    fi
    COMBINED_LINT_OUTPUT="${COMBINED_LINT_OUTPUT}${LINT_OUTPUT}"
}

# Run lint on agent_go
if [ -n "$GOLANGCI_LINT_CMD" ]; then
    run_lint_check "agent_go" "agent_go"
    echo ""
    run_lint_check "mcpagent" "mcpagent"
fi

# Evaluate results (same logic as pre-commit hook)
echo ""
echo -e "${BLUE}=== Results ===${NC}"

if [ $COMBINED_LINT_EXIT -eq 0 ]; then
    echo -e "${GREEN}✅ No linting issues found.${NC}"
    exit 0
fi

# Count issues
TOTAL_ISSUE_COUNT=$(echo "$COMBINED_LINT_OUTPUT" | grep -E "issues:" | grep -oE "[0-9]+ issues" | grep -oE "[0-9]+" || echo "0")
TOTAL_CRITICAL_ISSUES=$(echo "$COMBINED_LINT_OUTPUT" | grep -E "G201|G202|G204|G304" | grep -v "_test.go" | grep -v "/testing/" | wc -l | tr -d " ")
TOTAL_UNUSED_ISSUES=$(echo "$COMBINED_LINT_OUTPUT" | grep -E "is unused \(unused\)" | wc -l | tr -d " ")

echo "Total issues: $TOTAL_ISSUE_COUNT"
echo "Critical security issues: $TOTAL_CRITICAL_ISSUES"
echo -e "${YELLOW}Unused functions/variables/types: $TOTAL_UNUSED_ISSUES${NC}"
echo ""

if [ "$TOTAL_CRITICAL_ISSUES" -gt 0 ]; then
    echo -e "${RED}❌ Would BLOCK: Critical security issues detected ($TOTAL_CRITICAL_ISSUES)${NC}"
    echo "$COMBINED_LINT_OUTPUT" | grep -E "G201|G202|G204|G304" | head -10
    exit 1
elif [ "$TOTAL_UNUSED_ISSUES" -gt 0 ]; then
    echo -e "${RED}❌ Would BLOCK: Unused code detected ($TOTAL_UNUSED_ISSUES unused items)${NC}"
    echo ""
    echo "Unused code found:"
    echo "$COMBINED_LINT_OUTPUT" | grep -E "is unused \(unused\)" | head -20
    exit 1
elif [ "$TOTAL_ISSUE_COUNT" -gt 200 ]; then
    echo -e "${RED}❌ Would BLOCK: Too many issues ($TOTAL_ISSUE_COUNT)${NC}"
    exit 1
else
    echo -e "${YELLOW}⚠️  Would WARN: Non-critical issues ($TOTAL_ISSUE_COUNT issues)${NC}"
    echo "Would allow commit to proceed."
    exit 0
fi

