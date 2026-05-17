#!/bin/bash
# Script to run the MCP agent server with logging enabled
# This makes it easier to debug event issues by capturing all output to a log file
# Terminal output is suppressed as requested.

# Get script directory first (needed for both test and server modes)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TEST_CONNECTIONS=false
BACKGROUND_MODE=false
WITH_WORKSPACE=false
WITH_FRONTEND=false
ONLY_FRONTEND=false
UPDATE_MMX_CLI=false
FRONTEND_BUILD_MODE=false
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
        --with-frontend)
            WITH_FRONTEND=true
            ;;
        --only-frontend)
            ONLY_FRONTEND=true
            ;;
        --build)
            FRONTEND_BUILD_MODE=true
            ;;
        --update)
            UPDATE_MMX_CLI=true
            ;;
        *)
            POSITIONAL_ARGS+=("$arg")
            ;;
    esac
done

FRONTEND_PORT_EXPLICIT="${FRONTEND_PORT:-}"
LOCALHOST_BASE_URL="${LOCALHOST_BASE_URL:-http://localhost}"
FRONTEND_HOST="${FRONTEND_HOST:-}"
FRONTEND_BIND_HOST="${FRONTEND_HOST:-127.0.0.1}"
FRONTEND_URL_HOST="${FRONTEND_URL_HOST:-127.0.0.1}"

port_in_use() {
    lsof -nP -iTCP:"$1" -sTCP:LISTEN > /dev/null 2>&1
}

print_port_status() {
    local port="$1"
    local label="$2"

    if [ -z "$port" ]; then
        return 0
    fi

    if port_in_use "$port"; then
        echo "⚠️  Port $port ($label) is still in use"
        lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sed 's/^/   /'
    else
        echo "✅ Port $port ($label) is free"
    fi
}

print_stop_target() {
    local label="$1"
    local pid="$2"
    local port="$3"

    if [ -n "$port" ]; then
        echo "🛑 Stopping $label (PID: $pid, port: $port)..."
    else
        echo "🛑 Stopping $label (PID: $pid)..."
    fi
}

kill_process_tree() {
    local root_pid="$1"
    local label="${2:-process}"
    local grace_attempts="${3:-20}"

    if [ -z "$root_pid" ] || ! kill -0 "$root_pid" 2>/dev/null; then
        return 0
    fi

    local child_pids
    child_pids="$(pgrep -P "$root_pid" 2>/dev/null || true)"
    for child_pid in $child_pids; do
        kill_process_tree "$child_pid" "$label child"
    done

    kill "$root_pid" 2>/dev/null || true

    local attempt
    for attempt in $(seq 1 "$grace_attempts"); do
        if ! kill -0 "$root_pid" 2>/dev/null; then
            return 0
        fi
        sleep 0.1
    done

    echo "⚠️  $label (PID: $root_pid) did not stop after SIGTERM; forcing stop..."
    kill -9 "$root_pid" 2>/dev/null || true
}

choose_frontend_port() {
    local preferred="${1:-51733}"
    local port="$preferred"

    if [ -n "$FRONTEND_PORT_EXPLICIT" ]; then
        echo "$preferred"
        return 0
    fi

    while port_in_use "$port"; do
        port=$((port + 1))
        if [ "$port" -gt 51999 ]; then
            return 1
        fi
    done

    echo "$port"
}

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

