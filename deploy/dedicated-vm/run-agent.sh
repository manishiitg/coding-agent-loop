#!/usr/bin/env bash
# Run MCP Agent directly on the server (bare metal, not Docker).
set -euo pipefail

export PATH="/usr/local/go/bin:/root/go/bin:$PATH"

AGENT_DIR="/opt/mcp-agent/src/agent_go"
LOG_PATH="/data/logs/agent_server.log"

cd "$AGENT_DIR"

# Load .env
if [ -f "/opt/mcp-agent/.env" ]; then
    set -a
    source /opt/mcp-agent/.env
    set +a
fi

# Agent configuration
export LOG_LEVEL="debug"
export TRACING_PROVIDER="console"
export MULTI_USER_MODE="${MULTI_USER_MODE:-false}"
export LOCAL_MODE="true"
export LOG_AGENT_PROMPTS="true"
export SPLIT_EXECUTION_LEARNING="true"
export MCP_CACHE_TTL_MINUTES="10080"
export MCP_CACHE_DIR="$AGENT_DIR/cache"
export MCP_GENERATED_DIR="$AGENT_DIR/generated"
export WORKSPACE_API_URL="http://localhost:8080"
export WORKSPACE_ENABLE_SEMANTIC_SEARCH="${WORKSPACE_ENABLE_SEMANTIC_SEARCH:-false}"
export WORKSPACE_DOCS_PATH="/data/docs"

# Context management
export ENABLE_CONTEXT_SUMMARIZATION="true"
export SUMMARIZE_ON_TOKEN_THRESHOLD="true"
export TOKEN_THRESHOLD_PERCENT="0.7"
export SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD="true"
export FIXED_TOKEN_THRESHOLD="200000"
export SUMMARY_KEEP_LAST_MESSAGES="4"
export ENABLE_CONTEXT_EDITING="false"
export LARGE_OUTPUT_THRESHOLD="50000"

# LLM defaults
export AGENT_PROVIDER="${AGENT_PROVIDER:-azure}"
export AGENT_MODEL="${AGENT_MODEL:-gpt-5.2}"

# Chromium for browser tools
export PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH="/usr/bin/chromium-browser"
export AGENT_BROWSER_EXECUTABLE_PATH="/usr/bin/chromium-browser"

# Create required dirs
mkdir -p "$AGENT_DIR/logs" "$AGENT_DIR/cache" "$AGENT_DIR/generated/agents" \
         "$AGENT_DIR/tool_output_folder" "$AGENT_DIR/reports" /data/logs

echo "==> Starting MCP Agent Server (bare metal)"
echo "    Log: $LOG_PATH"
echo "    Workspace API: $WORKSPACE_API_URL"

exec go run main.go server \
    --host 0.0.0.0 \
    --port 8000 \
    --log-level debug \
    --log-file "$LOG_PATH" \
    --debug \
    --mcp-config "configs/mcp_servers_clean_user.json" \
    --provider "$AGENT_PROVIDER" \
    --model "$AGENT_MODEL" \
    --max-turns 100
