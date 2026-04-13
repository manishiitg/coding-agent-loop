#!/bin/bash
# Script to run the MCP agent server with logging enabled
# This makes it easier to debug event issues by capturing all output to a log file
# Terminal output is suppressed as requested.

# Get script directory first (needed for both test and server modes)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TEST_CONNECTIONS=false
BACKGROUND_MODE=false
WITH_WORKSPACE=false
POSITIONAL_ARGS=()

for arg in "$@"; do
    case "$arg" in
        --test-connections|--test-mcp|-t)
            TEST_CONNECTIONS=true
            ;;
        --background|-b)
            BACKGROUND_MODE=true
            ;;
        --with-workspace)
            WITH_WORKSPACE=true
            ;;
        *)
            POSITIONAL_ARGS+=("$arg")
            ;;
    esac
done

if [ "$TEST_CONNECTIONS" = true ]; then
    TEST_CONNECTIONS=true
    echo "🔌 Testing MCP Server Connections"
    echo "========================================="
    
    # Change to script directory
    cd "$SCRIPT_DIR" || {
        echo "❌ Error: Failed to change to script directory: $SCRIPT_DIR"
        exit 1
    }
    
    # Source environment variables from .env file if it exists
    if [ -f "../agent_go/.env" ]; then
        echo "🔧 Loading environment variables from ../agent_go/.env..."
        source ../agent_go/.env
    elif [ -f ".env" ]; then
        echo "🔧 Loading environment variables from .env..."
        source .env
    fi
    
    # Get config file path (default or from second argument)
    MCP_CONFIG="${POSITIONAL_ARGS[0]:-configs/mcp_servers_clean.json}"
    
    # Verify main.go exists
    if [ ! -f "main.go" ]; then
        echo "❌ Error: main.go not found in current directory: $(pwd)"
        exit 1
    fi
    
    # Verify go is available
    if ! command -v go &> /dev/null; then
        echo "❌ Error: 'go' command not found. Please install Go."
        exit 1
    fi
    
    # Run the test-all command
    echo "🚀 Running MCP connection tests..."
    go run main.go mcp test-all --config "$MCP_CONFIG" >> "logs/server_debug.log" 2>&1
    exit $?
fi

if [ "$BACKGROUND_MODE" = true ]; then
    BACKGROUND_MODE=true
    echo "🚀 Starting MCP Agent Server with Logging (Background Mode)"
else
    echo "🚀 Starting MCP Agent Server with Logging"
fi
echo "========================================="

find_random_free_port_in_range() {
    local start="$1"
    local end="$2"
    local exclude_csv="${3:-}"
    local attempts=50
    local range_size=$((end - start + 1))
    local attempt
    local port

    is_port_excluded() {
        local candidate="$1"
        if [ -z "$exclude_csv" ]; then
            return 1
        fi

        local old_ifs="$IFS"
        IFS=','
        for excluded in $exclude_csv; do
            if [ "$candidate" = "$excluded" ]; then
                IFS="$old_ifs"
                return 0
            fi
        done
        IFS="$old_ifs"
        return 1
    }

    for attempt in $(seq 1 "$attempts"); do
        port=$((start + RANDOM % range_size))
        if ! is_port_excluded "$port" && ! lsof -ti:"$port" > /dev/null 2>&1; then
            echo "$port"
            return 0
        fi
    done

    for port in $(seq "$start" "$end"); do
        if ! is_port_excluded "$port" && ! lsof -ti:"$port" > /dev/null 2>&1; then
            echo "$port"
            return 0
        fi
    done

    return 1
}

if [ -n "${AGENT_PORT:-}" ]; then
    echo "🔎 Using requested agent server port: $AGENT_PORT"
    if lsof -ti:"$AGENT_PORT" > /dev/null 2>&1; then
        echo "❌ Error: Requested AGENT_PORT $AGENT_PORT is already in use"
        exit 1
    fi
else
    echo "🔎 Selecting random agent server port in range 18000-19000..."
    AGENT_PORT="$(find_random_free_port_in_range 18000 19000)"
    if [ -z "$AGENT_PORT" ]; then
        echo "❌ Error: No free port available in range 18000-19000"
        exit 1
    fi
fi
export AGENT_PORT
export MCP_AGENT_SERVER_URL="http://127.0.0.1:${AGENT_PORT}"
echo "✅ Using agent server port: $AGENT_PORT"