if [ "$ONLY_FRONTEND" = true ]; then
    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "🎨 Starting frontend only (static build + Electron) — no backend, no workspace"
    else
        echo "🎨 Starting frontend only (Vite + Electron) — no backend, no workspace"
    fi
    echo "========================================="

    cd "$SCRIPT_DIR" || {
        echo "❌ Error: Failed to change to script directory: $SCRIPT_DIR"
        exit 1
    }

    FRONTEND_PORT="$(choose_frontend_port "${FRONTEND_PORT:-51733}")" || {
        echo "❌ Error: No free frontend port available in range 51733-51999"
        exit 1
    }
    if [ -z "$FRONTEND_PORT_EXPLICIT" ] && [ "$FRONTEND_PORT" != "51733" ]; then
        echo "🔎 Frontend port 51733 is busy; using $FRONTEND_PORT"
    fi
    FRONTEND_URL="http://${FRONTEND_URL_HOST}:${FRONTEND_PORT}"
    FRONTEND_DIR="${SCRIPT_DIR}/../frontend"
    DESKTOP_DIR="${SCRIPT_DIR}/../desktop"
    ELECTRON_BIN="${DESKTOP_DIR}/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"
    FRONTEND_RUNTIME_CONFIG_PATH="${SCRIPT_DIR}/../frontend/public/runtime-config.js"

    # Fall back to reading ports from the runtime-config.js written by a previous
    # backend run if AGENT_PORT / WORKSPACE_PORT aren't explicitly set.
    if [ -z "${AGENT_PORT:-}" ] && [ -f "$FRONTEND_RUNTIME_CONFIG_PATH" ]; then
        detected_agent_port="$(grep -oE 'apiBaseUrl:[[:space:]]*"http://[^"]+"' "$FRONTEND_RUNTIME_CONFIG_PATH" | grep -oE '[0-9]+"' | tr -d '"' | head -1)"
        if [ -n "$detected_agent_port" ]; then
            AGENT_PORT="$detected_agent_port"
            echo "🔎 Detected AGENT_PORT=$AGENT_PORT from existing runtime-config.js"
        fi
    fi
    if [ -z "${WORKSPACE_PORT:-}" ] && [ -f "$FRONTEND_RUNTIME_CONFIG_PATH" ]; then
        detected_workspace_port="$(grep -oE 'workspaceApiBaseUrl:[[:space:]]*"http://[^"]+"' "$FRONTEND_RUNTIME_CONFIG_PATH" | grep -oE '[0-9]+"' | tr -d '"' | head -1)"
        if [ -n "$detected_workspace_port" ]; then
            WORKSPACE_PORT="$detected_workspace_port"
            echo "🔎 Detected WORKSPACE_PORT=$WORKSPACE_PORT from existing runtime-config.js"
        fi
    fi

    if [ -z "${AGENT_PORT:-}" ]; then
        echo "❌ Error: AGENT_PORT is not set and could not be detected from $FRONTEND_RUNTIME_CONFIG_PATH"
        echo "   Either start the backend first (it writes the runtime config), or pass AGENT_PORT explicitly."
        echo "   Example: AGENT_PORT=18080 ./run_server_with_logging.sh --only-frontend"
        exit 1
    fi

    WORKSPACE_PORT="${WORKSPACE_PORT:-8081}"

    export MCP_AGENT_SERVER_URL="${LOCALHOST_BASE_URL}:${AGENT_PORT}"
    export WORKSPACE_API_URL="${LOCALHOST_BASE_URL}:${WORKSPACE_PORT}"

    # Sanity-check the backend is actually reachable on that port.
    if ! curl -fsS "${MCP_AGENT_SERVER_URL}/api/health" >/dev/null 2>&1; then
        echo "⚠️  Warning: backend at $MCP_AGENT_SERVER_URL did not respond to /api/health."
        echo "   Make sure the backend is running before the frontend tries to call it."
    fi

    echo "🔧 Backend (expected running): $MCP_AGENT_SERVER_URL"
    echo "🔧 Workspace (expected running): $WORKSPACE_API_URL"
    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "🔧 Static frontend URL: $FRONTEND_URL"
    else
        echo "🔧 Vite URL: $FRONTEND_URL"
    fi

    mkdir -p logs
    mkdir -p "$(dirname "$FRONTEND_RUNTIME_CONFIG_PATH")"
    cat > "$FRONTEND_RUNTIME_CONFIG_PATH" <<EOF
