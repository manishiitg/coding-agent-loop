# LLM Fallback & Configuration Refactoring

## Status: ✅ COMPLETED (Phase 1 + Phase 2)

## Summary

1. **Phase 1**: Unified fallback system replacing hardcoded cross-provider fallbacks with user-prioritized `FallbackModel[]` structure.
2. **Phase 2**: Saved LLM Configurations system allowing users to create named presets and select them as primary/fallback.

---

## Phase 2: Saved LLM Configurations - ✅ Complete

### New Data Structure

**SavedLLMConfig** (Named preset):
```typescript
interface SavedLLMConfig {
  id: string                // UUID
  name: string              // "Production Claude", "Fast Grok"
  provider: LLMProvider     // 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string          // 'gpt-4o', 'claude-sonnet-4', etc.
  options?: LLMOptions      // temperature, max_tokens, provider-specific options
  created_at: string        // ISO timestamp
  updated_at: string        // ISO timestamp
}
```

### Files Changed

| File | Change |
|------|--------|
| `frontend/src/services/api-types.ts` | Added `SavedLLMConfig` type |
| `frontend/src/stores/useSavedLLMConfigsStore.ts` | **New** - Zustand store for saved configs with localStorage persistence |
| `frontend/src/stores/index.ts` | Export new store |
| `frontend/src/components/SavedConfigsSection.tsx` | **New** - UI for creating/editing/selecting saved configs |
| `frontend/src/components/LLMConfigurationModal.tsx` | Added "Saved Configs" tab, integrated SavedConfigsSection |
| `frontend/src/components/sidebar/LLMConfigurationSection.tsx` | Refactored to use saved configs for selection |

### Key Features

1. **Create Named Configs**: Users can create configs with custom names, provider, model, and options
2. **Primary Selection**: Select any saved config as the primary LLM
3. **Fallback Selection**: Add multiple saved configs as fallbacks with priority ordering
4. **Options Support**: Each config can have temperature, max_tokens, and other options
5. **Automatic Sync**: Selections sync to `useLLMStore.primaryConfig` for backend communication
6. **localStorage Persistence**: Configs persist across browser sessions

### Store Actions

```typescript
// CRUD
addConfig(config)       // Create new saved config
updateConfig(id, updates) // Update existing config
deleteConfig(id)        // Delete a config
duplicateConfig(id, newName) // Clone a config

// Selection
setPrimaryConfigId(id)  // Set primary config
addFallbackConfigId(id) // Add to fallbacks
removeFallbackConfigId(id) // Remove from fallbacks
reorderFallbackConfigIds(newOrder) // Change fallback order
```

### UI Flow

1. Open LLM Configuration Modal → "Saved Configs" tab (default)
2. Create new config with name, provider, model, options
3. Click star icon to set as primary
4. Click checkmark to add as fallback
5. Use up/down arrows to reorder fallbacks
6. Sidebar shows quick selection dropdown

---

## Phase 1: Unified Fallback System - ✅ Complete

### Backend (Go) Changes

| File | Change |
|------|--------|
| `mcpagent/agent/agent.go` | Added `FallbackModel`, `LLMOptions` structs, `WithOptions()` |
| `mcpagent/agent/llm_generation.go` | Unified fallback logic via `tryUnifiedFallback()`, `getFallbackModelsInPriority()` |
| `mcpagent/agent/conversation.go` | Added `applyLLMOptions()` for provider-specific options |
| `mcpagent/llm/types.go` | Re-exported `WithReasoningEffort`, `WithThinkingLevel`, `WithVerbosity` |
| `mcpagent/events/data.go` | Updated `LLMGenerationWithRetryEvent` with `FallbackModels []FallbackModelInfo` |
| `agent_go/pkg/orchestrator/agents/interfaces.go` | Added `FallbackModel`, `LLMOptions`, provider-specific option structs |
| `agent_go/pkg/orchestrator/base_orchestrator_types.go` | Updated `LLMConfig` with new types |
| `agent_go/pkg/agentwrapper/llm_agent.go` | Passes `FallbackModels` and `Options` to agent |
| `agent_go/cmd/server/server.go` | Parses new `LLMConfig` format from requests |
| `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/*.go` | All workflow agents propagate `Options` |

### Frontend (TypeScript) Changes

| File | Change |
|------|--------|
| `frontend/src/services/api-types.ts` | Added `LLMProvider`, `FallbackModel`, `LLMOptions` types |
| `frontend/src/stores/useLLMStore.ts` | Added `addFallbackModel`, `removeFallbackModel`, `reorderFallbackModels` |
| `frontend/src/components/LLMConfigurationModal.tsx` | Added `UnifiedFallbackSection` component |
| `frontend/src/components/AnthropicSection.tsx` | Unified fallback UI |

---

## Data Structures

### FallbackModel
```typescript
interface FallbackModel {
  model_id: string
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  priority: number  // 1 = highest priority
  options?: LLMOptions  // Optional per-model overrides
}
```

### LLMOptions
```typescript
interface LLMOptions {
  // Common
  temperature?: number
  max_tokens?: number
  top_p?: number
  top_k?: number
  
  // Provider-specific (only one used based on provider)
  openai?: { reasoning_effort?: string, seed?: number, ... }
  anthropic?: { extended_thinking?: boolean, thinking_budget_tokens?: number }
  vertex?: { thinking_level?: string, safety_settings?: string, ... }
  bedrock?: { guardrail_identifier?: string, ... }
  openrouter?: { transforms?: string[], route?: string, ... }
}
```

---

## Removed

- `CrossProviderFallback` struct and all references
- `cross_provider_fallback` field from `LLMConfiguration`
- Hardcoded cross-provider fallback logic
- `detectProviderFromModelID()` (provider now explicit in `FallbackModel`)

---

## Key Backend Functions

- `tryUnifiedFallback()` - Tries all fallback models in priority order
- `getFallbackModelsInPriority()` - Sorts fallbacks by priority
- `createFallbackLLMFromModel()` - Creates LLM instance for fallback
- `applyLLMOptions()` - Applies provider-specific options to LLM calls

---

## Build Status

- **Backend**: ✅ `go build` passes
- **Frontend**: ✅ `npm run build` passes

---

## Local Development

The `go.mod` files are configured to use the local `multi-llm-provider-go` library:

```go
// In mcpagent/go.mod and agent_go/go.mod
replace github.com/manishiitg/multi-llm-provider-go => /Users/mipl/ai-work/multi-llm-provider-go
```