# Source environment variables from .env file if it exists
if [ -f "../agent_go/.env" ]; then
    echo "🔧 Loading environment variables from ../agent_go/.env..."
    source ../agent_go/.env
    echo "✅ Environment variables loaded (including Langfuse configuration)"
elif [ -f ".env" ]; then
    echo "🔧 Loading environment variables from .env..."
    source .env
    echo "✅ Environment variables loaded (including Langfuse configuration)"
else
    echo "⚠️  No .env file found. Langfuse tracing will be disabled."
fi

# Set environment variables for the server
export LOG_LEVEL="debug"
# Use LOG_PATH for the shell script to redirect output
LOG_PATH="logs/server_debug.log"
# Unset LOG_FILE to ensure the Go application logs to stdout (avoiding duplicates)
unset LOG_FILE

export TRACING_PROVIDER="console"
export LANGFUSE_DEBUG="true"
export OBSERVABILITY_DEBUG="true"
export OBSERVABILITY_ENABLED="true"

# Set MCP_GENERATED_DIR to point to agent_go/generated/
# This ensures code generation happens in the correct location
# (SCRIPT_DIR already set above for test-connections mode)
export MCP_GENERATED_DIR="${SCRIPT_DIR}/generated"
echo "🔧 Set MCP_GENERATED_DIR to: $MCP_GENERATED_DIR"

# WORKSPACE_DOCS_PATH: absolute path to workspace-docs as seen by the workspace server.
# When workspace runs in Docker (default), this is /app/workspace-docs.
# Only override for desktop/native deployments where workspace runs on the host.
# export WORKSPACE_DOCS_PATH="/app/workspace-docs"  # default, no need to set

WORKSPACE_PID=""
WORKSPACE_LOG_PATH=""
WORKSPACE_DIR="${SCRIPT_DIR}/../workspace"
FRONTEND_RUNTIME_CONFIG_PATH="${SCRIPT_DIR}/../frontend/public/runtime-config.js"

# Change to script directory to ensure relative paths work correctly
cd "$SCRIPT_DIR" || {
    echo "❌ Error: Failed to change to script directory: $SCRIPT_DIR"
    exit 1
}
echo "📁 Working directory: $(pwd)"

# Use repo go.work so the server uses local multi-llm-provider-go (Azure streaming fix, etc.)
if [ -f "${SCRIPT_DIR}/../go.work" ]; then
    export GOWORK="${SCRIPT_DIR}/../go.work"
    echo "🔧 GOWORK=$GOWORK (using local multi-llm-provider-go)"
fi

if [ "$WITH_WORKSPACE" = true ]; then
    if [ -n "${WORKSPACE_PORT:-}" ]; then
        echo "🔎 Using requested workspace server port: $WORKSPACE_PORT"
        if lsof -ti:"$WORKSPACE_PORT" > /dev/null 2>&1; then
            echo "❌ Error: Requested WORKSPACE_PORT $WORKSPACE_PORT is already in use"
            exit 1
        fi
    else
        echo "🔎 Selecting random workspace server port in range 18000-19000..."
        WORKSPACE_PORT="$(find_random_free_port_in_range 18000 19000 "$AGENT_PORT")"
        if [ -z "$WORKSPACE_PORT" ]; then
            echo "❌ Error: No free workspace port available in range 18000-19000"
            exit 1
        fi
    fi
else
    WORKSPACE_PORT="${WORKSPACE_PORT:-8081}"
fi
export WORKSPACE_PORT

if [ "$WITH_WORKSPACE" = true ]; then
    if [ ! -f "${WORKSPACE_DIR}/main.go" ]; then
        echo "❌ Error: workspace main.go not found: ${WORKSPACE_DIR}/main.go"
        exit 1
    fi

    if [ -z "${WORKSPACE_DOCS_PATH:-}" ]; then
        WORKSPACE_DOCS_PATH="${SCRIPT_DIR}/../workspace-docs"
    fi
    mkdir -p "$WORKSPACE_DOCS_PATH"
    WORKSPACE_DOCS_PATH="$(cd "$WORKSPACE_DOCS_PATH" && pwd)"
    export WORKSPACE_DOCS_PATH
    export WORKSPACE_API_URL="http://127.0.0.1:${WORKSPACE_PORT}"

    export NATIVE_WORKSPACE="true"
    echo "🧩 Native workspace start enabled"
    echo "🔧 WORKSPACE_API_URL=$WORKSPACE_API_URL"
    echo "🔧 WORKSPACE_DOCS_PATH=$WORKSPACE_DOCS_PATH"
