#!/bin/bash
# Script to clear MCP cache and force rebuild with fixed schemas

echo "🧹 Clearing MCP cache to force rebuild with fixed schemas..."
echo ""

# Find and remove all cache files
CACHE_DIR="agent_go/cache"
if [ -d "$CACHE_DIR" ]; then
    CACHE_COUNT=$(find "$CACHE_DIR" -name "*.json" -type f | wc -l | tr -d ' ')
    echo "📊 Found $CACHE_COUNT cache files"
    
    # Remove all cache files
    find "$CACHE_DIR" -name "*.json" -type f -delete
    echo "✅ Cleared $CACHE_COUNT cache files"
else
    echo "⚠️  Cache directory not found: $CACHE_DIR"
fi

echo ""
echo "✅ Cache cleared! Restart the server to rebuild cache with fixed schemas."
echo ""
echo "Next steps:"
echo "1. Restart the server: cd agent_go && go run main.go server"
echo "2. The cache will be rebuilt automatically with the correct schemas"
echo "3. Test with playwright tools to verify the fix"

