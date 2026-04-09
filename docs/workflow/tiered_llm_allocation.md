# Tiered LLM Allocation

## Overview

Tiered allocation is the workflow's auto-selection mode for runtime LLM choice.

- **Manual mode**: preset and step configs choose explicit models
- **Tiered mode**: the runtime resolves Tier 1/2/3 from learning maturity and a few explicit overrides
- **Phase LLM**: configured separately and not part of the maturity resolver

The current resolver lives in `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/tiered_llm.go`.

## Tiers

- **Tier 1**: High reasoning
- **Tier 2**: Medium reasoning
- **Tier 3**: Low reasoning

## Learning Maturity

The runtime uses four maturity states:

- `NoLearnings`: no learning files
- `HasLearnings`: one learning file
- `MatureLearnings`: two or more learning files
- `LockedLearnings`: step is effectively optimized/locked and can use the lowest viable tier

Current maturity behavior from `getLearningMaturity()`:

- empty learnings folder => `NoLearnings`
- one file => `HasLearnings`
- one file with `learning_mode == "human_assisted"` => `MatureLearnings`
- two or more files => `MatureLearnings`

At execution time, optimized steps are pushed to `LockedLearnings` when they already have learnings:

- `optimized=true` and maturity `>= HasLearnings` => treat as `LockedLearnings`

If `disable_tier_optimization=true`, execution and conditional agents stay on Tier 1 regardless of maturity.

## Current Resolver Rules

| Agent / Path | No Learnings | Has Learnings | Mature | Locked |
|---|---|---|---|---|
| Execution | Tier 1 | Tier 1 | Tier 2 | Tier 3 |
| Learning | Tier 2 | Tier 2 | Tier 3 | Tier 2 |
| Conditional | Tier 1 | Tier 1 | Tier 2 | Tier 3 |

Notes:

- There is no dedicated validation tier resolver in the current main workflow path.
- `LockedLearnings` is only used by execution and conditional resolution.
- Learning agents do not have a separate locked branch; they use the normal learning resolver.

## Selection Priority

### Execution agents

Current priority in `selectExecutionLLM()`:

1. `sub_agent_llm` from context, unless dynamic tier selection is enabled
2. temp LLM override (`tempLLM1` / `tempLLM2`) when allowed
3. step `execution_llm`, only when tiered mode is **not** active
4. tiered resolution
5. no valid config => error

Inside tiered resolution, execution uses this order:

1. workshop tier override from context
2. `preferred_tier` from sub-agent context
3. `disable_tier_optimization=true` => Tier 1
4. evaluation mode => Tier 2
5. maturity-based resolution

### Learning agents

Current priority in `selectLearningLLM()`:

1. `temp_learning_llm`
2. cost-optimization temp LLM after stability threshold
3. step `learning_llm`, only when tiered mode is **not** active
4. tiered learning resolution
5. no valid config => error

### Conditional agents

Conditional agents use the tier resolver directly:

1. `disable_tier_optimization=true` => Tier 1
2. otherwise maturity-based conditional resolution

## Phase LLM

Phase agents are independent of tiered maturity selection.

- planning/evaluation-design/debugging-style phase work uses the configured `presetPhaseLLM`
- the phase LLM is set separately from Tier 1/2/3
- if no phase LLM is configured, those phase paths can fail

One exception exists:

- final output generation prefers **Tier 2** in tiered mode and only falls back to `presetPhaseLLM` if Tier 2 cannot be resolved

## Todo Task Behavior

Todo-task steps add two tier-related controls in `agent_configs`.

### Orchestrator tier

- `orchestrator_llm`: exact model override, highest priority
- `todo_task_orchestrator_tier`: explicit tier override in tiered mode
- default: Tier 1

Current priority in `selectTodoTaskOrchestratorLLM()`:

1. `orchestrator_llm`
2. `todo_task_orchestrator_tier`
3. Tier 1

### Dynamic sub-agent tier selection

When tiered mode is active, dynamic tier selection is effectively default-on unless the step explicitly sets:

- `enable_dynamic_tier_selection=false`

If dynamic selection is enabled:

- todo-task tools expose optional `preferred_tier`
- the orchestrator can choose Tier 1/2/3 per sub-agent call
- `preferred_tier` is passed through context and checked before maturity-based resolution
- `sub_agent_llm` is intentionally skipped while dynamic tier selection is enabled

If dynamic selection is disabled:

- `sub_agent_llm` can directly force the sub-agent model

## Manual vs Tiered Mode

In tiered mode:

- per-step `execution_llm` and `learning_llm` are ignored by the main selectors
- runtime uses the tier resolver instead
- `orchestrator_llm` and `sub_agent_llm` still work because they are separate todo-task overrides

In manual mode:

- explicit step/preset model config is used
- tier resolver is absent

## Frontend Notes

Relevant UI files:

- `frontend/src/components/PresetModal.tsx`
- `frontend/src/components/WorkflowLLMConfigModal.tsx`
- `frontend/src/components/events/orchestrator/StepEditPanel.tsx`
- `frontend/src/components/workflow/BulkStepConfigModal.tsx`

The UI still exposes:

- preset tier configuration
- independent Phase LLM
- todo-task orchestrator tier
- dynamic tier selection toggle
- direct `orchestrator_llm` and `sub_agent_llm` overrides

## Events

`StepProgressUpdatedEvent` can include:

- `used_tier`
- `used_tier_label`

This is populated for tiered start events using execution maturity resolution.

## Key Files

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/tiered_llm.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/final_output.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/workflow_events.go`
