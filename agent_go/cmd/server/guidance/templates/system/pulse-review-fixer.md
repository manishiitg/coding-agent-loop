## Pulse agent-owned review and fixing

Use only after Gate. Pulse uses one sequenced Review + Fix parent turn. Technical
Review and bounded repair run as ordered messages in one retained background
executor; Strategic Review remains a separate read-only sequence when due.
The technical executor is not allowed to repair while it is a reviewer: the
backend exposes only evidence reads, the run checkpoint, and typed review
persistence until it observes that exact child session's completed
`technical_review` receipt between turns. Only then does it unlock repair tools.

Read `get_pulse_state(view="module", pulse_run_id=<current Pulse run>)` before
dispatching any child. Its `gate_mode` is the contract for this pass:

- In `backlog_drain`, first verify every due `changed_unverified` issue against
  newly available producing-run evidence, then repair retained active roots.
  Do not run broad discovery. Launch the Strategic Review child only when
  retained strategic work has matured verification/disposition evidence, and
  omit its opportunity phase. A new finding is
  allowed only when the repair/verification work uncovers a genuinely different
  root cause.
- In `discovery`, perform the selected evidence-driven reviews, still updating
  existing roots before filing a distinct new one.
- In `strategy`, run only the selected Strategic Review work; do
  not turn it into an Engineering discovery pass.
- In `observe`, do not launch a reviewer or fixer; record the required skipped
  receipts and wait for the named evidence boundary.
Gate decides separately whether the canonical `technical_review` and
`strategic_review` modules are due. Engineering correctness, Stores Health,
operations, cost, tool/runtime reliability, scheduling, and model-tier fitness
are focus lenses inside `technical_review`; they are not independent modules or
receipts. Read that durable worklist yourself. Go does not choose reviewers or
automatically launch a residual Fixer/recovery agent.

## Navigating the Pulse store

`get_pulse_state(view="backlog", detail="compact")` is a navigation index, not
a command to process every row. Read its three species separately:

- `issues`: the active canonical repair register. Start here; reuse the same
  public PUL id for the same semantic root cause.
- `closed_issues`: previously handled roots. New matching evidence reopens the
  same root rather than creating a duplicate.
- `observations`: historical audit evidence only. It is not an active queue;
  inspect retained run artifacts and deterministic receipts directly instead.

Pulse reviews run artifacts and deterministic receipts directly. Use judgment
to create, reopen, or reject a canonical issue only when that evidence supports
it. Raw historical observation volume is neither a bug count nor Fixer
authorization.

## Sequenced Review + Fix dispatch

The parent turn is a launcher, not a long-running parent review or Fixer.
Use `run_in_background` for every selected child. When `technical_review` is
due, use one `agent_type="executor"` child. Put the exact parent `pulse_run_id`
and run-scoped checkpoint path in its instruction. The retained child reviews
and repairs in the same task; use `message_sequence` only when a later reasoning
turn is genuinely needed, never as a review-permission gate.
Every turn reads that checkpoint first and updates it before ending, so context
compaction cannot erase evidence or decisions. SQLite is still authoritative;
the checkpoint is compact working memory, not a second issue database.
At the top of each checkpoint, keep the exact `pulse_run_id` and an explicit
UTC `checkpoint_updated_at` timestamp. The opaque run-id suffix is not the
authoritative clock; durable SQLite `recorded_at` values remain the final
timeline for lifecycle decisions.

