**Saved-code paths:** Read `workflow.json.code_layout_version` first. In this reference, `<script-dir>` means `code/<step-id>` for version 1, or `learnings/<step-id>` for absent/zero (legacy). Resolve the placeholder before using a path; never infer the version from folders or migrate an existing workflow implicitly. Version 1 executes and repairs canonical source directly, with shared helpers under `WORKFLOW_CODE_ROOT`; only legacy workflows copy code into runs and save it back.

## Plan drift review

`plan_drift_review` is a review-**and**-fix module, the same shape as
`technical_review`: in one retained turn it establishes ground truth per due
step, applies safe workflow-owned repairs directly, verifies each fix, and
only routes to a human decision or a platform-owned boundary what it
genuinely cannot resolve itself. It does not hand routine drift off to
`technical_review` to redo — a check this module already fixed and verified
must never reappear as a separate `technical_review` finding for the same
root cause.

`plan_drift_review` is event-triggered, not cadenced: it becomes due whenever
any canonical plan step has no `drift_review` record at all, or has one with
`needs_review == true` — flagged by the same hook that flags
`description_reviewed` stale on any persisted plan-step field change (this
includes a title-only edit; nothing is classified as cosmetic). It is not
about time passing; it is about a step's configuration having moved since it
was last checked.

This is a **stale flag**, not a null-and-rebuild: a flagged step's prior
review (`checks`, `reviewed_at`, `reviewed_by`, `reviewed_through_change_id`)
stays exactly as it was until you replace it with a completed review. Use
that prior evidence — read it, and read only the `planning/changelog/`
entries for this step **after** `reviewed_through_change_id` (if the step had
no prior review, or no `reviewed_through_change_id`, treat every relevant
changelog entry as new) to understand exactly what changed since the last
check, rather than re-auditing the whole step from scratch.

### 1. Read the precomputed evidence

Call `get_pulse_state(view="module", pulse_run_id=<this run's id>)`. Its
`plan_drift_candidates` array lists every step with no `drift_review` record
or `needs_review == true`, each carrying its plan.json `step_type` (empty if
plan.json could not be read) and already carrying Go-computed results for:

- `report_query_compatibility` — every `window.report.query(...)` in
  `db/reports/index.html` still resolves against the live schema.
- `validation_schema_db_rules` — only when the step's `step_config.json`
  carries a `validation_schema.db[]` override; a plan.json-only declared
  schema with no override is not covered here (check it yourself if the step
  has one).
- `scripted_code_db_queries` — the step's `main.py`, if scripted, still
  resolves its `sqlite3` queries against the live schema.
- `db_readme_contract` — `db/README.md`'s documented `CREATE TABLE` DDL still
  matches the live schema (workflow-wide, attached to every candidate step).

Read `plan_drift_candidates_note` for the exact coverage boundary. Treat a
precomputed `fail` as real evidence — do not re-derive it, and do not accept a
`pass` at face value without confirming it against the check's own explicit
scope (an empty rule set legitimately passes).

### Workflow-level deletion audit (candidates with no real step_id)

`plan_drift_candidates` may include one entry whose `step_id` is
`__workflow_drift_review__` — not a real plan step. It appears whenever
`delete_plan_steps` has run since the last workflow-level audit: a deleted
step's own `drift_review` record is removed along with it, so the per-step
scan above structurally cannot see anything requiring review for a step that
no longer exists, even though the deletion can leave dangling references
elsewhere. Treat this candidate as its own due item, using the exact same
tools as every other step (steps 3-6 below apply to it unchanged) — it just
has no real step to attach fixes to.

Audit procedure:

1. Read this record's own prior evidence (`checks`, `reviewed_at`,
   `reviewed_through_change_id`) the same stale-flag way as a real step's
   record — do not start from nothing if a prior workflow-level audit exists.
2. List `planning/changelog/changelog-*.json` in filename order and collect
   every `delete_plan_steps` entry **after** `reviewed_through_change_id` (or
   all of them, if this is the first workflow-level audit). Each entry's
   `deleted_steps`/`step_ids` names exactly which step IDs were removed and
   why (`reason`).
