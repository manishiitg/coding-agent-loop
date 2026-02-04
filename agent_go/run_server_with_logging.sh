#!/bin/bash
# Script to run the MCP agent server with logging enabled
# This makes it easier to debug event issues by capturing all output to a log file
# Terminal output is suppressed as requested.

# Get script directory first (needed for both test and server modes)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check for test-connections mode
TEST_CONNECTIONS=false
if [[ "$1" == "--test-connections" || "$1" == "--test-mcp" || "$1" == "-t" ]]; then
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
    MCP_CONFIG="${2:-configs/mcp_servers_clean.json}"
    
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

# Check if background mode is requested
BACKGROUND_MODE=false
if [[ "$1" == "--background" || "$1" == "-b" ]]; then
    BACKGROUND_MODE=true
    echo "🚀 Starting MCP Agent Server with Logging (Background Mode)"
else
    echo "🚀 Starting MCP Agent Server with Logging"
fi
echo "========================================="

# Kill any existing server on port 8000
echo "🔪 Checking for existing server on port 8000..."
if lsof -ti:8000 > /dev/null 2>&1; then
    echo "⚠️  Found existing server on port 8000, killing it..."
    lsof -ti:8000 | xargs kill -9
    sleep 2
    echo "✅ Existing server killed"
else
    echo "✅ No existing server found on port 8000"
fi

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

# Change to script directory to ensure relative paths work correctly
cd "$SCRIPT_DIR" || {
    echo "❌ Error: Failed to change to script directory: $SCRIPT_DIR"
    exit 1
}
echo "📁 Working directory: $(pwd)"

# Set agent mode to simple for better reliability
export DEEP_SEARCH_AGENT_MODE="simple"

# Enable split execution learning feature (separates learning reading from execution)
export SPLIT_EXECUTION_LEARNING="true"

# Set tool execution timeout to 15 minutes
export TOOL_EXECUTION_TIMEOUT="15m"

# Set MCP cache TTL to 7 days (10080 minutes)
export MCP_CACHE_TTL_MINUTES="10080"

# Workspace semantic search configuration (disabled by default - requires Qdrant)
export WORKSPACE_ENABLE_SEMANTIC_SEARCH="${WORKSPACE_ENABLE_SEMANTIC_SEARCH:-false}"
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
export ENABLE_CONTEXT_EDITING="false"  # Enable context editing (default: true)
export CONTEXT_EDITING_THRESHOLD="10000"  # Compact outputs larger than 10k tokens (default: 10000)
export CONTEXT_EDITING_TURN_THRESHOLD="20"  # Compact outputs older than 20 turns (default: 20)

# Set main LLM configuration (uses Bedrock with AWS credentials from environment)
# Note: Frontend Published LLMs override this for actual agent execution
export DEEP_SEARCH_MAIN_LLM_PROVIDER="bedrock"
export DEEP_SEARCH_MAIN_LLM_MODEL="global.anthropic.claude-sonnet-4-5-20250929-v1:0"
export DEEP_SEARCH_MAIN_LLM_TEMPERATURE="0.0"
export DEEP_SEARCH_MAIN_LLM_MAX_TOKENS="40000"

# Set agent provider environment variable (used by server.go for internal operations)
# Note: Actual agent execution uses Published LLMs from frontend with their own API keys
export AGENT_PROVIDER="bedrock"
export AGENT_MODEL="global.anthropic.claude-sonnet-4-5-20250929-v1:0"

# Set available models for each provider
export BEDROCK_AVAILABLE_MODELS="global.anthropic.claude-sonnet-4-5-20250929-v1:0,us.anthropic.claude-sonnet-4-20250514-v1:0,us.anthropic.claude-3-7-sonnet-20250219-v1:0"
export OPENROUTER_AVAILABLE_MODELS="x-ai/grok-code-fast-1,x-ai/grok-4-fast"
export OPENAI_AVAILABLE_MODELS="gpt-5-mini,gpt-4.1-mini"

# Supported LLM providers (controls which providers appear in the UI)
export SUPPORTED_LLM_PROVIDERS="azure,anthropic"

# Set structured output LLM to Bedrock for better JSON generation
export DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER="bedrock"
export DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL="global.anthropic.claude-sonnet-4-5-20250929-v1:0"
export DEEP_SEARCH_STRUCTURED_OUTPUT_TEMPERATURE="0.0"

# Obsidian configuration removed - now using workspace tools

# Create logs directory if it doesn't exist
mkdir -p logs