fi

write_frontend_runtime_config() {
    mkdir -p "$(dirname "$FRONTEND_RUNTIME_CONFIG_PATH")"
    cat > "$FRONTEND_RUNTIME_CONFIG_PATH" <<EOF
window.__APP_RUNTIME_CONFIG__ = {
  apiBaseUrl: "${MCP_AGENT_SERVER_URL}",
  workspaceApiBaseUrl: "${WORKSPACE_API_URL:-http://127.0.0.1:${WORKSPACE_PORT}}"
};
EOF
    echo "📝 Frontend runtime config written to: $FRONTEND_RUNTIME_CONFIG_PATH"
}

write_frontend_runtime_config

# Explicitly set single-user mode (no authentication required)
export MULTI_USER_MODE="false"

# Enable local mode (enables CDP browser connection and other local-only features)
export LOCAL_MODE="true"

# Log all agent prompts (system prompt + user message) to logs/agent_prompts/
export LOG_AGENT_PROMPTS="true"

# Enable split execution learning feature (separates learning reading from execution)
export SPLIT_EXECUTION_LEARNING="true"

# Set tool execution timeout to 15 minutes for normal tools.
# Long-running workflow delegation tools (for example call_sub_agent) should
# use their own per-tool timeout instead of stretching this global default.
export TOOL_EXECUTION_TIMEOUT="15m"

# Set MCP cache TTL to 7 days (10080 minutes)
export MCP_CACHE_TTL_MINUTES="10080"

# Set MCP cache directory to ensure consistent path across restarts
export MCP_CACHE_DIR="${SCRIPT_DIR}/cache"
echo "🔧 Set MCP_CACHE_DIR to: $MCP_CACHE_DIR"

# Context summarization configuration
export ENABLE_CONTEXT_SUMMARIZATION="true"
export SUMMARIZE_ON_TOKEN_THRESHOLD="true"
export TOKEN_THRESHOLD_PERCENT="0.7"  # 70% threshold (default: 0.7 = 70%)
export SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD="true"  # Enable fixed token threshold
export FIXED_TOKEN_THRESHOLD="200000"  # Trigger summarization at 200k tokens (default: 200000)
export SUMMARY_KEEP_LAST_MESSAGES="4"  # Keep last 4 messages when summarizing (roughly 2 turns)

# Context editing configuration (compacts large tool outputs)
# Note: Higher thresholds preserve cached tokens for cost efficiency
export ENABLE_CONTEXT_EDITING="false"  # Enable context editing (default: false)
export CONTEXT_EDITING_THRESHOLD="10000"  # Compact outputs larger than 10k tokens (default: 10000)
export CONTEXT_EDITING_TURN_THRESHOLD="20"  # Compact outputs older than 20 turns (default: 20)

# Context offloading configuration (offloads large tool outputs to filesystem)
# Tool outputs larger than this threshold are saved to file and replaced with a reference
export LARGE_OUTPUT_THRESHOLD="50000"  # Offload outputs larger than 50k tokens (default: 10000)

# Set main LLM configuration (uses Bedrock with AWS credentials from environment)
# Note: Frontend Published LLMs override this for actual agent execution
export DEEP_SEARCH_MAIN_LLM_PROVIDER="bedrock"
export DEEP_SEARCH_MAIN_LLM_MODEL="global.anthropic.claude-sonnet-4-5-20250929-v1:0"
export DEEP_SEARCH_MAIN_LLM_TEMPERATURE="0.0"
export DEEP_SEARCH_MAIN_LLM_MAX_TOKENS="40000"

# Set agent provider environment variable (used by server.go for internal operations)
# Note: Actual agent execution uses Published LLMs from frontend with their own API keys
export AGENT_PROVIDER="${AGENT_PROVIDER:-azure}"
export AGENT_MODEL="${AGENT_MODEL:-gpt-5.2}"

# Gemini CLI bridge safety: restrict Gemini tool usage to execute_shell_command and get_api_spec
# for server-launched sessions. Callers can still override by pre-setting the env var.
export MCPAGENT_GEMINI_ENFORCE_HTTP_TOOL_ROUTING="${MCPAGENT_GEMINI_ENFORCE_HTTP_TOOL_ROUTING:-true}"