window.__APP_RUNTIME_CONFIG__ = {
  apiBaseUrl: "${MCP_AGENT_SERVER_URL}",
  workspaceApiBaseUrl: "${WORKSPACE_API_URL}"
};
EOF
    echo "📝 Frontend runtime config written to: $FRONTEND_RUNTIME_CONFIG_PATH"

    FRONTEND_LOG_PATH="logs/frontend_debug.log"
    ELECTRON_LOG_PATH="logs/electron_debug.log"
    > "$FRONTEND_LOG_PATH"
    > "$ELECTRON_LOG_PATH"

    if [ ! -f "${FRONTEND_DIR}/package.json" ]; then
        echo "❌ Error: frontend package.json not found: ${FRONTEND_DIR}/package.json"
        exit 1
    fi
    if port_in_use "$FRONTEND_PORT"; then
        echo "❌ Error: Port $FRONTEND_PORT is already in use."
        if [ -n "$FRONTEND_PORT_EXPLICIT" ]; then
            echo "   FRONTEND_PORT was explicitly set; choose another value or stop the existing process."
        else
            echo "   Port became busy after selection; retry the command."
        fi
        exit 1
    fi

    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "🔨 Building frontend..."
        (
            cd "$FRONTEND_DIR" || exit 1
            npm run build
        ) >> "$FRONTEND_LOG_PATH" 2>&1 || {
            echo "❌ Error: Frontend build failed. Check logs: $FRONTEND_LOG_PATH"
            tail -30 "$FRONTEND_LOG_PATH"
            exit 1
        }
        echo "🚀 Vite Preview Session Started: $(date)" >> "$FRONTEND_LOG_PATH"
    else
        echo "🚀 Vite Dev Session Started: $(date)" > "$FRONTEND_LOG_PATH"
    fi
    if [ "$BACKGROUND_MODE" = true ]; then
        if [ "$FRONTEND_BUILD_MODE" = true ]; then
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run preview -- --host \"$FRONTEND_BIND_HOST\" --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        elif [ -n "$FRONTEND_HOST" ]; then
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run dev -- --host \"$FRONTEND_HOST\" --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        else
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run dev -- --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        fi
    else
        (
            cd "$FRONTEND_DIR" || exit 1
            if [ "$FRONTEND_BUILD_MODE" = true ]; then
                exec npm run preview -- --host "$FRONTEND_BIND_HOST" --port "$FRONTEND_PORT" --strictPort
            elif [ -n "$FRONTEND_HOST" ]; then
                exec npm run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT" --strictPort
            else
                exec npm run dev -- --port "$FRONTEND_PORT" --strictPort
            fi
        ) >> "$FRONTEND_LOG_PATH" 2>&1 &
    fi
    FRONTEND_PID=$!
    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "✅ Static frontend server started (PID: $FRONTEND_PID) — $FRONTEND_URL"
    else
        echo "✅ Vite dev server started (PID: $FRONTEND_PID) — $FRONTEND_URL"
    fi

    frontend_ready=false
    for attempt in $(seq 1 60); do
        if curl -fsS "$FRONTEND_URL" >/dev/null 2>&1; then
            frontend_ready=true
            break
        fi
        if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
            echo "❌ Error: Frontend server exited during startup. Check logs: $FRONTEND_LOG_PATH"
            tail -20 "$FRONTEND_LOG_PATH"
            exit 1
        fi
        sleep 1
    done
    if [ "$frontend_ready" != true ]; then
        echo "❌ Error: Frontend server did not become ready in time. Check logs: $FRONTEND_LOG_PATH"
        tail -20 "$FRONTEND_LOG_PATH"
        kill_process_tree "$FRONTEND_PID" "frontend server"
        exit 1
    fi

    if [ ! -f "${DESKTOP_DIR}/package.json" ]; then
        echo "❌ Error: desktop package.json not found: ${DESKTOP_DIR}/package.json"
        kill_process_tree "$FRONTEND_PID" "frontend server"
        exit 1
    fi
    if [ ! -x "$ELECTRON_BIN" ]; then
        echo "❌ Error: Electron binary not found or not executable: $ELECTRON_BIN"
        kill_process_tree "$FRONTEND_PID" "frontend server"
        print_port_status "$FRONTEND_PORT" "frontend"
        exit 1
    fi

    echo "🚀 Electron Session Started: $(date)" > "$ELECTRON_LOG_PATH"
    if [ "$BACKGROUND_MODE" = true ]; then
        nohup bash -lc "cd \"$DESKTOP_DIR\" && DEV_URL=\"$FRONTEND_URL\" exec \"$ELECTRON_BIN\" ." >> "$ELECTRON_LOG_PATH" 2>&1 &
    else
        (
            cd "$DESKTOP_DIR" || exit 1
            DEV_URL="$FRONTEND_URL" exec "$ELECTRON_BIN" .
        ) >> "$ELECTRON_LOG_PATH" 2>&1 &
    fi
    ELECTRON_PID=$!
    echo "✅ Electron started (PID: $ELECTRON_PID)"
    sleep 2
    if ! kill -0 "$ELECTRON_PID" 2>/dev/null; then
        echo "❌ Error: Electron exited immediately. Check logs: $ELECTRON_LOG_PATH"
        tail -30 "$ELECTRON_LOG_PATH"
        kill_process_tree "$FRONTEND_PID" "frontend server"
        print_port_status "$FRONTEND_PORT" "frontend"
        exit 1
    fi

    cleanup_frontend_only() {
        if [ "$BACKGROUND_MODE" != true ]; then
            if [ -n "$ELECTRON_PID" ] && kill -0 "$ELECTRON_PID" 2>/dev/null; then
                print_stop_target "Electron" "$ELECTRON_PID"
                kill_process_tree "$ELECTRON_PID" "Electron"
                wait "$ELECTRON_PID" 2>/dev/null
            fi
            if [ -n "$FRONTEND_PID" ] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
                print_stop_target "frontend server" "$FRONTEND_PID" "$FRONTEND_PORT"
                kill_process_tree "$FRONTEND_PID" "frontend server"
                wait "$FRONTEND_PID" 2>/dev/null
                print_port_status "$FRONTEND_PORT" "frontend"
            fi
        fi
    }
    trap cleanup_frontend_only EXIT
    trap "exit 130" INT TERM

    if [ "$BACKGROUND_MODE" = true ]; then
        echo ""
        echo "✅ Frontend services running in background:"
        echo "   - Frontend server (PID: $FRONTEND_PID) — $FRONTEND_URL"
        echo "   - Electron (PID: $ELECTRON_PID)"
        echo "   Logs: $FRONTEND_LOG_PATH (vite), $ELECTRON_LOG_PATH (electron)"
        echo "🛑 To stop: kill $FRONTEND_PID $ELECTRON_PID"
        exit 0
    fi

    echo ""
    echo "✅ Frontend services running (foreground):"
    echo "   - Frontend server (PID: $FRONTEND_PID) — $FRONTEND_URL"
    echo "   - Electron (PID: $ELECTRON_PID)"
    echo "   Backend expected at: $MCP_AGENT_SERVER_URL"
    echo "   Press Ctrl+C to stop."
    echo ""
    while true; do
        if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
            echo "❌ Frontend server exited. Check logs: $FRONTEND_LOG_PATH"
            tail -20 "$FRONTEND_LOG_PATH"
            exit 1
        fi
        if ! kill -0 "$ELECTRON_PID" 2>/dev/null; then
            echo "❌ Electron exited. Check logs: $ELECTRON_LOG_PATH"
            tail -30 "$ELECTRON_LOG_PATH"
            kill_process_tree "$FRONTEND_PID" "frontend server"
            print_port_status "$FRONTEND_PORT" "frontend"
            exit 1
        fi
        sleep 1
    done
    exit 0
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
        if ! is_port_excluded "$port" && ! port_in_use "$port"; then
            echo "$port"
            return 0
        fi
    done

    for port in $(seq "$start" "$end"); do
        if ! is_port_excluded "$port" && ! port_in_use "$port"; then
            echo "$port"
            return 0
        fi
    done

    return 1
}