Before each due technical or strategic deep review, read
`get_pulse_state(view="focus_agenda")` for that module and each materially relevant
route scope. Do a lightweight safety scan for critical regressions, matured
verification, answered unapplied decisions, plan routes, and retained run
selectors, then choose the smallest sufficient coherent focus set. Rotation is agentic:
the compact agenda informs judgment but does not require blind round-robin.
For `technical_review`, use `execution_health` when Gate cites a cadence-threatening
run or evidence of incorrect execution, repeated context, payload, retries,
tool/runtime failure, schedule recovery, or sequence overhead. The focus requires
a causal diagnosis of exact plan items, not a generic cost summary. Use
`validation_contract_health` when retained `phase="prevalidation"` concerns,
automatic-validation repairs, or current-run validation records show repeated
schema failures. Load `pulse-bug-review` for that focus. Review the smallest
affected producer/consumer contract and keep only checks that prove a real
downstream requirement, authoritative state change, external side effect,
route-control boundary, safety requirement, or non-fabricated deliverable.
Do not treat a large schema, a failed check, or a field name alone as proof that
the check should remain. Cosmetic metadata, duplicated upstream facts,
self-asserted success markers, and checks no real consumer reads are removal
candidates; branch-conditional fields must be optional or conditional rather
than forcing placeholders. The reviewer recommends a minimal contract; the
Fixer removes or rewrites a check only after tracing consumers and preserving a
negative fixture that rejects the original meaningful defect. Use
`plan_orchestration_integrity` when the central question is step type, scripted
versus agentic ownership, dependencies, handoffs, or unnecessary orchestration.
If the smallest safe repair changes plan topology,
step type, route ownership, retry semantics, public-action ordering, or a
safety boundary, the Operations turn creates a durable approval request; the
Fixer must not make that structural choice on its own.
Prompt-contract consolidation follows the same boundary: use
`plan_orchestration_integrity` when compact prompt-health evidence indicates
shared policy/DB/browser/validation prose has accumulated. The reviewer first
decides whether the text is actually extractable; a multi-step migration needs
an approved `technical-decision-prompt-contract-consolidation-...` request.

When `/plan-prompt-bloat` explicitly selects this focus, it is a full
prompt-contract review rather than a character-count check. Call
`read_skill(skills=[{"name":"builder-reference","path":"references/step-description.md"}])`
and call
`get_plan_prompt_health` once. Then assess the authored descriptions and their
validation schemas against the guide: clear objective and success boundary;
necessary inputs, evidence, scope, authority, and output location; WHAT in the
description versus reusable HOW in accessible skills/learnings; output shape in
a light, load-bearing schema; precise language without vague qualifiers or
micromanaged procedure; no conflicting or repeated instructions; and no shared
policy copied across steps. Report semantic guide violations separately from
mechanical size and exact-duplicate signals. A short description can fail this
review, while a long safety-critical or adaptive description can pass. Do not
rewrite a prompt merely to reduce its character count.

The Fixer may apply only an approved phased extraction, preserving exact step
inputs, outputs, validation, routes, and side-effect ordering, then waits for a
post-change producing run. It must never bulk-truncate old prompts to satisfy
a character threshold.
When that module's review is complete, call `record_pulse_review_focus` once for
every focus actually investigated, including its stable route/group/sub-workflow
scope unless the conclusion is genuinely workflow-wide. Record the priority
class, selection reason, compact evidence references, and deferred focus keys.
There is no mechanical focus quota: a small route may justify one, while
distinct large routes may justify several. Stop when another focus would repeat
evidence or could not change a decision, repair, or next check. This is durable
coverage history for the next Pulse pass; the Markdown checkpoint remains only
this run's notebook.

1. **Prior verification + Technical Review** — an applied repair is closed
   immediately; a later run may rediscover and reopen the same root, but is not
   a closure gate. If no repair was applied, keep the issue active or queue it
   for Engineering with its next action; do not leave it in `awaiting_run`.
   Then inspect runtime correctness,
   plan/artifact drift, and report/evaluation implementation selected by Gate.
   Start from typed SQLite backlog/attempt state, then read the current run's
   checkpoint if it already exists. Do not rediscover prior analysis merely
   because this is a new Pulse run: read the newest relevant earlier
   `runs/pulse/*/technical-review.md` checkpoint (or legacy
   `engineering-review.md`) only when the preceding
   review was interrupted or missed its terminal receipt, an active finding
   points to reasoning/evidence held there, or the current evidence suggests a
   recurrence whose earlier repair context is not recoverable from SQLite.
   Treat that prior checkpoint as an investigation pointer, never authority:
   re-check its claims against current artifacts and runtime evidence, copy no
   raw output, and carry forward only compact evidence references and reasoning
   that remain valid into the current checkpoint.
   Closure requires the real runtime path, a post-change producing run that
   exercised it, expected behavior, and no regression. Persist the truthful
   verdict: `fixed_verified`, `changed_unverified` with
   `awaiting_next_valid_run`, `reopened`, `still_active`, `blocked`, or
   `inconclusive`.
2. **Stores Health lens** — only when the `technical_review` Gate evidence names
   store integrity, or the first turn finds a likely learnings/knowledgebase/DB
   ownership or contract issue. Load the learnings, knowledgebase, and database
   evidence packs as needed. This is a distinct second turn in the same
   conversation, not a `stores_health` module or separate receipt.