# Claude Code bridge safety: restrict tool usage to execute_shell_command and get_api_spec
# for server-launched sessions. Callers can still override by pre-setting the env var.
export MCPAGENT_CLAUDE_ENFORCE_HTTP_TOOL_ROUTING="${MCPAGENT_CLAUDE_ENFORCE_HTTP_TOOL_ROUTING:-true}"

# Available models for each provider (optional - set in .env to customize; unset = empty lists, users add custom models)
# Removed hardcoded restrictions - use .env or leave unset for maximum flexibility
# BEDROCK_AVAILABLE_MODELS, OPENROUTER_AVAILABLE_MODELS, OPENAI_AVAILABLE_MODELS, AZURE_AVAILABLE_MODELS

# Supported LLM providers (optional - unset = all 6 providers shown: openrouter, bedrock, openai, vertex, anthropic, azure)
# Removed default restriction to azure only
# SUPPORTED_LLM_PROVIDERS

# Obsidian configuration removed - now using workspace tools

# Create logs directory if it doesn't exist
mkdir -p logs

# Truncate the log files to start fresh
echo "📝 Truncating log files for clean start..."
> "$LOG_PATH"
echo "✅ Server log file truncated: $LOG_PATH"
> "logs/llm_debug.log"
echo "✅ LLM log file truncated: logs/llm_debug.log"
if [ "$WITH_WORKSPACE" = true ]; then
    WORKSPACE_LOG_PATH="logs/workspace_debug.log"
    > "$WORKSPACE_LOG_PATH"
    echo "✅ Workspace log file truncated: $WORKSPACE_LOG_PATH"
fi

# Log rotation cap (used by background daemon)
LOG_ROTATE_LINES=500000

