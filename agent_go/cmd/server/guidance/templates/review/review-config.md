Audit every step in this workflow's plan and recommend the right values for: declared_execution_mode, knowledgebase_access, knowledgebase_contribution, lock_learnings, and lock_code (learn_code steps only). This is a READ-ONLY analysis pass — do NOT apply any changes via update_step_config. Produce a recommendation table the user will review and apply selectively.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

PROCEDURE
1. Read planning/plan.json and planning/step_config.json. For each step capture: id, title, description, declared_execution_mode, and current AgentConfigs (knowledgebase_access, knowledgebase_contribution, lock_learnings, lock_code, optimized, successful_runs, description_hash).
2. For each step, inspect learnings/{step-id}/:
   - SKILL.md exists? size? recent edit signal (read first/last lines for context).
   - main.py exists? Read learnings/{step-id}/script_metadata.json for successful_runs counts (code_exec/learn_code) and recent failure history.
3. Sample run evidence from runs/iteration-0/{first-group}/logs/{step-id}/ when available:
   - learn_code_fast_path.json — recent main.py outcomes.
   - pre_validation.json — validation pass/fail.
   - Any signs of recent fix-loop rewrites or transient failures.
4. Read experiments/active.json and builder/improve.md if present. Note active experiment status and whether any active intervention touches the step, its prompt/config, learnings, validation, KB, db schema, or upstream/downstream context dependencies.

DECISION RULES (apply per step)

EXECUTION MODE FIT (code_exec | learn_code):
- Browser/UI-heavy steps should generally be `code_exec`, not `learn_code`. If a step uses browser capability, live auth/session state, dynamic selectors, pagination/lazy-loading, or third-party page timing and is currently `learn_code` with saved main.py, flag it. Recommend `declared_execution_mode="code_exec"` unless the user explicitly requested scripted browser execution AND the script has durable selectors, state-driven waits, fresh snapshots, and proven stability across runs.
- Recommend `learn_code` only when ALL are true:
  - The step behavior is stable, mechanical, and deterministic on a stable input shape.
  - The step does not need per-instance LLM judgment, adaptive browsing, or live UI interpretation.
  - The step has 10+ successful runs across 2+ variable groups.
  - Eval/metric evidence is at or above target across the recent measurement window.
  - No active experiment is touching this step, its prompt/config/learnings/validation, or adjacent upstream/downstream contracts. If any related experiment is proposed, awaiting approval, measuring, or evaluating, recommend deferring learn_code promotion/locking until the experiment concludes.
- If a current `learn_code` step violates any of the above, recommend reverting to or keeping `code_exec`, and clear/avoid lock_code until stability is proven after experiments conclude.

KB_ACCESS (none | read | write | read-write):
- Step CONSUMES durable subject-matter facts (entities, relationships, prior strategies)? → "read"
- Step PRODUCES durable facts (extracts companies, identifies recurring patterns, captures decisions)? → "write" + must set knowledgebase_contribution to a non-empty extraction instruction
- Both? → "read-write"
- Pure I/O / orchestration / data movement? → "none" (default; KB is opt-in per step)
- FLAG: knowledgebase_contribution is non-empty but knowledgebase_access lacks write — the post-step KB update agent is silently skipped (controller_kb_update.go gates on kbAccessAllowsWrite). Recommend either set access to "write"/"read-write" OR clear the contribution.

DB ACCESS:
- Every step already has db/ read+write via folder guard (unconditional, no opt-in). The audit question is INTENT: should this step write to db/?
- Recommend writing to db/{file}.json when: the step produces cross-run state, per-key data that subsequent runs upsert into, or data the report widgets need to bind to.
- Suggest the file name + primary_key convention (e.g. db/account_status.json, primary_key=group_name).
- FLAG: step description claims to "save state" / "update results" / "track" something but doesn't explicitly name a db/ file — likely a portability bug; the step is probably writing into runs/{iter}/ instead of the workspace-level db/.

LOCK PRINCIPLE (read this before applying lock rules below)
Lock is a **reward for proven outcome quality, not a default for operational stability**. A step that ran cleanly 3 times isn't proven; a step whose linked eval score / outcome metric has stayed at-or-above target across 10+ runs spanning 2+ variable groups IS proven. The framework's three layers are the basis: PLAN defines the work + goal, EVAL scores both how the plan ran and whether the goal was met, METRICS are the numeric trajectory. Lock decisions ride on EVAL + METRICS evidence, not on operational counts alone. If a step has no eval coverage at all, you cannot prove outcome quality — recommend the user add eval coverage first OR accept the risk and lock manually; do NOT recommend auto-lock for outcome-blind steps.