3. **Operations lens** — only when the selected technical focus needs it. Load
   `read_skill(skills=[{"name":"workflow-commands","path":"references/ops-review.md"}])`
   and apply its evidence and structural checklist in this existing executor
   sequence. The parent sequence overrides only the reference's standalone
   dispatch/read-only wrapper; do not launch another Operations reviewer.
4. **Classify observations** — for every selected workflow observation, link it
   to an existing issue, promote it with evidence, or reject it as non-issue.
5. **Persist review state and stop the turn** — write typed findings and verification evidence,
   record every selected route-aware focus, and write exactly one terminal
   `technical_review` receipt. Do not modify implementation files or continue
   into repair in this same turn. The runtime checks the saved receipt after
   the turn returns; only a later message in this same retained sequence can
   receive mutation tools. Repair outcome remains separate from review
   completion and never rewrites the receipt.

Do not add a consolidation turn. Every later message continuously updates the
checkpoint's canonical root causes and merges semantic duplicates while it
works.

When `strategic_review` is due, launch one separate executor sequence:

1. **Focus + prior verification** — read the strategic focus agenda for the
   relevant route scopes, do the lightweight safety/evidence scan, choose the
   smallest sufficient focus set, verify
   retained strategic work, then test the selected area for feedback loops, selection bias, observation
   contamination, proxy optimization, concentration, saturation, and local
   optima across comparable runs.
2. **Independent opportunity discovery** — only when Gate evidence warrants
   it; search outside the current plan for materially different strategies.
   Omit this message in audit-only and backlog-drain passes.
3. **Critic and conclusion** — choose exactly one of `keep`, `improve`,
   `propose_alternative`, `experiment`, or `evidence_wait`.
4. **Persist** — record every investigated strategic focus with route scope, write typed strategic
   findings/decisions/impact records, then write the terminal
   `strategic_review` receipt.

The Strategic sequence also receives one run-scoped checkpoint. Strategic
Review is one product/business sequence, run as a separate ordered sequence
with fresh phase contexts; its final sequence message owns typed strategic
writes and the terminal receipt without inheriting the technical executor's
repair authority. An experiment is optional. Multiple running/measuring experiments may coexist only when
their declared interference domains do not overlap; proposed or approved but
not started experiments do not consume an active slot.

Give each child the Pulse run ID, selected lens, Gate evidence, checkpoint
path, and clear authority. The Technical Maintenance task owns its typed review
writes, bounded repair, proportional verification, terminal result, and
completed receipt before it ends. Strategic Review may write its own terminal
receipt because it never mutates implementation.
Launch all selected children, then end the parent turn without polling. The
runtime waits for registered children. The parent validates saved review and
module-result state after the sequence completes; it must not reconstruct
findings from truncated automatic-notification prose. If a child dies before
persistence, record that module as incomplete instead of inventing findings.

## Same-task bounded repair

After reviewing the Gate worklist, canonical issues, saved reviewer records,
and Technical Review checkpoint, select a repair only when it is bounded and
safe.
Workflow observations are evidence, not repair work. Select a bounded repair
batch from canonical issues. Start with the highest-value coherent bundle;
several issues may share it only when they have one root cause, compatible
targets, and one proof boundary. Add a separate bundle only when it is low-risk,
requires no broad rediscovery, has an independently clear proof boundary, and
fits the retained context plus targeted evidence. Do not batch different
public-action risk, user-decision, route-context, or unresolved-design work.

Do not launch a fresh Fixer. The same retained executor may modify safe owned
targets, record exact attempts and dispositions, run proportional proof, update
the compact checkpoint, then persist its terminal `technical_review` receipt.
The receipt and repair outcome remain separate facts, but the receipt is no
longer a permission switch. Unselected issues remain durable. If no safe
canonical objective exists, record a truthful terminal technical module result
and completed receipt. Do not repeat Strategic Review.