DEFAULT_AGENT_PORT=18743
DEFAULT_WORKSPACE_PORT=18744

choose_default_then_random_port() {
    local preferred="$1"
    local start="$2"
    local end="$3"
    local exclude_csv="${4:-}"

    if [ -z "$exclude_csv" ]; then
        if ! port_in_use "$preferred"; then
            echo "$preferred"
            return 0
        fi
    elif ! is_csv_value "$preferred" "$exclude_csv" && ! port_in_use "$preferred"; then
        echo "$preferred"
        return 0
    fi

    find_random_free_port_in_range "$start" "$end" "$exclude_csv"
}

is_csv_value() {
    local candidate="$1"
    local csv="$2"
    local old_ifs="$IFS"
    IFS=','
    for value in $csv; do
        if [ "$candidate" = "$value" ]; then
            IFS="$old_ifs"
            return 0
        fi
    done
    IFS="$old_ifs"
    return 1
}

if [ -n "${AGENT_PORT:-}" ]; then
    echo "🔎 Using requested agent server port: $AGENT_PORT"
    if port_in_use "$AGENT_PORT"; then
        echo "❌ Error: Requested AGENT_PORT $AGENT_PORT is already in use"
        exit 1
    fi
else
    echo "🔎 Selecting agent server port: default ${DEFAULT_AGENT_PORT}, random fallback in range 18000-19000..."
    AGENT_PORT="$(choose_default_then_random_port "$DEFAULT_AGENT_PORT" 18000 19000)"
    if [ -z "$AGENT_PORT" ]; then
        echo "❌ Error: No free port available in range 18000-19000"
        exit 1
    fi
fi
export AGENT_PORT
export MCP_AGENT_SERVER_URL="${LOCALHOST_BASE_URL}:${AGENT_PORT}"
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

# Browser session limits (explicit export so child process inherits them)
export MAX_BROWSER_SESSIONS_PER_AGENT=1
export MAX_BROWSER_SESSIONS_PER_WORKFLOW=4
export MAX_BROWSER_SESSIONS_GLOBAL=8

# Set environment variables for the server
export LOG_LEVEL="debug"
# Use LOG_PATH for the shell script to redirect output
LOG_PATH="logs/server_debug.log"
# Unset LOG_FILE to ensure the Go application logs to stdout (avoiding duplicates)
unset LOG_FILE

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

FRONTEND_PID=""
ELECTRON_PID=""
FRONTEND_LOG_PATH=""
ELECTRON_LOG_PATH=""
FRONTEND_DIR="${SCRIPT_DIR}/../frontend"
DESKTOP_DIR="${SCRIPT_DIR}/../desktop"
ELECTRON_BIN="${DESKTOP_DIR}/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"
FRONTEND_PORT="$(choose_frontend_port "${FRONTEND_PORT:-51733}")" || {
    echo "❌ Error: No free frontend port available in range 51733-51999"
    exit 1
}
if [ -z "$FRONTEND_PORT_EXPLICIT" ] && [ "$FRONTEND_PORT" != "51733" ]; then
    echo "🔎 Frontend port 51733 is busy; using $FRONTEND_PORT"
fi
FRONTEND_URL="http://${FRONTEND_URL_HOST}:${FRONTEND_PORT}"

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
        if port_in_use "$WORKSPACE_PORT"; then
            echo "❌ Error: Requested WORKSPACE_PORT $WORKSPACE_PORT is already in use"
            exit 1
        fi
    else
        echo "🔎 Selecting workspace server port: default ${DEFAULT_WORKSPACE_PORT}, random fallback in range 18000-19000..."
        WORKSPACE_PORT="$(choose_default_then_random_port "$DEFAULT_WORKSPACE_PORT" 18000 19000 "$AGENT_PORT")"
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

    # Always use local workspace-docs for native workspace (ignore Docker paths from .env)
    WORKSPACE_DOCS_PATH="${SCRIPT_DIR}/../workspace-docs"
    mkdir -p "$WORKSPACE_DOCS_PATH"
    WORKSPACE_DOCS_PATH="$(cd "$WORKSPACE_DOCS_PATH" && pwd)"
    export WORKSPACE_DOCS_PATH
    export WORKSPACE_API_URL="${LOCALHOST_BASE_URL}:${WORKSPACE_PORT}"

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
  workspaceApiBaseUrl: "${WORKSPACE_API_URL:-${LOCALHOST_BASE_URL}:${WORKSPACE_PORT}}"
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

