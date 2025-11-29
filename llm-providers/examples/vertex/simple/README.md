# Vertex AI Simple Example

A minimal example showing how to use the Vertex AI provider for basic text generation.

## Prerequisites

1. **Go 1.21+** installed
2. **Google API Key** - Get one from [Google AI Studio](https://makersuite.google.com/app/apikey)

## Setup

1. Set your Google API key:
```bash
export VERTEX_API_KEY=your-api-key-here
# Or alternatively:
export GOOGLE_API_KEY=your-api-key-here
```

2. Navigate to the examples directory:
```bash
cd llm-providers/examples/vertex/simple
```

3. Run the example:
```bash
go run vertex_simple.go
```

## What it does

- Initializes the Vertex AI provider with `gemini-2.5-flash` model
- Sends a simple greeting message
- Displays the response and token usage

## Key Features Demonstrated

- **Nil Logger and EventEmitter**: Shows that both can be `nil` - the library will use no-op implementations
- **Simple Configuration**: Minimal setup required to get started
- **Token Usage**: Displays token consumption information from the response

## More Vertex AI Examples

For an advanced example demonstrating parallel tool calls with streaming enabled, see the [`streaming-tools/`](../streaming-tools/) directory.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (OpenAI, Bedrock, Anthropic, etc.)

See the main [README.md](../../../README.md) and [TESTING.md](../../../docs/TESTING.md) for comprehensive usage examples.