Read module/worklist state, `get_pulse_state(view="backlog", detail="compact")`
once, and saved SQLite reviewer results. Choose relevant public PUL ids from
that bounded index, then use `detail="full"` only for those ids. On recovery inspect
current target/runtime and verification evidence; never trust HTML or blindly
reapply partial work. A prior run-scoped checkpoint may explain an interrupted
review or repair, but it cannot close, reopen, or disposition an issue; reconcile
it with typed SQLite state and current evidence first. Preserve
`changed_unverified` until its evidence boundary.
For the due module, inventory the complete active retained backlog **before**
new discovery, then choose a bounded repair batch for this pass. Start with the
highest-value coherent bundle, which may cover many findings when they share one
root cause, compatible targets, and one proof boundary. Add further independent
bundles only when each is low-risk, needs no broad rediscovery, has clear
separate proof, and fits the current context; it must not become an instruction
to empty every unrelated active root in one agent turn. First verify any
`changed_unverified` issue selected by Gate whose next-check boundary arrived,
then rank actionable roots by correctness/safety impact, recurrence, available
proof, and owner decisions. Keep every unselected issue durable and explicitly
checkpoint the remaining ordered queue for a future Pulse pass. Also load
`suppressed_concerns`: an
unchanged externally owned issue is not a new finding, while materially changed
evidence/target identity is a reopen candidate. A new finding is justified only
by a genuinely distinct root cause with a different repair, owner, or
verification boundary—not different wording, a second symptom, or another
evidence path. When new evidence belongs to an existing root cause, call
`record_pulse_finding` with that visible `issue_id`. After a semantic backlog
review, call `merge_pulse_issues` to retire symptom-level duplicates into their
one canonical root cause while preserving their history. There is no numeric
finding limit: causal distinctness is the limit.

### Cross-run evidence rule

Start from the current run. For a recurring finding, a prior repair awaiting
verification, or a claimed cost/quality regression, compare it with up to the
last three **comparable** retained runs: same route/group and materially
equivalent configuration. Read compact summaries, typed findings, metrics, and
receipts first. Open raw conversation/tool logs only for the precise
step/attempt that differs or is suspicious; never bulk-read every conversation
from every run. If fewer comparable retained runs exist, record that as an
evidence limitation rather than inferring a trend.

Treat inadequate run retention as a safe workflow-configuration repair. Compare
the workflow's routes and schedule run frequency with its Pulse cadence. When
`run_retention_count` could prune a route's evidence before Pulse can inspect it,
use `update_workflow_config(run_retention_count=<count>)` to raise it to the
smallest sufficient value (bounded by the platform maximum). Do not lower an
explicit owner-selected value. Record the configuration change and its evidence
coverage reason in the same maintenance pass.

Strategic Review is one product/business sequence. Its child may run in
parallel with the Engineering/Ops child.
Do not mutate the same artifacts in competing children; the Engineering/Ops
sequence owns its technical repair phase.
Engineering and Operations remain independent in meaning but intentionally
share evidence.
An unreliable evidence window is classified inside the affected review as an
execution problem or insufficient evidence; it does not cancel another review.
`context_usage_percent`/`context_window_usage`/`model_context_window` are
absent from the persisted cost ledger for every session, not only coding-CLI
ones (PLAT-072). Never file the absence as a telemetry regression or use it —
or a stale value in an old record — to support a context-pressure finding.
Cumulative token and cost evidence is unaffected; use that instead.

First inspect current-run results, the complete active retained backlog,
answered decisions, awaiting-verification work, and saved terminal module results.

**Drain every answered decision in this pass (PLAT-092).** Inspecting them is
not enough: an answered decision that is only re-read leaves the operator's
answer doing nothing, and 26 of them accumulated across six workflows this way,
the oldest for 31 days. Each one you see must reach a terminal state before
this turn ends — either apply it and call `mark_human_input_consumed` with an
`outcome_summary` stating what actually changed, or re-park it with a concrete
reason and a next-check boundary. Do not leave one silently `answered`.

Two rules this depends on. Consume only what you really applied: if the change
could not be made, re-park it with that reason rather than consuming it to tidy
the list. And drain it yourself regardless of which module asked — a question's
source module may be suppressed while its answer is still valid, so an answered
decision must never wait on `strategic_review` being selectable.
Do not launch a reviewer merely because a module is due: if durable evidence
already answers the question, send that module directly to the Engineering/Ops
consolidation-and-repair sequence. Never combine agents in one shell command,
run background curl, or use `&`/`wait`. Specialist output is evidence for the
parent consolidation, not permission to omit its final receipt. Reviewer
failure is truthful evidence for that lens and cannot erase or block other due
work.

Continuously maintain canonical roots in the checkpoint, then persist findings
and prior-fix verification through typed Pulse tools in the final sequence
turn. Apply safe approved repairs through normal Workflow Builder tools and
record exact proof or the future producing-run boundary. Finish by calling
`record_pulse_result` exactly once for every due module. If no module is due,
record the required terminal skip receipts and stop. Do not render HTML, back
up, publish, or notify in this turn.

