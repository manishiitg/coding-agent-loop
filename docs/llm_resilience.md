# LLM Resilience: Fallbacks & Retries

The MCP Agent includes a comprehensive resilience layer designed to handle the inherent instability of LLM APIs. This system ensures high availability and reliability even when primary models fail due to rate limits, context window exhaustion, or service outages.

## 🛡️ Core Features

### 1. Robust Retry Logic
The agent automatically retries failed requests with exponential backoff.
- **Throttling Errors**: Automatically detects `429 Too Many Requests` or `ThrottlingException`.
- **Connection Errors**: Handles network timeouts, connection resets, and broken pipes.
- **Internal Errors**: Retries on `500`, `502`, `503`, `504` errors.

### 2. Multi-Phase Fallback System
When a request fails permanently (e.g., context window exceeded) or after retries are exhausted, the agent triggers a multi-phase fallback system.

#### Phase 1: Same-Provider Fallback
First, the agent tries other models within the same provider. This is often faster and maintains better prompt compatibility.
*   **Example (Bedrock)**: `anthropic.claude-3-5-sonnet-20240620-v1:0` -> `anthropic.claude-3-haiku-20240307-v1:0`
*   **Example (OpenAI)**: `gpt-4o` -> `gpt-4o-mini`

#### Phase 2: Cross-Provider Fallback
If same-provider models fail (or if the entire provider is down), the agent switches to a completely different provider.
*   **Example**: `Bedrock (Claude)` -> `OpenAI (GPT-4o)`

### 3. Error Classification
The system intelligently classifies errors to determine the best recovery strategy:
- **Max Token/Context Errors**: Triggers fallback immediately (retrying won't help).
- **Throttling**: Triggers retry with backoff, then fallback if persistent.
- **Empty Content**: Specific handling for zero-length responses.

## ⚙️ Configuration

### Default Behavior
By default, the agent uses hardcoded fallback chains based on the primary provider.
- **Bedrock**: Sonnet -> Haiku -> Opus
- **OpenAI**: GPT-4o -> GPT-4o-mini -> GPT-4

### Custom Configuration
You can configure custom fallback chains via the `WithCrossProviderFallback` option.

```go
agent, err := mcpagent.NewAgent(..., 
    mcpagent.WithCrossProviderFallback(&mcpagent.CrossProviderFallback{
        Provider: "openai",
        Models:   []string{"gpt-4o", "gpt-4-turbo"},
    }),
)
```

## 🧩 Implementation Details

The core logic resides in `pkg/mcpagent/llm_generation.go`.

### `GenerateContentWithRetry`
This is the main entry point for all LLM calls. It wraps the standard `LLM.GenerateContent` call with the retry and fallback loop.

### `isMaxTokenError` & `isThrottlingError`
Helper functions that analyze error messages strings to classify the failure type. This is necessary because different providers return different error formats.

### `createFallbackLLM`
Dynamically instantiates a new LLM client for the fallback model. This ensures that the fallback attempt uses a fresh, clean client configuration.

## 📊 Observability

The resilience layer emits detailed events to track system health:

- `llm_generation_with_retry`: Tracks the overall operation.
- `fallback_attempt`: Emitted for each fallback attempt (Phase 1 & 2).
- `model_change`: Emitted when the agent permanently switches to a fallback model for the remainder of the turn.
- `throttling_detected`: Tracks rate limit occurrences.

## 💡 Best Practices

1.  **Context Management**: While fallbacks help, it's better to prevent context errors. Use Smart Routing to keep prompts small.
2.  **Provider Diversity**: Configure at least two different providers (e.g., AWS and OpenAI) to ensure true high availability.
3.  **Monitoring**: Watch for `fallback_attempt` events. Frequent fallbacks indicate your primary model is undersized for the workload.
