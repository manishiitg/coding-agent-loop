# Tiered LLM Allocation System

## Overview

The Tiered LLM Allocation system provides an alternative to the default "Fixed Models" mode where each agent type (execution, validation, learning, phase) has a manually selected LLM. In **Tiered Auto** mode, users configure 3 LLM tiers at the preset level, and the system automatically selects the appropriate tier based on **learning maturity**.

## Modes

The two allocation modes are **mutually exclusive** at the preset level:

| | Fixed Models (default) | Tiered Auto |
|---|---|---|
| Preset config | 4 LLM dropdowns (Execution, Validation, Learning, Phase) | 3 tier dropdowns + Phase LLM dropdown |
| Phase LLM | Configurable | Configurable (falls back to Tier 1 if not set) |
| Temp LLM overrides | Available | Disabled |
| Per-step LLM overrides | Available | Disabled |
| LLM selection | Manual | Automatic via tier resolver (except Phase) |

## Tier Configuration

- **Tier 1 (High Reasoning)**: Most capable model for complex first-time tasks
- **Tier 2 (Medium Reasoning)**: Balanced model for tasks with existing learnings
- **Tier 3 (Low Reasoning)**: Cost-efficient model for validation and mature learnings

## Allocation Rules

The tier resolver selects the LLM based on agent type and learning maturity:

| Agent Type | No Learnings (0 files) | Has Learnings (1 file) | Mature (2+ files) |
|---|---|---|---|
| Execution | Tier 1 | Tier 1 | Tier 2 |
| Learning | Tier 2 | Tier 2 | Tier 3 |
| Validation | Tier 3 | Tier 3 | Tier 3 |
| Phase Agents | Phase LLM (or Tier 1 fallback) | Phase LLM (or Tier 1 fallback) | Phase LLM (or Tier 1 fallback) |
| Conditional | Tier 1 | Tier 1 | Tier 2 |

Learning maturity is determined by counting files in the step's learnings folder (excluding metadata).

**Phase Agents**: In tiered mode, phase agents (planning, evaluation, anonymization, plan improvement, etc.) use the explicitly configured Phase LLM if set, otherwise fall back to Tier 1. This allows users to configure a separate model for phase agents even when using tiered allocation for execution agents.

## Backend Architecture