# Truncate the log files to start fresh
echo "📝 Truncating log files for clean start..."
> "$LOG_PATH"
echo "✅ Server log file truncated: $LOG_PATH"
> "logs/llm_debug.log"
echo "✅ LLM log file truncated: logs/llm_debug.log"

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
echo "- Agent Mode: $DEEP_SEARCH_AGENT_MODE" >> "$LOG_PATH"
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
echo "- Structured Output LLM: $DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER/$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL" >> "$LOG_PATH"
echo "- Workspace tools: Enabled" >> "$LOG_PATH"
echo "- Workspace Semantic Search: $WORKSPACE_ENABLE_SEMANTIC_SEARCH" >> "$LOG_PATH"
echo "- Context Summarization: $ENABLE_CONTEXT_SUMMARIZATION" >> "$LOG_PATH"
echo "- Token Threshold: $TOKEN_THRESHOLD_PERCENT (70%) | Fixed: ${FIXED_TOKEN_THRESHOLD} tokens" >> "$LOG_PATH"
echo "- Keep Last Messages: $SUMMARY_KEEP_LAST_MESSAGES" >> "$LOG_PATH"
echo "- Context Editing: $ENABLE_CONTEXT_EDITING (Threshold: ${CONTEXT_EDITING_THRESHOLD} tokens, Age: ${CONTEXT_EDITING_TURN_THRESHOLD} turns)" >> "$LOG_PATH"
echo "=========================================" >> "$LOG_PATH"
echo "" >> "$LOG_PATH"

# Start the server with enhanced logging and structured output LLM
echo "🚀 Starting MCP Agent Server with enhanced logging..."
echo "📝 Log file: $LOG_PATH"
echo "🧠 Agent Mode: $DEEP_SEARCH_AGENT_MODE"
echo "🔀 Split Execution Learning: $SPLIT_EXECUTION_LEARNING"
echo "⏱️  Tool Timeout: $TOOL_EXECUTION_TIMEOUT"
echo "💾 MCP Cache TTL: $MCP_CACHE_TTL_MINUTES minutes (7 days)"
echo "📁 Workspace Tools: Enabled"
echo "🔍 Workspace Semantic Search: $WORKSPACE_ENABLE_SEMANTIC_SEARCH"
echo "📝 Context Summarization: $ENABLE_CONTEXT_SUMMARIZATION (Threshold: $TOKEN_THRESHOLD_PERCENT = 70%, Fixed: ${FIXED_TOKEN_THRESHOLD} tokens, Keep: $SUMMARY_KEEP_LAST_MESSAGES msgs)"
echo "✂️  Context Editing: $ENABLE_CONTEXT_EDITING (Threshold: ${CONTEXT_EDITING_THRESHOLD} tokens, Age: ${CONTEXT_EDITING_TURN_THRESHOLD} turns)"
echo "📊 Debug level: $LOG_LEVEL"

# Database configuration based on DATABASE_URL
DB_TYPE_FLAG="sqlite"
if [ -n "$DATABASE_URL" ]; then
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

# Run the server with all the enhanced configuration
echo "🚀 Starting server with 'go run'..."

if [ "$BACKGROUND_MODE" = true ]; then
    # Background mode: run in background and capture PID
    echo "🔄 Starting server in background mode..."
    go run main.go server \
        --log-level debug \
        --debug \
        --db-type "$DB_TYPE_FLAG" \
        --db-path "./chat_history.db" \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 50 \
        --mcp-config "configs/mcp_servers_clean.json" \
        --agent-mode "$DEEP_SEARCH_AGENT_MODE" \
        --structured-output-provider "$DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER" \
        --structured-output-model "$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL" \
        --structured-output-temp "$DEEP_SEARCH_STRUCTURED_OUTPUT_TEMPERATURE" >> "$LOG_PATH" 2>&1 &
    
    SERVER_PID=$!
    echo "✅ Server started in background (PID: $SERVER_PID)"
    echo "📝 Logs are being written to: $LOG_PATH"
    echo "🛑 To stop the server, run: kill $SERVER_PID"
    
    # Wait a moment to check if server started successfully
    sleep 3
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo "❌ Error: Server process died immediately. Check logs: $LOG_PATH"
        tail -20 "$LOG_PATH"
        exit 1
    fi
    
    # Check if server is listening on port 8000
    if lsof -ti:8000 > /dev/null 2>&1; then
        echo "✅ Server is running and listening on port 8000"
    else
        echo "⚠️  Warning: Server process is running but not listening on port 8000 yet"
        echo "   Check logs: $LOG_PATH"
    fi
else
    # Foreground mode: run in foreground with output visible
    echo "🔄 Starting server in foreground mode..."
    echo "   (Press Ctrl+C to stop)"
    echo ""
    
    go run main.go server \
        --log-level debug \
        --debug \
        --db-type "$DB_TYPE_FLAG" \
        --db-path "./chat_history.db" \
        --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
        --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
        --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
        --max-turns 50 \
        --mcp-config "configs/mcp_servers_clean.json" \
        --agent-mode "$DEEP_SEARCH_AGENT_MODE" \
        --structured-output-provider "$DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER" \
        --structured-output-model "$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL" \
        --structured-output-temp "$DEEP_SEARCH_STRUCTURED_OUTPUT_TEMPERATURE" >> "$LOG_PATH" 2>&1
    
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