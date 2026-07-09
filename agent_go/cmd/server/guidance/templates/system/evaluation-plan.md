## EVALUATION PLAN — evaluation/evaluation_plan.json

Workshop owns the eval plan: write it, validate it, run it against `iteration-0`, and keep it sharp as you harden the workflow.

### Division of labor — evals measure the GOAL; Pulse owns the rest

Pulse (the per-run monitor) already triages every run for operational breakage: errored/skipped steps, empty or placeholder artifacts, hallucinated "successes", broken eval/report layers. `pre_validation` owns mechanical run-shape checks. Evals exist for the one thing neither gives you: a stable, repeatable judgment of whether the run **achieved the success criteria in `soul/soul.md`**.

- Do NOT write eval steps for operational checks ("file exists", "step ran", "output non-empty", "JSON parses"). They duplicate Pulse/pre-validation, and because they pass on every clean run they inflate the score and mask real goal shortfall.
- **Anchor every eval step to a success criterion.** One eval step per criterion (route-scoped where routes apply), and say in the description which criterion it measures. Pulse maps eval verdicts onto the per-criterion goal card in `builder/improve.html` — an eval step tied to no criterion has no consumer, and a criterion with no eval step is unmeasured.
- **Evals are the ruler for both loops.** Pulse confirms harden Decisions and Goal Advisor judges replans by comparing eval reports across runs. Keep the instrument stable — same steps, same scale, same rubric — so score movement means the workflow changed, not the measurement.

### Cost discipline — eval is a per-run tax

Auto-eval runs after every successful execution, so every eval step's cost recurs on every run (tracked under `costs/evaluation/`). Keep it lean:

- Few steps: one per success criterion, not one per execution step. Retire steps that duplicate Pulse/pre-validation or another eval step.
- Extract facts with code (cheap, `low` tier) and spend model judgment only on the verdict; reserve `high` tier for genuinely ambiguous judgment.
- Read targeted artifacts through `{{"{{TARGET_RUN_PATH}}"}}` — never re-walk the whole run folder.
- Route-gate with `applies_to_routes` so non-applicable steps are skipped, not executed.
- Eval spend that rivals execution spend is itself an eval-improvement finding.

### Eval plan rules