# Set tool execution timeout to 90 minutes to match call_sub_agent's per-tool
# budget. In code-exec-only mode, orchestrator Python scripts call sub-agents
# over HTTP via execute_shell_command, so the script must be allowed to wait
# as long as the sub-agent itself is allowed to run.
export TOOL_EXECUTION_TIMEOUT="90m"

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
if [ "$WITH_FRONTEND" = true ]; then
    FRONTEND_LOG_PATH="logs/frontend_debug.log"
    ELECTRON_LOG_PATH="logs/electron_debug.log"
    > "$FRONTEND_LOG_PATH"
    > "$ELECTRON_LOG_PATH"
    echo "✅ Frontend log file truncated: $FRONTEND_LOG_PATH"
    echo "✅ Electron log file truncated: $ELECTRON_LOG_PATH"
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
        if [ "$WITH_FRONTEND" = true ]; then
            [ -n "$FRONTEND_LOG_PATH" ] && rotate_log_file "$FRONTEND_LOG_PATH"
            [ -n "$ELECTRON_LOG_PATH" ] && rotate_log_file "$ELECTRON_LOG_PATH"
        fi
    done
}
log_rotate_daemon &
LOG_ROTATE_PID=$!

stop_native_workspace() {
    if [ -n "$WORKSPACE_PID" ] && kill -0 "$WORKSPACE_PID" 2>/dev/null; then
        print_stop_target "native workspace server" "$WORKSPACE_PID" "$WORKSPACE_PORT"
        kill_process_tree "$WORKSPACE_PID" "native workspace server"
        wait "$WORKSPACE_PID" 2>/dev/null
        print_port_status "$WORKSPACE_PORT" "workspace"
    fi
}

stop_electron() {
    if [ -n "$ELECTRON_PID" ] && kill -0 "$ELECTRON_PID" 2>/dev/null; then
        print_stop_target "Electron" "$ELECTRON_PID"
        kill_process_tree "$ELECTRON_PID" "Electron"
        wait "$ELECTRON_PID" 2>/dev/null
    fi
}

stop_frontend_dev() {
    if [ -n "$FRONTEND_PID" ] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
        print_stop_target "frontend server" "$FRONTEND_PID" "$FRONTEND_PORT"
        kill_process_tree "$FRONTEND_PID" "frontend server"
        wait "$FRONTEND_PID" 2>/dev/null
        print_port_status "$FRONTEND_PORT" "frontend"
    fi
}

stop_agent_server() {
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        print_stop_target "agent server" "$SERVER_PID" "$AGENT_PORT"
        # Give the Go server enough time to run SIGTERM cleanup. In particular,
        # Claude Code experimental runs live in detached tmux sessions, and a
        # fast SIGKILL can leave them orphaned.
        kill_process_tree "$SERVER_PID" "agent server" 200
        wait "$SERVER_PID" 2>/dev/null
        print_port_status "$AGENT_PORT" "agent"
    fi
}

