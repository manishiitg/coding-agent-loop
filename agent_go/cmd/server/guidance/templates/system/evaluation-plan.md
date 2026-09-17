## EVALUATION PLAN — evaluation/evaluation_plan.json

Use `add_evaluation_step` to create a new evaluation step (including the first
step in an empty plan), `update_evaluation_plan` to edit an existing step, and
`delete_evaluation_step` to remove one. These governed tools validate the plan
and record the change in `planning/changelog`; do not edit the file directly.

Workshop owns the eval plan: write it, validate it, and keep it aligned as the workflow evolves. Interactive tests target `iteration-0`; a scheduled invocation targets its server-bound `iteration-N-sched` run.

### Division of labor — evals measure the GOAL; Pulse owns the rest

Pulse Gate and Bug Review already inspect every run for operational breakage: errored/skipped steps, empty or placeholder artifacts, hallucinated "successes", broken eval/report layers. `pre_validation` owns mechanical run-shape checks. Strategy Auditor and Goal Advisor go well past operational checks — they reconstruct the goal-to-action-to-**observation** causal chain across many runs and judge whether the current strategy itself is working. But that cross-run strategic judgment is built ON TOP OF a stable per-run measurement, not a replacement for producing one: without a fixed rubric anchoring the same score the same way every cycle, there is nothing comparable to trend, and "is this a strategy problem or just this run" becomes unanswerable. Evals exist to produce exactly that measurement: a stable, repeatable judgment of whether ONE run **achieved the success criteria in `soul/soul.md`** — the observation layer everything else, including Strategy Auditor and Goal Advisor, reads from.

- Do NOT write eval steps for operational checks ("file exists", "step ran", "output non-empty", "JSON parses"). They duplicate Pulse/pre-validation, and because they pass on every clean run they inflate the score and mask real goal shortfall.
- **Empty is not automatically missing.** A rubric must distinguish failed or unverified collection from a source-grounded legitimate zero-cardinality state. When trustworthy evidence proves that zero records is the correct business result, score semantic correctness normally; never deduct points merely because an array is empty. State the evidence required to prove a valid zero so fabricated or silently missing data still fails closed.
- **Anchor every eval step to a success criterion.** One eval step per criterion (route-scoped where routes apply), and say in the description which criterion it measures. Pulse maps eval verdicts to the Goal verdict/progress read from `soul/soul.md` — an eval step tied to no criterion has no consumer, and a criterion with no eval step is unmeasured.
- **Evals are the ruler for both loops.** Pulse verifies reliability fixes and Goal Advisor judges strategy changes by comparing eval reports across runs. Keep the instrument stable — same steps, same scale, same rubric — so score movement means the workflow changed, not the measurement.

### Outcome-based eval steps — measuring against durable human judgment, not this run's own artifacts

Some success criteria cannot be honestly scored from one run's own artifacts at all, because the run's own artifacts are exactly what's in question. A workflow that files bugs, proposes changes, or produces anything a human later accepts or rejects can score itself as "working" every single cycle while a human is quietly closing most of what it produces as noise — a per-run artifact check has no way to see that, because the artifacts always look internally consistent from the inside.

When a criterion is really "does this workflow's output hold up against independent outside judgment," anchor the eval step to a durable, human-reconciled outcome table instead of (or in addition to) this run's own `{{"{{TARGET_RUN_PATH}}"}}`:

