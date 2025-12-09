# LLM Fallback & Configuration Refactoring Plan

## Overview

This document outlines the refactoring plan for the LLM configuration and fallback system to:
1. Make API keys and LLM config work primarily via UI (with env vars as initial defaults)
2. Replace hardcoded cross-provider fallbacks with a flexible, user-prioritized fallback system
3. Ensure consistent API key propagation across all agent creation paths

---

## Current Problems

### Problem 1: Inconsistent API Key Flow
- Frontend sends API keys in request, but not all code paths use them
- Some internal agents (workflow steps) don't propagate API keys
- Fallback LLM creation sometimes falls back to env vars instead of passed keys

### Problem 2: Hardcoded Cross-Provider Fallbacks
- In `multi-llm-provider-go`, cross-provider fallbacks are hardcoded:
  - Bedrock → OpenAI only (`BEDROCK_OPENAI_FALLBACK_MODELS`)
  - OpenAI → Bedrock only (`OPENAI_BEDROCK_FALLBACK_MODELS`)
- Users cannot choose which providers to use as fallbacks
- No flexibility for Anthropic, Vertex, or OpenRouter as fallback targets

---

## Proposed Changes

### Data Model Changes

#### Frontend Types (`frontend/src/services/api-types.ts`)

**Current:**
```typescript
interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string
  fallback_models: string[]
  cross_provider_fallback?: {
    provider: 'openai' | 'bedrock' | ...
    models: string[]
  }
  api_keys?: {...}
}
```

**Proposed:**
```typescript
interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string
  
  // UNIFIED: Any model from any provider, in priority order
  fallback_models: FallbackModel[]
  
  api_keys?: {
    openrouter?: string
    openai?: string
    anthropic?: string
    vertex?: string
    bedrock?: { region: string }
  }
}

// NEW: Rich fallback model structure
interface FallbackModel {
  model_id: string
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  priority: number  // User-defined order (1 = highest priority)
}
```

#### Backend Types (`agent_go/pkg/orchestrator/base_orchestrator_types.go`)

**Current:**
```go
type LLMConfig struct {
    Provider              string
    ModelID               string
    FallbackModels        []string
    CrossProviderFallback *agents.CrossProviderFallback
    APIKeys               *APIKeys
}
```

**Proposed:**
```go
type LLMConfig struct {
    Provider       string           `json:"provider"`
    ModelID        string           `json:"model_id"`
    FallbackModels []FallbackModel  `json:"fallback_models"`
    APIKeys        *APIKeys         `json:"api_keys,omitempty"`
}

// NEW: Rich fallback model structure
type FallbackModel struct {
    ModelID  string `json:"model_id"`
    Provider string `json:"provider"`
    Priority int    `json:"priority"`
}
```

---

## Implementation Phases

### Phase 1: External Library Changes (`multi-llm-provider-go`)

#### Functions to REMOVE

Remove these functions entirely from `providers.go`:

```go
// REMOVE: GetCrossProviderFallbackModels
// This function hardcodes cross-provider relationships (Bedrock→OpenAI, etc.)
func GetCrossProviderFallbackModels(provider Provider) []string {
    // DELETE ENTIRE FUNCTION
}
```

#### Functions to MODIFY

Keep `GetDefaultFallbackModels` but simplify - it now only provides env var defaults:

```go
// KEEP but treat as "initial defaults only"
// UI settings will override these
func GetDefaultFallbackModels(provider Provider) []string {
    switch provider {
    case ProviderBedrock:
        return getEnvModels("BEDROCK_FALLBACK_MODELS")
    case ProviderOpenAI:
        return getEnvModels("OPENAI_FALLBACK_MODELS")
    case ProviderOpenRouter:
        return getEnvModels("OPENROUTER_FALLBACK_MODELS")
    case ProviderAnthropic:
        return getEnvModels("ANTHROPIC_FALLBACK_MODELS")
    case ProviderVertex:
        return getEnvModels("VERTEX_FALLBACK_MODELS")
    default:
        return []string{}
    }
}
```

#### Remove Cross-Provider Env Vars

These env vars should no longer be read:
- `BEDROCK_OPENAI_FALLBACK_MODELS` - DELETE
- `OPENAI_BEDROCK_FALLBACK_MODELS` - DELETE
- `OPENROUTER_CROSS_FALLBACK_MODELS` - DELETE

---

### Phase 2: mcpagent Layer Changes

