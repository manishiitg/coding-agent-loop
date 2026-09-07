**TEMPORARY diagnostic/migration command (PLAT-259).** Not a permanent workflow-maintenance flow — it exists to let the operator align an existing plan (authored before the `routing`/`branch` split) with the new semantics in one pass, and to exercise the split against a real workflow. Remove this command (`cmd/server/guidance/guidance.go`'s `migrate-routing-to-branch` entry, this file, and the `/migrate-routing-to-branch` frontend command in `frontend/src/commands/builtin-commands.tsx`) once the operator is done using it.{{if .Focus}} Focus: {{.Focus}}.{{end}}

## What this does

`routing` used to mean "any deterministic N-way switch," and every plan authored before PLAT-259 still uses it that way. Going forward, `routing` means specifically a major, self-contained sub-workflow fork, and `branch` means a small in-flow next-step decision. This command:

1. Reclassifies each existing `routing` step in the current plan as `branch` if it's really the small-decision case.
2. Checks every `routing` step that stays `routing` against the two route best practices (no shared exclusive interior steps between sibling routes (explicit convergence is allowed); when an eval plan exists, every route should have real eval coverage) — the same judgment checks `plan_drift_review` applies (`references/plan-drift-review.md`), run here directly against the live plan instead of waiting for that module's schedule.
3. Reports a clear summary: what converted, what stayed `routing` and why, and what best-practice findings were filed.

Take action by default per the normal workshop philosophy — do not ask permission before converting an unambiguous case. Only stop and ask the operator when a routing step's classification is genuinely ambiguous (see step 1).

## Step 1 — reclassify `routing` steps

For each step in `planning/plan.json` with `type: "routing"`:

- **Judge structurally, not by route count.** A `routing` step is a genuine major fork when its routes lead to meaningfully different downstream sub-workflows — several steps deep, their own distinct learnings/eval/context scope, or clearly separable concerns (e.g. "new customer onboarding" vs "returning customer renewal"). It is really a `branch` when the routes converge quickly (their `next_step_id`s point at the same step, or nearly-identical short paths), or the decision is a single local fork with no independent downstream identity (e.g. "did the API call succeed or fail?").
- **Convert obvious `branch` cases** with `convert_routing_branch_step_type(existing_step_id=<id>, target_type="branch", reason=<one-sentence classification rationale>)`. This is a purpose-built atomic tool: it relabels the step in place — same `id`, same `routes[]`/`default_route_id`/`route_source_file`, `branch_question` set from the old `routing_question` text — without deleting and recreating anything. Do **not** try to do this by hand with `add_branch_step` + rerouting inbound references + `delete_plan_steps` — that sequence necessarily orphans and prunes the old id's `planning/step_config.json` row (`drift_review`, `execution_tier`, etc.) before the id can ever be reused, so it does not actually preserve history the way it looks like it should. `convert_routing_branch_step_type` never touches `step_config.json` at all, because the id never changes.
- **Flag ambiguous cases to the operator instead of guessing** — name the step and state the specific reason you couldn't classify it confidently.
- **Leave a step alone entirely** if it's already a clear major-fork `routing` step.

## Step 2 — route best practices for steps that stayed `routing`

For each step still typed `routing` after step 1, judge (do not just check mechanically):

- **`route_structural_isolation`** — trace each route's `next_step_id` chain. Two sibling routes legitimately reconverge at a shared step (both terminal steps pointing at the same downstream step, or both at `"end"`); flag it when an *interior* step is reachable from more than one route.
- **`route_eval_pairing`** — if `evaluation/evaluation_plan.json` exists, collect every eval step whose `applies_to_routes` names this routing step, union their `route_ids`, and compare against the step's own declared routes. Missing routes are the finding, with two judgment carve-outs: a genuinely route-agnostic eval step (no `applies_to_routes` at all) can count as coverage; a route landing on a trivial no-op destination may legitimately need nothing.

For any violation found, call `record_pulse_finding(module="plan_drift_review", recommended_route="fixer_handoff", ...)` naming the step and the specific violation, exactly as `plan_drift_review`'s own reviewer turn would — this keeps the finding visible to the normal repair queue instead of only living in this command's chat output.

## Step 3 — report

State, for every `routing` step that existed before this pass: converted (with its new id) / stayed routing, clean / stayed routing, best-practice finding filed (with the finding's issue_id). If nothing needed to change, say so plainly rather than padding the report.