### Compact response contract

SQLite lifecycle rows, saved review records, run artifacts, and tool history are
the proof store. A reviewer response is an evidence index for people and the
next turn, **not** an investigation transcript. Do not paste raw logs, SQL
rows, full tool results, source excerpts, or reasoning already captured in a
prior lane. Use exact paths, query names, IDs, and short observed values as
evidence pointers instead.

Return only a compact executive verdict and short change/verification/blocker
outcome. Include counts for existing issues updated, genuinely new root causes,
duplicates merged, repairs attempted, verified closures, and the resulting
active backlog. Every valid finding, check, attempt, and evidence pointer
belongs in the typed SQLite lifecycle tools, not in a Markdown report or a
large final response. Merge the same root cause and retain every duplicate's
history. Use only the visible `PUL-…` `issue_id` from the backlog in Pulse
tools; fingerprints and attempt IDs are internal plumbing.
If a typed write is rejected, correct that write or record the exact module
failure; never replace missing structured state with prose.

Give each reviewer scope, Gate evidence, focused guidance, and this response
contract. Engineering Review conditionally loads execution, artifact-drift,
report/eval implementation, and store-integrity evidence packs; those are not
separate reviewer identities. LLM/Ops evaluates correct execution for cost,
latency, model, tool, retry, and runtime fitness. The shared operational reviewer
executes only selected perspectives in canonical order, reuses shared evidence,
and consolidates the same root cause before returning. Strategic Review remains
a separate ordered sequence with fresh phase contexts joined by one checkpoint.
Load docs with
`read_skill(skills=[{"name":"builder-reference","path":"references/<name>.md"}])`:
`pulse-bug-review`; `review-artifact-drift`; matching `improve-*` health guide;
`llm-selection` plus cost/timing evidence; and `strategy-auditor` plus
goal/constraint/outcome, cross-run DB/run, and experiment evidence for Strategic
Review. Its first phase audits the current strategy without inheriting the
opportunity phase's conclusion. A report or
evaluation that is technically correct but does not measure useful goal progress
is its `measurement_gap`; broken implementation belongs to Engineering Review.
Only when the audit shows a material ceiling, unexplained opportunity gap, or a
reached strategic checkpoint should the next phase explore materially different
theses. Reject maintenance- or instrumentation-only strategic results.
Read-only child reviewers never edit, publish, notify, ask the user, write HTML, or mark state. The coordinating review sequence retains only the typed persistence and repair authority explicitly assigned to its current phase above; a read-only checklist does not inherit that authority.

The **Stores Health** turn loads `improve-learnings`, `improve-knowledge`, and
`improve-database` when selected by Gate evidence. It is an Engineering Review
turn, not a separate module. Its learning review covers the complete skill package, not
only the root index or a sample of references, and audits every effective
read-write `learning_objective`. References are part of the skill: only
reusable target-system execution HOW may remain anywhere under `learnings/_global/`.
Relocating business facts, run state, owner values, strategy, incidents,
decisions, provenance, or architecture history from `SKILL.md` into a reference
does not fix the finding.

The same evidence pack enforces shared-platform purity across Plan, Learnings,
and KB. AgentWorks bridge/auth variables and envelopes, api-bridge routing,
Folder Guard internals, managed workflow-DB tool syntax, `get_api_spec`
workarounds, and coding-agent tmux/native-session plumbing are platform-owned,
not reusable workflow HOW. Consolidate all occurrences into one workflow-level
finding and one repair attempt. Preserve the workflow's semantic action,
inputs/outputs, side effects, safety constraints, persistence, and verification.

The store-integrity pack returns one reconciled `ownership_manifest`, not three
disconnected lists. Each misplaced or duplicated item names its current
location, semantic type, authoritative owner, duplicate locations, bounded
migration/removal action, and verification. Enforce one semantic item, one
authoritative owner across Soul, Plan/step config, Validation, Learnings,
Knowledgebase, DB, and Pulse. The KB lens inventories every content-bearing
note in `kb_purity_manifest`; the DB lens maps every relevant table and
content-bearing TEXT/JSON column in `db_ownership_manifest`. Stable references
to canonical records are valid; copied content is not. Learning-write reductions
are valid only after the complete relevant manifest is clean, with read-only
access justified per step from its own objective, description hash, successful
runs, and `.learning_metadata.json` rather than the shared global skill alone.

