Audit every configurable execution target in this workflow — workflow steps, evaluation steps, and the reserved final evaluation scoring target `__evaluation_scoring__` — and recommend the right values for: declared_execution_mode, learnings_access, learning_objective, learnings_write_method, knowledgebase_access, knowledgebase_contribution, lock_learnings, and lock_code (learn_code/scripted targets only). This is a READ-ONLY analysis pass — do NOT apply any changes via update_step_config. Produce a recommendation table the user will review and apply selectively.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

PROCEDURE
1. Read planning/plan.json and planning/step_config.json. For each workflow step capture: id, title, description, declared_execution_mode, and current AgentConfigs (learnings_access, learning_objective, learnings_write_method, knowledgebase_access, knowledgebase_contribution, lock_learnings, lock_code, successful_runs, description_hash, review_notes).
2. Read evaluation/evaluation_plan.json and evaluation/step_config.json if present. For each eval step capture the same fields from evaluation/step_config.json. Also inspect the reserved scoring config id `__evaluation_scoring__` in evaluation/step_config.json; treat it as scope `evaluation_scoring` with title "Final Evaluation Scoring Agent".
3. For every target, inspect learnings/{step-id}/:
   - SKILL.md exists? size? recent edit signal (read first/last lines for context).
   - main.py exists? Read learnings/{step-id}/script_metadata.json for successful_runs counts (code_exec/learn_code) and recent failure history.
   - For scoring, use `learnings/__evaluation_scoring__/main.py` and `learnings/__evaluation_scoring__/script_metadata.json`.
4. Sample run evidence from runs/iteration-0/{first-group}/logs/{step-id}/ when available:
   - learn_code_fast_path.json — recent main.py outcomes.
   - pre_validation.json — validation pass/fail.
   - Any signs of recent fix-loop rewrites or transient failures.
   - For eval steps, also inspect evaluation run outputs/reports under runs/iteration-0/{first-group}/evaluation/ when available.
5. Read builder/improve.md if present. Note recent harden/replan/metric actions and whether any recent or queued change touches the target, its prompt/config, learnings, validation, KB, db schema, evaluation criteria, scoring logic, or upstream/downstream context dependencies.

DECISION RULES (apply per step)

EXECUTION MODE FIT (code_exec | learn_code):
- Browser/UI-heavy steps should generally be `code_exec`, not `learn_code`. If a step uses browser capability, live auth/session state, dynamic selectors, pagination/lazy-loading, or third-party page timing and is currently `learn_code` with saved main.py, flag it. Recommend `declared_execution_mode="code_exec"` unless the user explicitly requested scripted browser execution AND the script has durable selectors, state-driven waits, fresh snapshots, and 10+ scenario-covering successful runs.
- If a step is currently `code_exec` but `learnings/{step-id}/main.py` still exists, flag it for deletion and recommend clearing `lock_code`. code_exec does not use persistent main.py, so keeping the file creates stale-script drift for future reviewers.
- Recommend `learn_code` only when ALL are true:
  - The user explicitly asked for learn_code/scripted fast-path conversion.
  - The step behavior is stable, mechanical, and deterministic on a stable input shape.
  - The step does not need per-instance LLM judgment, adaptive browsing, or live UI interpretation.
  - The step has 10+ successful runs across 2+ variable groups.
  - Eval/metric evidence is at or above target across the recent measurement window.
  - No recent harden/replan/metric action is still changing this step, its prompt/config/learnings/validation, or adjacent upstream/downstream contracts.
- If a current `learn_code` step violates any of the above, recommend reverting to or keeping `code_exec`, and clear/avoid lock_code until stability is proven after recent changes settle.
- Apply the same mode-fit rule to eval steps and `__evaluation_scoring__`: deterministic file-shape/schema checks can be `learn_code` after stability evidence; subjective scoring, ambiguous judgment, or criteria that are still being revised should stay `code_exec`. If recent changes could alter expected outputs or scoring criteria, defer eval/scoring learn_code promotion and lock decisions.

LEARNINGS (learnings_access, learning_objective, learnings_write_method):
- Global learning READ is normally useful and defaults to `read`. Do not recommend `learnings_access="none"` unless existing SKILL.md content would actively mislead this target or the target is intentionally isolated from workflow know-how.
- Recommend `learnings_access="read-write"` only when the target can produce durable HOW-to-run knowledge that will improve future runs: selectors, auth/login flow quirks, timing/wait strategies, API/MCP parameter pitfalls, pagination/tool-call patterns, rate-limit handling, file format quirks, or reusable recovery steps.
- Do NOT recommend learning writes for pure plumbing/data movement, one-off output formatting, deterministic transforms whose behavior is already captured in code/tests, or eval/scoring targets that only record whether an output passed. Those should usually keep `read` only.
- When learning writes are enabled, `learning_objective` must be concrete and bounded. Good: "Capture durable HDFC login selectors, OTP timing, and retry behavior that changed during this run." Bad: "learn from this step", "improve future runs", "capture anything useful".
- If `learnings_access="read-write"` but `learning_objective` is empty/vague, flag it. Recommend either writing a precise objective or downgrading to `read`.
- If `learning_objective` exists but `learnings_access` is not `read-write`, flag it as a silent no-op/misconfig. Recommend `read-write` only if the objective is justified; otherwise clear the objective.
- Check `review_notes` or surrounding evidence for the reason learning was enabled. A good reason names the reusable operational uncertainty being learned and why future runs need it. If there is no reason, or the reason is just "step is important", flag it and ask for a better rationale before enabling/keeping writes.
- For new write-capable steps, recommend `learnings_write_method="direct"` by default. Recommend `agent` only when the user explicitly asked for a separate post-step learning agent/reviewer.
- For eval steps and `__evaluation_scoring__`, learning writes should be rare: use them only for reusable scoring mechanics or evaluation-tool quirks, not for changing rubric/business judgment.