- Query the full history in `db/db.sqlite` (e.g. a `github_issues`-style reconciliation table with `status`/`resolution`/`human_comment`/`verdict_author`/`reconciled_at`), not just this run's slice of it. The score is a rolling rate over the last N outcomes — it will read the same on every run until new outcomes land, and that stability is correct, not a bug: the measurement only moves when reality moves.
- **Self-claimed resolution is not human judgment.** A status like "fixed-claimed" or "closed" set by the same automation (or the same fix) that produced the thing being judged must never be counted as acceptance. Only count a resolution as genuine acceptance/rejection when it came from an independent human verdict (check `verdict_author`, `human_comment`, or an equivalent field naming who decided). Blending self-reported closure into an "accepted" rate lets the workflow grade its own homework and silently inflates the score.
- **Name what this measures and what it doesn't.** An acceptance-rate-style criterion measures precision (of what it produced, how much held up) — it says nothing about recall (what it never found or produced at all). State that explicitly in the step's description rather than implying the single number is a complete measure of the goal. If recall genuinely cannot be measured from available data, say so and leave it unmeasured — an honest partial measure beats a fabricated complete one.
- This does not replace a per-run check when a criterion genuinely is about this run's own artifacts (did this cycle's output match the source data). Use an outcome-based step only when the criterion is inherently about durable acceptance, not immediate correctness.

### Rating subjective quality — anchor the scale, don't just name it

"Is this analysis good?" and "is this bug report clear?" have no formula. That does not make them unmeasurable, and it is not a reason to drop the criterion or fold it into a vaguer proxy — it means the rubric carries more of the weight than an objective check needs, because there is no formula to fall back on when the judge is uncertain.