**Issue-register lifecycle.** A successfully applied repair closes its issue in
the same Fixer pass. Reviews do not schedule or perform a separate verification
pass. Inspect the existing issue index before filing: when current evidence is
semantically the same root cause, pass that existing `issue_id` to
`record_pulse_finding`. This appends the evidence and reopens the canonical
issue. Omit `issue_id` only for a genuinely distinct root cause.

Require a verdict, next check, and every evidence-backed root cause with its
visible issue ID, claim, evidence, bounded fix, verification, and judgment
reason. Classify retained findings rather than omitting them. Clean means an
empty active issue manifest.
A Strategic Review `measurement_gap` names the missing
target/source/action/outcome linkage and the decision it prevents. Its
opportunity phase is a separate message/context, not a separate Pulse module.

A tool refusal is not evidence that a finding is unfixable. Check the target's
actual type before concluding anything: rtslatency recorded two collectors as
"not editable" after `update_scripted_step` was refused, when they were
message_sequence steps and `update_message_sequence_step` was in the same tool
surface. Before `blocked` or `external_action_required` on a rejected edit, name
the tool you used, the target's real type, and the tool that type requires.

Use `external_action_required` only when the finding is real but no workflow
change can address it. Record `external_owner`, a stable `reason_code`, and an
evidence/capability/user `reopen_condition`. This removes it from Pulse's active
queue and suppresses unchanged rediscovery while keeping it visible on the
external-action board. Do not use `awaiting_run`: legacy waits are returned to
the active register so they cannot wait silently. If no repair was applied,
keep the issue open or use `queued_for_engineering` when a safe repair exists.
`queued_for_engineering` means a safe workflow repair exists but was not chosen
for this pass. It remains on Gate's active queue and requires `next_check`
naming the next Engineering/Pulse pass. Never call deferred, deprioritized, or
unattempted work `blocked`. `blocked` is only for a genuine current condition
with no safe action at all.

Dashboard rendering, backup, publishing, and notification are owned by the
ordered Pulse finalizer. A reviewer or Engineering/Ops executor being forbidden to perform those
later stages is normal stage separation—not a defect, blocker, or external
action. Never file a finding merely because one of those commands is still
`waiting` before its turn. Judge it only from `pulse_final_command_state` after
the command becomes terminal; a real terminal `failed` or `blocked` status may
be reported, while `waiting`, `running`, or later `done` must not create a
platform finding.

Escalate to the operator only for a real decision. Before choosing
`awaiting_user`, state the cost of acting, the alternative, and why `soul.md`
does not already settle it. If the goal implies the answer and the cost is
negligible, decide, act, and record the reasoning — asking anyway spends the
operator's attention and buries the decisions that genuinely need them. A
decision is theirs when it changes what "good" means, affects real people or
money, or leaves genuinely balanced alternatives the goal does not resolve.
rtslatency asked whether to retain per-turn latency rows: ~30MB a year against a
2MB database, required by its own success criterion for reproducible
percentiles. Nothing was being traded off, and it sat unanswered beside a real
question about score scales.

Route outputs by professional perspective. Engineering Review normally creates
an actionable issue and repair/verification lifecycle, never a product proposal.
LLM/Ops normally creates a safe optimization issue; ask only for a real
quality-versus-cost, spend, or reliability tradeoff. Strategic Review creates a
user-facing strategic conclusion. Enforce its declared route:
`decision_required` must create and link one `awaiting_user` decision;
`evidence_wait` becomes `proposal_only` only with the exact non-empty
`next_check`; `engineering_handoff` must be attempted through the normal safe repair
lifecycle rather than parked as a proposal. Use
`create_human_input_request(source="strategic_review", input_id="strategy-proposal-...")`
for a strategic decision so the UI preserves who asked. An actionable alternative
must create an approve/reject/defer decision using that same source and prefix;
only a thesis explicitly waiting on named future
evidence may use `proposal_only`, with that boundary in `next_check`. Route any
technical prerequisite to the Engineering/Ops executor and never apply a materially different
approach before that exact approval. Engineering may reach
`awaiting_user` only for an exceptional repair that changes business meaning,
affects real users or money, or leaves a genuinely balanced choice not settled
by `soul.md`.

