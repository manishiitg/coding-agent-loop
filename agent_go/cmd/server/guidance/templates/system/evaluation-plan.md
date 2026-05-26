## EVALUATION PLAN — evaluation/evaluation_plan.json

Workshop owns the eval plan: write it, validate it, run it against `iteration-0`, and keep it sharp as you harden the workflow.

### Eval plan rules

- Each eval step in `evaluation/evaluation_plan.json` must have: `id`, `title`, `description`.
- Optional per-step field: `pre_validation`.
- Optional route gating field: `applies_to_routes`. Use it for workflows with routing so eval only runs checks for the path the target run actually took. Example: `applies_to_routes: [{"routing_step_id":"workflow-mode-router","route_ids":["route-bid"]}]`.
- Eval step IDs must NOT collide with execution-plan step IDs because both share `learnings/{stepID}/`.
- Focus eval steps on workflow outcomes, not intermediate files, unless a file check is truly the outcome.
- `pre_validation` checks files inside the eval step execution folder, not the original run folder.
- Eval step descriptions may reference `{{"{{TARGET_RUN_PATH}}"}}`, which resolves to the absolute path of the original execution folder being scored. Use that placeholder when the eval needs to inspect original run artifacts directly; never hardcode iteration paths.
- For eval step config in `evaluation/step_config.json`:
  - default to `declared_execution_mode=code_exec`; promote eval steps to `learn_code` only when the user explicitly asks to freeze/reuse scoring code, the check is fully deterministic, and 10+ eval runs cover the relevant output scenarios
  - use `declared_execution_mode=code_exec` when the eval still needs adaptive model judgment, the rubric is changing, or the scenario coverage is not proven
  - set per-eval-step `execution_tier` with `update_step_config(step_id="eval-step-id", execution_tier="high|medium|low")`; the tool writes `evaluation/step_config.json` when the id is from `evaluation/evaluation_plan.json`
  - tier rule of thumb: `high` for subjective/ambiguous judgment, `medium` for normal eval checks, `low` for deterministic/file-shape checks
  - there is no final combined scoring agent; each eval step must emit the structured verdict fields that metrics need
- After every edit to `evaluation/evaluation_plan.json`, call `validate_evaluation_plan`.
- When you want to test the current eval plan, call `run_full_evaluation(group_name="...")`. Evaluation always targets `iteration-0`.

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