cleanup_on_exit() {
    kill "$LOG_ROTATE_PID" 2>/dev/null
    wait "$LOG_ROTATE_PID" 2>/dev/null
    if [ "$BACKGROUND_MODE" != true ]; then
        stop_electron
        stop_frontend_dev
        stop_agent_server
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

    if port_in_use "$WORKSPACE_PORT"; then
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

wait_for_frontend_health() {
    local health_url="$FRONTEND_URL"
    local attempt
    for attempt in $(seq 1 60); do
        if curl -fsS "$health_url" >/dev/null 2>&1; then
            echo "✅ Frontend server is ready at: $health_url"
            return 0
        fi
        if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
            echo "❌ Error: Frontend server exited during startup. Check logs: $FRONTEND_LOG_PATH"
            tail -20 "$FRONTEND_LOG_PATH"
            return 1
        fi
        sleep 1
    done

    echo "❌ Error: Frontend server did not become ready in time. Check logs: $FRONTEND_LOG_PATH"
    tail -20 "$FRONTEND_LOG_PATH"
    return 1
}

start_frontend_dev() {
    if [ "$WITH_FRONTEND" != true ]; then
        return 0
    fi

    if [ ! -f "${FRONTEND_DIR}/package.json" ]; then
        echo "❌ Error: frontend package.json not found: ${FRONTEND_DIR}/package.json"
        return 1
    fi

    if port_in_use "$FRONTEND_PORT"; then
        echo "❌ Error: Port $FRONTEND_PORT is already in use."
        if [ -n "$FRONTEND_PORT_EXPLICIT" ]; then
            echo "   FRONTEND_PORT was explicitly set; choose another value or stop the existing process."
        else
            echo "   Port became busy after selection; retry the command."
        fi
        return 1
    fi

    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "🚀 Starting static frontend server..."
    else
        echo "🚀 Starting Vite dev server..."
    fi
    echo "📝 Frontend log file: $FRONTEND_LOG_PATH"
    echo "🌐 Frontend URL: $FRONTEND_URL"

    if [ "$FRONTEND_BUILD_MODE" = true ]; then
        echo "🔨 Building frontend..."
        echo "🔨 Frontend Build Session Started: $(date)" > "$FRONTEND_LOG_PATH"
        (
            cd "$FRONTEND_DIR" || exit 1
            npm run build
        ) >> "$FRONTEND_LOG_PATH" 2>&1 || {
            echo "❌ Error: Frontend build failed. Check logs: $FRONTEND_LOG_PATH"
            tail -30 "$FRONTEND_LOG_PATH"
            return 1
        }
        echo "🚀 Vite Preview Session Started: $(date)" >> "$FRONTEND_LOG_PATH"
    else
        echo "🚀 Vite Dev Session Started: $(date)" > "$FRONTEND_LOG_PATH"
    fi
    echo "=========================================" >> "$FRONTEND_LOG_PATH"
    echo "- Port: $FRONTEND_PORT" >> "$FRONTEND_LOG_PATH"
    echo "- URL: $FRONTEND_URL" >> "$FRONTEND_LOG_PATH"
    echo "=========================================" >> "$FRONTEND_LOG_PATH"

    if [ "$BACKGROUND_MODE" = true ]; then
        if [ "$FRONTEND_BUILD_MODE" = true ]; then
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run preview -- --host \"$FRONTEND_BIND_HOST\" --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        elif [ -n "$FRONTEND_HOST" ]; then
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run dev -- --host \"$FRONTEND_HOST\" --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        else
            nohup bash -lc "cd \"$FRONTEND_DIR\" && exec npm run dev -- --port \"$FRONTEND_PORT\" --strictPort" >> "$FRONTEND_LOG_PATH" 2>&1 &
        fi
    else
        (
            cd "$FRONTEND_DIR" || exit 1
            if [ "$FRONTEND_BUILD_MODE" = true ]; then
                exec npm run preview -- --host "$FRONTEND_BIND_HOST" --port "$FRONTEND_PORT" --strictPort
            elif [ -n "$FRONTEND_HOST" ]; then
                exec npm run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT" --strictPort
            else
                exec npm run dev -- --port "$FRONTEND_PORT" --strictPort
            fi
        ) >> "$FRONTEND_LOG_PATH" 2>&1 &
    fi

    FRONTEND_PID=$!
    echo "✅ Frontend server process started (PID: $FRONTEND_PID)"
    wait_for_frontend_health
}

wait_for_agent_health() {
    local url="${MCP_AGENT_SERVER_URL%/}/api/health"
    local attempt
    for attempt in $(seq 1 180); do
        if curl -fsS "$url" >/dev/null 2>&1; then
            echo "✅ Agent server is healthy at: $url"
            return 0
        fi
        if [ -n "$SERVER_PID" ] && ! kill -0 "$SERVER_PID" 2>/dev/null; then
            echo "❌ Error: Agent server exited during startup. Check logs: $LOG_PATH"
            tail -30 "$LOG_PATH"
            return 1
        fi
        sleep 1
    done

    echo "❌ Error: Agent server did not become healthy in time. Check logs: $LOG_PATH"
    tail -30 "$LOG_PATH"
    return 1
}

start_electron() {
    if [ "$WITH_FRONTEND" != true ]; then
        return 0
    fi

    if [ ! -f "${DESKTOP_DIR}/package.json" ]; then
        echo "❌ Error: desktop package.json not found: ${DESKTOP_DIR}/package.json"
        return 1
    fi
    if [ ! -x "$ELECTRON_BIN" ]; then
        echo "❌ Error: Electron binary not found or not executable: $ELECTRON_BIN"
        return 1
    fi

    local dev_url="$FRONTEND_URL"
    echo "🚀 Starting Electron (DEV_URL=$dev_url)..."
    echo "📝 Electron log file: $ELECTRON_LOG_PATH"

    echo "🚀 Electron Session Started: $(date)" > "$ELECTRON_LOG_PATH"
    echo "=========================================" >> "$ELECTRON_LOG_PATH"
    echo "- DEV_URL: $dev_url" >> "$ELECTRON_LOG_PATH"
    echo "=========================================" >> "$ELECTRON_LOG_PATH"

    if [ "$BACKGROUND_MODE" = true ]; then
        nohup bash -lc "cd \"$DESKTOP_DIR\" && DEV_URL=\"$dev_url\" exec \"$ELECTRON_BIN\" ." >> "$ELECTRON_LOG_PATH" 2>&1 &
    else
        (
            cd "$DESKTOP_DIR" || exit 1
            DEV_URL="$dev_url" exec "$ELECTRON_BIN" .
        ) >> "$ELECTRON_LOG_PATH" 2>&1 &
    fi

    ELECTRON_PID=$!
    echo "✅ Electron process started (PID: $ELECTRON_PID)"
    sleep 2
    if ! kill -0 "$ELECTRON_PID" 2>/dev/null; then
        echo "❌ Error: Electron exited immediately. Check logs: $ELECTRON_LOG_PATH"
        tail -30 "$ELECTRON_LOG_PATH"
        return 1
    fi
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

update_mmx_cli_if_requested() {
    if [ "$UPDATE_MMX_CLI" != true ]; then
        return 0
    fi

    if ! command -v npm &> /dev/null; then
        echo "❌ Error: --update requested but 'npm' command not found. Please install Node.js/npm."
        return 1
    fi

    echo "📦 Updating mmx-cli to latest because --update was provided..."
    npm install -g mmx-cli@latest 2>&1 | tail -5
    local npm_status=${PIPESTATUS[0]}
    if [ "$npm_status" -ne 0 ]; then
        echo "❌ Error: failed to update mmx-cli"
        return "$npm_status"
    fi

    if command -v mmx &> /dev/null; then
        echo "✅ mmx-cli updated: $(mmx --version 2>/dev/null || echo 'version unknown')"
    else
        echo "⚠️  mmx-cli update completed but 'mmx' was not found on PATH"
    fi
}

update_mmx_cli_if_requested || exit 1

ensure_tmux_for_claude_code() {
    if command -v tmux &> /dev/null; then
        local version major
        version="$(tmux -V 2>/dev/null || true)"
        major="$(printf '%s\n' "$version" | sed -E 's/^tmux ([0-9]+).*/\1/')"
        if [ "$major" -ge 3 ] 2>/dev/null; then
            echo "✅ Claude Code experimental runtime dependency available: $version"
            return 0
        fi
        echo "⚠️  Claude Code experimental runtime dependency ${version:-unknown} found, but version 3.x or newer is required."
    fi

    echo "📦 Installing/upgrading Claude Code experimental runtime dependency..."
    if [[ "$(uname -s)" == "Darwin" ]] && command -v brew &> /dev/null; then
        brew upgrade tmux || brew install tmux
    elif command -v apt-get &> /dev/null; then
        if [ "$(id -u)" -eq 0 ]; then
            apt-get update && apt-get install -y --no-install-recommends tmux
        elif command -v sudo &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y --no-install-recommends tmux
        fi
    fi

    if command -v tmux &> /dev/null; then
        version="$(tmux -V 2>/dev/null || true)"
        major="$(printf '%s\n' "$version" | sed -E 's/^tmux ([0-9]+).*/\1/')"
        if [ "$major" -ge 3 ] 2>/dev/null; then
            echo "✅ Claude Code experimental runtime dependency installed: $version"
        else
            echo "⚠️  Claude Code experimental runtime dependency ${version:-unknown} is still below 3.x. Claude Code provider will fail until tmux is upgraded."
        fi
    else
        echo "⚠️  Claude Code experimental runtime dependency is still missing. Claude Code provider will fail until tmux is installed."
    fi
}

ensure_tmux_for_claude_code

# Always update agent-browser to latest on startup so browser automation stays current.
echo "📦 Updating agent-browser to latest..."
npm install -g agent-browser@latest 2>&1 | tail -3
echo "✅ agent-browser updated: $(agent-browser --version 2>/dev/null || echo 'version unknown')"

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

# Kill all leftover agent-browser daemon processes from previous runs.
# These accumulate as zombies when Chrome crashes — the daemon stays alive with a
# dead CDP connection. We kill them all at startup so the new server gets a clean slate.
ZOMBIE_COUNT=$(pgrep -f 'agent-browser-darwin-arm64' 2>/dev/null | wc -l | tr -d ' ')
if [ "$ZOMBIE_COUNT" -gt 0 ]; then
    echo "🧹 Killing $ZOMBIE_COUNT leftover agent-browser daemon(s) from previous run..."
    pkill -9 -f 'agent-browser-darwin-arm64' 2>/dev/null || true
    echo "✅ agent-browser daemons cleared"
else
    echo "✅ No leftover agent-browser daemons"
fi

# Kill orphaned Chrome for Testing processes (children of killed daemons that survived).
# These consume hundreds of MB each and cause OOM kills on new Chrome launches.
CHROME_COUNT=$(pgrep -f 'Google Chrome for Testing' 2>/dev/null | wc -l | tr -d ' ')
if [ "$CHROME_COUNT" -gt 0 ]; then
    echo "🧹 Killing $CHROME_COUNT orphaned Chrome for Testing process(es)..."
    pkill -9 -f 'Google Chrome for Testing' 2>/dev/null || true
    echo "✅ Chrome for Testing processes cleared"
else
    echo "✅ No orphaned Chrome for Testing processes"
fi

# Clean up stale agent-browser runtime state (dead PID/socket files)
# Prevents "CDP response channel closed" errors from leftover state.
for ab_dir in "$HOME/.agent-browser" "/tmp/.agent-browser"; do
    if [ -d "$ab_dir" ]; then
        for pidfile in "$ab_dir"/*.pid; do
            [ -f "$pidfile" ] || continue
            ab_pid=$(cat "$pidfile" 2>/dev/null | tr -d '[:space:]')
            if [ -n "$ab_pid" ] && ! kill -0 "$ab_pid" 2>/dev/null; then
                base="${pidfile%.pid}"
                echo "🧹 Cleaning stale agent-browser state: PID $ab_pid ($(basename "$pidfile"))"
                rm -f "$pidfile" "${base}.sock" "${base}.stream" "${base}.engine" "${base}.version"
            fi
        done
    fi
done

# Run the server with all the enhanced configuration
echo "🚀 Starting server with 'go run'..."

if [ "$BACKGROUND_MODE" = true ]; then
    # Background mode: run in background and capture PID
    echo "🔄 Starting server in background mode..."
    nohup go run main.go server \
        --port "$AGENT_PORT" \
        --log-level debug \
        --debug \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 500 \
        --mcp-config "configs/mcp_servers_clean.json" >> "$LOG_PATH" 2>&1 &
    
    SERVER_PID=$!
    echo "✅ Server started in background (PID: $SERVER_PID)"
    echo "📝 Logs are being written to: $LOG_PATH"
    echo "🌐 Agent API URL: $MCP_AGENT_SERVER_URL"

    if [ "$WITH_FRONTEND" = true ]; then
        wait_for_agent_health || { stop_native_workspace; exit 1; }
        start_frontend_dev || { stop_native_workspace; exit 1; }
        start_electron || { stop_frontend_dev; stop_native_workspace; exit 1; }
    else
        sleep 3
        if ! kill -0 $SERVER_PID 2>/dev/null; then
            echo "❌ Error: Server process died immediately. Check logs: $LOG_PATH"
            tail -20 "$LOG_PATH"
            if [ "$WITH_WORKSPACE" = true ]; then
                stop_native_workspace
            fi
            exit 1
        fi
        if port_in_use "$AGENT_PORT"; then
            echo "✅ Server is running and listening on port $AGENT_PORT"
        else
            echo "⚠️  Warning: Server process is running but not listening on port $AGENT_PORT yet"
            echo "   Check logs: $LOG_PATH"
        fi
    fi

    echo "🛑 To stop the server, run: kill $SERVER_PID"
    if [ "$WITH_WORKSPACE" = true ]; then
        echo "✅ Native workspace is running in background (PID: $WORKSPACE_PID)"
        echo "📝 Workspace logs are being written to: $WORKSPACE_LOG_PATH"
        echo "🌐 Workspace health: ${WORKSPACE_API_URL%/}/health"
    fi
    if [ "$WITH_FRONTEND" = true ]; then
        echo "✅ Vite dev server is running in background (PID: $FRONTEND_PID)"
        echo "📝 Frontend logs: $FRONTEND_LOG_PATH"
        echo "✅ Electron is running in background (PID: $ELECTRON_PID)"
        echo "📝 Electron logs: $ELECTRON_LOG_PATH"
        echo "🛑 To stop all, run: kill $SERVER_PID $FRONTEND_PID $ELECTRON_PID${WORKSPACE_PID:+ $WORKSPACE_PID}"
    fi
elif [ "$WITH_FRONTEND" = true ]; then
    # Foreground + frontend: detach server so we can start frontend after it's healthy,
    # then tail the server log so the user still sees server output.
    echo "🔄 Starting server in foreground mode (with frontend)..."
    echo "   Agent API URL: $MCP_AGENT_SERVER_URL"
    nohup go run main.go server \
        --port "$AGENT_PORT" \
        --log-level debug \
        --debug \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 500 \
        --mcp-config "configs/mcp_servers_clean.json" >> "$LOG_PATH" 2>&1 &

    SERVER_PID=$!
    wait_for_agent_health || exit 1
    start_frontend_dev || exit 1
    start_electron || exit 1

    echo ""
    echo "✅ All services running:"
    echo "   - Agent server (PID: $SERVER_PID) — $MCP_AGENT_SERVER_URL"
    echo "   - Vite (PID: $FRONTEND_PID) — http://127.0.0.1:${FRONTEND_PORT}"
    echo "   - Electron (PID: $ELECTRON_PID)"
    echo "   Logs: $LOG_PATH (server), $FRONTEND_LOG_PATH (vite), $ELECTRON_LOG_PATH (electron)"
    echo "   Press Ctrl+C to stop all."
    echo ""
    wait "$SERVER_PID"
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
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 500 \
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
