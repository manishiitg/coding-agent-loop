# llm-providers

A Go module providing a unified interface for multiple Large Language Model (LLM) providers, including AWS Bedrock, OpenAI, Anthropic, OpenRouter, Google Vertex AI, and Azure AI.

## Overview

This module abstracts the differences between various LLM providers, providing a consistent API for:
- Text generation
- Tool calling
- Streaming responses
- Token usage tracking
- Structured output

## Installation

```bash
go get github.com/manishiitg/multi-llm-provider-go
```

Or with a specific version:

```bash
go get github.com/manishiitg/multi-llm-provider-go@v0.1.0
```

## Supported Providers

- **AWS Bedrock** - Claude models via Bedrock Runtime API
- **OpenAI** - GPT models (GPT-4, GPT-3.5, etc.)
- **Anthropic** - Claude models via direct API
- **OpenRouter** - Multi-provider access via OpenRouter API
- **Vertex AI** - Google Gemini models and Anthropic Claude via Vertex AI
- **Azure AI** - OpenAI models via Azure AI Services/Foundry

## Quick Start

```go
package main

import (
    "context"
    "github.com/manishiitg/multi-llm-provider-go"
    "github.com/manishiitg/multi-llm-provider-go/interfaces"
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
│   │   ├── vertex/
│   │   └── azure/
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
- `AZURE_AI_ENDPOINT`, `AZURE_AI_API_KEY` - Azure AI Services endpoint and API key

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
- **RunStreamingContentTest** - Content streaming validation
- **RunStreamingToolCallTest** - Tool call streaming validation
- **RunStreamingMixedTest** - Mixed content and tool call streaming
- **RunStreamingParallelToolCallsTest** - Parallel tool call streaming
- **RunStreamingWithFuncTest** - Function calling with streaming
- **RunStreamingMultiTurnTest** - Multi-turn conversation streaming
- **RunStreamingCancellationTest** - Streaming cancellation handling

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
- **Streaming Tests** - Comprehensive streaming response validation (varies by provider)

### Provider Test Coverage

All providers have **identical test coverage** using the same standardized tests:

#### Test Coverage Matrix

| Provider | Plain Text | Tool Calls | Structured Output | Image | Token Usage | Streaming |
|----------|------------|------------|-------------------|-------|-------------|----------|
| **Anthropic** | ✅ | ✅ (4 tests) | ✅ (Tool-based) | ✅ (3 tests) | ✅ (with cache) | ✅ (6 tests) |
| **OpenAI** | ✅ | ✅ (4 tests) | ✅ (JSON Schema) | ✅ (3 tests) | ✅ (with cache) | ✅ (7 tests) |
| **Bedrock** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) | ✅ (7 tests) |
| **OpenRouter** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) | ❌ |
| **Vertex AI** | ✅ | ✅ (4 tests) | ✅ (JSON mode) | ✅ (3 tests) | ✅ (with cache) | ✅ (4 tests) |
| **Azure AI** | ✅ | ✅ (4 tests) | ✅ (JSON Schema) | ✅ (3 tests) | ✅ (with cache) | ✅ (7 tests + Responses API) |

#### Anthropic (`anthropic-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `anthropic` | Basic text generation |
| Tool Calls | `anthropic-tool-call` | 4 standardized tests |
| Structured Output | `anthropic-structured-output` | Tool-based approach |
| Image Understanding | `anthropic-image` | 3 standardized tests |
| Token Usage | `anthropic-token-usage` | Simple, complex, cache tests |
| Streaming Content | `anthropic-streaming-content` | Content streaming validation |
| Streaming Mixed | `anthropic-streaming-mixed` | Mixed content/tool call streaming |
| Streaming Parallel | `anthropic-streaming-parallel` | Parallel tool call streaming |
| Streaming Func | `anthropic-streaming-func` | Function calling with streaming |
| Streaming Multi-Turn | `anthropic-streaming-multiturn` | Multi-turn conversation streaming |
| Streaming Cancellation | `anthropic-streaming-cancellation` | Streaming cancellation handling |
| Parallel Tool Response | `anthropic-parallel-tool-response` | Parallel tool calls with responses and continued conversation |

