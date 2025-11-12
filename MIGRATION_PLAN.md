# Migration Plan: agent_go to llm-providers (No Backward Compatibility)

## Overview
Replace all duplicate LLM provider implementation and llmtypes in `agent_go` with direct usage of the external `llm-providers` package. No backward compatibility layer - all code will be updated to use `llm-providers` types and functions directly.

## Goals
1. Remove duplicate LLM provider code from `agent_go/internal/llm`
2. Migrate `llmtypes` from `agent_go/internal/llmtypes` to `llm-providers/llmtypes`
3. Update all callers to use `llm-providers` directly
4. Remove all duplicate adapter implementations

## Implementation Steps

### 1. Add replace directive to agent_go/go.mod
- Add `replace llm-providers => ../llm-providers` to point to local module
- Run `go mod tidy` to update dependencies

### 2. Migrate llmtypes imports (44+ files)
Update all imports from `mcp-agent/agent_go/internal/llmtypes` to `llm-providers/llmtypes`

**Key files to update:**
- `agent_go/internal/llm/providers.go`
- `agent_go/internal/llm/events.go`
- `agent_go/internal/llm/types.go`
- `agent_go/pkg/mcpclient/tool_convert.go`
- `agent_go/pkg/mcpagent/agent.go`
- `agent_go/pkg/mcpagent/conversation.go`
- `agent_go/pkg/events/data.go`
- `agent_go/pkg/mcpagent/llm_generation.go`
- `agent_go/pkg/external/llm.go`
- `agent_go/pkg/external/agent.go`
- `agent_go/pkg/agentwrapper/llm_agent.go`
- `agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go`
- All test files in `agent_go/cmd/testing/` (20+ files)
- All other files importing `internal/llmtypes`

**After migration:**
- Delete `agent_go/internal/llmtypes/` directory

### 3. Update all LLM provider imports and usage
Update all imports from `mcp-agent/agent_go/internal/llm` to `llmproviders "llm-providers"`

**Type changes:**
- `llm.Provider` → `llmproviders.Provider`
- `llm.Config` → `llmproviders.Config`

**Function changes:**
- `llm.InitializeLLM` → `llmproviders.InitializeLLM`

**Key files to update:**
- `agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go`
- `agent_go/pkg/agentwrapper/llm_agent.go`
- `agent_go/pkg/external/llm.go`
- `agent_go/pkg/external/config.go`
- `agent_go/pkg/external/agent_builder.go`
- `agent_go/pkg/mcpagent/agent.go`
- All test files in `agent_go/cmd/testing/` (20+ files)

### 4. Create adapter utilities for type conversion
Create new file: `agent_go/internal/llm/adapters.go`

**Contents:**
- `loggerAdapter` struct implementing `interfaces.Logger` wrapping `utils.ExtendedLogger`
- `ConvertConfig` function to convert agent_go style config to `llmproviders.Config`:
  - Convert `observability.Tracer[]` to `interfaces.EventEmitter` using `NewEventEmitterAdapter`
  - Convert `utils.ExtendedLogger` to `interfaces.Logger` using logger adapter
  - Convert `observability.TraceID` to `interfaces.TraceID`

### 5. Update agent_go/internal/llm/providers.go
**Remove:**
- `Provider` type and constants (use `llmproviders.Provider` directly)
- `Config` struct (use `llmproviders.Config` directly)
- `InitializeLLM` function (use `llmproviders.InitializeLLM` directly)
- All `initialize*` functions (bedrock, openai, anthropic, openrouter, vertex)
- `ProviderAwareLLM` wrapper

**Keep (if agent_go specific, update to use llmproviders types):**
- `ValidateProvider` → use `llmproviders.Provider` type
- `GetDefaultFallbackModels` → update return types
- `GetCrossProviderFallbackModels` → update return types

**Or:** Delete this file entirely if it only contains wrappers

### 6. Update agent_go/internal/llm/events.go
- Update import to use `llm-providers/llmtypes` instead of `internal/llmtypes`
- Ensure `EventEmitterAdapter` properly converts between types
- Verify all event emission methods work correctly

### 7. Remove duplicate code
**Delete directories:**
- `agent_go/internal/llm/anthropicadapter/`
- `agent_go/internal/llm/bedrockadapter/`
- `agent_go/internal/llm/openaiadapter/`
- `agent_go/internal/llm/vertex/`
- `agent_go/internal/llmtypes/`

**Review and potentially delete:**
- `agent_go/internal/llm/providers.go` (if only contains wrappers)
- `agent_go/internal/llm/types.go` (if empty or duplicate)

### 8. Verify and test
- Build `agent_go` module: `cd agent_go && go build ./...`
- Run tests to ensure all imports resolve correctly
- Verify all LLM provider calls work with llm-providers

## Files Summary

### Files to Modify
- `agent_go/go.mod` - Add replace directive
- `agent_go/internal/llm/adapters.go` - **NEW FILE** with adapter utilities
- `agent_go/internal/llm/providers.go` - Remove or simplify to adapter utilities only
- `agent_go/internal/llm/events.go` - Update llmtypes import
- 44+ files importing `internal/llmtypes` - Update to `llm-providers/llmtypes`
- All files using `llm.Provider`, `llm.Config`, `llm.InitializeLLM` - Update to use `llmproviders` directly

### Files to Delete
- `agent_go/internal/llm/anthropicadapter/` (entire directory)
- `agent_go/internal/llm/bedrockadapter/` (entire directory)
- `agent_go/internal/llm/openaiadapter/` (entire directory)
- `agent_go/internal/llm/vertex/` (entire directory)
- `agent_go/internal/llmtypes/` (entire directory)
- `agent_go/internal/llm/providers.go` (if only contains wrappers)
- `agent_go/internal/llm/types.go` (if empty or duplicate)

## Migration Strategy
- **No backward compatibility layer** - direct migration to llm-providers
- All callers must be updated to use `llmproviders` package directly
- Adapter utilities only for type conversion (observability → interfaces)
- Remove all duplicate implementations

## Checklist

- [ ] Add replace directive to `agent_go/go.mod`
- [ ] Run `go mod tidy` in `agent_go`
- [ ] Update all 44+ `llmtypes` imports
- [ ] Create `adapters.go` with logger and config conversion utilities
- [ ] Update all `llm` package imports to `llmproviders`
- [ ] Update all `llm.Provider` references to `llmproviders.Provider`
- [ ] Update all `llm.Config` references to `llmproviders.Config`
- [ ] Update all `llm.InitializeLLM` calls to `llmproviders.InitializeLLM`
- [ ] Update `providers.go` (remove or simplify)
- [ ] Update `events.go` llmtypes import
- [ ] Delete duplicate adapter directories
- [ ] Delete `internal/llmtypes` directory
- [ ] Review and clean up `providers.go` and `types.go`
- [ ] Build and verify all imports work
- [ ] Run tests to ensure functionality

## Notes
- The `llm-providers` module is already created and organized
- Both `llmtypes` packages are identical, so migration should be straightforward
- `EventEmitterAdapter` already exists in `events.go` and bridges to observability
- Logger adapter needs to be created to bridge `utils.ExtendedLogger` to `interfaces.Logger`

