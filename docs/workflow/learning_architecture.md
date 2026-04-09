# Learning Architecture

This document describes the current workflow architecture as implemented now.

The older model in this repo treated learning as part of a larger learning-plus-validation architecture with LLM validation modes, per-step learning files, and exploration/exploitation prompt strategies. That is no longer the right mental model.

## Current Reality

Today the learning side of workflow runtime is built around two simpler ideas:

- **Learning writes into a shared global skill** at `learnings/_global/`.
- **Scripted code steps can also persist step-specific scripts** under their own learnings folder.

If you remember the older architecture, these are the most important updates:

- Learning is no longer primarily about per-step prose learnings.
- The learning agent now writes domain knowledge into a global skill folder, usually centered on `learnings/_global/SKILL.md`.
- For `learn_code` steps, `main.py` is the executable truth; `SKILL.md` is secondary guidance.

## Learning

### What learning writes now

The main learning destination is the **global skill**:

- `learnings/_global/SKILL.md`
- optional supporting files under:
  - `learnings/_global/references/`
  - `learnings/_global/scripts/`
  - other skill-structured files

The learning agent prompt in [`learning_agent.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/learning_agent.go) is explicit:

- accumulate **domain knowledge across all workflow steps**
- keep it focused on the target system
- merge findings into one shared skill
- follow skill structure, not old flat learning-note files

The controller also hardwires global learning mode in [`controller_learning.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning.go):

- `UseGlobalLearning = "true"`
- `ContributingStepID`
- `ContributingStepTitle`
- optional `GlobalSkillObjective`

### Step-specific learnings still exist, but differently

There are still step-specific artifacts, but they are no longer the main prose-learning model:

- `learn_code` / scripted steps save reusable scripts under `learnings/{step-id}/`
- especially `learnings/{step-id}/main.py`
- scripted steps may also keep `SKILL.md` notes for edge cases and repair hints
- metadata remains per step in `learnings/{step-id}/.learning_metadata.json`

So the current split is:

- **global domain knowledge** → `learnings/_global/`
- **step-specific executable artifacts** → `learnings/{step-id}/`

### Learning objective

The current system expects a workflow-level objective for the global skill:

- `global_skill_objective`

This tells the learning agent what kind of reusable knowledge should be accumulated, for example:

- auth flow patterns
- selectors
- API patterns
- common failure modes
- target-system structure

This is a better description of the current design than the older “extract learnings per step until stable” framing.

## Learning lifecycle

### Success learning

After a successful step:

- runtime can launch **success learning** in the background
- it reads recent execution history and validation result
- it updates the global skill
- it updates step metadata

This happens in [controller_learning.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning.go).

Important current details:

- success learning is the real active learning path
- learning detection via a separate LLM-based “did we learn something new?” phase has been removed
- metadata is updated using a rule-based path instead

### Auto-locking

Auto-locking still exists, but the old complexity-based thresholds in the deleted architecture doc are no longer accurate.

Current metadata logic in [`controller_learning_detection.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning_detection.go):

- per-step learnings auto-lock after **3 successful runs**
- fallback safety lock at **15 total iterations**
- global learning (`_global`) does **not** auto-lock

So the current rule is much simpler than the older “simple/medium/complex with 3/5/10 thresholds” model.

### Locking and disabling learning

The important current controls are:

- `disable_learning`
- `lock_learnings`
- `learning_detail_level`
- `global_skill_objective`

Recommended usage from workshop guidance:

- disable learning for steps that do not contribute useful domain knowledge
- lock learnings for mature steps after reviewing the global skill
- keep learning enabled for steps that interact directly with the target system

### Failure learning

The older docs described a full failure-learning architecture. That is not a good description of the current codebase.

What still exists:

- some comments, metadata fields, and workshop text still reference failure learning

What matters operationally now:

- the active, clearly implemented learning path is success learning into the global skill

Until failure-learning behavior is re-established as a first-class runtime path, docs should not present it as a central architecture pillar.

## Scripted code steps

For `learn_code` steps, learning and execution are intentionally split:

- `main.py` is the executable artifact
- `SKILL.md` is supporting knowledge
- global skill captures reusable domain knowledge
- step folder captures the reusable script and related metadata

That means learning for scripted steps is not just “write prose notes.” It is:

- maintain reusable code in `learnings/{step-id}/main.py`
- maintain reusable domain knowledge in `learnings/_global/`

See [learn_code_flow.md](learn_code_flow.md).

## Current file layout

### Global

```text
learnings/
  _global/
    SKILL.md
    references/
    scripts/
```

### Step-specific

```text
learnings/
  <step-id>/
    .learning_metadata.json
    SKILL.md                # optional supporting notes
    main.py                 # scripted steps
    scripts/
    diffs/
```

Not every step uses every file. The important distinction is:

- `_global/` is the shared workflow skill
- `<step-id>/` is the step-specific artifact area

## What to update in other docs

When editing related workflow docs, keep these rules consistent:

- describe learning as global-skill-first
- describe `lock_learnings` as a per-step contribution control, not as a switch for an older per-step learning world
- for scripted steps, describe `main.py` as the executable source of truth
- leave validation details to the dedicated pre-validation docs

## Code references

- [`controller_execution.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go): learning triggers and post-execution flow
- [`controller_learning.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning.go): success learning and global-skill write path
- [`learning_agent.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/learning_agent.go): global skill prompt and skill-structured output
- [`controller_learning_detection.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning_detection.go): metadata and auto-lock rules
- [`interactive_workshop_manager.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go): current user-facing guidance for global learning

## Related docs

- [pre_validation_guide.md](pre_validation_guide.md)
- [step_config_format_specification.md](step_config_format_specification.md)
- [learn_code_flow.md](learn_code_flow.md)