- **Write what every point on the scale looks like, not just the ends.** "0 = terrible, 10 = perfect" leaves 9 points undefined, and that's exactly where a judge drifts run to run. A usable rubric names each point (or each band, for a fine scale): what does a 2 look like versus a 3, concretely, in terms of what's present or missing. If a point on the scale can't be described in concrete terms, collapse the scale until every remaining point can be — a well-anchored 4-point scale beats a vague 10-point one.
- **Extract the facts first, judge second.** Even for a subjective criterion, pull out what's objectively checkable before asking for a verdict — "does it name a specific file/line/root cause," "does it cite the actual observed behavior vs. just describing what should happen," "how many claims are unsupported by evidence." Let the judgment reason over those extracted facts instead of forming a holistic impression from scratch; this is the same split scripted-extraction-then-judged-verdict pattern used everywhere else in this file, applied to the judgment step itself.
- **Anchor with a real example when the description alone is ambiguous.** "Clear and specific" is still open to interpretation; one real output that would score a 4 and one that would score a 2, with the reason named, closes most of the gap a written description alone leaves open.
- **Design against known judge failure modes**, not just against not having a rubric at all: leniency drift (scores creep upward over repeated similar judgments unless the anchors force a real comparison each time), length/verbosity bias (a judge tends to reward longer output regardless of whether it's better), and self-inconsistency (the same input can score differently on repeat judgment without a frozen, concrete rubric to anchor against). The frozen rubric plus per-point anchors is the actual defense against all three — that's why "Rubric stability" below treats changing the scale as a deliberate, tracked act, not a routine edit.
- Subjective does not mean lower rigor. A subjective criterion with a real anchored rubric is more trustworthy than an "objective-looking" check that quietly measures the wrong thing (see Division of labor above) — anchoring the scale is the rigor, not a substitute for it.

### Cost discipline — eval is a per-run tax

Auto-eval runs after every successful execution, so every eval step's cost recurs on every run (tracked under `costs/evaluation/`). Keep it lean:

- Few steps: one per success criterion, not one per execution step. Retire steps that duplicate Pulse/pre-validation or another eval step.
- Extract facts with code (cheap, `low` tier) and spend model judgment only on the verdict; reserve `high` tier for genuinely ambiguous judgment.
- Read targeted artifacts through `{{"{{TARGET_RUN_PATH}}"}}` — never re-walk the whole run folder.
- Route-gate with `applies_to_routes` so non-applicable steps are skipped, not executed.
- Eval spend that rivals execution spend is itself an eval-improvement finding.

### Eval plan rules

- Each eval step in `evaluation/evaluation_plan.json` must have: `id`, `title`, `description`.
- Optional per-step field: `validation_schema` (`pre_validation` is accepted only for legacy plans).
- Optional route gating field: `applies_to_routes`. Use it for workflows with routing so eval only runs checks for the path the target run actually took. Example: `applies_to_routes: [{"routing_step_id":"workflow-mode-router","route_ids":["route-bid"]}]`.
- Eval step IDs must be globally unique across both the execution plan and the eval plan (enforced at write time by `validateCrossPlanStepIDUniqueness`) — both share the `learnings/{stepID}/` namespace, and a collision clobbers saved scripts and metadata.
- Focus eval steps on workflow outcomes, not intermediate files, unless a file check is truly the outcome.
- `validation_schema` checks files inside the eval step execution folder, not the original run folder.
- Eval step descriptions may reference `{{"{{TARGET_RUN_PATH}}"}}`, which resolves to the absolute path of the original execution folder being scored. Use that placeholder when the eval needs to inspect original run artifacts directly; never hardcode iteration paths.
- `{{"{{TARGET_RUN_PATH}}"}}` is the execution root, not an artifact directory. Execution outputs normally live below their producer step folder. Name the exact producer-relative path (for example `{{"{{TARGET_RUN_PATH}}"}}/score-and-plan/trade_plan_summary.json`) or an explicit single-match pattern for generated sub-step folders. Never say only "the artifact under TARGET_RUN_PATH," and never require a root-level copy unless the producer contract explicitly creates one.
- For each eval step (its execution mode lives on the step itself in `evaluation/evaluation_plan.json`; tier and tools live in `evaluation/step_config.json`):
  - **split each eval into scripted extraction + judged verdict.** Compute the facts (counts, totals, diffs vs the source artifact, fixed samples) in code so they are identical run-to-run; judge the verdict against the success criterion on top of those facts.
  - set `execution_mode: "scripted"` on the eval step (`update_evaluation_plan(step_id, execution_mode="scripted")`) when the WHOLE check is mechanical AND anchored to a stable contract (a `db/README.md` schema, the report contract, a fixed output format). Never script against incidental artifact shapes — plan changes alter those, and every shape-coupled eval script becomes a recurring reliability bug. (Unlike an execution step's `main.py`, eval scripting has no 10-run gate — script an objective, contract-anchored check anytime; the explicit-user-request + coverage bar applies only to LOCKING a saved script.)
  - leave `execution_mode` empty (or set `"agentic"`) for genuinely subjective judgment ("is the analysis useful?", groundedness, relevance) — but with a FROZEN rubric: a fixed scale plus written anchors for what a low vs high score looks like, so the judge stays comparable across runs
  - set per-eval-step `execution_tier` with `update_step_config(step_id="eval-step-id", execution_tier="high|medium|low")`; the tool writes `evaluation/step_config.json` when the id is from `evaluation/evaluation_plan.json`
  - tier rule of thumb: `high` for subjective/ambiguous judgment, `medium` for normal eval checks, `low` for deterministic/file-shape checks
  - there is no final combined scoring agent; each eval step must emit structured verdict fields that downstream reviews and reports can read
- After every edit to `evaluation/evaluation_plan.json`, call `validate_evaluation_plan`.
- When you want to test the current eval plan, call `run_full_evaluation(group_name="...")`. Interactive Workshop evaluation targets `iteration-0`; a scheduled invocation targets its bound `iteration-N-sched` folder.

### Writing a GOOD eval (best practices)

A good eval catches a bad run — including one that *looks* successful. Design for that:

- **Deterministic facts, judged verdicts.** If a fact can be computed — "does the figure equal the sum from the source table?", "how many rows landed?" — compute it in code and let the verdict reason over the computed facts. Fully scripted only for contract-anchored mechanical checks; fully agentic only for subjective quality with a frozen rubric.
- **Score outcomes, not artifacts.** Evaluate whether `soul.md` success criteria were actually met — not "did a file get written." A file existing ≠ success. Operational checks belong to Pulse/pre-validation, not here.
- **One eval step per `soul.md` success criterion**, each emitting its own verdict and naming its criterion, so Goal progress stays diagnosable — not one blob score.
- **Anti-placeholder / anti-fabrication.** Score 0 when a value is empty, `N/A`, a placeholder, or a plausible-but-unsourced number. Non-empty ≠ correct.
- **Cross-check against the source.** Verify the claimed result against the actual source artifact (the parsed PDF / file / db row), NOT the step's own summary of what it claims it did.
- **Ground in real run evidence.** Inspect the actual run via `{{"{{TARGET_RUN_PATH}}"}}` and `db/db.sqlite`; never score from the agent's narrative or chat memory. Quote which file/row/value justified the score.
- **Fail loud / fail closed.** Missing input → fail (0), don't skip or assume. An eval that errors or finds nothing must register as failure, never as success. A failure caused by missing/broken input is **Bug evidence** — name the missing path in `reasoning` so Pulse routes it to Bug Review/Fixer instead of reading it as a goal regression.
- **Explicit rubric + thresholds.** State the pass/fail criteria; define what 100 vs 0 means and any partial-credit rule, so scoring is repeatable.
- **Rubric stability.** Changing scale, thresholds, or rubric changes what scores MEAN across runs. Do it only in a deliberate eval improvement pass (`/improve-evaluation`), record a typed Pulse decision flagging that score semantics changed, and never bundle it with a workflow behavior or strategy change in the same pass — otherwise before/after comparisons are uninterpretable.
- **Sandbox hygiene.** Interactive eval steps execute under `evaluation/runs/iteration-0[/group]`; scheduled evals use the matching `evaluation/runs/iteration-N-sched[/group]`. Read evidence ONLY via `{{"{{TARGET_RUN_PATH}}"}}` (and `db/` for persistent stores); never base a score on unrelated files found in an eval folder except facts your own step just extracted. Remember `pre_validation` checks the eval execution folder, not the target run.
- **Route-gate** with `applies_to_routes` so an eval only runs for the path the target run actually took.
- **Cheap checks first.** Presence / format / SQL gates before any expensive model judgment.
- **Independence.** Don't reuse the same reasoning that produced the data to also judge it.
- **Actionable failure.** Emit *why* it failed and *what's missing*, so Pulse Bug Review/Fixer, report/eval repair, or a Goal Advisor proposal can act — not just a number.

### When to write/update evaluation/evaluation_plan.json

- When the user wants to add or change eval coverage
- When success criteria have changed and eval logic must follow
- When Pulse Bug Review or Goal Advisor reveals missing or weak evaluation
- When the scoring logic or eval-step descriptions need tightening

### Evaluation workflow

1. Clarify what the user wants the eval to prove if needed.
2. Edit `evaluation/evaluation_plan.json`.
3. Call `validate_evaluation_plan`.
4. Fix validation errors, then validate again until clean.
5. If needed, call `run_full_evaluation(group_name="...")` to test the plan against the current interactive `iteration-0` group or the schedule-bound group.

### Files

- Plan: `evaluation/evaluation_plan.json`
- Step config: `evaluation/step_config.json`
- Eval runs + reports: `evaluation/runs/iteration-0[/group]/` interactively, or matching `evaluation/runs/iteration-N-sched[/group]/` for schedules.

### Where verdicts end up

Each step's own `score`/`max_score`/`reasoning` (or `pass_fail_reason`) written into its
output is the source of truth — there is no separate scoring agent. After the run, Go
extracts that verdict into `evaluation_report.json` (published under the target run's
`evaluation/runs/<run_folder>/`) and mirrors the same rows into `db/db.sqlite`'s
framework-owned `eval_results` table (`run_folder`, `step_id`, `score`, `max_score`,
`reasoning`, `evidence`), read-only, so Engineering Review can surface verdicts via
`window.report.query` without a separate measurement step. Never insert into
`eval_results` from an eval step — it is written by the framework, the same way
`run_concerns` is.