3. For every deleted step ID, search dependent artifacts for a dangling
   reference to it: other steps' `next_step_id`/`routes[].next_step_id`/
   `predefined_routes` (a route or chain that still points at the deleted
   ID), `evaluation/evaluation_plan.json` (`applies_to_routes` naming the
   deleted step, or an eval step whose whole purpose was evaluating it),
   `db/reports/index.html` and `db/README.md` (mentions of the deleted step
   by name/id), and `learnings/_global/` notes referencing it. A route/chain
   left pointing at a deleted step is real drift — the workflow can no longer
   execute that path.
4. Fix what is safe and workflow-owned directly (redirect a dangling
   `next_step_id`, remove an orphaned eval step or route reference, update a
   stale doc mention), verify each fix, and route anything you cannot safely
   fix in this turn using the exact same classification scheme as step 4
   below (`step_id="__workflow_drift_review__"` on every `record_pulse_finding`
   call).
5. Persist with `record_plan_drift_review(step_id="__workflow_drift_review__",
   checks=[...], reviewed_through_change_id=<latest delete_plan_steps
   change_id you actually read>)` — one call, same shape as a real step's,
   using a `check_id` such as `deleted_step_dependent_artifact_audit` per
   deleted step ID or per distinct artifact surface checked.

### 2. Fill the gaps Go could not precompute

For each candidate step, directly check what the deterministic pass skipped:

- **`validation_schema` file rules** — if the step declares
  `validation_schema.files[].json_checks` or `must_exist`, read that step's
  most recent real run output and verify those assertions still hold by hand.
- **Orphaned tables** — once per pass, not per step: list `db.sqlite`'s
  tables via `query_workflow_db`, cross-reference against report queries, any
  step's DB rules/scripted queries, and `db/README.md`. A table matching none
  of those and not a platform-reserved table is a candidate, not a certainty
  — say so in the evidence.

Then do the judgment checks a Go function cannot:

- **Step description accuracy** — does the description still match the
  step's actual configured behavior (prompt, tools, store access)?
- **Learnings / KB content staleness** — does `<script-dir>/main.py`
  or its knowledgebase notes still describe what the step currently does?
- **Learnings / KB access appropriateness** — is the step's
  `knowledgebase_access` / learnings access mode still the
  right choice given the step's current maturity (not just internally
  consistent with itself)?
- **DB schema normalization** — once per pass, not per step: informed by
  `PRAGMA table_info` across every table, judge whether the schema stays
  reasonably normalized, not merely whether each table's own contract holds.

For a candidate whose `step_type` is `"message_sequence"`, first load
`read_skill(skills=[{"name":"builder-reference","path":"references/message-sequence.md"}])`
and record one additional **`message_sequence_best_practices`** check. Compare
the actual step description, `items[]`, validation schema, context boundaries,
and access configuration against that reference. The evidence must explicitly
cover whether the sequence:

- is one coherent shared-context span rather than unrelated work accumulated
  into one conversation;
- completes the outcome, re-opens authoritative evidence, and repairs plus
  double-checks verified gaps (or explains why a smaller sequence is sufficient);
- keeps deterministic API/CLI fetching, stable parsing, and mechanical
  persistence in scripted regular steps rather than spending agent turns on it;
- uses `prevalidation` only for a real intermediate boundary before a costly or
  hard-to-undo later turn, never as a duplicate of the automatic final gate;
- validates the durable output or real side effect with run-specific proof and
  does not hide durable handoffs or retries only in conversation memory; and
- narrows item-level write access where needed without exceeding or needlessly
  broadening the step-level permissions.

This is a judgment check, not a regex. Repair a bounded structural or prompt
violation in this same pass when safe; otherwise file and link the exact Pulse
finding before recording a failed check. `record_plan_drift_review` rejects a
message-sequence review that omits this check.

Apply the same reference-backed pattern to the other specialized step types:

- For `step_type: "regular"` (the scripted step), load
  `references/scripted.md` and record **`scripted_best_practices`**. Confirm the
  work is genuinely deterministic rather than judgment-heavy; declared mode,
  `<script-dir>/main.py`, inputs, outputs, and description agree; fixed
  API/CLI calls, parsing, persistence, retries, and errors are fail-closed and
  idempotent where required; validation proves the real durable result; and a
  retained recent run shows the script actually performs its stated job. If a
  regular step is still agentic, treat that as a migration question: bounded
  deterministic work should become scripted, while conversational judgment
  should become `message_sequence`.