### Key Files

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/tiered_llm.go` - Core types and `TierResolver`
- `agent_go/pkg/database/models.go` - `PresetLLMConfig` extended with `llm_allocation_mode` and `tiered_config`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go` - `getLearningMaturity()` helper, tiered mode fields on orchestrator
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go` - Tiered checks at top of `selectExecutionLLM`, `selectValidationLLM`, `selectLearningLLM`
- `agent_go/pkg/orchestrator/types/workflow_orchestrator.go` - Tiered config extraction and propagation

### Flow

1. Preset is saved with `llm_allocation_mode: "tiered"` and `tiered_config` containing 3 tier LLM configs
2. `NewWorkflowOrchestrator` extracts tiered config and passes it through to `NewStepBasedWorkflowOrchestrator`
3. Controller creates a `TierResolver` and sets `useTieredMode = true`
4. When `selectExecutionLLM` / `selectValidationLLM` / `selectLearningLLM` are called, the tiered check at the top short-circuits all manual selection logic
5. Phase agents use explicitly configured Phase LLM if set, otherwise fall back to Tier 1 at construction time

### Backward Compatibility

- Existing presets with no `llm_allocation_mode` field default to `"manual"` and work identically
- The tiered config fields are stored as JSON within the existing `llm_config` column (no DB migration needed)

## Frontend Architecture

### Key Files

- `frontend/src/services/api-types.ts` - `PresetLLMConfig` type extended
- `frontend/src/components/PresetModal.tsx` - Mode toggle + 3-tier config UI
- `frontend/src/components/workflow/canvas/WorkflowToolbar.tsx` - Temp LLM button hidden in tiered mode
- `frontend/src/components/workflow/BulkStepConfigModal.tsx` - Per-step LLM dropdowns replaced with info panel in tiered mode
- `frontend/src/components/events/orchestrator/StepProgressUpdatedEvent.tsx` - Tier badge display

### UI

In the Preset Modal, a segmented control toggles between **Fixed Models** and **Tiered Auto**:

- **Fixed Models**: Shows the existing 4 agent-specific LLM dropdowns (Execution, Validation, Learning, Phase)
- **Tiered Auto**: Shows 3 tier dropdowns (Tier 1/2/3) plus a Phase LLM dropdown with an info panel explaining auto-selection rules

When tiered mode is active:
- The temp LLM override button in the toolbar is disabled with a tooltip
- The BulkStepConfigModal LLM section shows an info panel instead of dropdowns
- Phase LLM dropdown is still available, allowing a separate model for phase agents (falls back to Tier 1 if not configured)

## TodoTask Sub-Agent Tier Selection

When tiered mode is active, todo_task steps gain two additional capabilities configured via `agent_configs`:

### Orchestrator Tier Selection

- Field: `todo_task_orchestrator_tier` (integer, 1/2/3)
- Controls which tier the todo task orchestrator agent itself uses
- Current default behavior: todo task orchestrators stay on Tier 1 (High) unless an explicit direct override is configured
- UI: Dropdown in the BulkStepConfigModal tiered mode section (only shown when todo_task steps exist)

### Dynamic Sub-Agent Tier Selection

- Field: `enable_dynamic_tier_selection` (boolean)
- When enabled, `call_sub_agent` and `call_generic_agent` tools gain an optional `preferred_tier` parameter (1/2/3)
- The orchestrator agent receives system prompt guidance on when to use each tier
- The preferred tier is propagated via Go context (`PreferredTierContextKey`) through the execution chain
- `selectExecutionLLM` checks context for the tier override before maturity-based auto-resolution
- Only the execution LLM is overridden; validation (always Tier 3) and learning (own progression) are unaffected
- UI: Toggle switch in the BulkStepConfigModal tiered mode section

## Direct LLM Configuration (Works in Both Modes)

In addition to tier-based selection, users can specify exact LLM provider/model for orchestrators and sub-agents. These direct overrides work in **both tiered and manual modes** and take highest priority.

### Orchestrator LLM Override

- Field: `orchestrator_llm` (object with `provider` and `model_id`)
- Directly specifies the LLM for the todo task orchestrator agent
- **Highest priority**: Overrides both `todo_task_orchestrator_tier` and maturity-based selection
- Works in both tiered and manual allocation modes
- UI: LLM dropdown in the BulkStepConfigModal todo task settings section

### Sub-Agent LLM Override

- Field: `sub_agent_llm` (object with `provider` and `model_id`)
- Directly specifies the LLM for ALL sub-agents spawned by the orchestrator
- **Highest priority**: Overrides `preferred_tier`, maturity-based selection, and manual mode settings
- Works in both tiered and manual allocation modes
- Propagated via Go context (`SubAgentLLMContextKey`) through the execution chain
- UI: LLM dropdown in the BulkStepConfigModal todo task settings section

### Priority Order

LLM selection follows this priority (highest to lowest):

**For Orchestrator Agent:**
1. `orchestrator_llm` (direct LLM override) - works in both modes
2. `todo_task_orchestrator_tier` (tiered mode only)
3. `selectExecutionLLM()` fallback (mode-dependent)

**For Sub-Agents:**
1. `sub_agent_llm` from context (direct LLM override) - works in both modes
2. `preferred_tier` from context (tiered mode only, set by orchestrator)
3. Maturity-based tier resolution (tiered mode only)
4. Manual mode step config / preset settings

### Flow

1. User configures `todo_task_orchestrator_tier` and/or `enable_dynamic_tier_selection` on a todo_task step
2. Orchestrator tier: `selectTodoTaskOrchestratorLLM` checks the field → resolves from `TierResolver.ResolveTier()`
3. Dynamic tier: `CreateSubAgentTools(true)` adds `preferred_tier` parameter to both tools
4. Tool handler extracts `preferred_tier` from args → sets in Go context via `context.WithValue`
5. Context propagates through `wrapSubAgentToolExecutor` → `executeSingleStep` → `selectExecutionLLM`
6. `selectExecutionLLM` checks `PreferredTierContextKey` in context before maturity-based resolution

### Key Files

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go` - `AgentConfigs` struct fields (`orchestrator_llm`, `sub_agent_llm`, `todo_task_orchestrator_tier`, `enable_dynamic_tier_selection`)
- `agent_go/cmd/server/virtual-tools/sub_agent_tools.go` - `PreferredTierContextKey`, `SubAgentLLMContextKey`, parameterized `CreateSubAgentTools(bool)`, handler extraction
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go` - `selectTodoTaskOrchestratorLLM` checks `orchestrator_llm` first, then tier, `buildTodoTaskOrchestratorTemplateVars` dynamic tier flag
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go` - `wrapSubAgentToolExecutor` injects `sub_agent_llm` into context, `selectExecutionLLM` checks `SubAgentLLMContextKey` first, then `PreferredTierContextKey`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/todo_task_orchestrator_agent.go` - System prompt conditional tier guidance section
- `frontend/src/utils/stepConfigMatching.ts` - `AgentConfigs` interface fields
- `frontend/src/components/workflow/BulkStepConfigModal.tsx` - Tier controls and direct LLM dropdowns in both tiered and manual mode panels

## Events

The `StepProgressUpdatedEvent` includes optional `used_tier` (integer) and `used_tier_label` (string) fields populated when tiered mode is active, displayed as a colored badge in the events UI.
