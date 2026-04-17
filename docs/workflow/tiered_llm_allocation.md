# Tiered LLM Allocation

## Overview

Tiered allocation is the workflow's auto-selection mode for runtime LLM choice.

- **Manual mode**: preset and step configs choose explicit models
- **Tiered mode**: the runtime resolves Tier 1/2/3 from a per-agent-type default plus a few explicit overrides
- **Phase LLM**: configured separately and not part of the tier resolver

The current resolver lives in `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/tiered_llm.go`.

## Tiers

- **Tier 1**: High reasoning
- **Tier 2**: Medium reasoning
- **Tier 3**: Low reasoning

## Default Tier per Agent Type

| Agent | Default Tier |
|---|---|
| Execution | Tier 1 (High) |
| Learning | Tier 2 (Medium) |
| Conditional | Tier 1 (High) |

Learning-maturity-based auto-downgrade has been removed. Tier selection no longer
looks at the contents of the learnings folder. To run a step on a cheaper tier,
use one of the explicit overrides below (`preferred_tier`, workshop `tier`
argument, or the per-step `execution_llm` override).

`disable_tier_optimization=true` still forces execution and conditional agents
to Tier 1. The `optimized` flag no longer affects tier selection.

## Selection Priority

### Execution agents

Current priority in `selectExecutionLLM()`:

1. step `execution_llm`, always when set
2. `sub_agent_llm` from context, unless dynamic tier selection is enabled
3. tiered resolution
4. no valid config => error

Inside tiered resolution, execution uses this order:

1. workshop tier override from context
2. `preferred_tier` from sub-agent context
3. `disable_tier_optimization=true` => Tier 1
4. evaluation mode => Tier 2
5. default execution tier (Tier 1)

### Learning agents

Current priority in `selectLearningLLM()`:

1. step `learning_llm`, only when tiered mode is **not** active
2. tiered learning resolution
3. no valid config => error

### Conditional agents

Conditional agents use the tier resolver directly:

1. `disable_tier_optimization=true` => Tier 1
2. otherwise default conditional tier (Tier 1)

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

Tier selection for sub-agents is automatically controlled by whether the parent
todo-task step pins an `execution_llm`:

- **Parent step has no `execution_llm`** → dynamic tier selection is on:
  - todo-task tools expose `preferred_tier` as a REQUIRED parameter
  - the orchestrator must choose Tier 1/2/3 for every sub-agent call; calls without `preferred_tier` are rejected by the handler
  - `preferred_tier` flows through context and is honored by `selectExecutionLLM`

- **Parent step has `execution_llm` set** → dynamic tier selection is off:
  - the parent step's `execution_llm` is propagated to every sub-agent spawned by the orchestrator via the `sub_agent_llm` context key
  - `preferred_tier` is not exposed on the sub-agent tools
  - all sub-agents use the pinned parent LLM

There is no separate `enable_dynamic_tier_selection` flag — the mode is derived
directly from the presence of `execution_llm` on the parent step.

## Manual vs Tiered Mode

In tiered mode:

- per-step `execution_llm` still overrides the execution selector directly
- per-step `learning_llm` is still ignored by the main learning selector
- runtime otherwise uses the tier resolver
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

This is populated for tiered start events using the default execution tier.

## Key Files

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/tiered_llm.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/final_output.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/workflow_events.go`