- For `step_type: "orchestrator"` (the orchestrator), load
  `references/orchestrator.md` and record **`orchestrator_best_practices`**. Confirm
  runtime orchestration is genuinely dynamic or adaptive rather than a fixed
  child list; route eligibility/conditions are distinct; inline versus shared
  orphan ownership is valid; sub-agent context, outputs, retries, and completion
  receipts are explicit; and the parent does not duplicate child work.
- For `step_type: "routing"`, load `references/routing.md` and record
  **`routing_best_practices`** in addition to the two route checks below.
  Confirm this is a major self-contained fork, selection is deterministic and
  fail-closed, targets exist, sibling paths cannot fall through, and every path
  intentionally converges or ends.
- For `step_type: "branch"`, load `references/branch.md` and record
  **`branch_best_practices`**. Confirm this is a small in-flow decision rather
  than a major route, selection is deterministic and fail-closed, targets
  exist, sibling paths cannot fall through, and convergence/end behavior is
  explicit.

These are also judgment checks with same-pass repair authority. The persistence
tool rejects a step receipt that omits its matching reference-backed check.

For a candidate whose `step_type` is `"routing"` only (never `"branch"` —
branch is deliberately the small in-flow decision, these two checks do not
apply to it; see `references/routing.md`/`references/branch.md`), also
judge:

