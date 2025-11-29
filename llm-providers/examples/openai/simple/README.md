# OpenAI Simple Example

A minimal example showing how to use the OpenAI provider for basic text generation.

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
go run openai_simple.go
```

## What it does

- Initializes the OpenAI provider with `gpt-4.1-mini` model
- Sends a simple greeting message
- Displays the response and token usage

## Key Features Demonstrated

- **Nil Logger and EventEmitter**: Shows that both can be `nil` - the library will use no-op implementations
- **Simple Configuration**: Minimal setup required to get started
- **Token Usage**: Displays token consumption information from the response

## More OpenAI Examples

For an advanced example demonstrating parallel tool calls with streaming enabled, see the [`openai_streaming_tool_calls/`](../openai_streaming_tool_calls/) directory.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (Bedrock, Anthropic, etc.)

See the main [README.md](../../README.md) and [TESTING.md](../../docs/TESTING.md) for comprehensive usage examples.