#### File: `mcpagent/llm/providers.go`

**Remove:**
```go
// REMOVE: This re-exports the hardcoded cross-provider function
func GetCrossProviderFallbackModels(provider Provider) []string {
    return llmproviders.GetCrossProviderFallbackModels(llmproviders.Provider(provider))
}
```

**Keep (for env var defaults):**
```go
// KEEP: Provides initial defaults from env vars
func GetDefaultFallbackModels(provider Provider) []string {
    return llmproviders.GetDefaultFallbackModels(llmproviders.Provider(provider))
}
```

#### File: `mcpagent/agent/llm_generation.go`

**Major Refactoring Required**

Current code has ~1700 lines with duplicate fallback handling for:
- Same-provider fallbacks
- Cross-provider fallbacks (for each error type)

**New Unified Approach:**

```go
// GenerateContentWithRetry handles LLM generation with unified fallback logic
func GenerateContentWithRetry(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, opts []llmtypes.CallOption, turn int, sendMessage func(string)) (*llmtypes.ContentResponse, error, observability.UsageMetrics) {
    maxRetries := 5
    var lastErr error
    var usage observability.UsageMetrics

    for attempt := 0; attempt < maxRetries; attempt++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err(), usage
        default:
        }

        // Try primary model
        resp, err := a.LLM.GenerateContent(ctx, messages, opts...)
        if err == nil {
            usage = extractUsageMetricsWithMessages(resp, messages)
            return resp, nil, usage
        }

        // Classify error and attempt fallback
        errorType := classifyError(err)
        
        // Use unified fallback function for all error types
        resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, err, errorType, turn, attempt, maxRetries, sendMessage, messages, opts)
        if fallbackErr == nil {
            return resp, nil, fallbackUsage
        }
        
        lastErr = fallbackErr
        
        // For throttling, add delay before retry
        if errorType == "throttling" && attempt < maxRetries-1 {
            delay := calculateBackoffDelay(attempt)
            time.Sleep(delay)
        }
    }

    return nil, lastErr, usage
}

// tryUnifiedFallback attempts all configured fallback models in priority order
func (a *Agent) tryUnifiedFallback(ctx context.Context, originalErr error, errorType string, turn int, attempt int, maxRetries int, sendMessage func(string), messages []llmtypes.MessageContent, opts []llmtypes.CallOption) (*llmtypes.ContentResponse, error, observability.UsageMetrics) {
    var usage observability.UsageMetrics
    
    // Get fallback models in priority order
    fallbacks := a.getFallbackModelsInPriority()
    
    if len(fallbacks) == 0 {
        return nil, fmt.Errorf("no fallback models configured: %w", originalErr), usage
    }

    sendMessage(fmt.Sprintf("\n⚠️ %s error detected. Trying %d fallback models...", errorType, len(fallbacks)))

    for i, fallback := range fallbacks {
        // Skip if we don't have credentials for this provider
        if !a.hasCredentialsForProvider(fallback.Provider) {
            sendMessage(fmt.Sprintf("\n⏭️ Skipping %s - no API key configured", fallback.ModelID))
            continue
        }

        sendMessage(fmt.Sprintf("\n🔄 Trying fallback %d/%d: %s (%s)", i+1, len(fallbacks), fallback.ModelID, fallback.Provider))

        // Create fallback LLM
        fallbackLLM, err := a.createFallbackLLM(ctx, fallback.ModelID)
        if err != nil {
            sendMessage(fmt.Sprintf("\n❌ Failed to initialize %s: %v", fallback.ModelID, err))
            continue
        }

        // Try the fallback
        resp, err := fallbackLLM.GenerateContent(ctx, messages, opts...)
        if err == nil {
            usage = extractUsageMetricsWithMessages(resp, messages)
            
            // PERMANENTLY update agent to use this model
            a.ModelID = fallback.ModelID
            a.LLM = fallbackLLM
            a.provider = llm.Provider(fallback.Provider)
            
            // Emit success events
            a.emitFallbackSuccessEvent(turn, fallback)
            
            sendMessage(fmt.Sprintf("\n✅ Fallback succeeded: %s (%s) - Model updated permanently", fallback.ModelID, fallback.Provider))
            return resp, nil, usage
        }

        sendMessage(fmt.Sprintf("\n❌ Fallback %s failed: %v", fallback.ModelID, err))
    }

    return nil, fmt.Errorf("all %d fallback models failed for %s: %w", len(fallbacks), errorType, originalErr), usage
}

// getFallbackModelsInPriority returns fallback models sorted by priority
func (a *Agent) getFallbackModelsInPriority() []FallbackModel {
    // 1. If agent has UI-configured fallbacks, use them
    if len(a.FallbackModels) > 0 {
        // Sort by priority
        sorted := make([]FallbackModel, len(a.FallbackModels))
        copy(sorted, a.FallbackModels)
        sort.Slice(sorted, func(i, j int) bool {
            return sorted[i].Priority < sorted[j].Priority
        })
        return sorted
    }
    
    // 2. Fall back to env var defaults (initial defaults)
    defaults := llm.GetDefaultFallbackModels(a.provider)
    
    // 3. Convert to FallbackModel format with auto-detected providers
    result := make([]FallbackModel, len(defaults))
    for i, modelID := range defaults {
        result[i] = FallbackModel{
            ModelID:  modelID,
            Provider: string(detectProviderFromModelID(modelID)),
            Priority: i + 1,
        }
    }
    return result
}

// hasCredentialsForProvider checks if API keys are configured for the provider
func (a *Agent) hasCredentialsForProvider(provider string) bool {
    if a.APIKeys == nil {
        // Fall back to env var check
        switch provider {
        case "openai":
            return os.Getenv("OPENAI_API_KEY") != ""
        case "anthropic":
            return os.Getenv("ANTHROPIC_API_KEY") != ""
        case "openrouter":
            return os.Getenv("OPENROUTER_API_KEY") != ""
        case "bedrock":
            return os.Getenv("AWS_ACCESS_KEY_ID") != "" || os.Getenv("AWS_REGION") != ""
        case "vertex":
            return os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" || os.Getenv("VERTEX_API_KEY") != ""
        }
        return false
    }
    
    switch provider {
    case "openai":
        return a.APIKeys.OpenAI != nil && *a.APIKeys.OpenAI != ""
    case "anthropic":
        return a.APIKeys.Anthropic != nil && *a.APIKeys.Anthropic != ""
    case "openrouter":
        return a.APIKeys.OpenRouter != nil && *a.APIKeys.OpenRouter != ""
    case "bedrock":
        return a.APIKeys.Bedrock != nil && a.APIKeys.Bedrock.Region != ""
    case "vertex":
        return a.APIKeys.Vertex != nil && *a.APIKeys.Vertex != ""
    }
    return false
}
```

