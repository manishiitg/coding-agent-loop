## EVALUATION PLAN — evaluation/evaluation_plan.json

Workshop owns the eval plan: write it, validate it, run it against `iteration-0`, and keep it sharp as you harden the workflow.

### Eval plan rules

- Each eval step in `evaluation/evaluation_plan.json` must have: `id`, `title`, `description`.
- Optional per-step field: `pre_validation`.
- Optional route gating field: `applies_to_routes`. Use it for workflows with routing so eval only runs checks for the path the target run actually took. Example: `applies_to_routes: [{"routing_step_id":"workflow-mode-router","route_ids":["route-bid"]}]`.
- Eval step IDs must be globally unique across both the execution plan and the eval plan (enforced at write time by `validateCrossPlanStepIDUniqueness`) — both share the `learnings/{stepID}/` namespace, and a collision clobbers saved scripts and metadata.
- Focus eval steps on workflow outcomes, not intermediate files, unless a file check is truly the outcome.
- `pre_validation` checks files inside the eval step execution folder, not the original run folder.
- Eval step descriptions may reference `{{"{{TARGET_RUN_PATH}}"}}`, which resolves to the absolute path of the original execution folder being scored. Use that placeholder when the eval needs to inspect original run artifacts directly; never hardcode iteration paths.
- For eval step config in `evaluation/step_config.json`:
  - **prefer `declared_execution_mode=scripted` whenever the check is objective/deterministic** — file/row/value/format/count assertions and source cross-checks belong in code: scripted scoring is cheaper, repeatable, and impossible to game or drift. (Unlike an execution step's `main.py`, eval scoring code does not freeze runtime behavior, so there is no 10-run gate to start scripting an objective check.)
  - use `declared_execution_mode=agentic` only for genuinely subjective judgment (e.g. "is the report well-written?") or while the rubric is still being worked out; once the criteria are stable, promote that eval agentic→scripted
  - set per-eval-step `execution_tier` with `update_step_config(step_id="eval-step-id", execution_tier="high|medium|low")`; the tool writes `evaluation/step_config.json` when the id is from `evaluation/evaluation_plan.json`
  - tier rule of thumb: `high` for subjective/ambiguous judgment, `medium` for normal eval checks, `low` for deterministic/file-shape checks
  - there is no final combined scoring agent; each eval step must emit structured verdict fields that downstream reviews and reports can read
- After every edit to `evaluation/evaluation_plan.json`, call `validate_evaluation_plan`.
- When you want to test the current eval plan, call `run_full_evaluation(group_name="...")`. Evaluation always targets `iteration-0`.

### Writing a GOOD eval (best practices)

A good eval catches a bad run — including one that *looks* successful. Design for that:

- **Prefer SCRIPTED (deterministic) evals.** If a check can be expressed as code — file/row/value/format/count assertions, "does the figure equal the sum from the source table?", "is this a real parsed number, not a placeholder?" — write it as a script, not model judgment. Scripted evals are cheaper, repeatable, and impossible to game or drift. Reach for `agentic` only for genuinely subjective quality or an unsettled rubric; promote to scripted once stable. **Default bias: if you can express it as a check, script it.**
- **Score outcomes, not artifacts.** Evaluate whether `soul.md` success criteria were actually met — not "did a file get written." A file existing ≠ success.
- **One eval per success dimension**, each emitting its own verdict, so failures are diagnosable — not one blob score.
- **Anti-placeholder / anti-fabrication.** Score 0 when a value is empty, `N/A`, a placeholder, or a plausible-but-unsourced number. Non-empty ≠ correct.
- **Cross-check against the source.** Verify the claimed result against the actual source artifact (the parsed PDF / file / db row), NOT the step's own summary of what it claims it did.
- **Ground in real run evidence.** Inspect the actual run via `{{"{{TARGET_RUN_PATH}}"}}` and `db/db.sqlite`; never score from the agent's narrative or chat memory. Quote which file/row/value justified the score.
- **Fail loud / fail closed.** Missing input → fail (0), don't skip or assume. An eval that errors or finds nothing must register as failure, never as success.
- **Explicit rubric + thresholds.** State the pass/fail criteria; define what 100 vs 0 means and any partial-credit rule, so scoring is repeatable.
- **Route-gate** with `applies_to_routes` so an eval only runs for the path the target run actually took.
- **Cheap checks first.** Presence / format / SQL gates before any expensive model judgment.
- **Independence.** Don't reuse the same reasoning that produced the data to also judge it.
- **Actionable failure.** Emit *why* it failed and *what's missing*, so `harden_workflow` / `replan_workflow_from_results` can act — not just a number.

### When to write/update evaluation/evaluation_plan.json

- When the user wants to add or change eval coverage
- When success criteria have changed and eval logic must follow
- When optimizer-mode hardening reveals missing or weak evaluation
- When the scoring logic or eval-step descriptions need tightening

### Evaluation workflow

1. Clarify what the user wants the eval to prove if needed.
2. Edit `evaluation/evaluation_plan.json`.
3. Call `validate_evaluation_plan`.
4. Fix validation errors, then validate again until clean.
5. If needed, call `run_full_evaluation(group_name="...")` to test the plan against a group in `iteration-0`.

### Files

- Plan: `evaluation/evaluation_plan.json`
- Step config: `evaluation/step_config.json`
- Eval runs + reports: `evaluation/runs/iteration-0[/group]/`
