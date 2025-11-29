# OpenAI Examples

Examples demonstrating how to use the OpenAI provider for various use cases.

## Available Examples

### Simple Text Generation

A minimal example showing basic text generation.

**Location**: [`simple/`](./simple/)

**Quick Start**:
```bash
cd llm-providers/examples/openai/simple
export OPENAI_API_KEY=your-api-key-here
go run openai_simple.go
```

See [`simple/README.md`](./simple/README.md) for detailed instructions.

### Streaming with Multi-Tool Calls

An advanced example demonstrating parallel tool calls with streaming enabled and multi-turn conversations.

**Location**: [`streaming-tools/`](./streaming-tools/)

**Quick Start**:
```bash
cd llm-providers/examples/openai/streaming-tools
export OPENAI_API_KEY=your-api-key-here
go run openai_streaming_tool_calls.go
```

See [`streaming-tools/README.md`](./streaming-tools/README.md) for detailed instructions.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (Bedrock, Anthropic, etc.)

See the main [README.md](../../README.md) and [TESTING.md](../../docs/TESTING.md) for comprehensive usage examples.