**Example:**
```bash
./bin/llm-test anthropic --model claude-haiku-4-5-20251001
./bin/llm-test anthropic-tool-call
./bin/llm-test anthropic-structured-output
./bin/llm-test anthropic-image
./bin/llm-test anthropic-streaming-content
./bin/llm-test anthropic-streaming-mixed
./bin/llm-test anthropic-parallel-tool-response --model claude-haiku-4-5-20251001
```

#### OpenAI (`openai-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `openai` | Basic text generation |
| Tool Calls | `openai-tool-call` | 4 standardized tests |
| Structured Output | `openai-structured-output` | JSON Schema with strict mode |
| Image Understanding | `openai-image` | 3 standardized tests |
| Token Usage | `openai-token-usage` | Simple, complex, cache tests |
| Streaming Tool Call | `openai-streaming-tool-call` | Tool call streaming validation |
| Streaming Content | `openai-streaming-content` | Content streaming validation |
| Streaming Mixed | `openai-streaming-mixed` | Mixed content/tool call streaming |
| Streaming Parallel | `openai-streaming-parallel` | Parallel tool call streaming |
| Streaming Func | `openai-streaming-func` | Function calling with streaming |
| Streaming Multi-Turn | `openai-streaming-multiturn` | Multi-turn conversation streaming |
| Streaming Cancellation | `openai-streaming-cancellation` | Streaming cancellation handling |
| Parallel Tool Response | `openai-parallel-tool-response` | Parallel tool calls with responses and continued conversation |

**Example:**
```bash
./bin/llm-test openai --model gpt-4o-mini
./bin/llm-test openai-tool-call --model gpt-4o-mini
./bin/llm-test openai-structured-output --model gpt-4o-mini
./bin/llm-test openai-image --model gpt-4o-mini
./bin/llm-test openai-streaming-tool-call --model gpt-4o-mini
./bin/llm-test openai-streaming-content --model gpt-4o-mini
```

#### AWS Bedrock (`bedrock-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `bedrock` | Basic text generation |
| Tool Calls | `llm-tool-call` | 4 standardized tests |
| Structured Output | `bedrock-structured-output` | JSON mode with validation |
| Image Understanding | `bedrock-image` | 3 standardized tests |
| Token Usage | `bedrock-token-usage` | Simple, complex, cache tests |
| Streaming Content | `bedrock-streaming-content` | Content streaming validation |
| Streaming Mixed | `bedrock-streaming-mixed` | Mixed content/tool call streaming |
| Streaming Parallel | `bedrock-streaming-parallel` | Parallel tool call streaming |
| Streaming Func | `bedrock-streaming-func` | Function calling with streaming |
| Streaming Multi-Turn | `bedrock-streaming-multiturn` | Multi-turn conversation streaming |
| Streaming Cancellation | `bedrock-streaming-cancellation` | Streaming cancellation handling |
| Streaming Tool Call History | `bedrock-streaming-toolcall-history` | Tool call with conversation history |
| Parallel Tool Response | `bedrock-parallel-tool-response` | Parallel tool calls with responses and continued conversation |

