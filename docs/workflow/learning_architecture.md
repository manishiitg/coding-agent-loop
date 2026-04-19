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

### Auto-locking (description-hash scoped)

Auto-locking is driven by a **description-hash-scoped counter**, not a raw total-runs counter.

Current metadata logic in [`controller_learning_detection.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learning_detection.go):

- the step description is hashed (SHA256 of trimmed `step.GetDescription()`) on every successful run
- if the hash matches the previously-stored `last_description_hash`, `description_hash_runs` increments
- if the hash differs, `description_hash_runs` resets to 1 and the stored hash is updated
- per-step learnings auto-lock when `description_hash_runs` reaches **3** — i.e. three successful runs in a row against the *same* description
- fallback safety lock at **15 total iterations**
- global learning (`_global`) does **not** auto-lock

Editing the step description invalidates the "converged" signal: the next run resets the counter, and the step must accumulate 3 fresh successful runs against the new description before re-locking.

### Auto-unlock on description change

If a step was **auto-locked** (metadata has `auto_locked_at`) and its description later changes, the next successful run will:

- detect the hash change
- clear `auto_locked_at` in metadata, set `auto_unlocked_at`
- clear `lock_learnings` and `optimized` in `step_config.json` (the two flags always move together)
- emit a step-config-updated event so the frontend reflects the unlock

Manual locks — steps where `lock_learnings: true` was set by a human without an `auto_locked_at` record — are **never** auto-unlocked. Auto-unlock is strictly the inverse of auto-lock.

### Locking and disabling learning

The important current controls on `AgentConfigs`:

- `learnings_access` (string enum: `"read" | "read-write" | "none"`) — primary gate. Mirrors `knowledgebase_access`.
  - `"read"` (default) — step sees `_global/SKILL.md` in its prompt; does NOT contribute.
  - `"read-write"` — step reads AND contributes. Requires `learning_objective` to be non-empty.
  - `"none"` — step neither reads nor contributes. The true disable.
- `learning_objective` (string) — the **extraction instruction** for the post-step learning agent. Required when access is `"read-write"`. No longer a gate.
- `lock_learnings` (bool) — freezes the learning agent for this step even while access is `"read-write"`. Existing `SKILL.md` still flows into execution prompts. Auto-unlock clears this when the description changes (for auto-locked steps only).
- `global_skill_objective` (workflow-level, not per-step) — describes what domain knowledge the global skill should accumulate.

Auto-migration for legacy configs (runtime-only, no file rewrites): if `learnings_access` is unset, `learning_objective` non-empty infers `"read-write"`; empty infers `"read"`.

Recommended usage:

- leave `learnings_access` unset (defaults to `"read"`) for most steps — they benefit from cross-step context.
- set `learnings_access: "read-write"` + a non-empty `learning_objective` on steps that produce durable HOW-knowledge about the target system.
- set `learnings_access: "none"` for steps that are truly throwaway or whose context would pollute the global skill (e.g. pure file moves, human-input steps — the latter is forced to `"none"` automatically).
- let auto-lock + auto-unlock handle the lifecycle of `lock_learnings` — manual locks are for curated SKILL.md content the human wants frozen regardless of description changes.

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
- gate read access and write contribution through `learnings_access` — `lock_learnings` is a freeze switch, not the enable/disable mechanism
- for scripted steps, describe `main.py` as the executable source of truth
- auto-lock is scoped to the **description hash**; editing the description invalidates the lock countdown and auto-unlocks previously-auto-locked steps
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