---

### Phase 3: API Key Propagation

#### Files to Update

| File | Change |
|------|--------|
| `agent_go/cmd/server/server.go` | Ensure `LLMConfig.APIKeys` propagates to all agent types |
| `agent_go/pkg/orchestrator/base_orchestrator_agent_factory.go` | Pass API keys to all sub-agent creation |
| `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go` | Inherit API keys for all workflow step agents |
| `agent_go/pkg/agentwrapper/llm_agent.go` | Use API keys from config |

#### Pattern for API Key Inheritance

```go
// In workflow orchestrator - all sub-agents inherit API keys
func (o *BaseOrchestrator) createSubAgent(ctx context.Context, agentType string) (*Agent, error) {
    config := agents.OrchestratorAgentConfig{
        Provider:       o.config.Provider,
        Model:          o.config.Model,
        FallbackModels: o.config.FallbackModels,  // Inherit fallbacks
        APIKeys:        o.config.APIKeys,          // CRITICAL: Inherit API keys
    }
    return CreateAgent(ctx, config)
}
```

---

### Phase 4: Backend Type Updates

#### File: `agent_go/pkg/orchestrator/base_orchestrator_types.go`

```go
package orchestrator

import "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

// LLMConfig represents LLM configuration from frontend
type LLMConfig struct {
    Provider       string          `json:"provider"`
    ModelID        string          `json:"model_id"`
    FallbackModels []FallbackModel `json:"fallback_models"`
    APIKeys        *APIKeys        `json:"api_keys,omitempty"`
}

// FallbackModel represents a fallback model with priority
type FallbackModel struct {
    ModelID  string `json:"model_id"`
    Provider string `json:"provider"`
    Priority int    `json:"priority"`
}

// APIKeys represents API keys for different providers
type APIKeys struct {
    OpenRouter *string     `json:"openrouter,omitempty"`
    OpenAI     *string     `json:"openai,omitempty"`
    Anthropic  *string     `json:"anthropic,omitempty"`
    Vertex     *string     `json:"vertex,omitempty"`
    Bedrock    *BedrockKey `json:"bedrock,omitempty"`
}

// BedrockKey represents Bedrock configuration
type BedrockKey struct {
    Region string `json:"region"`
}
```