- **`route_structural_isolation`** — read this step's `routes[]` and
  `next_step_id` chains directly from plan.json. Trace each route forward
  step by step. Two sibling routes legitimately reconverge at a shared
  step — routing.md's documented convergence pattern, where both routes'
  terminal steps point their `next_step_id` at the same downstream step (or
  both at `"end"`); that is not drift. Flag it when an *interior* step
  (reached before either route's own convergence point) is reachable from
  more than one route: that means the routes are silently sharing exclusive
  path segments they shouldn't, not deliberately rejoining.
- **`route_eval_pairing`** — check whether the workflow has an
  `evaluation/evaluation_plan.json` at all. If it does not, this check is
  out of scope for this step — record it `pass` with evidence saying the
  workflow has no eval plan, nothing to pair. If it does, this check is
  about **coverage of every route this step declares, not just any one
  eval reference** (a single eval covering 1 of this step's 5 routes must
  not pass): collect every eval step whose `applies_to_routes` names this
  routing step's ID (e.g. `applies_to_routes:
  [{"routing_step_id":"<this step's id>", "route_ids":[...]}]` — see
  `references/evaluation-plan.md`), union their `route_ids`, and compare
  that union against this step's own declared `routes[].route_id` set.
  Missing routes are the finding — name them by `route_id` in the
  evidence. Two carve-outs, both judgment calls, not mechanical: (1) an
  eval step with no `applies_to_routes` at all runs for every execution
  regardless of route, so if it genuinely evaluates something the routing
  decision itself doesn't affect (e.g. a global output-format or
  cost-discipline check), it can count as intentionally route-agnostic
  coverage for routes that have no route-specific eval step — but if it
  only evaluates the behavior of the specific branch the run happened to
  take, it does not count as covering the routes it never sees. (2) a
  route whose destination is a trivial no-op (e.g. `next_step_id` goes
  straight to `"end"` with nothing produced) may legitimately have nothing
  worth evaluating — judge whether that's really true before treating it
  as covered.

### 3. Apply safe workflow-owned fixes and verify each one

For every check that failed, fix it now if it is a bounded, safe,
workflow-owned repair — the same standard `technical_review` applies to its
own repair batches. This is the normal path, not the exception:

- A broken `window.report.query(...)`, `validation_schema.db[]` rule, or
  scripted `main.py` query usually means a report/schema/script still
  references a renamed or dropped column/table — update the referencing
  artifact (the report SQL, the schema rule, or the script) to match the live
  schema. Route a genuine orphaned table through
  `apply_workflow_db_migration` (it auto-snapshots before any destructive
  statement) rather than a raw `DROP`.
- A stale description, stale learnings/KB content, or a wrong learnings/KB
  access mode is a direct edit through the normal Workflow Builder tools
  (`update_step_config` and friends).
- A `db/README.md` contract mismatch is a doc edit reconciling the
  documented DDL with the live schema (or the reverse, if the doc is right
  and the schema drifted — decide which side is authoritative from the
  step's actual current behavior, not merely which was easier to edit).

After each fix, verify it the same way `technical_review` verifies a repair:
re-run the specific check (re-derive the report query result, re-check the
schema rule, re-read the file) rather than assuming the edit worked. Record
that check `status: "fixed"` with evidence describing both what was wrong and
what you changed and confirmed.

### 4. Route what you cannot safely fix in this turn

Not every drift check is a same-turn repair. Classify anything you did not
fix using the same routes `technical_review` uses — do not invent a
different scheme. Every `record_pulse_finding` call in this step must carry
`step_id=<this step's id>`: `record_plan_drift_review`'s finding-verification
requires each fail-status check's linked finding to be filed against the
exact step under review, not merely to exist somewhere in the backlog.

- **A genuine user decision** (e.g. two materially different ways to
  reconcile a contract, and only the operator can say which is intended):
  call `create_human_input_request` first, then `record_pulse_finding` with
  `step_id`, `recommended_route="decision_required"`, and that
  `human_input_id`.
- **A platform-owned boundary** (a runtime/harness/bridge limitation, not
  this workflow's own plan, config, code, or data): `record_pulse_finding`
  with `step_id` (`recommended_route` may be omitted — it is not a valid
  route for this case). Then, in the same turn's step 6 close-out, give that
  finding's `finding_dispositions[]` entry on `record_pulse_result`
  `disposition="external_action_required"` with a `reason_code`, an
  `external_owner`, and a `reopen_condition` — those three fields belong to
  the disposition, not to `record_pulse_finding` itself.
- **Insufficient evidence to fix safely right now** (e.g. a real fix needs a
  future run's output to confirm): `record_pulse_finding` with `step_id`,
  `recommended_route="evidence_wait"`, and an exact `next_check`.
- Only as a last resort — a fix that is real, workflow-owned, and safe in
  principle, but too large or cross-cutting for this focused pass to
  complete and verify on its own — fall back to `record_pulse_finding` with
  `step_id` and `recommended_route="fixer_handoff"` so `technical_review`
  picks it up. Keep this rare: a `fixer_handoff` finding for something this
  module could safely have fixed itself is exactly the extra Pulse cycle
  this design exists to remove.

Reuse an existing issue's `issue_id` for the same semantic root cause instead
of filing a duplicate — check the compact backlog first (an existing issue
keeps its original `step_id`; a later `step_id` argument on an `issue_id`
update does not move it to a different step). File findings **before**
persisting the review: `record_plan_drift_review` rejects a fail-status check
with no `finding_id`, and rejects a `finding_id` that does not resolve to a
real, active, already-filed Pulse finding for this exact step — this is what
keeps a routed check from ever being persisted as "reviewed" with no
corresponding tracked item.

### 5. Persist the merged result per step

Call `record_plan_drift_review(step_id=..., checks=[...], reviewed_through_change_id=...)`
exactly once per candidate step, merging the precomputed checks from step 1
with the ones you ran directly in step 2, using `status: "fixed"` for
step-3 repairs and `status: "fail"` plus the linked `finding_id` for
step-4 routes. Every check needs a real `check_id`, a `status` of
`pass`/`fail`/`fixed`, and specific `evidence` — never a placeholder like
"ok" or "n/a" (the tool rejects evidence under 15 characters for exactly this
reason). Pass `reviewed_through_change_id` as the latest
`planning/changelog/` `change_id` you actually read for this step, so the
next review resumes exactly where this one left off. This call always fully
replaces the step's prior evidence and clears `needs_review` — there is no
partial update.

### 6. Close out

Update the run-scoped checkpoint (`runs/pulse/<run>/plan-drift-review.md`)
with a compact per-step summary before ending. Call `record_pulse_result`
exactly once for the terminal `plan_drift_review` module result, with a
`finding_dispositions[]` entry for every finding filed this turn — including
`disposition="external_action_required"` (with `reason_code`,
`external_owner`, `reopen_condition`) for step 4's platform-owned findings.
Do not render HTML, back up, publish, or notify — those belong to the
finalizer stage.

A child still running without a checkpoint or receipt is not a failed review.
Wait for its terminal result before judging it. If a premature failure was
already recorded for this same `pulse_run_id`, a verified correction to
`done`/`changed` must include `verification` plus `evidence` or
`finding_dispositions`. A `changed` correction still needs the normal complete
repair proof. The backend updates current state and audit atomically and keeps
the previous receipt in history; do not fabricate proof to clear an error.