# Clean up agent prompt logs to start fresh
echo "🧹 Cleaning logs/agent_prompts..."
if [ -d "logs/agent_prompts" ]; then
    rm -rf logs/agent_prompts/*
    echo "✅ logs/agent_prompts cleaned"
else
    mkdir -p logs/agent_prompts
    echo "✅ logs/agent_prompts created"
fi

# Clean up tool_output_folder to start fresh
echo "🧹 Cleaning tool_output_folder..."
if [ -d "tool_output_folder" ]; then
    rm -rf tool_output_folder/*
    echo "✅ tool_output_folder cleaned (all files and subdirectories removed)"
else
    mkdir -p tool_output_folder
    echo "✅ tool_output_folder created (was missing)"
fi

# Clean up generated/agents directory to start fresh
echo "🧹 Cleaning generated/agents..."
if [ -d "generated/agents" ]; then
    rm -rf generated/agents/*
    echo "✅ generated/agents cleaned (all files and subdirectories removed)"
else
    mkdir -p generated/agents
    echo "✅ generated/agents created (was missing)"
fi

# Add timestamp header to log file
echo "🚀 MCP Agent Server Session Started: $(date)" > "$LOG_PATH"
echo "=========================================" >> "$LOG_PATH"
echo "Configuration:" >> "$LOG_PATH"
echo "- Split Execution Learning: $SPLIT_EXECUTION_LEARNING" >> "$LOG_PATH"
echo "- Tool Execution Timeout: $TOOL_EXECUTION_TIMEOUT" >> "$LOG_PATH"
echo "- MCP Cache TTL: $MCP_CACHE_TTL_MINUTES minutes (7 days)" >> "$LOG_PATH"
echo "- Agent Provider: $AGENT_PROVIDER" >> "$LOG_PATH"
echo "- Agent Model: $AGENT_MODEL" >> "$LOG_PATH"
echo "- Main LLM Provider: $DEEP_SEARCH_MAIN_LLM_PROVIDER" >> "$LOG_PATH"
echo "- Main LLM Model: $DEEP_SEARCH_MAIN_LLM_MODEL" >> "$LOG_PATH"
echo "- Main LLM Temperature: $DEEP_SEARCH_MAIN_LLM_TEMPERATURE" >> "$LOG_PATH"
echo "- Available Bedrock Models: $BEDROCK_AVAILABLE_MODELS" >> "$LOG_PATH"
echo "- Available OpenRouter Models: $OPENROUTER_AVAILABLE_MODELS" >> "$LOG_PATH"
echo "- Available OpenAI Models: $OPENAI_AVAILABLE_MODELS" >> "$LOG_PATH"
echo "- Available Azure Models: $AZURE_AVAILABLE_MODELS" >> "$LOG_PATH"
echo "- Workspace tools: Enabled" >> "$LOG_PATH"
echo "- Context Summarization: $ENABLE_CONTEXT_SUMMARIZATION" >> "$LOG_PATH"
echo "- Token Threshold: $TOKEN_THRESHOLD_PERCENT (70%) | Fixed: ${FIXED_TOKEN_THRESHOLD} tokens" >> "$LOG_PATH"
echo "- Keep Last Messages: $SUMMARY_KEEP_LAST_MESSAGES" >> "$LOG_PATH"
echo "- Context Editing: $ENABLE_CONTEXT_EDITING (Threshold: ${CONTEXT_EDITING_THRESHOLD} tokens, Age: ${CONTEXT_EDITING_TURN_THRESHOLD} turns)" >> "$LOG_PATH"
echo "- Large Output Threshold: ${LARGE_OUTPUT_THRESHOLD} tokens" >> "$LOG_PATH"
echo "- Agent API URL: $MCP_AGENT_SERVER_URL" >> "$LOG_PATH"
if [ "$WITH_WORKSPACE" = true ]; then
    echo "- Native Workspace: Enabled (${WORKSPACE_API_URL}, docs=${WORKSPACE_DOCS_PATH})" >> "$LOG_PATH"
fi
echo "=========================================" >> "$LOG_PATH"
echo "" >> "$LOG_PATH"

# Start background log rotation: keep only last 500000 lines every 30 seconds
rotate_log_file() {
    local file_path="$1"
    if [ -f "$file_path" ]; then
        lines=$(wc -l < "$file_path" 2>/dev/null)
        if [ "$lines" -gt "$LOG_ROTATE_LINES" ]; then
            excess=$((lines - LOG_ROTATE_LINES))
            sed -i '' "1,${excess}d" "$file_path"
        fi
    fi
}

log_rotate_daemon() {
    while true; do
        sleep 30
        rotate_log_file "$LOG_PATH"
        if [ "$WITH_WORKSPACE" = true ] && [ -n "$WORKSPACE_LOG_PATH" ]; then
            rotate_log_file "$WORKSPACE_LOG_PATH"
        fi
    done
}
log_rotate_daemon &
LOG_ROTATE_PID=$!

stop_native_workspace() {
    if [ -n "$WORKSPACE_PID" ] && kill -0 "$WORKSPACE_PID" 2>/dev/null; then
        echo "🛑 Stopping native workspace server (PID: $WORKSPACE_PID)..."
        kill "$WORKSPACE_PID" 2>/dev/null
        wait "$WORKSPACE_PID" 2>/dev/null
    fi
}

cleanup_on_exit() {
    kill "$LOG_ROTATE_PID" 2>/dev/null
    wait "$LOG_ROTATE_PID" 2>/dev/null
    if [ "$BACKGROUND_MODE" != true ]; then
        stop_native_workspace
    fi
}

trap cleanup_on_exit EXIT
trap "exit 130" INT TERM
echo "🔄 Log rotation started (keeping last $LOG_ROTATE_LINES lines, PID: $LOG_ROTATE_PID)"

wait_for_workspace_health() {
    local health_url="${WORKSPACE_API_URL%/}/health"
    local attempt
    for attempt in $(seq 1 90); do
        if curl -fsS "$health_url" >/dev/null 2>&1; then
            echo "✅ Native workspace is healthy at: $health_url"
            return 0
        fi
        if ! kill -0 "$WORKSPACE_PID" 2>/dev/null; then
            echo "❌ Error: Native workspace exited during startup. Check logs: $WORKSPACE_LOG_PATH"
            tail -20 "$WORKSPACE_LOG_PATH"
            return 1
        fi
        sleep 1
    done

    echo "❌ Error: Native workspace did not become healthy in time. Check logs: $WORKSPACE_LOG_PATH"
    tail -20 "$WORKSPACE_LOG_PATH"
    return 1
}

start_native_workspace() {
    if [ "$WITH_WORKSPACE" != true ]; then
        return 0
    fi

    if lsof -ti:"$WORKSPACE_PORT" > /dev/null 2>&1; then
        echo "❌ Error: Port $WORKSPACE_PORT is already in use."
        echo "   Stop the existing process or set WORKSPACE_PORT to another value."
        return 1
    fi

    echo "🚀 Starting native workspace server..."
    echo "📝 Workspace log file: $WORKSPACE_LOG_PATH"
    echo "🌐 Workspace API URL: $WORKSPACE_API_URL"

    echo "🚀 Native Workspace Session Started: $(date)" > "$WORKSPACE_LOG_PATH"
    echo "=========================================" >> "$WORKSPACE_LOG_PATH"
    echo "- Port: $WORKSPACE_PORT" >> "$WORKSPACE_LOG_PATH"
    echo "- Docs Path: $WORKSPACE_DOCS_PATH" >> "$WORKSPACE_LOG_PATH"
    echo "=========================================" >> "$WORKSPACE_LOG_PATH"
    echo "" >> "$WORKSPACE_LOG_PATH"

    if [ "$BACKGROUND_MODE" = true ]; then
        nohup bash -lc "cd \"$WORKSPACE_DIR\" && exec go run . server --debug --port \"$WORKSPACE_PORT\" --docs-dir \"$WORKSPACE_DOCS_PATH\"" >> "$WORKSPACE_LOG_PATH" 2>&1 &
    else
        (
            cd "$WORKSPACE_DIR" || exit 1
            exec go run . server --debug --port "$WORKSPACE_PORT" --docs-dir "$WORKSPACE_DOCS_PATH"
        ) >> "$WORKSPACE_LOG_PATH" 2>&1 &
    fi

    WORKSPACE_PID=$!
    echo "✅ Native workspace process started (PID: $WORKSPACE_PID)"
    wait_for_workspace_health
}

# Start the server with enhanced logging and structured output LLM
echo "🚀 Starting MCP Agent Server with enhanced logging..."
echo "📝 Log file: $LOG_PATH"
echo "🔀 Split Execution Learning: $SPLIT_EXECUTION_LEARNING"
echo "⏱️  Tool Timeout: $TOOL_EXECUTION_TIMEOUT"
echo "💾 MCP Cache TTL: $MCP_CACHE_TTL_MINUTES minutes (7 days)"
echo "📁 Workspace Tools: Enabled"
echo "🌐 Agent API URL: $MCP_AGENT_SERVER_URL"
echo "📝 Context Summarization: $ENABLE_CONTEXT_SUMMARIZATION (Threshold: $TOKEN_THRESHOLD_PERCENT = 70%, Fixed: ${FIXED_TOKEN_THRESHOLD} tokens, Keep: $SUMMARY_KEEP_LAST_MESSAGES msgs)"
echo "✂️  Context Editing: $ENABLE_CONTEXT_EDITING (Threshold: ${CONTEXT_EDITING_THRESHOLD} tokens, Age: ${CONTEXT_EDITING_TURN_THRESHOLD} turns)"
echo "📦 Large Output Threshold: ${LARGE_OUTPUT_THRESHOLD} tokens"
echo "📊 Debug level: $LOG_LEVEL"

# Database configuration based on DATABASE_URL
# Set USE_SQLITE=true to force SQLite for local testing (ignores DATABASE_URL)
USE_SQLITE="${USE_SQLITE:-false}"
DB_TYPE_FLAG="sqlite"
if [ "$USE_SQLITE" = "true" ]; then
    unset DATABASE_URL
    echo "🗄️  USE_SQLITE=true — forcing SQLite (ignoring DATABASE_URL)"
    DB_TYPE_FLAG="sqlite"
elif [ -n "$DATABASE_URL" ]; then
    echo "🗄️  Detected DATABASE_URL, using PostgreSQL (Supabase)"
    DB_TYPE_FLAG="postgres"
else
    echo "🗄️  No DATABASE_URL found, using SQLite"
    DB_TYPE_FLAG="sqlite"
fi

# Verify main.go exists before attempting to run
if [ ! -f "main.go" ]; then
    echo "❌ Error: main.go not found in current directory: $(pwd)"
    exit 1
fi

# Verify go is available
if ! command -v go &> /dev/null; then
    echo "❌ Error: 'go' command not found. Please install Go."
    exit 1
fi

# Pre-install camofox packages globally (skips if already installed — avoids slow npx -y each time)
if ! command -v camofox-browser &> /dev/null; then
    echo "📦 Installing camofox-browser globally (first time only)..."
    npm install -g camofox-browser@latest 2>&1 | tail -1
    echo "✅ camofox-browser installed"
else
    echo "✅ camofox-browser already installed"
fi
if ! command -v camofox-mcp &> /dev/null; then
    echo "📦 Installing camofox-mcp globally (first time only)..."
    npm install -g camofox-mcp@latest 2>&1 | tail -1
    echo "✅ camofox-mcp installed"
else
    echo "✅ camofox-mcp already installed"
fi

# Build mcpbridge binary (required for CLI provider MCP bridge)
# Install from local source to pick up latest fixes (e.g., virtual tool scoping)
echo "🔨 Building mcpbridge binary from local source..."
(cd "${SCRIPT_DIR}/../../mcpagent" && go install ./cmd/mcpbridge/) 2>&1
if [ $? -eq 0 ]; then
    echo "✅ mcpbridge binary installed from local source: $(which mcpbridge || echo ~/go/bin/mcpbridge)"
else
    # Fallback to published module if local build fails
    echo "⚠️  Local build failed, falling back to published release..."
    GOWORK=off go install github.com/manishiitg/mcpagent/cmd/mcpbridge@latest 2>&1
    if [ $? -eq 0 ]; then
        echo "✅ mcpbridge binary installed from published release: $(which mcpbridge || echo ~/go/bin/mcpbridge)"
    else
        echo "⚠️  Failed to install mcpbridge (CLI provider MCP bridge will not work)"
    fi
fi

if [ "$WITH_WORKSPACE" = true ]; then
    start_native_workspace || exit 1
fi

# Run the server with all the enhanced configuration
echo "🚀 Starting server with 'go run'..."

if [ "$BACKGROUND_MODE" = true ]; then
    # Background mode: run in background and capture PID
    echo "🔄 Starting server in background mode..."
    nohup go run main.go server \
        --port "$AGENT_PORT" \
        --log-level debug \
        --debug \
        --db-type "$DB_TYPE_FLAG" \
        --db-path "./chat_history.db" \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 50 \
        --mcp-config "configs/mcp_servers_clean.json" >> "$LOG_PATH" 2>&1 &
    
    SERVER_PID=$!
    echo "✅ Server started in background (PID: $SERVER_PID)"
    echo "📝 Logs are being written to: $LOG_PATH"
    echo "🛑 To stop the server, run: kill $SERVER_PID"
    echo "🌐 Agent API URL: $MCP_AGENT_SERVER_URL"
    if [ "$WITH_WORKSPACE" = true ]; then
        echo "✅ Native workspace is running in background (PID: $WORKSPACE_PID)"
        echo "📝 Workspace logs are being written to: $WORKSPACE_LOG_PATH"
        echo "🌐 Workspace health: ${WORKSPACE_API_URL%/}/health"
    fi
    
    # Wait a moment to check if server started successfully
    sleep 3
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo "❌ Error: Server process died immediately. Check logs: $LOG_PATH"
        tail -20 "$LOG_PATH"
        if [ "$WITH_WORKSPACE" = true ]; then
            stop_native_workspace
        fi
        exit 1
    fi
    
    # Check if server is listening on the selected port
    if lsof -ti:"$AGENT_PORT" > /dev/null 2>&1; then
        echo "✅ Server is running and listening on port $AGENT_PORT"
    else
        echo "⚠️  Warning: Server process is running but not listening on port $AGENT_PORT yet"
        echo "   Check logs: $LOG_PATH"
    fi
else
    # Foreground mode: run in foreground with output visible
    echo "🔄 Starting server in foreground mode..."
    echo "   (Press Ctrl+C to stop)"
    echo "   Agent API URL: $MCP_AGENT_SERVER_URL"
    echo ""
    
    go run main.go server \
        --port "$AGENT_PORT" \
        --log-level debug \
        --debug \
        --db-type "$DB_TYPE_FLAG" \
        --db-path "./chat_history.db" \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 50 \
        --mcp-config "configs/mcp_servers_clean.json" >> "$LOG_PATH" 2>&1
    
    EXIT_CODE=$?
    if [ $EXIT_CODE -ne 0 ]; then
        echo ""
        echo "❌ Error: Server exited with code $EXIT_CODE"
        echo "📝 Check logs for details: $LOG_PATH"
        if [ -f "$LOG_PATH" ]; then
            echo ""
            echo "Last 20 lines of log file:"
            tail -20 "$LOG_PATH"
        fi
        exit $EXIT_CODE
    fi
fi