Reconcile answered advisor-specialization decisions separately from finding
dispositions. For a `report_human_inputs` row with source `pulse` and id prefix
`advisor-specialization-`: on `activate`, call
`update_workflow_config(advisor_specialization_approval_input_id="<id>")` so the
tool resolves and activates the exact approved pair; never copy or rewrite the
texts yourself. On `reject`, preserve the current specialization and consume the
decision with that outcome. On `revise`, call
`get_workflow_command_guidance(kind="specialize-advisors", focus="<the owner's revision note>")`,
create the replacement proposal it specifies, then consume the old decision only
after the replacement exists. These decisions are configuration work, not Pulse
findings, and must not be converted into duplicate issues.

`awaiting_user` remains in the decision queue and requires a still-pending
`create_human_input_request`, passed as `human_input_id`. Create the decision
first: a finding marked awaiting_user with no question leaves the operator told
that something needs them and given nothing to answer.
Attribute the question to the reviewer that actually asks it. New technical
questions use `source="technical_review"` with a stable
`technical-decision-...` id; legacy engineering/ops sources remain readable
history but must not be emitted by new reviews. `strategic_review` uses
`source="strategic_review"` with a `strategic-proposal-...` id. Pass that id as
`human_input_id` on the corresponding `record_pulse_finding` call so the
finding and pending decision are linked at creation; never leave
`recommended_route="decision_required"` as an unlinked label for a later turn.

Every new decision that authorizes a workflow change must include an
`apply_contract` in `create_human_input_request`. This is the only authority
pre-run uses; user-facing context is not a machine patch. Use
`mode="targeted_fixer"` for prompt, plan, route, validation, database, tool,
model, reporting, or any cross-artifact change, with a bounded
`approved_scope`, exact `pre_run_checks`, an honest `post_run_proof`, and
`failure_policy="continue_unchanged"` unless a failed application must block a
safety/public-action run. `direct_apply` is only for one already-defined
setting with a known exact check. Use `no_change` for reject/defer outcomes and
`external_wait` when an external prerequisite must arrive. Do not leave the
contract empty for a new repair decision; legacy prose-only requests are never
auto-applied by pre-run. The custom tool is an HTTP JSON contract: send
`apply_contract` as an object, `pre_run_checks` as an array of strings, and both
`approved_scope` and `post_run_proof` as single strings (not nested objects or
arrays). Fetch and follow the published tool schema instead of inventing nested
field shapes.
For `strategic_review`, the question source must be `strategic_review` and its
id must start `strategy-proposal-`. The backend rejects a strategic
`proposal_only` disposition with no `next_check`, so every accepted
recommendation has a concrete route to action or evidence.

Deduplicate findings before filing: match stable target/component, behavioral
claim, and evidence boundary against active and suppressed findings. Reuse the
existing issue for unchanged or newly evidenced instances, and record the new
evidence through the typed finding tool.
Create a new finding only for a genuinely different target/claim, or reopen one
whose recorded reopen condition is now satisfied. Then map conflicts by target. Resolve by explicit user
constraints, correctness/data integrity, preserved goal meaning, strategy
improvement, then cost. If evidence cannot decide, create one focused decision,
block affected modules, and do not mutate that target.

The Engineering/Ops executor first calls
`get_pulse_state(view="backlog", detail="compact")` without a module filter and
freezes the complete active starting manifest. It does not reload that index to
filter a handful of IDs; it requests targeted full detail for selected IDs. Due
reviewer modules decide which reviews ran; they do not hide retained work from
the inventory. It then builds one compact, priority-ordered Fix queue and
selects a bounded repair batch that it can carry through mutation and honest
proof in this pass. Each queue item is a coherent repair bundle. Start with the
highest-value bundle, then add an independent bundle only if it is low-risk,
requires no broad rediscovery, has its own clear proof boundary, and fits the
current context plus targeted evidence. Group findings only when they share the
same root cause, require compatible changes to the same target, and have one
verification condition. Cross-reviewer grouping is allowed; conflicts remain
separate. Different public-action risks, user decisions, route contexts, and
unresolved design investigations stay separate. Waiting-on-run, waiting-on-user,
proposal-only, and externally owned findings stay visible but do not enter the
actionable queue. Coherence, impact, and available proof choose the batch—not an
arbitrary top-N issue count. No finding may disappear: checkpoint the exact
unselected queue with defer reasons, but do not re-file or manufacture a
current-pass disposition for issues the executor did not investigate. Process
and disposition each selected bundle before the next; the backend opens the
durable fix-attempt record from the disposition you write, so there is nothing
to declare before mutating.
Before mutation capture targets, time, hashes/versions, and baseline. Load
`read_skill(skills=[{"name":"builder-reference","path":"references/pulse-fixer-practices.md"},{"name":"builder-reference","path":"references/fix-verification.md"}])`;
follow the engineering-practices reference to diagnose and bundle the root cause,
including its **Bounded backlog progress contract**, then the verification reference
to establish proof. Maintain the exact remaining visible issue-ID list
throughout the pass and reconcile it with a final compact
`get_pulse_state(view="backlog", detail="compact")` read only when mutation may
have changed lifecycle state;
old artifacts are not immediate proof. If stronger runtime proof needs a future
run, do not trigger it solely for verification: record `changed_unverified`.
That applied repair closes now, and later normal-run recurrence reopens it.

