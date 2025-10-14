#!/bin/bash

# Script to run the MCP agent server with logging enabled
# This makes it easier to debug event issues by capturing all output to a log file
# AND displaying it in real-time on the console using tee

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
export LOG_FILE="logs/server_debug.log"
export TRACING_PROVIDER="console"
export LANGFUSE_DEBUG="true"
export OBSERVABILITY_DEBUG="true"
export OBSERVABILITY_ENABLED="true"

# Set agent mode to simple for better reliability
export DEEP_SEARCH_AGENT_MODE="simple"

# Set tool execution timeout to 2 minutes
export TOOL_EXECUTION_TIMEOUT="2m"

# Set MCP cache TTL to 7 days (10080 minutes)
export MCP_CACHE_TTL_MINUTES="10080"

# Set main LLM configuration
export DEEP_SEARCH_MAIN_LLM_PROVIDER="openrouter"
export DEEP_SEARCH_MAIN_LLM_MODEL="x-ai/grok-code-fast-1"
export DEEP_SEARCH_MAIN_LLM_TEMPERATURE="0.2"
export DEEP_SEARCH_MAIN_LLM_MAX_TOKENS="40000"

# Set agent provider environment variable (used by server.go)
export AGENT_PROVIDER="openrouter"
export AGENT_MODEL="x-ai/grok-code-fast-1"
# export AGENT_MODEL="deepseek/deepseek-chat-v3.1:free" 
# export AGENT_MODEL="z-ai/glm-4.5" 
# export AGENT_MODEL="x-ai/grok-code-fast-1"
# export AGENT_MODEL="openrouter/sonoma-dusk-alpha"

# Set OpenRouter fallback models
export OPENROUTER_FALLBACK_MODELS="x-ai/grok-code-fast-1,openai/gpt-5-mini"

# Set cross-model fallback configuration (if OpenRouter fails, fall back to OpenAI)
export OPENROUTER_CROSS_FALLBACK_PROVIDER="openai"
export OPENROUTER_CROSS_FALLBACK_MODELS="gpt-5-mini"

# Set available models for each provider
export BEDROCK_AVAILABLE_MODELS="global.anthropic.claude-sonnet-4-5-20250929-v1:0,us.anthropic.claude-sonnet-4-20250514-v1:0,us.anthropic.claude-3-7-sonnet-20250219-v1:0"
export OPENROUTER_AVAILABLE_MODELS="x-ai/grok-code-fast-1,x-ai/grok-4-fast"
export OPENAI_AVAILABLE_MODELS="gpt-5-mini,gpt-4.1-mini"

# Set structured output LLM to Bedrock for better JSON generation
export DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER="bedrock"
export DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL="global.anthropic.claude-sonnet-4-5-20250929-v1:0"
export DEEP_SEARCH_STRUCTURED_OUTPUT_TEMPERATURE="0.0"

# Obsidian configuration removed - now using workspace tools

# Set Memory API configuration
# Use Docker internal network URL if running in Docker, otherwise localhost
if [ -n "$DOCKER_CONTAINER" ] || [ -n "$MEMORY_API_INTERNAL_URL" ]; then
    export MEMORY_API_URL="http://memory-api:8000"
else
    export MEMORY_API_URL="http://localhost:8055"
fi

# Create logs directory if it doesn't exist
mkdir -p logs

# Truncate the log file to start fresh
echo "📝 Truncating log file for clean start..."
> "$LOG_FILE"
echo "✅ Log file truncated: $LOG_FILE"