**Example:**
```bash
./bin/llm-test bedrock
./bin/llm-test llm-tool-call
./bin/llm-test bedrock-structured-output
./bin/llm-test bedrock-image
./bin/llm-test bedrock-streaming-content
./bin/llm-test bedrock-streaming-mixed
./bin/llm-test bedrock-streaming-toolcall-history
./bin/llm-test bedrock-parallel-tool-response
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
| Streaming Content | `vertex-streaming-content` | Content streaming validation |
| Streaming Mixed | `vertex-streaming-mixed` | Mixed content/tool call streaming |
| Streaming Multi-Turn | `vertex-streaming-multiturn` | Multi-turn conversation streaming |
| Streaming Cancellation | `vertex-streaming-cancellation` | Streaming cancellation handling |
| Parallel Tool Response | `vertex-parallel-tool-response` | Parallel tool calls with responses and continued conversation |

**Example:**
```bash
./bin/llm-test vertex --model gemini-2.5-flash
./bin/llm-test vertex-tool-call
./bin/llm-test vertex-structured-output
./bin/llm-test vertex-image
./bin/llm-test vertex-streaming-content
./bin/llm-test vertex-streaming-mixed
./bin/llm-test vertex-parallel-tool-response --model gemini-3-pro-preview
```

#### Azure AI (`azure-*`)

| Test Type | Command | Features |
|-----------|---------|----------|
| Plain Text | `azure` | Basic text generation |
| Tool Calls | `azure-tool-call` | 4 standardized tests |
| Tool Call Events | `azure-tool-call-events` | Tool call event emission test |
| Structured Output | `azure-structured-output` | JSON Schema with strict mode |
| Image Understanding | `azure-image` | 3 standardized tests |
| Token Usage | `azure-token-usage` | Simple, complex, cache tests |
| Model Metadata | `azure-model-metadata` | Display model pricing, context window, capabilities |
| Streaming Content | `azure-streaming-content` | Content streaming validation |
| Streaming Mixed | `azure-streaming-mixed` | Mixed content/tool call streaming |
| Streaming Parallel | `azure-streaming-parallel` | Parallel tool call streaming |
| Streaming Func | `azure-streaming-func` | Function calling with streaming |
| Streaming Multi-Turn | `azure-streaming-multiturn` | Multi-turn conversation streaming |
| Streaming Cancellation | `azure-streaming-cancellation` | Streaming cancellation handling |
| Responses API | `azure-responses` | Test agentic models (e.g., gpt-5.2-codex) using the specialized Responses API |

**Note:** Azure AI requires `AZURE_AI_ENDPOINT` and `AZURE_AI_API_KEY` environment variables. The model parameter is the deployment name in your Azure AI resource.

**Example:**
```bash
./bin/llm-test azure --model gpt-4o
./bin/llm-test azure-tool-call --model gpt-4o
./bin/llm-test azure-structured-output --model gpt-4o
./bin/llm-test azure-image --model gpt-4o
./bin/llm-test azure-streaming-content --model gpt-4o
./bin/llm-test azure-streaming-mixed --model gpt-4o
./bin/llm-test azure-streaming-parallel --model gpt-4o
./bin/llm-test azure-token-usage --model gpt-4o
./bin/llm-test azure-responses --model gpt-5.2-codex

# Model metadata (shows pricing, context window, capabilities)
./bin/llm-test azure-model-metadata --all           # List all models
./bin/llm-test azure-model-metadata --model gpt-4o  # Show specific model
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

### Streaming Tests

Streaming tests validate real-time response streaming capabilities across providers. These tests ensure that:
- Content chunks are streamed immediately as they're generated
- Tool calls are streamed when complete
- Streamed content matches final response content
- Streaming works correctly in multi-turn conversations
- Cancellation works properly during streaming

#### Streaming Test Types

1. **Content Streaming** (`*-streaming-content`)
   - Tests basic content streaming without tool calls
   - Validates that streamed chunks match final response
   - Tests both short and longer content generation

2. **Parallel Tool Call with Response** (`*-parallel-tool-response`)
   - Tests complete flow: parallel tool calls → tool responses → continued conversation
   - Validates tool response matching for multiple parallel tools
   - Ensures LLM can continue conversation using tool results
   - Tests thought signature handling (for Gemini 3 Pro)
   - Available for: Vertex AI (other providers can be added)
   - Verifies chunk ordering and completeness

2. **Tool Call Streaming** (`*-streaming-tool-call`)
   - Tests streaming with tool calls
   - Validates that tool calls are streamed when complete
   - Ensures streamed tool calls match final response
   - **Available**: OpenAI only

3. **Mixed Streaming** (`*-streaming-mixed`)
   - Tests scenarios with both content and tool calls
   - Validates chunk ordering (content before tool calls)
   - Ensures both content and tool calls stream correctly

4. **Parallel Tool Call Streaming** (`*-streaming-parallel`)
   - Tests streaming with multiple parallel tool calls
   - Validates that all tool calls are streamed correctly
   - **Available**: Anthropic, OpenAI, Bedrock

5. **Function Calling Streaming** (`*-streaming-func`)
   - Tests function calling with streaming enabled
   - Validates streaming behavior with function tools
   - **Available**: Anthropic, OpenAI, Bedrock