LOCK_LEARNINGS (true | false):
- Lock when ALL of:
  - successful_runs >= 10 (raise from the legacy "3" — 3 is operational stability only, not outcome confidence)
  - successful runs span >= 2 different variable groups (so the step has been seen across the variation surface, not just one group repeated)
  - description_hash matches stored value
  - **Eval evidence**: at least one of (a) an eval_step targets this step (by id) and its average score over the last 10 runs is at or above the eval's pass threshold, OR (b) a metric in metrics.json sources from this step's eval_step and has been at-or-above its target/floor across the last 10 runs (with linked_success_criteria so the trace to the goal is auditable).
  - SKILL.md is stable (learning agent producing near-duplicate edits in recent runs)
- DO NOT lock when ANY: description recently changed (hash mismatch), recent failures triggered learning rewrites, < 10 successful runs, runs all on a single group, no eval coverage, eval is below threshold or missing.
- If currently locked but description_hash drifted → recommend UNLOCK (learnings are stale relative to intent).
- If currently locked but linked eval/metric has dropped below target on recent runs → recommend UNLOCK (the locked behavior stopped working).

LOCK_CODE (true | false; learn_code steps only):
- Lock when ALL of:
  - main.py exists AND script_metadata.json shows >= 10 successful runs across >= 2 different variable groups
  - No recent fix-loop rewrites of main.py (last 5 runs)
  - Description hash matches stored value
  - **Eval evidence** as defined above (eval_step at threshold for last 10 runs, OR linked metric at-or-above target/floor for last 10 runs)
- DO NOT lock when ANY: main.py rewritten in last 5 runs, transient failures present, description being iterated, < 10 successful runs, runs all on a single group, no eval coverage, eval below threshold.
- When recommending lock_code=true, also recommend lock_learnings=true and optimized=true together — they should move as a set per the workshop convention. Include optimized_reason citing the **outcome evidence** (groups passed, eval scores at threshold across N runs, linked metric trajectory at target).
- If currently locked but description_hash drifted → recommend UNLOCK (main.py may be stale).
- If currently locked but linked eval/metric has dropped below target on recent runs → recommend UNLOCK (locked code stopped delivering).

CONVERT REGULAR → LEARN_CODE (suggest only when ALL):
- Step's behavior is mechanical (deterministic transform on stable input shape — filter / aggregate / map fields) rather than per-instance LLM judgment.
- 10+ successful runs of the regular step across 2+ groups, eval at threshold throughout — proving the step's BEHAVIOR is stable enough to capture in code.
- No active experiment is touching this step, its prompt/config/learnings/validation, or adjacent upstream/downstream contracts. If an experiment is active, defer conversion until it concludes so the script doesn't freeze a moving target.
- The step is not browser/UI-heavy. Browser automation should usually remain code_exec unless the user explicitly requested scripted browser execution and the browser flow is proven stable with durable selectors and state-driven waits.
- Saving the LLM cost is meaningful (the step runs frequently, or is on the critical path).
- DO NOT suggest conversion for steps that need per-instance LLM judgment (classification, summarization, decision-making) — even if they look "shape-preserving" the answer changes per instance and locking the LLM out via main.py loses the value of the step.

OUTPUT FORMAT

Single table, one row per step:

| Step ID | Mode Fit (cur → rec) | KB Access (cur → rec) | KB Contribution | DB Output | Lock Learnings (cur → rec) | Lock Code (cur → rec) | Reason |
|---|---|---|---|---|---|---|---|

Then summary sections:
- **Mode fit issues** — browser/UI-heavy learn_code steps, unstable learn_code steps, or learn_code candidates blocked by active experiments.
- **To lock this round** — steps recommended for lock_learnings + lock_code (+ optimized).
- **KB misconfigs** — knowledgebase_contribution set but access blocks writes (silent skip).
- **Should write to db/ but doesn't** — steps producing cross-run state outside db/.
- **Stale locks (unlock + re-review)** — currently locked but description_hash drifted.

END WITH READY-TO-PASTE COMMANDS
List the exact update_step_config calls the user can copy/paste to apply each recommendation, one per line. Group by recommendation type (mode updates, KB updates, lock updates) so the user can apply them selectively. Do NOT call update_step_config yourself.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what config you reviewed, the main misconfigs found, the recommended changes, and items flagged for follow-up.

**Finding IDs.** Every distinct misconfig or recommendation gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id> — <mode|kb|db|lock_learnings|lock_code|convert>: <finding>` so close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.
