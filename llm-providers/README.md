# llm-providers

A Go module providing a unified interface for multiple Large Language Model (LLM) providers, including AWS Bedrock, OpenAI, Anthropic, OpenRouter, and Google Vertex AI.

## Overview

This module abstracts the differences between various LLM providers, providing a consistent API for:
- Text generation
- Tool calling
- Streaming responses
- Token usage tracking
- Structured output

## Installation

```bash
go get llm-providers
```

Or if using a local module:

```bash
go mod edit -replace llm-providers=./llm-providers
```

## Supported Providers

- **AWS Bedrock** - Claude models via Bedrock Runtime API
- **OpenAI** - GPT models (GPT-4, GPT-3.5, etc.)
- **Anthropic** - Claude models via direct API
- **OpenRouter** - Multi-provider access via OpenRouter API
- **Vertex AI** - Google Gemini models and Anthropic Claude via Vertex AI

## Quick Start

```go
package main

import (
    "context"
    "llm-providers"
    "llm-providers/interfaces"
)

func main() {
    // Initialize an LLM provider
    config := llmproviders.Config{
        Provider:    llmproviders.ProviderOpenAI,
        ModelID:     "gpt-4o",
        Temperature: 0.7,
        Logger:      yourLogger,
        EventEmitter: yourEventEmitter,
    }
    
    llm, err := llmproviders.InitializeLLM(config)
    if err != nil {
        panic(err)
    }
    
    // Generate content
    ctx := context.Background()
    response, err := llm.GenerateContent(ctx, []llmtypes.MessageContent{
        llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Hello, world!"),
    })
    if err != nil {
        panic(err)
    }
    
    fmt.Println(response.Choices[0].Content)
}
```

## Module Structure

```
llm-providers/
├── cmd/
│   └── llm-test/              # Test binary
├── pkg/
│   ├── adapters/              # Provider-specific adapters
│   │   ├── bedrock/
│   │   ├── openai/
│   │   ├── anthropic/
│   │   └── vertex/
│   └── interfaces/            # Public interfaces
├── internal/
│   └── testing/               # Test utilities
├── llmtypes/                  # Type definitions
├── providers.go               # Main provider initialization
├── events.go                  # Event definitions
└── types.go                   # Type re-exports
```

## Configuration

### Environment Variables

See `.env.example` for all available environment variables. Key variables:

- `OPENAI_API_KEY` - OpenAI API key
- `ANTHROPIC_API_KEY` - Anthropic API key
- `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` - AWS credentials for Bedrock
- `GOOGLE_API_KEY` or `VERTEX_API_KEY` - Google API key for Vertex AI
- `OPEN_ROUTER_API_KEY` - OpenRouter API key

### Provider Configuration

Each provider can be configured with:
- Model ID
- Temperature
- Max tokens
- Fallback models (for rate limiting)
- Custom options

## Testing

Build and run the test tool:

```bash
cd llm-providers
make build
./bin/llm-test --help
```

## Test Coverage

The `llm-test` tool provides comprehensive test coverage for all LLM providers. All providers use **standardized shared test functions** ensuring identical test coverage across all providers.

### Standardized Test Architecture

All providers use the same shared test functions from `internal/testing/commands/shared/test_functions.go`:
- **RunPlainTextTest** - Basic text generation
- **RunToolCallTest** - 4 standardized tool calling tests
- **RunStructuredOutputTest** - Structured JSON output validation
- **RunImageTest** - 3 standardized image understanding tests

Each provider's test files only initialize their LLM instance and call these shared functions, ensuring:
- ✅ **Consistency**: All providers run identical tests
- ✅ **Maintainability**: Fix bugs or add features once in shared code
- ✅ **Simplicity**: Provider files are minimal (just LLM initialization)
- ✅ **Testability**: Easy to verify all providers have same coverage

### Test Command Structure

Each provider supports the following test types:
- **Plain Text Generation** - Basic text generation test
- **Tool Call Tests** - 4 standardized function calling tests
- **Token Usage Tests** - Validate token usage extraction (with cache tests)
- **Structured Output Tests** - Test structured JSON outputs
- **Image Understanding Tests** - 3 standardized vision/image understanding tests

### Provider Test Coverage

All providers have **identical test coverage** using the same standardized tests:

#### Test Coverage Matrix

| Provider | Plain Text | Tool Calls | Structured Output | Image | Token Usage |
|----------|------------|------------|-------------------|-------|-------------|
| **Anthropic** | ✅ | ✅ (4 tests) | ✅ (Tool-based) | ✅ (3 tests) | ✅ (with cache) |
| **OpenAI** | ✅ | ✅ (4 tests) | ✅ (JSON Schema) | ✅ (3 tests) | ✅ (with cache) |
| **Bedrock** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) |
| **OpenRouter** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) |
| **Vertex AI** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) |

