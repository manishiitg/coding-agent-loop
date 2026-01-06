# User-Driven LLM Fallback System

**Status**: Implementation Complete
**Updated**: 2026-01-05

---

## 📋 Overview

User-controlled LLM configuration where the **Primary LLM is selected from Published LLMs**. Each Published LLM carries its own API key, temperature, and model-specific options. Backend requires no API keys at startup.

**Key Principles:**
- Primary LLM = Selected from Published LLMs (not configured directly)
- Self-contained auth per Published LLM
- Backend uses ambient credentials (AWS) for internal operations only
- Simple ordered fallback array

---

## 📁 Key Files & Locations

| Component | File | Key Functions |
|-----------|------|---------------|
| **LLM Store** | `frontend/src/stores/useLLMStore.ts` | `savedLLMs`, `refreshAvailableLLMs()`, `getCurrentLLMOption()` |
| **Published LLM Tab** | `frontend/src/components/llm/LibraryTab.tsx` | Publish/delete/select LLMs |
| **Fallbacks Tab** | `frontend/src/components/llm/FallbacksTab.tsx` | Primary selection, fallback chain |
| **LLM Dropdown** | `frontend/src/components/LLMSelectionDropdown.tsx` | Rich metadata display |
| **LLM Types** | `frontend/src/types/llm.ts` | `LLMOption` interface |
| **API Types** | `frontend/src/services/api-types.ts` | `SavedLLM`, `LLMModel`, `AgentLLMConfiguration` |
| **Backend Server** | `agent_go/cmd/server/server.go` | No internal LLM required |

---

## 🔄 How It Works

```
User configures LLM in Provider Tab (OpenRouter, Bedrock, etc.)
    ↓
Publishes to "Published LLM" list (saves API key, temp, options)
    ↓
Selects Published LLM as Primary (from Fallbacks or Published LLM tab)
    ↓
LLM Dropdown shows all Published LLMs with metadata
    ↓
Agent execution uses Published LLM config (with its stored API key)
```

---

## 🏗️ Architecture

### Data Flow

| Step | Frontend | Backend |
|------|----------|---------|
| 1. Configure | Provider tabs (OpenRouter, Bedrock, etc.) | - |
| 2. Publish | `saveLLM()` → `savedLLMs[]` | - |
| 3. Select Primary | `handleLibrarySelect()` → `agentConfig.primary` | - |
| 4. Execute | Sends `LLMConfig` with API key | Uses provided credentials |

### Types

```typescript
// Published LLM (stored in frontend)
interface SavedLLM extends LLMModel {
  id: string
  name: string
  api_key?: string      // Stored per-LLM
  temperature?: number
  options?: Record<string, unknown>  // reasoning_effort, thinking_level, etc.
}

// Agent configuration sent to backend
interface AgentLLMConfiguration {
  primary: LLMModel     // Selected from savedLLMs
  fallbacks: LLMModel[] // Ordered fallback chain
}
```

---

## ⚙️ Configuration

### Backend Defaults (run_server_with_logging.sh)

| Variable | Value | Purpose |
|----------|-------|---------|
| `DEEP_SEARCH_MAIN_LLM_PROVIDER` | `bedrock` | Uses AWS credentials (no API key) |
| `DEEP_SEARCH_MAIN_LLM_MODEL` | `global.anthropic.claude-sonnet-4-5-*` | Default model |
| `AGENT_PROVIDER` | `bedrock` | Internal operations only |

**Note:** Backend no longer creates an `internalLLM` at startup. All agent execution uses Published LLM configs from frontend.

---

## 🛠️ UI Components

| Component | Purpose | Key Features |
|-----------|---------|--------------|
| **Published LLM Tab** | Manage saved configs | Publish current, set as primary, delete, show API key last 4 digits |
| **Fallbacks Tab** | Configure fallback chain | Change Primary button, add from Published LLM or custom |
| **LLM Dropdown** | Select LLM for execution | Rich metadata (cost, context, temp, reasoning options) |

### LLM Dropdown Display

```
┌─────────────────────────────────────────┐
│ OPENROUTER                              │
│   My GPT-4o Config                      │
│   gpt-4o                                │
│   📦 128k  💲$2.50/1M  🌡️ 0.7          │
│   Reasoning: medium                     │
├─────────────────────────────────────────┤
│ BEDROCK                                 │
│   Production Claude                     │
│   claude-sonnet-4.5                     │
│   📦 200k  💲$3.00/1M  🌡️ 0.0          │
└─────────────────────────────────────────┘
```

---

## 🔍 For LLMs: Quick Reference

**Constraints:**
- ✅ Primary LLM must be selected from Published LLMs
- ✅ Each Published LLM stores its own API key
- ✅ Backend uses Bedrock (AWS credentials) for internal ops
- ❌ No hardcoded API keys in backend
- ❌ No `internalLLM` created at startup

**Key Store Actions:**
```typescript
// Publish current config
saveLLM(llm, name, modelName, authMethod)

// Select as primary (from FallbacksTab or LibraryTab)
handleLibrarySelect(savedLLM) → updates agentConfig.primary

// Refresh dropdown options
refreshAvailableLLMs() → builds from savedLLMs with metadata
```

**Auto-refresh triggers:**
- `saveLLM()` → calls `refreshAvailableLLMs()`
- `deleteSavedLLM()` → calls `refreshAvailableLLMs()`
- `loadDefaults()` → calls `refreshAvailableLLMs()`

---

## 📖 Related Documentation

- [Model Metadata](../../multi-llm-provider-go/llmtypes/model_metadata.go) - Pricing, context, capabilities
- [LLM Store](../../frontend/src/stores/useLLMStore.ts) - State management
- [Doc Writing Guide](../../../mcpagent/docs/doc_writing_guide.md) - Documentation standards
