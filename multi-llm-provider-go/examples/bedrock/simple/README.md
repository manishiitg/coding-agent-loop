# Bedrock Simple Example

A minimal example showing how to use the AWS Bedrock provider for basic text generation.

## Prerequisites

1. **Go 1.21+** installed
2. **AWS Credentials** - You need AWS access keys with Bedrock permissions

## Setup

1. Set your AWS credentials:
```bash
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-here
export AWS_SECRET_ACCESS_KEY=your-secret-key-here
```

2. Navigate to the examples directory:
```bash
cd llm-providers/examples/bedrock/simple
```

3. Run the example:
```bash
go run bedrock_simple.go
```

## What it does

- Initializes the Bedrock provider with `us.anthropic.claude-3-haiku-20240307-v1:0` model
- Sends a simple greeting message
- Displays the response and token usage

## Key Features Demonstrated

- **Nil Logger and EventEmitter**: Shows that both can be `nil` - the library will use no-op implementations
- **Simple Configuration**: Minimal setup required to get started
- **Token Usage**: Displays token consumption information from the response
- **AWS Region Configuration**: Shows how to configure the AWS region (defaults to us-east-1)

## More Bedrock Examples

For an advanced example demonstrating parallel tool calls with streaming enabled, see the [`streaming-tools/`](../streaming-tools/) directory.

## More Examples

For more advanced examples including:
- Structured outputs
- Multiple providers (OpenAI, Vertex AI, Anthropic, etc.)

See the main [README.md](../../../README.md) and [TESTING.md](../../../docs/TESTING.md) for comprehensive usage examples.