6. **Multi-Turn Streaming** (`*-streaming-multiturn`)
   - Tests streaming across multiple conversation turns
   - Validates streaming consistency in conversations
   - Ensures context is maintained across turns

7. **Streaming Cancellation** (`*-streaming-cancellation`)
   - Tests proper cancellation handling during streaming
   - Validates that streams are closed correctly on cancellation
   - Ensures no resource leaks

8. **Tool Call History Streaming** (`*-streaming-toolcall-history`)
   - Tests tool call streaming with conversation history
   - Validates streaming with previous tool call results
   - **Available**: Bedrock only

9. **Parallel Tool Call with Response** (`*-parallel-tool-response`)
   - Tests complete flow: parallel tool calls → tool responses → continued conversation
   - Validates tool response matching for multiple parallel tools
   - Ensures LLM can continue conversation using tool results
   - Tests thought signature handling (for Gemini 3 Pro)
   - **Available**: Vertex AI (other providers can be added)

#### Streaming Test Coverage by Provider

| Provider | Content | Tool Call | Mixed | Parallel | Func | Multi-Turn | Cancellation | History | Parallel Response |
|----------|---------|-----------|-------|----------|------|------------|--------------|---------|-------------------|
| **Anthropic** | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **OpenAI** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Bedrock** | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **OpenRouter** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Vertex AI** | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| **Azure AI** | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |

**Note**: OpenRouter does not currently support streaming tests. Vertex AI has partial streaming support (content, mixed, multi-turn, and cancellation only). Azure AI uses OpenAI-compatible API and supports most streaming features.

### Running Tests

**Basic usage:**
```bash
# Plain text generation (all providers)
./bin/llm-test anthropic --model claude-haiku-4-5-20251001
./bin/llm-test openai --model gpt-4o-mini
./bin/llm-test bedrock
./bin/llm-test openrouter --model moonshotai/kimi-k2
./bin/llm-test vertex --model gemini-2.5-flash
./bin/llm-test azure --model gpt-4o

# Tool call tests (all providers have same 4 tests)
./bin/llm-test anthropic-tool-call
./bin/llm-test openai-tool-call --model gpt-4o-mini
./bin/llm-test llm-tool-call  # Bedrock
./bin/llm-test openrouter-tool-call --model moonshotai/kimi-k2
./bin/llm-test vertex-tool-call
./bin/llm-test azure-tool-call --model gpt-4o

# Token usage tests (all providers have cache tests)
./bin/llm-test anthropic-token-usage --prompt "Hello world"
./bin/llm-test openai-token-usage --prompt "Hello world"
./bin/llm-test bedrock-token-usage --prompt "Hello world"
./bin/llm-test openrouter-token-usage --prompt "Hello world"
./bin/llm-test vertex-token-usage --prompt "Hello world"
./bin/llm-test azure-token-usage --prompt "Hello world" --model gpt-4o

# Structured output tests
./bin/llm-test anthropic-structured-output
./bin/llm-test openai-structured-output --model gpt-4o-mini
./bin/llm-test bedrock-structured-output
./bin/llm-test openrouter-structured-output --model moonshotai/kimi-k2
./bin/llm-test vertex-structured-output
./bin/llm-test azure-structured-output --model gpt-4o

# Image understanding tests (all providers have same 3 tests)
./bin/llm-test anthropic-image --model claude-sonnet-4-5-20250929
./bin/llm-test openai-image --model gpt-4o-mini
./bin/llm-test bedrock-image
./bin/llm-test openrouter-image --model openai/gpt-4o-mini
./bin/llm-test vertex-image
./bin/llm-test azure-image --model gpt-4o

# Image tests with custom images
./bin/llm-test openai-image --image-url https://example.com/image.jpg
./bin/llm-test openai-image --image-path /path/to/image.jpg

# Streaming tests
# Anthropic streaming
./bin/llm-test anthropic-streaming-content
./bin/llm-test anthropic-streaming-mixed
./bin/llm-test anthropic-streaming-parallel
./bin/llm-test anthropic-streaming-func
./bin/llm-test anthropic-streaming-multiturn
./bin/llm-test anthropic-streaming-cancellation

# OpenAI streaming
./bin/llm-test openai-streaming-tool-call --model gpt-4o-mini
./bin/llm-test openai-streaming-content --model gpt-4o-mini
./bin/llm-test openai-streaming-mixed --model gpt-4o-mini
./bin/llm-test openai-streaming-parallel --model gpt-4o-mini
./bin/llm-test openai-streaming-func --model gpt-4o-mini
./bin/llm-test openai-streaming-multiturn --model gpt-4o-mini
./bin/llm-test openai-streaming-cancellation --model gpt-4o-mini

# Bedrock streaming
./bin/llm-test bedrock-streaming-content
./bin/llm-test bedrock-streaming-mixed
./bin/llm-test bedrock-streaming-parallel
./bin/llm-test bedrock-streaming-func
./bin/llm-test bedrock-streaming-multiturn
./bin/llm-test bedrock-streaming-cancellation
./bin/llm-test bedrock-streaming-toolcall-history

# Vertex AI streaming
./bin/llm-test vertex-streaming-content
./bin/llm-test vertex-streaming-mixed
./bin/llm-test vertex-streaming-multiturn
./bin/llm-test vertex-streaming-cancellation

# Azure AI streaming
./bin/llm-test azure-streaming-content --model gpt-4o
./bin/llm-test azure-streaming-mixed --model gpt-4o
./bin/llm-test azure-streaming-parallel --model gpt-4o
./bin/llm-test azure-streaming-func --model gpt-4o
./bin/llm-test azure-streaming-multiturn --model gpt-4o
./bin/llm-test azure-streaming-cancellation --model gpt-4o
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

## Security & Secret Scanning

This project uses [gitleaks](https://github.com/gitleaks/gitleaks) to prevent accidental secret commits. The configuration is in `.gitleaks.toml`.

### Pre-commit Hooks

Install pre-commit hooks to automatically run linting and secret scanning before each commit:

```bash
# Install hooks (installs golangci-lint and gitleaks if needed)
make install-hooks
# or
./scripts/install-git-hooks.sh
```

The pre-commit hook will:
- Run `golangci-lint` to check code quality
- Run `gitleaks` to scan for secrets
- Block commits if any issues are found

### Manual Secret Scanning

```bash
# Scan entire repository
make scan-secrets
# or
./scripts/scan-secrets.sh

