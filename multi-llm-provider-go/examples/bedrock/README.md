# Bedrock Examples

Examples demonstrating how to use the AWS Bedrock provider for various use cases.

## Available Examples

### Simple Text Generation

A minimal example showing basic text generation.

**Location**: [`simple/`](./simple/)

**Quick Start**:
```bash
cd llm-providers/examples/bedrock/simple
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-here
export AWS_SECRET_ACCESS_KEY=your-secret-key-here
go run bedrock_simple.go
```

See [`simple/README.md`](./simple/README.md) for detailed instructions.

### Streaming with Multi-Tool Calls

An advanced example demonstrating parallel tool calls with streaming enabled and multi-turn conversations.

**Location**: [`streaming-tools/`](./streaming-tools/)

**Quick Start**:
```bash
cd llm-providers/examples/bedrock/streaming-tools
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-here
export AWS_SECRET_ACCESS_KEY=your-secret-key-here
go run bedrock_streaming_tool_calls.go
```

See [`streaming-tools/README.md`](./streaming-tools/README.md) for detailed instructions.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (OpenAI, Vertex AI, Anthropic, etc.)

See the main [README.md](../../README.md) and [TESTING.md](../../docs/TESTING.md) for comprehensive usage examples.