#### Anthropic (`anthropic-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `anthropic` | Basic text generation |
| Tool Calls | `anthropic-tool-call` | 4 standardized tests |
| Structured Output | `anthropic-structured-output` | Tool-based approach |
| Image Understanding | `anthropic-image` | 3 standardized tests |
| Token Usage | `anthropic-token-usage` | Simple, complex, cache tests |

**Example:**
```bash
./bin/llm-test anthropic --model claude-haiku-4-5-20251001
./bin/llm-test anthropic-tool-call
./bin/llm-test anthropic-structured-output
./bin/llm-test anthropic-image
```

#### OpenAI (`openai-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `openai` | Basic text generation |
| Tool Calls | `openai-tool-call` | 4 standardized tests |
| Structured Output | `openai-structured-output` | JSON Schema with strict mode |
| Image Understanding | `openai-image` | 3 standardized tests |
| Token Usage | `openai-token-usage` | Simple, complex, cache tests |

**Example:**
```bash
./bin/llm-test openai --model gpt-4o-mini
./bin/llm-test openai-tool-call --model gpt-4o-mini
./bin/llm-test openai-structured-output --model gpt-4o-mini
./bin/llm-test openai-image --model gpt-4o-mini
```

#### AWS Bedrock (`bedrock-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `bedrock` | Basic text generation |
| Tool Calls | `llm-tool-call` | 4 standardized tests |
| Structured Output | `bedrock-structured-output` | JSON mode with validation |
| Image Understanding | `bedrock-image` | 3 standardized tests |
| Token Usage | `bedrock-token-usage` | Simple, complex, cache tests |

**Example:**
```bash
./bin/llm-test bedrock
./bin/llm-test llm-tool-call
./bin/llm-test bedrock-structured-output
./bin/llm-test bedrock-image
```

#### OpenRouter (`openrouter-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `openrouter` | Basic text generation |
| Tool Calls | `openrouter-tool-call` | 4 standardized tests |
| Structured Output | `openrouter-structured-output` | JSON mode |
| Image Understanding | `openrouter-image` | 3 standardized tests |
| Token Usage | `openrouter-token-usage` | Simple, complex, cache tests |

**Note:** OpenRouter image tests require vision-capable models (e.g., `openai/gpt-4o-mini`)

**Example:**
```bash
./bin/llm-test openrouter --model moonshotai/kimi-k2
./bin/llm-test openrouter-tool-call --model moonshotai/kimi-k2
./bin/llm-test openrouter-structured-output --model moonshotai/kimi-k2
./bin/llm-test openrouter-image --model openai/gpt-4o-mini
```

#### Vertex AI (`vertex-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `vertex` | Basic text generation |
| Tool Calls | `vertex-tool-call` | 4 standardized tests |
| Structured Output | `vertex-structured-output` | JSON mode |
| Image Understanding | `vertex-image` | 3 standardized tests |
| Token Usage | `vertex-token-usage` | Simple, complex, cache tests |

**Example:**
```bash
./bin/llm-test vertex --model gemini-2.5-flash
./bin/llm-test vertex-tool-call
./bin/llm-test vertex-structured-output
./bin/llm-test vertex-image
```

### Standardized Test Features

All providers use the same test implementations from shared functions:

#### Plain Text Generation Tests
- Simple "Hello! Can you introduce yourself?" prompt
- Validates response generation
- Displays token usage (input, output, total, cache tokens if available)

#### Tool Call Tests (4 Standardized Tests)
All providers run the same 4 tests:
- **Test 1**: Simple tool call (`read_file` tool)
- **Test 2**: Multiple tools (model selects from `read_file` and `get_weather`)
- **Test 3**: Parallel tool calls (multiple tools in single response - `get_weather` and `get_current_time`)
- **Test 4**: Tool with no parameters (`get_server_status`)
- All tests include token usage logging and tool call validation

#### Token Usage Tests
All providers run the same tests:
- **Test 1**: Simple query validation
- **Test 2**: Complex reasoning query validation
- **Test 3**: Multi-turn conversation with cache (validates cache token extraction)
- Validates input/output/total token extraction
- Langfuse tracing integration

#### Structured Output Tests
Provider-specific approaches but same validation:
- **OpenAI**: JSON Schema with strict mode
- **Bedrock/OpenRouter/Vertex**: JSON mode
- **Anthropic**: Tool-based approach
- All validate cookie recipe schema (recipeName + ingredients array)
- Response structure validation with detailed error reporting

