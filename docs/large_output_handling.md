# Large Tool Output Handling

The MCP Agent includes a sophisticated system for handling large tool outputs (e.g., massive JSON files, long logs, or extensive search results) that would otherwise exceed the LLM's context window.

## 🎯 The Problem

When a tool returns a very large result (e.g., 100,000+ tokens):
1.  **Context Overflow**: The LLM's context window is exceeded, causing the request to fail.
2.  **Cost**: Processing massive inputs is expensive.
3.  **Performance**: Large inputs slow down generation and can confuse the model.

## 🛠️ The Solution

The agent automatically intercepts large outputs, saves them to a file, and provides the LLM with specialized "Virtual Tools" to inspect the data efficiently.

### Process Flow

1.  **Detection**: After every tool execution, the agent checks the output size.
    *   It uses **Token Counting** (via `tiktoken`) for accurate estimation.
    *   Default threshold: **20,000 characters** (configurable).
2.  **Interception**: If the output is too large:
    *   The full content is saved to a file in `tool_output_folder/{session_id}/`.
    *   The original output in the conversation history is **replaced** with a summary message.
3.  **Notification**: The summary message contains:
    *   The file path where data is saved.
    *   A preview (first 50% of the threshold characters).
    *   Instructions on how to use virtual tools to read the rest.
4.  **Inspection**: The LLM uses virtual tools to read, search, or query the file as needed.

## 🧰 Virtual Tools

These tools are automatically enabled when Large Output Handling is active.

### 1. `read_large_output`
Reads a specific range of characters from the file.
- **Params**: `filename`, `start`, `end`
- **Use Case**: Reading a file chunk by chunk (pagination).

### 2. `search_large_output`
Searches for regex patterns within the file using `ripgrep` (rg).
- **Params**: `filename`, `pattern`, `case_sensitive`, `max_results`
- **Use Case**: Finding specific error logs or keywords in a massive text file.

### 3. `query_large_output`
Executes `jq` queries on JSON files.
- **Params**: `filename`, `query`
- **Use Case**: Extracting specific fields from a large JSON response (e.g., `.items[0].name`).

## ⚙️ Configuration

You can configure the handler using functional options when creating the agent.

```go
agent, err := mcpagent.NewAgent(..., 
    // Enable Large Output Handling (enabled by default)
    mcpagent.WithLargeOutputHandling(true),
    
    // Set the threshold (in characters)
    mcpagent.WithLargeOutputThreshold(20000),
    
    // Set the output folder
    mcpagent.WithToolOutputFolder("./my_tool_outputs"),
)
```

## 🧩 Implementation Details

### `ToolOutputHandler`
Located in `internal/utils/tool_output_handler.go`.
- Manages file writing and naming (timestamped filenames).
- Handles token counting logic (`IsLargeToolOutputWithModel`).
- Generates the summary message with instructions.

### `Virtual Tools`
Located in `pkg/mcpagent/large_output_virtual_tools.go`.
- Implements the actual logic for reading, searching (`ripgrep`), and querying (`jq`).
- Includes security validation to prevent path traversal.

## 🔒 Security

- **Path Validation**: All file operations are restricted to the configured output folder. Path traversal (`../`) is blocked.
- **Command Injection**: `ripgrep` and `jq` are executed using `exec.Command` with separate arguments, preventing shell injection attacks.