# Scan specific path
./scripts/scan-secrets.sh path/to/directory
```

## Continuous Integration

GitHub Actions workflows automatically run on every push and pull request:

### CI Workflow (`.github/workflows/ci.yml`)

Runs on every push and PR to `main`, `master`, or `develop` branches:

1. **Lint**: Runs `golangci-lint` to check code quality
2. **Security Scan**: Runs `gitleaks` to detect secrets
3. **Tests**: Runs all Go tests (`go test ./...`)
4. **Build**: Builds all binaries to ensure compilation succeeds
5. **Test Suite**: Runs the full test suite (`./bin/llm-test test-suite`) in replay mode
   - Automatically discovers and runs all recorded tests
   - Validates all test scenarios without making real API calls
   - Fast execution (~200-500µs per test)

### Security Scan Workflow (`.github/workflows/security-scan.yml`)

Runs daily at 2 AM UTC and on every push/PR:
- Comprehensive secret scanning with full git history
- Uploads results to GitHub Security tab (SARIF format)

All workflows can also be manually triggered from the GitHub Actions tab.

### Local CI Testing

You can test GitHub Actions workflows locally using [act](https://github.com/nektos/act):

```bash
# Install act
make install-act

# List available jobs
make list-ci-jobs

# Run all CI jobs locally (can be slow on first run)
make test-ci

# Run a specific job
make test-ci-job JOB=lint
make test-ci-job JOB=test-suite
```

⚠️ **Note**: Act can be slow on first run (5-10 minutes) while pulling Docker images. Output may not appear immediately - be patient.

See [docs/LOCAL_CI_TESTING.md](docs/LOCAL_CI_TESTING.md) for detailed instructions and troubleshooting.

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

This module is part of the Multi Agent Builder project. For contributions, please follow the main project's contribution guidelines.

