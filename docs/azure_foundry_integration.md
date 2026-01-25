# Azure AI Foundry & Responses API Integration

This document outlines the implementation details and fixes for integrating Azure AI Foundry and supporting specialized models like the `gpt-5.2-codex` series via the Azure Responses API.

## 1. Provider Support

The `azure` provider has been fully integrated into the orchestrator and token management systems.

- **Orchestrator**: The system now recognizes `azure` as a valid provider string.
- **Token Usage Tracking**: Updated `agent_go/pkg/orchestrator/base_orchestrator_tokens.go` to include the Azure adapter. This enables:
    - Automatic retrieval of model metadata (context window, capabilities).
    - Accurate pricing calculation for Azure-hosted models.
    - Persistence of Azure token usage in `token_usage.json`.

## 2. Responses API Implementation

Specialized agentic models (e.g., `gpt-5.2-codex`) on Azure require the **Responses API** instead of the standard Chat Completions API.

- **Endpoint Routing**: Requests for agentic models are automatically routed to the `/openai/v1/responses` endpoint.
- **API Versioning**: Fixed a requirement where the Responses API specifically requires the `api-version=v1` query parameter on `cognitiveservices.azure.com` endpoints.
- **Payload Structure**:
    - Implemented a specialized payload format where tool definitions must include a `name` field at the top level of the tool object.
    - Added `isAgenticModel` checks to identify when to use this specialized routing.

## 3. Model Parameters & Mapping

Azure's newer model deployments have specific parameter requirements that differ from standard OpenAI or earlier Azure implementations.

- **Max Tokens**: For `gpt-5.2` and similar variants, the system now maps the standard `max_tokens` parameter to `max_completion_tokens` to ensure compatibility.
- **Model Metadata**: Added static and dynamic metadata support for the latest Azure models, ensuring the orchestrator knows the correct context window limits and pricing tiers.

## 4. Configuration

To use Azure AI Foundry, ensure the following environment variables or configurations are set:

- **Provider**: `azure`
- **Endpoint**: `https://<resource-name>.cognitiveservices.azure.com`
- **Model ID**: The deployment name of your model (e.g., `gpt-4o`, `gpt-4.1-mini`, `gpt-5.2-codex-2026-01-14`).

## 5. Frontend Integration

### 5.1 Endpoint Persistence
- **Fix**: Added `endpoint` field to the `preserveUserConfig` function in `useLLMStore.ts`
- **Issue**: Azure endpoint was being forgotten after page refresh
- **Solution**: Now preserves `endpoint`, `options`, and `temperature` across sessions

### 5.2 API Version Configuration
- **Fix**: Added API Version field to `AzureSection.tsx` with default value `v1`
- **Issue**: UnsupportedApiVersion error with date-based versions like `2024-12-01-preview`
- **Solution**: Responses API requires `v1` as the API version

### 5.3 Corrected Endpoint Handling
- **Fix**: Frontend now handles `corrected_options` from backend validation response
- **Issue**: Backend derives optimized `cognitiveservices.azure.com` endpoint but frontend wasn't using it
- **Solution**: Added `corrected_options` to `APIKeyValidationResponse` type and update UI when endpoint is corrected

## 6. Responses API Message Format Fixes

### 6.1 Tool Message Format
- **Fix**: Changed tool response format from Chat Completions to Responses API format
- **Issue**: Error `Invalid value: 'tool'. Supported values are: 'assistant', 'system', 'developer', and 'user'.`
- **Solution**: Tool responses now use `function_call_output` format:
  ```json
  {"type": "function_call_output", "call_id": "...", "output": "..."}
  ```
  Instead of:
  ```json
  {"role": "tool", "tool_call_id": "...", "content": "..."}
  ```

### 6.2 Function Call Items in Conversation History
- **Fix**: Added `function_call` items when converting AI messages with tool calls
- **Issue**: Error `No tool call found for function call output with call_id ...`
- **Solution**: When AI makes tool calls, we now emit separate `function_call` items:
  ```json
  {"type": "function_call", "call_id": "...", "name": "...", "arguments": "..."}
  ```
  This allows `function_call_output` items to find their matching tool calls.

### 6.3 Tool Definition Format
- **Fix**: Changed tool definitions to flat structure for Responses API
- **Issue**: Model was generating empty arguments `{}` for tool calls
- **Solution**: Tools now use flat format:
  ```json
  {"type": "function", "name": "...", "description": "...", "parameters": {...}}
  ```
  Instead of nested format:
  ```json
  {"type": "function", "name": "...", "function": {"name": "...", "description": "...", "parameters": {...}}}
  ```

### 6.4 Arguments Parsing
- **Fix**: Added `convertArgumentsToString()` for robust argument extraction
- **Issue**: Arguments could come as string or object from API
- **Solution**: Now handles both formats and converts to JSON string

## 7. Summary of All Fixes

| Feature | Fix / Implementation | File(s) |
| :--- | :--- | :--- |
| **Provider Validation** | Added `azure` to supported providers list | `base_orchestrator_tokens.go` |
| **Codex Support** | Implemented Responses API routing for agentic/codex models | `azure_adapter.go` |
| **API Versioning** | Enforced `api-version=v1` for Responses API | `azure_adapter.go` |
| **Token Params** | Switched to `max_completion_tokens` for GPT-5+ models | `azure_adapter.go` |
| **Endpoint Persistence** | Added endpoint to `preserveUserConfig` | `useLLMStore.ts` |
| **API Version UI** | Added API Version field with default `v1` | `AzureSection.tsx` |
| **Corrected Endpoint** | Handle backend's corrected endpoint in frontend | `api-types.ts`, `useLLMStore.ts`, `AzureSection.tsx` |
| **Tool Message Format** | Use `function_call_output` instead of `role: tool` | `azure_adapter.go` |
| **Function Call History** | Emit `function_call` items for AI tool calls | `azure_adapter.go` |
| **Tool Definition Format** | Flat structure (not nested under `function`) | `azure_adapter.go` |
| **Arguments Parsing** | Handle string/object arguments robustly | `azure_adapter.go` |

## 8. Message Conversion Flow

The `convertMessagesForResponsesAPI` function handles the conversion:

1. **System/User messages**: `{"role": "system/user", "content": "..."}`
2. **AI messages with content**: `{"role": "assistant", "content": "..."}`
3. **AI tool calls**: `{"type": "function_call", "call_id": "...", "name": "...", "arguments": "..."}`
4. **Tool responses**: `{"type": "function_call_output", "call_id": "...", "output": "..."}`

## 9. Testing

Verified working with:
- Model: `gpt-5.2-codex`
- Endpoint: `https://<resource>.cognitiveservices.azure.com`
- API Version: `v1`
- Multi-turn conversations with tool calls
- Tool arguments properly passed (e.g., `{"query": "MCP protocol", "max_results": 10}`)
- Tool responses properly received by model
- Final answers generated based on tool results
