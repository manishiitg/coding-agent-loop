# OpenAI Streaming with Multi-Tool Calls Example

A comprehensive example demonstrating how to use OpenAI with multiple parallel tool calls and streaming enabled.

## Prerequisites

1. **Go 1.21+** installed
2. **OpenAI API Key** - Get one from [OpenAI Platform](https://platform.openai.com/api-keys)

## Setup

1. Set your OpenAI API key:
```bash
export OPENAI_API_KEY=your-api-key-here
```

2. Navigate to the examples directory:
```bash
cd llm-providers/examples/openai
```

3. Run the example:
```bash
go run openai_streaming_tool_calls.go
```

## What it does

- Initializes the OpenAI provider with `gpt-4o-mini` model
- Defines three tools: `read_file`, `get_weather`, and `calculate`
- **Turn 1**: Sends a request that triggers multiple parallel tool calls
- Enables streaming to receive real-time tool call chunks
- Validates that streamed tool calls match the final response
- **Turn 2**: Simulates tool execution and creates tool responses
- Sends tool responses back to the LLM in a multi-turn conversation
- Shows how the LLM uses tool results to generate a comprehensive response
- Displays streaming statistics and token usage for both turns

## Key Features Demonstrated

- **Multiple Parallel Tool Calls**: Shows how OpenAI can call multiple tools simultaneously in a single response
- **Streaming**: Demonstrates real-time streaming of tool calls as they're generated
- **Stream Chunk Processing**: Handles both content chunks and tool call chunks from the stream
- **Response Validation**: Verifies that streamed responses match final responses
- **Goroutine Pattern**: Uses a separate goroutine to process streaming chunks without blocking

## Expected Output

When you run this example, you should see output similar to:

```
Initializing OpenAI provider with model: gpt-4o-mini
🚀 Making request with streaming and multiple tools...
   📦 Streamed tool call 1: read_file (ID: call_abc123, Args: {"path":"go.mod"})
   📦 Streamed tool call 2: get_weather (ID: call_def456, Args: {"location":"San Francisco"})
   📦 Streamed tool call 3: calculate (ID: call_ghi789, Args: {"expression":"25 * 17"})

✅ Request completed in 1.234s

📊 Streaming Statistics:
   Content chunks received: 0
   Streamed tool calls: 3
   Final tool calls: 3
   Streamed content length: 0 chars
   Final content length: 0 chars

📋 Streamed Tool Calls:
   1. read_file
      ID: call_abc123
      Arguments: {"path":"go.mod"}
   2. get_weather
      ID: call_def456
      Arguments: {"location":"San Francisco"}
   3. calculate
      ID: call_ghi789
      Arguments: {"expression":"25 * 17"}

📋 Final Tool Calls:
   1. read_file
      ID: call_abc123
      Arguments: {"path":"go.mod"}
   2. get_weather
      ID: call_def456
      Arguments: {"location":"San Francisco"}
   3. calculate
      ID: call_ghi789
      Arguments: {"expression":"25 * 17"}

✅ All 3 tool calls were streamed correctly!
   ✅ Tool call read_file (ID: call_abc123) matched
   ✅ Tool call get_weather (ID: call_def456) matched
   ✅ Tool call calculate (ID: call_ghi789) matched

🎯 All tool calls validated successfully!

💰 Token Usage:
   Input tokens: 245
   Output tokens: 89
   Total tokens: 334
```

## Key Concepts

### Streaming Channel

Use `WithStreamingChan()` to enable streaming. The channel receives chunks as they're generated:

```go
streamChan := make(chan llmtypes.StreamChunk, 100)
resp, err := llm.GenerateContent(ctx, messages,
    llmtypes.WithStreamingChan(streamChan),
)
```

### Chunk Types

- **`StreamChunkTypeContent`**: Text content being generated (streamed incrementally)
- **`StreamChunkTypeToolCall`**: Complete tool calls (streamed when ready)

### Parallel Tool Calls

OpenAI can call multiple tools in a single response. All tool calls are streamed when complete, allowing you to process them in real-time.

### Tool Choice

Use `WithToolChoiceString("auto")` to let the model decide which tools to use, or specify a specific tool:

```go
llmtypes.WithToolChoiceString("auto")  // Let model decide
llmtypes.WithToolChoiceString("read_file")  // Force specific tool
```

## Multi-Turn Conversation Flow

The example demonstrates the complete multi-turn conversation flow:

1. **Turn 1**: Request with tools → Receive parallel tool calls (streamed)
2. **Tool Execution**: Simulate executing tools and create tool responses
3. **Turn 2**: Send conversation history (user message + tool calls + tool responses) back to LLM
4. **LLM Response**: LLM uses tool results to generate a comprehensive response (streamed)

This pattern is essential for building agents that can:
- Make multiple tool calls in parallel
- Process tool results
- Continue conversations based on tool outputs
- Provide real-time feedback through streaming

## Related Examples

- [`openai_simple.go`](./openai_simple.go) - Basic text generation example
- See the main [README.md](../../README.md) and [TESTING.md](../../docs/TESTING.md) for more comprehensive usage examples