#### Remove from `agent_go/pkg/orchestrator/agents/interfaces.go`

```go
// REMOVE: CrossProviderFallback struct
type CrossProviderFallback struct {
    Provider string   `json:"provider"`
    Models   []string `json:"models"`
}
```

---

### Phase 5: Frontend Changes

#### File: `frontend/src/services/api-types.ts`

```typescript
// LLM Configuration types
export interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string
  fallback_models: FallbackModel[]
  api_keys?: {
    openrouter?: string
    openai?: string
    bedrock?: {
      region: string
    }
    anthropic?: string
    vertex?: string
  }
}

// NEW: FallbackModel type
export interface FallbackModel {
  model_id: string
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  priority: number
}

// REMOVE: cross_provider_fallback from LLMConfiguration
```

#### File: `frontend/src/stores/useLLMStore.ts`

**New State:**
```typescript
interface LLMState extends StoreActions {
  primaryConfig: LLMConfiguration
  
  // Provider-specific configs (existing)
  openrouterConfig: ExtendedLLMConfiguration
  bedrockConfig: ExtendedLLMConfiguration
  // ...
  
  // NEW: Actions for fallback management
  addFallbackModel: (model: FallbackModel) => void
  removeFallbackModel: (modelId: string) => void
  reorderFallbackModels: (orderedModelIds: string[]) => void
  
  // NEW: Helper to get providers with valid API keys
  getConfiguredProviders: () => string[]
}
```

**New Actions:**
```typescript
addFallbackModel: (model) => {
  set((state) => ({
    primaryConfig: {
      ...state.primaryConfig,
      fallback_models: [
        ...state.primaryConfig.fallback_models,
        { ...model, priority: state.primaryConfig.fallback_models.length + 1 }
      ]
    }
  }))
},

removeFallbackModel: (modelId) => {
  set((state) => {
    const filtered = state.primaryConfig.fallback_models.filter(m => m.model_id !== modelId)
    // Renumber priorities
    const renumbered = filtered.map((m, i) => ({ ...m, priority: i + 1 }))
    return {
      primaryConfig: {
        ...state.primaryConfig,
        fallback_models: renumbered
      }
    }
  })
},

reorderFallbackModels: (orderedModelIds) => {
  set((state) => {
    const reordered = orderedModelIds.map((id, i) => {
      const model = state.primaryConfig.fallback_models.find(m => m.model_id === id)
      return { ...model!, priority: i + 1 }
    })
    return {
      primaryConfig: {
        ...state.primaryConfig,
        fallback_models: reordered
      }
    }
  })
},

getConfiguredProviders: () => {
  const state = get()
  const providers: string[] = []
  if (state.openrouterConfig.api_key) providers.push('openrouter')
  if (state.openaiConfig.api_key) providers.push('openai')
  if (state.anthropicConfig.api_key) providers.push('anthropic')
  if (state.vertexConfig.api_key) providers.push('vertex')
  if (state.bedrockConfig.region) providers.push('bedrock')
  return providers
}
```

#### File: `frontend/src/components/LLMConfigurationModal.tsx`

**New Fallback Section:**

```tsx
// Add drag-drop library: @dnd-kit/core, @dnd-kit/sortable

function FallbackModelsSection() {
  const { 
    primaryConfig, 
    addFallbackModel, 
    removeFallbackModel, 
    reorderFallbackModels,
    getConfiguredProviders 
  } = useLLMStore()
  
  const configuredProviders = getConfiguredProviders()
  
  return (
    <div className="fallback-models-section">
      <h3>🔄 Fallback Models (Priority Order)</h3>
      <p className="hint">Drag to reorder. Only providers with API keys are shown.</p>
      
      <DndContext onDragEnd={handleDragEnd}>
        <SortableContext items={primaryConfig.fallback_models.map(m => m.model_id)}>
          {primaryConfig.fallback_models.map((model, index) => (
            <SortableItem key={model.model_id} id={model.model_id}>
              <div className="fallback-item">
                <span className="priority">{index + 1}.</span>
                <span className="model-name">{model.model_id}</span>
                <span className="provider-badge">{model.provider}</span>
                <button onClick={() => removeFallbackModel(model.model_id)}>🗑️</button>
              </div>
            </SortableItem>
          ))}
        </SortableContext>
      </DndContext>
      
      <AddFallbackForm 
        availableProviders={configuredProviders}
        onAdd={addFallbackModel}
      />
    </div>
  )
}
```

