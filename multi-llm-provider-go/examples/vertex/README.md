# Vertex AI Examples

Examples demonstrating how to use the Vertex AI provider for various use cases.

## Available Examples

### Simple Text Generation

A minimal example showing basic text generation.

**Location**: [`simple/`](./simple/)

**Quick Start**:
```bash
cd llm-providers/examples/vertex/simple
export VERTEX_API_KEY=your-api-key-here
go run vertex_simple.go
```

See [`simple/README.md`](./simple/README.md) for detailed instructions.

### Streaming with Multi-Tool Calls

An advanced example demonstrating parallel tool calls with streaming enabled and multi-turn conversations.

**Location**: [`streaming-tools/`](./streaming-tools/)

**Quick Start**:
```bash
cd llm-providers/examples/vertex/streaming-tools
export VERTEX_API_KEY=your-api-key-here
go run vertex_streaming_tool_calls.go
```

See [`streaming-tools/README.md`](./streaming-tools/README.md) for detailed instructions.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (OpenAI, Bedrock, Anthropic, etc.)

See the main [README.md](../../README.md) and [TESTING.md](../../docs/TESTING.md) for comprehensive usage examples.