- Each eval step in `evaluation/evaluation_plan.json` must have: `id`, `title`, `description`.
- Optional per-step field: `pre_validation`.
- Optional route gating field: `applies_to_routes`. Use it for workflows with routing so eval only runs checks for the path the target run actually took. Example: `applies_to_routes: [{"routing_step_id":"workflow-mode-router","route_ids":["route-bid"]}]`.
- Eval step IDs must be globally unique across both the execution plan and the eval plan (enforced at write time by `validateCrossPlanStepIDUniqueness`) — both share the `learnings/{stepID}/` namespace, and a collision clobbers saved scripts and metadata.
- Focus eval steps on workflow outcomes, not intermediate files, unless a file check is truly the outcome.
- `pre_validation` checks files inside the eval step execution folder, not the original run folder.
- Eval step descriptions may reference `{{"{{TARGET_RUN_PATH}}"}}`, which resolves to the absolute path of the original execution folder being scored. Use that placeholder when the eval needs to inspect original run artifacts directly; never hardcode iteration paths.
- For eval step config in `evaluation/step_config.json`:
  - **split each eval into scripted extraction + judged verdict.** Compute the facts (counts, totals, diffs vs the source artifact, fixed samples) in code so they are identical run-to-run; judge the verdict against the success criterion on top of those facts.
  - use `declared_execution_mode=scripted` when the WHOLE check is mechanical AND anchored to a stable contract (a `db/README.md` schema, the report contract, a fixed output format). Never script against incidental artifact shapes — replans change those, and every shape-coupled eval script becomes a recurring Bug to harden. (Unlike an execution step's `main.py`, eval scripting has no 10-run gate — script an objective, contract-anchored check anytime; the explicit-user-request + coverage bar applies only to LOCKING a saved script.)
  - use `declared_execution_mode=agentic` for genuinely subjective judgment ("is the analysis useful?", groundedness, relevance) — but with a FROZEN rubric: a fixed scale plus written anchors for what a low vs high score looks like, so the judge stays comparable across runs
  - set per-eval-step `execution_tier` with `update_step_config(step_id="eval-step-id", execution_tier="high|medium|low")`; the tool writes `evaluation/step_config.json` when the id is from `evaluation/evaluation_plan.json`
  - tier rule of thumb: `high` for subjective/ambiguous judgment, `medium` for normal eval checks, `low` for deterministic/file-shape checks
  - there is no final combined scoring agent; each eval step must emit structured verdict fields that downstream reviews and reports can read
- After every edit to `evaluation/evaluation_plan.json`, call `validate_evaluation_plan`.
- When you want to test the current eval plan, call `run_full_evaluation(group_name="...")`. Evaluation always targets `iteration-0`.

### Writing a GOOD eval (best practices)

A good eval catches a bad run — including one that *looks* successful. Design for that:

- **Deterministic facts, judged verdicts.** If a fact can be computed — "does the figure equal the sum from the source table?", "how many rows landed?" — compute it in code and let the verdict reason over the computed facts. Fully scripted only for contract-anchored mechanical checks; fully agentic only for subjective quality with a frozen rubric.
- **Score outcomes, not artifacts.** Evaluate whether `soul.md` success criteria were actually met — not "did a file get written." A file existing ≠ success. Operational checks belong to Pulse/pre-validation, not here.
- **One eval step per `soul.md` success criterion**, each emitting its own verdict and naming its criterion, so the goal card stays diagnosable — not one blob score.
- **Anti-placeholder / anti-fabrication.** Score 0 when a value is empty, `N/A`, a placeholder, or a plausible-but-unsourced number. Non-empty ≠ correct.
- **Cross-check against the source.** Verify the claimed result against the actual source artifact (the parsed PDF / file / db row), NOT the step's own summary of what it claims it did.
- **Ground in real run evidence.** Inspect the actual run via `{{"{{TARGET_RUN_PATH}}"}}` and `db/db.sqlite`; never score from the agent's narrative or chat memory. Quote which file/row/value justified the score.
- **Fail loud / fail closed.** Missing input → fail (0), don't skip or assume. An eval that errors or finds nothing must register as failure, never as success. A failure caused by missing/broken input is **Bug evidence** — name the missing path in `reasoning` so Pulse triage routes it to `harden_workflow` instead of reading it as a goal regression.
- **Explicit rubric + thresholds.** State the pass/fail criteria; define what 100 vs 0 means and any partial-credit rule, so scoring is repeatable.
- **Rubric stability.** Changing scale, thresholds, or rubric changes what scores MEAN across runs. Do it only in a deliberate eval improvement pass (`/improve-evaluation`), record a Decision entry in `builder/improve.html` flagging that score semantics changed, and never bundle it with a harden/replan in the same pass — otherwise before/after comparisons are uninterpretable.
- **Sandbox hygiene.** Eval steps execute in a shared sandbox (`evaluation/runs/iteration-0[/group]`) that is NOT cleaned between evaluations. Read evidence ONLY via `{{"{{TARGET_RUN_PATH}}"}}` (and `db/` for persistent stores); never base a score on files found in the sandbox except facts your own step just extracted. Remember `pre_validation` checks the sandbox, not the target run.
- **Route-gate** with `applies_to_routes` so an eval only runs for the path the target run actually took.
- **Cheap checks first.** Presence / format / SQL gates before any expensive model judgment.
- **Independence.** Don't reuse the same reasoning that produced the data to also judge it.
- **Actionable failure.** Emit *why* it failed and *what's missing*, so `harden_workflow`, report/eval repair, or a Goal Advisor plan-change proposal can act — not just a number.

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
