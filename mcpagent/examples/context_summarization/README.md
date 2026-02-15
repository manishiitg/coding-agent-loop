# Context Summarization Example

This example demonstrates **Context Summarization**, a feature that automatically summarizes old conversation history when max turns is reached. This helps manage long conversations by reducing context size while preserving essential information.

## Complex Multi-Server Task

This example includes a complex task that uses multiple MCP servers together:
- **fetch**: Retrieves information from the web
- **sequential-thinking**: Analyzes and structures information step-by-step
- **memory**: Stores insights and findings for later retrieval
- **filesystem**: Creates documents and project structures

**Note**: context7 server was removed from the example as it can hang during connection. You can add it back if needed, but the example works fine without it.

The task requires many turns to complete, making it perfect for testing context summarization.

## What is Context Summarization?

Context summarization is part of the "Summarize When Needed" pattern from context engineering best practices. It:

1. **Triggers at max turns**: When the conversation reaches the maximum number of turns, summarization is automatically triggered
2. **Summarizes old history**: Older conversation messages are summarized using an LLM call
3. **Keeps recent context**: The last N messages (default: 8) are kept intact to maintain recent context
4. **Preserves tool interactions**: Ensures tool call/response pairs stay together (never splits them)
5. **Reduces context size**: Replaces many old messages with a single summary message

This pattern is inspired by [Manus's context engineering approach](https://rlancemartin.github.io/2025/10/15/manus/) and similar implementations in production agent systems.

## How It Works

1. **Conversation progresses**: Agent and user exchange messages, potentially using tools
2. **Max turns reached**: When the conversation hits the max turns limit (default: 25, set to 5 in this example for testing)
3. **Summarization triggered**:
   - Old messages (everything except the last N messages) are extracted
   - An LLM call generates a summary of the old conversation
   - The summary preserves key decisions, constraints, file paths, errors, and TODOs
4. **Context rebuilt**:
   - Original system prompt (preserved)
   - Summary message (new, as user message)
   - Recent messages (last N messages, unchanged)
   - Final user message asking for final answer
5. **Final answer**: Agent generates final response with reduced context

## Key Features

- ✅ **Automatic**: Triggers automatically when max turns is reached
- ✅ **Safe splitting**: Never breaks tool call/response pairs
- ✅ **Configurable**: Adjust how many recent messages to keep
- ✅ **Observable**: Emits events for monitoring (ContextSummarizationStarted, ContextSummarizationCompleted, ContextSummarizationError)
- ✅ **Preserves context**: Summary includes key information needed to continue

## Setup

### Prerequisites

1. **Docker** must be installed and running (required for filesystem and fetch servers)
2. **Node.js/npx** must be installed (required for memory and sequential-thinking servers)

### Configuration

1. **Create required directories**:
   ```bash
   mkdir -p projects
   ```
   - The filesystem server uses `./projects` folder (relative to where you run the example)
   - **Important**: Docker bind mounts require the source directory to exist, otherwise the filesystem server will hang
   - The `projects` directory will be created automatically if you run the example, but it's safer to create it first

2. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

3. Install dependencies:
   ```bash
   go mod tidy
   ```

4. Run the example:
   ```bash
   go run main.go
   ```

   Or with custom question:
   ```bash
   go run main.go mcp_servers.json "Your question here"
   ```

### MCP Servers Used

- **filesystem**: Docker-based file system operations (uses local `./projects` folder)
- **fetch**: Docker-based web fetching
- **memory**: NPM-based memory storage (uses local `./memory.jsonl` file)
- **sequential-thinking**: NPM-based sequential thinking analysis

## Configuration

The example is configured with:

- **Max turns**: 5 (low for testing - will trigger summarization quickly)
- **Context summarization**: Enabled
- **Keep last messages**: 8 (default - roughly 3-4 turns)
- **Temperature**: 0 (for deterministic summaries)
- **No max tokens**: Summarization LLM call uses model's default

In production, you'd typically use:
- Higher max turns (e.g., 25-50)
- Adjust `keepLastMessages` based on your needs (8-12 is typical)

## Example Output

When max turns is reached, you'll see:

1. **Logs** showing summarization process:
   ```
   INFO: Context summarization started
   INFO: Splitting messages for summarization
   INFO: Generating conversation summary
   INFO: Messages rebuilt with summary
   ```

2. **Events** emitted:
   - `ContextSummarizationStartedEvent` - When summarization begins
   - `ContextSummarizationCompletedEvent` - When summarization succeeds (includes summary text)
   - `ContextSummarizationErrorEvent` - If summarization fails

3. **Final answer** generated after summarization

## Understanding the Summary

The summary includes:
- **Key decisions and conclusions** made during the conversation
- **Important constraints or requirements** mentioned
- **File paths, tool names, and references** that were used
- **Errors or issues** encountered and how they were resolved
- **Open tasks or TODOs** that still need to be addressed
- **Context about offloaded tool outputs** (if any files were mentioned)

## Tool Call/Response Integrity

The implementation ensures that tool call/response pairs are never split:
- If a tool response is in the "keep" section, its tool call is also kept
- If a tool call is in the "old" section, all its tool responses are also in the old section
- The split point is automatically adjusted to maintain integrity

## Logs

Check the following log files:

- `logs/context-summarization.log` - Agent operations, summarization process
- `logs/llm.log` - LLM API calls, including summarization LLM call

## Related Features

- **Context Offloading**: Handles large tool outputs (see `examples/offload_context/`)
- **Max Turns**: Controls when summarization triggers
- **Dynamic Context Reduction**: Future enhancement for compacting stale results (see main README)

## References

- [Manus Context Engineering](https://rlancemartin.github.io/2025/10/15/manus/)
- Main README: Context Offloading and Pending: Dynamic Context Reduction sections