For learning-content repairs, follow the practices reference's **Learning and
skill purity repair** contract. Re-read the whole content-bearing skill package
and changed step configs after mutation. A smaller `SKILL.md`, valid Markdown,
or content moved into `references/` is not proof; semantic purity and correct
objective/access pairing are the postcondition.
Re-read `get_pulse_state(view="module")` and map each actionable finding to the
visible `issue_id` from the compact backlog index. Send that one ID back
as `issue_id`; the backend resolves its legacy storage row and current attempt.
IDs address records but never decide semantic sameness. If a finding lacks an issue
ID, block it as a reviewer-contract failure instead of making an untracked change.

Reconcile every selected issue ID to one disposition or one checked,
still-unmet waiting/external boundary. Confirm that every unselected starting ID
remains in the durable backlog or is linked to an independently recorded
lifecycle transition; do not touch it merely to satisfy accounting. A selected
issue missing from close-out blocks its module, but a truthful remaining queue
does not. Strategy or model-routing changes need exact valid approval. Preserve each
reviewer's conclusion under its owning module. Strategic Review may conclude
keep, improve, propose an alternative, run an approved bounded experiment, or
wait for named evidence. Operational observations
may be cross-referenced to the matching module during consolidation, but never
rewritten as a dependency or used to suppress that module's result.
Strategic operational findings remain out-of-scope observations.
Only the retained Technical Maintenance executor mutates workflow state. It
reviews, consolidates, repairs, and verifies in its one retained task; it must
not create a duplicate Fixer, recovery agent, or separately scoped repair
child. The parent continuation only records typed receipts
from completed children and never mutates workflow artifacts. Neither reviewer
nor executor writes a separate Pulse presentation artifact; the Pulse popup reads
the typed records directly.

Before finishing, call `record_pulse_impact` once when there is an intervention,
a matured assessment, or a trustworthy observation Gate did not already store.
Load the retained impact ledger first; do not duplicate Gate's current-run
observations. Create or advance one intervention per coherent verified repair
bundle or approved strategy experiment, and append an assessment only when its
comparable evidence window matured. Classify operational repairs as `reliability`, instrumentation
as `measurement`, and dashboard-only work as `presentation_maintenance`; none
may claim `direct_goal` impact. Use `inconclusive` or `confounded` rather than
inventing attribution when observations are missing or interventions overlap.

Record `record_pulse_result` once after each due module's child work has actually
finished. A running child with no checkpoint is not a failed reviewer. If a
premature failed/blocked result was already recorded, verify the child's terminal
outcome and persisted work before correcting it for the same `pulse_run_id`.
Supply `verification` plus `evidence` or `finding_dispositions`; `changed` still
requires all normal repair proof. The backend preserves the prior receipt and
updates current module state and audit together. For every finding pass a
structured `finding_dispositions` row with `issue_id`, disposition, summary,
changed files, and exact verification objects. Fingerprints and attempt IDs are
backend-only identifiers.
Verification verdict is exactly `passed`, `failed`, or `inconclusive`.
`fixed_verified` closes with passed immediate post-change proof;
`changed_unverified` also closes once the repair is applied. A later normal
workflow observation of the same semantic root cause reopens that issue ID.
Perform module finding-ID reconciliation and require a terminal current-run result before
claiming completion. Keep technical proof in SQLite review and lifecycle rows. Record a
reviewer failure as `Review incomplete`, without a conclusion or unsupported
change.
