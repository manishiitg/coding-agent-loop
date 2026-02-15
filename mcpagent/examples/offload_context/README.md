# Context Offloading Example

This example demonstrates **Context Offloading**, a pattern where large tool outputs are automatically saved to the filesystem instead of being kept in the LLM's context window. This prevents context window overflow and reduces token costs while enabling efficient on-demand data access.

## What is Context Offloading?

Context offloading is a context engineering strategy that:

1. **Stores tool results externally**: Large tool outputs are saved to the filesystem (not in context)
2. **Uses compact references**: The LLM receives a file path + preview instead of full content
3. **Enables on-demand access**: Virtual tools allow the agent to read, search, or query files when needed
4. **Saves tokens**: Prevents context window overflow and reduces attention budget depletion

This pattern is inspired by [Manus's context engineering approach](https://rlancemartin.github.io/2025/10/15/manus/) and similar implementations in Claude Code and other production agents.

## How It Works

1. **Detection**: After tool execution, the agent checks output size using token counting
2. **Offloading**: If output exceeds threshold (default: 10,000 tokens):
   - Full content is saved to `tool_output_folder/{session-id}/tool_YYYYMMDD_HHMMSS_toolname.ext`
   - Original output is replaced with a compact reference (file path + preview)
3. **Notification**: The LLM receives a message with:
   - File path where data is saved
   - Preview (first 50% of threshold characters)
   - Instructions for using virtual tools
4. **On-Demand Access**: The LLM can use virtual tools to access data incrementally:
   - `read_large_output` - Read specific character ranges
   - `search_large_output` - Search for patterns using ripgrep
   - `query_large_output` - Execute jq queries on JSON files

## Key Benefits

- ✅ **Prevents context window overflow**: Large outputs don't consume context tokens
- ✅ **Reduces token costs**: Only preview and file path are in context, not full content
- ✅ **Maintains performance**: LLM attention budget isn't depleted by large payloads
- ✅ **Enables efficient exploration**: Agent can access data incrementally as needed
- ✅ **Session-based organization**: Files are organized by conversation session ID

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Run the example:
   ```bash
   go run main.go
   ```

4. Or with custom questions:
   ```bash
   go run main.go mcp_servers.json "Search for comprehensive React documentation"
   ```

## Example Usage

The example demonstrates automatic context offloading:

### Scenario 1: Large Search Results

```bash
go run main.go mcp_servers.json "Search for comprehensive information about React library documentation and best practices"
```

**What happens:**
1. Agent calls search tool (e.g., `get_library_docs`)
2. Tool returns large result (>10k tokens)
3. System automatically:
   - Saves full result to `tool_output_folder/{session-id}/tool_YYYYMMDD_HHMMSS_get_library_docs.json`
   - Replaces output with compact reference: file path + preview
   - LLM receives instructions for using virtual tools
4. LLM can then use `read_large_output`, `search_large_output`, or `query_large_output` to access specific parts

### Scenario 2: Incremental Data Access

After a large output is offloaded, the LLM can:

**Read specific ranges:**
```json
{
  "tool": "read_large_output",
  "args": {
    "filename": "tool_20250727_093045_get_library_docs.json",
    "start": 0,
    "end": 5000
  }
}
```

**Search for patterns:**
```json
{
  "tool": "search_large_output",
  "args": {
    "filename": "tool_20250727_093045_get_library_docs.json",
    "pattern": "useState|useEffect",
    "case_sensitive": false,
    "max_results": 50
  }
}
```

**Query JSON data:**
```json
{
  "tool": "query_large_output",
  "args": {
    "filename": "tool_20250727_093045_get_library_docs.json",
    "query": ".sections[] | select(.title | contains(\"Hooks\"))",
    "compact": true
  }
}
```

## Virtual Tools

### 1. `read_large_output`

Reads a specific range of characters from an offloaded file.

**Parameters:**
- `filename` (string): Name of the saved file
- `start` (int): Starting character position (1-based)
- `end` (int): Ending character position (inclusive)

**Use Case:** Reading file content incrementally (pagination).

### 2. `search_large_output`

Searches for regex patterns within an offloaded file using `ripgrep` (rg).

**Parameters:**
- `filename` (string): Name of the saved file
- `pattern` (string): Regex pattern to search for
- `case_sensitive` (bool, optional): Whether search is case-sensitive (default: false)
- `max_results` (int, optional): Maximum number of results (default: 50)

**Use Case:** Finding specific keywords, error messages, or patterns in large text files.

### 3. `query_large_output`

Executes `jq` queries on JSON files.

**Parameters:**
- `filename` (string): Name of the saved JSON file
- `query` (string): jq query expression
- `compact` (bool, optional): Output compact JSON format (default: false)
- `raw` (bool, optional): Output raw string values (default: false)

**Use Case:** Extracting specific fields or filtering data from large JSON responses.

## Configuration

### Agent Options

```go
agent, err := mcpagent.NewAgent(
    ctx,
    llmModel,
    configPath,
    // Enable context offloading (enabled by default)
    mcpagent.WithContextOffloading(true),
    
    // Optional: Set custom threshold (default: 10000 tokens)
    mcpagent.WithLargeOutputThreshold(10000),
)
```

### Threshold Selection

The default threshold (10,000 tokens, roughly ~40,000 characters) works well for most use cases. Consider adjusting based on:

- **Model context window**: Larger windows can handle more content
- **Token costs**: Lower thresholds save more tokens but trigger offloading more often
- **Use case**: Search results may need lower thresholds than structured data

## File Organization

Offloaded files are organized by session:

```
tool_output_folder/
└── {session-id}/
    ├── tool_20250727_093045_get_library_docs.json
    ├── tool_20250727_093112_search_results.txt
    └── tool_20250727_093145_api_response.json
```

**Session ID**: Automatically set from agent's trace ID, ensuring files are organized by conversation.

**Filename Format**: `tool_YYYYMMDD_HHMMSS_toolname.ext`
- Timestamp ensures uniqueness
- Tool name helps identify source
- Extension (.json, .txt) based on content type

## Comparison: With vs Without Context Offloading

### Without Context Offloading

```
Tool Output: 50,000 characters
→ All 50,000 chars sent to LLM
→ Consumes ~12,500 tokens (assuming 4 chars/token)
→ Depletes attention budget
→ May cause context window overflow
```

### With Context Offloading

```
Tool Output: 50,000 characters
→ Saved to filesystem
→ Only ~200 chars (file path + preview) sent to LLM
→ Consumes ~50 tokens
→ LLM can access data incrementally as needed
→ No context window overflow
```

**Token Savings**: ~12,450 tokens per large output (99.6% reduction)

## Related Patterns

This implementation follows the **"Offload Context"** pattern from context engineering:

1. **Store tool results externally**: Filesystem instead of context window
2. **Use compact references**: File path + preview instead of full content
3. **Enable on-demand access**: Virtual tools for incremental data access
4. **Progressive disclosure**: Load only what's needed, when needed

Similar patterns are used in:
- **Manus**: Stores tool results in filesystem, uses `glob` and `grep` for access
- **Claude Code**: Skills stored in filesystem, accessed via filesystem utilities
- **LangChain**: External memory systems for conversation history

## Troubleshooting

### "file not found" error

- Check the exact filename from the summary message
- Use full path: `tool_output_folder/{session-id}/filename.ext`
- Ensure the file was actually created (check `tool_output_folder/` directory)

### "path traversal detected" error

- Only use filenames from the summary message
- Path traversal (`../`) is blocked for security
- Files must be within the `tool_output_folder/` directory

### `ripgrep` or `jq` not found

Install required utilities:

```bash
# macOS
brew install ripgrep jq

# Linux
apt-get install ripgrep jq

# Or use package manager for your system
```

### Large output not offloaded

- Check threshold setting: `WithLargeOutputThreshold()`
- Verify handler is enabled: `WithContextOffloading(true)`
- Check output size: Must exceed threshold to trigger offloading

## Next Steps

- Explore the `tool_output_folder/` directory to see offloaded files
- Try different questions that produce large outputs
- Experiment with virtual tools to access offloaded data
- Adjust threshold to see how it affects offloading behavior
- Review logs to see offloading events

## Documentation

For more details, see:
- [Context Offloading Documentation](../../docs/large_output_handling.md)
- [Tool-Use Agent Documentation](../../docs/tool_use_agent.md)
- [Smart Routing Documentation](../../docs/smart_routing.md)

## References

- [Manus: Context Engineering](https://rlancemartin.github.io/2025/10/15/manus/) - Context engineering strategies including offload context
- [Anthropic: Context Editing](https://docs.anthropic.com/claude/docs/context-editing) - Automatic context management
- [Chroma: Context Rot Study](https://www.trychroma.com/blog/context-rot) - Performance degradation with large contexts

