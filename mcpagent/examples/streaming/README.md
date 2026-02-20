# Streaming MCP Agent Example

This example demonstrates how to use the MCP Agent with **streaming enabled** to receive LLM text responses in real-time as they are generated.

## Features

- **Real-time text streaming**: See LLM responses appear token-by-token as they're generated
- **Event-based streaming**: Subscribe to streaming events via the event system
- **Callback-based streaming**: Alternative approach using direct callbacks
- **Tool call handling**: Tool calls are processed normally (not streamed)

## Setup

1. Create `.env` file with your OpenAI API key:
   ```bash
   OPENAI_API_KEY=your-api-key-here
   ```

2. Run from the example directory:
   ```bash
   cd examples/streaming
   go run main.go
   ```

## Usage

```bash
# Default question (creative story)
go run main.go

# Custom question
go run main.go mcp_servers.json "Explain quantum computing in simple terms"

# With custom config path
go run main.go custom_config.json "Your question here"
```

## How It Works

### Streaming Architecture

1. **Enable Streaming**: Use `WithStreaming(true)` when creating the agent
2. **Subscribe to Events**: Use `agent.SubscribeToEvents()` to receive streaming events
3. **Process Chunks**: Handle `StreamingChunkEvent` events for text fragments
4. **Tool Calls**: Processed normally (no streaming events)

### Two Approaches

#### Option 1: Event Subscription (Default in this example)

```go
agent, err := mcpagent.NewAgent(
    ctx,
    llmModel,
    configPath,
    mcpagent.WithStreaming(true),
)

// Subscribe to events
eventChan, unsubscribe, _ := agent.SubscribeToEvents(ctx)
defer unsubscribe()

// Handle streaming events
for event := range eventChan {
    if event.Data.GetEventType() == events.StreamingChunk {
        chunkEvent := event.Data.(*events.StreamingChunkEvent)
        if !chunkEvent.IsToolCall {
            fmt.Print(chunkEvent.Content) // Print as it arrives
        }
    }
}
```

#### Option 2: Streaming Callback (Simpler)

```go
agent, err := mcpagent.NewAgent(
    ctx,
    llmModel,
    configPath,
    mcpagent.WithStreaming(true),
    mcpagent.WithStreamingCallback(func(chunk llmtypes.StreamChunk) {
        if chunk.Type == llmtypes.StreamChunkTypeContent {
            fmt.Print(chunk.Content) // Print as it arrives
        }
    }),
)
```

### Streaming Events

The agent emits the following streaming events:

- **`StreamingStartEvent`**: Emitted when streaming begins
- **`StreamingChunkEvent`**: Emitted for each text fragment (content chunks only)
- **`StreamingEndEvent`**: Emitted when streaming completes

**Note**: Tool calls are **not** streamed - they are processed normally after the LLM response completes.

## What Gets Streamed

✅ **Streamed**:
- Text content fragments (token-by-token)

❌ **Not Streamed** (processed normally):
- Tool calls
- Tool execution results
- Final response assembly

## Example Output

```text
Question: Write a short story about a robot learning to paint.

Streaming response (text appears as it's generated):
---
Once upon a time, in a small workshop filled with canvases and paintbrushes, there lived a robot named Artie...
[Streamed 10 chunks so far...]
...who had always dreamed of creating something beautiful. Day after day, Artie watched the human artists...
[Streamed 20 chunks so far...]
...and slowly began to understand the magic of color and form.

=== Streaming Complete ===
Total chunks: 45
Total tokens: 234
Duration: 3.2s
========================

=== Final Complete Response ===
Once upon a time, in a small workshop filled with canvases and paintbrushes, there lived a robot named Artie who had always dreamed of creating something beautiful. Day after day, Artie watched the human artists and slowly began to understand the magic of color and form.
===============================

✅ Streamed content matches final response!
```

## Configuration

Edit `mcp_servers.json` to add/configure MCP servers. The example uses the same configuration as the basic example.

## Requirements

- Go 1.24.4+
- Node.js (for npx-based MCP servers)
- OpenAI API key in `.env` file

## Differences from Basic Example

| Feature | Basic Example | Streaming Example |
|---------|--------------|-------------------|
| Response | Complete at end | Streamed in real-time |
| User Experience | Wait for full response | See text as it's generated |
| Events | Standard LLM events | Streaming events + LLM events |
| Tool Calls | Normal processing | Normal processing (same) |

## Use Cases

- **Interactive chat interfaces**: Show responses as they're generated
- **Long-form content**: Provide immediate feedback for lengthy responses
- **User engagement**: Better UX with progressive text display
- **Debugging**: See exactly when and how text is generated