# Add timestamp header to log file
echo "🚀 MCP Agent Server Session Started: $(date)" | tee "$LOG_FILE"
echo "=========================================" | tee -a "$LOG_FILE"
echo "Configuration:" | tee -a "$LOG_FILE"
echo "- Agent Mode: $DEEP_SEARCH_AGENT_MODE" | tee -a "$LOG_FILE"
echo "- Tool Execution Timeout: $TOOL_EXECUTION_TIMEOUT" | tee -a "$LOG_FILE"
echo "- MCP Cache TTL: $MCP_CACHE_TTL_MINUTES minutes (7 days)" | tee -a "$LOG_FILE"
echo "- Agent Provider: $AGENT_PROVIDER" | tee -a "$LOG_FILE"
echo "- Agent Model: $AGENT_MODEL" | tee -a "$LOG_FILE"
echo "- Main LLM Provider: $DEEP_SEARCH_MAIN_LLM_PROVIDER" | tee -a "$LOG_FILE"
echo "- Main LLM Model: $DEEP_SEARCH_MAIN_LLM_MODEL" | tee -a "$LOG_FILE"
echo "- Main LLM Temperature: $DEEP_SEARCH_MAIN_LLM_TEMPERATURE" | tee -a "$LOG_FILE"
echo "- OpenRouter Fallback Models: $OPENROUTER_FALLBACK_MODELS" | tee -a "$LOG_FILE"
echo "- OpenRouter Cross-Provider Fallback: $OPENROUTER_CROSS_FALLBACK_PROVIDER/$OPENROUTER_CROSS_FALLBACK_MODELS" | tee -a "$LOG_FILE"
echo "- Available Bedrock Models: $BEDROCK_AVAILABLE_MODELS" | tee -a "$LOG_FILE"
echo "- Available OpenRouter Models: $OPENROUTER_AVAILABLE_MODELS" | tee -a "$LOG_FILE"
echo "- Available OpenAI Models: $OPENAI_AVAILABLE_MODELS" | tee -a "$LOG_FILE"
echo "- Structured Output LLM: $DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER/$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL" | tee -a "$LOG_FILE"
echo "- Workspace tools: Enabled" | tee -a "$LOG_FILE"
echo "- Memory API URL: $MEMORY_API_URL" | tee -a "$LOG_FILE"
echo "=========================================" | tee -a "$LOG_FILE"
echo "" | tee -a "$LOG_FILE"

# Start the server with enhanced logging and structured output LLM
echo "🚀 Starting MCP Agent Server with enhanced logging..."
echo "📝 Log file: $LOG_FILE"
echo "🧠 Agent Mode: $DEEP_SEARCH_AGENT_MODE"
echo "⏱️  Tool Timeout: $TOOL_EXECUTION_TIMEOUT"
echo "💾 MCP Cache TTL: $MCP_CACHE_TTL_MINUTES minutes (7 days)"
echo "🤖 Agent Provider: $AGENT_PROVIDER/$AGENT_MODEL"
echo "🔧 Main LLM: $DEEP_SEARCH_MAIN_LLM_PROVIDER/$DEEP_SEARCH_MAIN_LLM_MODEL"
echo "🔄 OpenRouter Cross-Provider Fallback: $OPENROUTER_CROSS_FALLBACK_PROVIDER/$OPENROUTER_CROSS_FALLBACK_MODELS"
echo "🔧 Structured Output LLM: $DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER/$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL"
echo "📁 Workspace Tools: Enabled"
echo "🧠 Memory API: $MEMORY_API_URL"
echo "📊 Debug level: $LOG_LEVEL"

# Run the server with all the enhanced configuration and log to both file and console
# Using 'tee' to capture output to file while also displaying on console
go run main.go server \
    --log-level debug \
    --debug \
    --log-file "$LOG_FILE" \
    --db-path "./chat_history.db" \
    --provider "$DEEP_SEARCH_MAIN_LLM_PROVIDER" \
    --model "$DEEP_SEARCH_MAIN_LLM_MODEL" \
    --temperature "$DEEP_SEARCH_MAIN_LLM_TEMPERATURE" \
    --max-turns 30 \
    --mcp-config "configs/mcp_servers_clean.json" \
    --agent-mode "$DEEP_SEARCH_AGENT_MODE" \
    --structured-output-provider "$DEEP_SEARCH_STRUCTURED_OUTPUT_PROVIDER" \
    --structured-output-model "$DEEP_SEARCH_STRUCTURED_OUTPUT_MODEL" \
    --structured-output-temp "$DEEP_SEARCH_STRUCTURED_OUTPUT_TEMPERATURE" 2>&1 | tee "$LOG_FILE" 