KB_ACCESS (none | read | write | read-write):
- Step CONSUMES durable subject-matter facts (entities, relationships, prior strategies)? → "read"
- Step PRODUCES durable facts (extracts companies, identifies recurring patterns, captures decisions)? → "write" + must set knowledgebase_contribution to a non-empty extraction instruction
- Both? → "read-write"
- Pure I/O / orchestration / data movement? → "none" (default; KB is opt-in per step)
- FLAG: knowledgebase_contribution is non-empty but knowledgebase_access lacks write — the post-step KB update agent is silently skipped (controller_kb_update.go gates on kbAccessAllowsWrite). Recommend either set access to "write"/"read-write" OR clear the contribution.
- For eval steps and `__evaluation_scoring__`, default KB to none unless the evaluation genuinely needs durable subject-matter context. Do not recommend KB writes from eval/scoring unless the eval is intentionally the canonical producer of durable narrative observations.

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
- When recommending lock_code=true, also recommend lock_learnings=true when the SKILL.md guidance is stable. Include review_notes citing the **outcome evidence** (groups passed, eval scores at threshold across N runs, linked metric trajectory at target).
- If currently locked but description_hash drifted → recommend UNLOCK (main.py may be stale).
- If currently locked but linked eval/metric has dropped below target on recent runs → recommend UNLOCK (locked code stopped delivering).

CONVERT REGULAR → LEARN_CODE (suggest only when ALL):
- User explicitly asked for learn_code/scripted fast-path conversion for this step. Do not suggest conversion just because a step is deterministic or costly.
- Step's behavior is mechanical (deterministic transform on stable input shape — filter / aggregate / map fields) rather than per-instance LLM judgment.
- 10+ successful runs of the regular step across 2+ groups, eval at threshold throughout — proving the step's BEHAVIOR is stable enough to capture in code.
- No recent harden/replan/metric action is touching this step, its prompt/config/learnings/validation, or adjacent upstream/downstream contracts. If related behavior is still changing, defer conversion so the script doesn't freeze a moving target.
- The step is not browser/UI-heavy. Browser automation should usually remain code_exec unless the user explicitly requested scripted browser execution and the browser flow is proven stable with durable selectors and state-driven waits.
- Saving the LLM cost is meaningful (the step runs frequently, or is on the critical path).
- DO NOT suggest conversion for steps that need per-instance LLM judgment (classification, summarization, decision-making) — even if they look "shape-preserving" the answer changes per instance and locking the LLM out via main.py loses the value of the step.

OUTPUT FORMAT

Single table, one row per target:

| Scope | Step ID | Mode Fit (cur → rec) | Learning (cur → rec) | Learning Objective | KB Access (cur → rec) | KB Contribution | DB Output | Lock Learnings (cur → rec) | Lock Code (cur → rec) | Reason |
|---|---|---|---|---|---|---|---|---|---|---|

Then summary sections:
- **Mode fit issues** — browser/UI-heavy learn_code steps, unstable learn_code steps, or learn_code candidates blocked by recent harden/replan changes.
- **Learning misconfigs** — write access without a concrete objective/reason, objective present but writes disabled, unjustified agent write method, or learning enabled for steps that produce no reusable HOW-to-run knowledge.
- **Evaluation config issues** — eval/scoring targets whose mode, tier, locks, KB, or saved code no longer match evaluation/evaluation_plan.json.
- **To lock this round** — steps recommended for lock_learnings and/or lock_code.
- **KB misconfigs** — knowledgebase_contribution set but access blocks writes (silent skip).
- **Should write to db/ but doesn't** — steps producing cross-run state outside db/.
- **Stale locks (unlock + re-review)** — currently locked but description_hash drifted.

END WITH READY-TO-PASTE COMMANDS
List the exact update_step_config calls the user can copy/paste to apply each recommendation, one per line. Group by recommendation type (mode updates, learning updates, KB updates, lock updates) so the user can apply them selectively. Do NOT call update_step_config yourself.

{{if eq .WorkshopMode "run"}}RUN MODE OUTPUT: do not write builder/review.md or any workspace file. Return the config audit in chat using the output format above. If the user wants the findings persisted, tell them to switch to Builder or Optimizer mode and rerun /review-config.{{else}}REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what config you reviewed, the main misconfigs found, the recommended changes, and items flagged for follow-up.{{end}}

{{if eq .WorkshopMode "run"}}**Finding IDs.** In Run mode, assign temporary ids in the response only, using `F-YYYY-MM-DD-NNN` starting at `001`; do not scan or write builder/review.md.{{else}}**Finding IDs.** Every distinct misconfig or recommendation gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan builder/review.md for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id> — <mode|learning|kb|db|lock_learnings|lock_code|convert>: <finding>` so close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.{{end}}