#### Image Understanding Tests (3 Standardized Tests)
All providers run the same 3 tests:
- **Test 1**: Basic image description ("What is in this image? Describe it in detail.")
- **Test 2**: Text extraction ("What text is written in this image? Extract all visible text.")
- **Test 3**: Complex image analysis (description, text, colors, composition, objects)
- Supports both base64 file uploads (`--image-path`) and URL-based images (`--image-url`)
- Default test image URL if no image provided
- All tests include token usage logging

### Running Tests

**Basic usage:**
```bash
# Plain text generation (all providers)
./bin/llm-test anthropic --model claude-haiku-4-5-20251001
./bin/llm-test openai --model gpt-4o-mini
./bin/llm-test bedrock
./bin/llm-test openrouter --model moonshotai/kimi-k2
./bin/llm-test vertex --model gemini-2.5-flash

# Tool call tests (all providers have same 4 tests)
./bin/llm-test anthropic-tool-call
./bin/llm-test openai-tool-call --model gpt-4o-mini
./bin/llm-test llm-tool-call  # Bedrock
./bin/llm-test openrouter-tool-call --model moonshotai/kimi-k2
./bin/llm-test vertex-tool-call

# Token usage tests (all providers have cache tests)
./bin/llm-test anthropic-token-usage --prompt "Hello world"
./bin/llm-test openai-token-usage --prompt "Hello world"
./bin/llm-test bedrock-token-usage --prompt "Hello world"
./bin/llm-test openrouter-token-usage --prompt "Hello world"
./bin/llm-test vertex-token-usage --prompt "Hello world"

# Structured output tests
./bin/llm-test anthropic-structured-output
./bin/llm-test openai-structured-output --model gpt-4o-mini
./bin/llm-test bedrock-structured-output
./bin/llm-test openrouter-structured-output --model moonshotai/kimi-k2
./bin/llm-test vertex-structured-output

# Image understanding tests (all providers have same 3 tests)
./bin/llm-test anthropic-image --model claude-sonnet-4-5-20250929
./bin/llm-test openai-image --model gpt-4o-mini
./bin/llm-test bedrock-image
./bin/llm-test openrouter-image --model openai/gpt-4o-mini
./bin/llm-test vertex-image

# Image tests with custom images
./bin/llm-test openai-image --image-url https://example.com/image.jpg
./bin/llm-test openai-image --image-path /path/to/image.jpg
```

### Test Output

All tests provide consistent output format:
- ✅ Pass/fail status for each test
- 📊 Token usage metrics (input, output, total, cache tokens if available)
- ⏱️ Execution time for each test
- 📝 Response previews and validation
- 🔍 Detailed error messages on failure
- 🎯 Completion summary for test suites

### Test Architecture Benefits

The standardized test architecture provides:

1. **Consistency**: All providers run identical tests, making it easy to compare behavior
2. **Maintainability**: Bug fixes and improvements in shared functions benefit all providers
3. **Simplicity**: Provider test files are minimal (~50-80 lines) - just LLM initialization
4. **Extensibility**: Adding new providers requires minimal code (just initialize LLM and call shared functions)
5. **Reliability**: Same test logic means same validation standards across all providers

## Code Quality

This project uses [golangci-lint](https://golangci-lint.run/) for production-critical code quality checks. The configuration focuses on security, error handling, and common bugs while excluding style-only suggestions.

### Quick Start

```bash
# Install and run linter
make lint

# Auto-fix issues
make lint-fix
```

### Configuration

The linter is configured in `.golangci.yml` with production-critical checks enabled:
- **Security**: gosec (security vulnerabilities)
- **Error Handling**: errcheck, errorlint, errname
- **Code Quality**: unused, govet, staticcheck, gosimple
- **Resource Management**: bodyclose, noctx (HTTP context)

Style-only linters (gocritic) are disabled to focus on critical issues. See `.golangci.yml` for full configuration.

## API Documentation

### Core Types

- `llmproviders.Provider` - Provider type enum
- `llmproviders.Config` - Provider configuration
- `llmtypes.Model` - LLM interface
- `llmtypes.MessageContent` - Message content types
- `llmtypes.ContentResponse` - LLM response

### Interfaces

- `interfaces.Logger` - Logging interface
- `interfaces.EventEmitter` - Event emission interface
- `interfaces.Tracer` - Tracing interface

## License

See LICENSE file for details.

## Contributing

This module is part of the MCP Agent Builder project. For contributions, please follow the main project's contribution guidelines.

