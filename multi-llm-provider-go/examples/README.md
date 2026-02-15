# Examples

Simple examples demonstrating how to use the `llm-providers` library.

## Available Examples

### OpenAI

Examples showing how to use the OpenAI provider for various use cases.

**Location**: [`openai/`](./openai/)

**Available Examples**:
- **Simple Text Generation**: [`openai/simple/`](./openai/simple/) - Basic text generation example
- **Streaming with Multi-Tool Calls**: [`openai/streaming-tools/`](./openai/streaming-tools/) - Advanced parallel tool calls with streaming

**Quick Start**:
```bash
# Simple example
cd llm-providers/examples/openai/simple
export OPENAI_API_KEY=your-api-key-here
go run openai_simple.go

# Streaming with tool calls example
cd llm-providers/examples/openai/streaming-tools
export OPENAI_API_KEY=your-api-key-here
go run openai_streaming_tool_calls.go
```

See [`openai/README.md`](./openai/README.md) for detailed instructions.

### Vertex AI

Examples showing how to use the Vertex AI provider for various use cases.

**Location**: [`vertex/`](./vertex/)

**Available Examples**:
- **Simple Text Generation**: [`vertex/simple/`](./vertex/simple/) - Basic text generation example
- **Streaming with Multi-Tool Calls**: [`vertex/streaming-tools/`](./vertex/streaming-tools/) - Advanced parallel tool calls with streaming

**Quick Start**:
```bash
# Simple example
cd llm-providers/examples/vertex/simple
export VERTEX_API_KEY=your-api-key-here
go run vertex_simple.go

# Streaming with tool calls example
cd llm-providers/examples/vertex/streaming-tools
export VERTEX_API_KEY=your-api-key-here
go run vertex_streaming_tool_calls.go
```

See [`vertex/README.md`](./vertex/README.md) for detailed instructions.

### Bedrock

Examples showing how to use the AWS Bedrock provider for various use cases.

**Location**: [`bedrock/`](./bedrock/)

**Available Examples**:
- **Simple Text Generation**: [`bedrock/simple/`](./bedrock/simple/) - Basic text generation example
- **Streaming with Multi-Tool Calls**: [`bedrock/streaming-tools/`](./bedrock/streaming-tools/) - Advanced parallel tool calls with streaming

**Quick Start**:
```bash
# Simple example
cd llm-providers/examples/bedrock/simple
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-here
export AWS_SECRET_ACCESS_KEY=your-secret-key-here
go run bedrock_simple.go

# Streaming with tool calls example
cd llm-providers/examples/bedrock/streaming-tools
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-here
export AWS_SECRET_ACCESS_KEY=your-secret-key-here
go run bedrock_streaming_tool_calls.go
```

See [`bedrock/README.md`](./bedrock/README.md) for detailed instructions.

### Custom Logger

An example demonstrating how to implement and use a custom logger that writes logs to a file.

**Location**: [`custom_logger/`](./custom_logger/)

**Quick Start**:
```bash
cd llm-providers/examples/custom_logger
export OPENAI_API_KEY=your-api-key-here
go run custom_logger.go
```

After running, check `test.log` for detailed logs.

See [`custom_logger/README.md`](./custom_logger/README.md) for detailed instructions.

### Custom Event Emitter

An example demonstrating how to implement and use a custom event emitter that captures LLM events to a file.

**Location**: [`custom_event_emitter/`](./custom_event_emitter/)

**Quick Start**:
```bash
cd llm-providers/examples/custom_event_emitter
export OPENAI_API_KEY=your-api-key-here
go run custom_event_emitter.go
```

After running, check `events.log` for all captured LLM events in JSON format.

See [`custom_event_emitter/README.md`](./custom_event_emitter/README.md) for detailed instructions.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (Bedrock, Anthropic, etc.)

See the main [README.md](../README.md) and [TESTING.md](../docs/TESTING.md) for comprehensive usage examples.
