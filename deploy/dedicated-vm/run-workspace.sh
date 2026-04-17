#!/usr/bin/env bash
# Run MCP Workspace API directly on the server (bare metal, not Docker).
set -euo pipefail

export PATH="/usr/local/go/bin:/root/go/bin:$PATH"

WORKSPACE_DIR="/opt/mcp-agent/src/workspace"
LOG_PATH="/data/logs/workspace_server.log"

cd "$WORKSPACE_DIR"

# Load .env for shared config (optional)
if [ -f "/opt/mcp-agent/.env" ]; then
    set -a
    source /opt/mcp-agent/.env
    set +a
fi

# Workspace-specific env
export PORT="8080"
export DOCS_DIR="/data/docs"
export DB_PATH="/data/workspace-db/workspace.db"
export ENABLE_SEMANTIC_SEARCH="${ENABLE_SEMANTIC_SEARCH:-false}"

# Create required dirs
mkdir -p /data/docs /data/workspace-db /data/logs \
         /data/docs/Downloads /data/docs/Chats /data/docs/Workspace

echo "==> Starting MCP Workspace API (bare metal)"
echo "    Port: $PORT"
echo "    Docs: $DOCS_DIR"
echo "    DB:   $DB_PATH"
echo "    Log:  $LOG_PATH"

exec go run . server \
    --port "$PORT" \
    --docs-dir "$DOCS_DIR"
