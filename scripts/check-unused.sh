#!/bin/bash

# Quick script to check for unused functions/variables/types
# Usage: ./scripts/check-unused.sh

set -e

echo "🔍 Checking for unused code..."
echo ""

cd agent_go 2>/dev/null || {
    echo "❌ agent_go directory not found. Run from coding-agent-loop root."
    exit 1
}

# Run golangci-lint with unused linter
OUTPUT=$(golangci-lint run ./pkg/orchestrator/agents/workflow/todo_creation_human/... 2>&1 || true)

# Count unused items
UNUSED_COUNT=$(echo "$OUTPUT" | grep -E "is unused \(unused\)" | wc -l | tr -d " ")

if [ "$UNUSED_COUNT" -eq 0 ]; then
    echo "✅ No unused code found!"
    exit 0
fi

echo "❌ Found $UNUSED_COUNT unused functions/variables/types:"
echo ""
echo "$OUTPUT" | grep -E "is unused \(unused\)"
echo ""
echo "💡 The pre-commit hook will BLOCK commits with unused code."
echo "💡 Remove or use these functions before committing."

exit 1