---

## Migration & Backward Compatibility

### Env Var Migration

Old env vars will still work as initial defaults:
- `BEDROCK_FALLBACK_MODELS` → Provides initial same-provider fallbacks
- `OPENAI_FALLBACK_MODELS` → Provides initial same-provider fallbacks

These env vars should be REMOVED (no longer read):
- `BEDROCK_OPENAI_FALLBACK_MODELS`
- `OPENAI_BEDROCK_FALLBACK_MODELS`
- `OPENROUTER_CROSS_FALLBACK_MODELS`

### Data Migration

If user has old `cross_provider_fallback` config in localStorage, convert it:

```typescript
// In useLLMStore.ts initialization
const migrateOldConfig = (config: any): LLMConfiguration => {
  if (config.cross_provider_fallback) {
    // Convert old format to new format
    const crossProviderModels = config.cross_provider_fallback.models.map((modelId: string, i: number) => ({
      model_id: modelId,
      provider: config.cross_provider_fallback.provider,
      priority: (config.fallback_models?.length || 0) + i + 1
    }))
    
    const existingFallbacks = (config.fallback_models || []).map((modelId: string, i: number) => ({
      model_id: modelId,
      provider: config.provider,
      priority: i + 1
    }))
    
    return {
      ...config,
      fallback_models: [...existingFallbacks, ...crossProviderModels],
      cross_provider_fallback: undefined  // Remove old field
    }
  }
  return config
}
```

---

## Testing Plan

### Unit Tests

1. **Fallback priority ordering** - Verify models are tried in priority order
2. **Provider detection** - Verify `detectProviderFromModelID` works for all providers
3. **API key checking** - Verify `hasCredentialsForProvider` works correctly
4. **Env var defaults** - Verify env vars provide initial defaults when no UI config

### Integration Tests

1. **End-to-end fallback** - Primary model fails → fallbacks tried in order → success
2. **API key propagation** - Workflow sub-agents inherit API keys
3. **UI to backend flow** - Fallback models from UI reach agent

### Manual Tests

1. **UI ordering** - Drag-drop reordering works
2. **Add/remove fallbacks** - Can add models from any configured provider
3. **Persistence** - Fallback config persists across page reloads

---

## Files Changed Summary

| File | Action |
|------|--------|
| `multi-llm-provider-go/providers.go` | Remove `GetCrossProviderFallbackModels`, remove cross-provider env vars |
| `mcpagent/llm/providers.go` | Remove `GetCrossProviderFallbackModels` re-export |
| `mcpagent/agent/llm_generation.go` | Major refactor - unified fallback logic |
| `agent_go/pkg/orchestrator/base_orchestrator_types.go` | Update `LLMConfig`, add `FallbackModel` |
| `agent_go/pkg/orchestrator/agents/interfaces.go` | Remove `CrossProviderFallback` |
| `agent_go/pkg/orchestrator/base_orchestrator_agent_factory.go` | API key propagation |
| `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go` | API key inheritance |
| `agent_go/cmd/server/server.go` | Update request handling |
| `frontend/src/services/api-types.ts` | Update types |
| `frontend/src/stores/useLLMStore.ts` | Add fallback management |
| `frontend/src/components/LLMConfigurationModal.tsx` | Add fallback UI section |

---

## Implementation Order

```
1. External Library (multi-llm-provider-go)
   - Remove GetCrossProviderFallbackModels
   - Remove cross-provider env var reading
   ↓
2. mcpagent Layer
   - Remove GetCrossProviderFallbackModels re-export
   - Refactor llm_generation.go with unified fallback
   ↓
3. Backend Types (agent_go)
   - Update LLMConfig, add FallbackModel
   - Remove CrossProviderFallback
   ↓
4. API Key Propagation
   - Update agent factory, workflow controller
   ↓
5. Server
   - Update request handling for new format
   ↓
6. Frontend Types
   - Update api-types.ts
   ↓
7. Frontend Store
   - Add fallback management actions
   ↓
8. Frontend UI
   - Add drag-drop fallback section to LLMConfigurationModal
   ↓
9. Testing & Validation
```

