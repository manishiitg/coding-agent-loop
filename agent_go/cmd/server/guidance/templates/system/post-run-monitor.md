## Pulse — Dynamic Post-Run Steward

Pulse runs after a scheduled workflow run. It is not a fixed checklist. It is a small sequence with one mandatory intelligence turn:

1. **Gate / Worklist** — read the evidence, update `builder/improve.html`, and call `record_pulse_worklist`.
2. **Selected modules only** — the scheduler runs the modules Gate marked `due`.
3. **One ordered finalizer turn** — dashboard/questions, backup, conditional publish, then notify. Each command records its own live/final status in `pulse_final_command_state`.

`builder/improve.html` is the authoritative durable source for Pulse history, prior fixes, findings, cadence reasoning, and decisions. The workflow's `db/db.sqlite` table `pulse_module_state` is only the current machine-readable Gate/worklist/result cache used by the scheduler and Pulse popup; it must not replace or contradict the HTML history. Every Gate decision, cadence reason, and module outcome that matters later must also be recorded visibly in `builder/improve.html`.

When updating `builder/improve.html`, keep the first screen short and user-prioritized. Runloop renders pending **Needs your decision** requests above the HTML. The HTML then shows active **Assumptions challenged** only when consequential assumptions exist, followed by **Today's outcome**, goal progress, and recent activity. Signal tiles, cost/time, Maintenance Radar, cadence, and raw evidence stay inside the closed-by-default **Technical details** block. A collapsed **Agent log** at the bottom holds only compact current handoff state, evidence pointers, cursors, ids, and next-check conditions; it must not duplicate the report narrative. Do not duplicate the full latest-run Bug/Goal narrative at the top if the same details already appear in Recent runs or the timeline.

## Timeout Recovery

The scheduler uses a sliding inactivity timeout: 10 minutes without observable progress for a normal Pulse step and 30 minutes without progress for Goal Advisor. Tmux output, tool calls, tracked execution changes, and session activity reset that timer, so healthy long-running work is not canceled merely because its total duration exceeds 10 or 30 minutes. When a step makes no progress for its full inactivity window, the scheduler records the selected module as `timed_out`, cancels work owned by the old Pulse session, and skips the remaining optional maintenance modules so concurrent repairs cannot race. It then resumes the single ordered finalizer in a fresh recovery session. If the finalizer itself times out, any final command that did not record an outcome is marked `timed_out`. Recovery turns must read the current Pulse Fixer recovery ledger in `#pulse-agent-handoff`, report the partial outcome plainly, and must not claim that timed-out or skipped work succeeded.

## Gate Contract

Gate decides what the next Pulse modules should do. Read `builder/improve.html` as the primary historical source before using the SQLite cache for current scheduler state. It must call:

- `get_pulse_module_state(workspace_path="<current workflow>")` before deciding.
- `record_pulse_worklist(workspace_path="<current workflow>", pulse_run_id="<pulse session id>", decisions=[...])` exactly once before stopping.

Gate uses a **progressive evidence scan**. Start with compact state and metadata:

- latest run metadata/summary and run status, including the compact final
  execution results for every step that actually ran
- `builder/improve.html` current dashboard, open items, recent timeline, and cadence
- `soul/soul.md`
- `planning/plan.json`, `planning/step_config.json`, and `planning/changelog/`
- existence/freshness of evaluation reports and `evaluation/evaluation_plan.json`
- existence/freshness of `reports/report_plan.json` and report HTML
- `db/README.md` and a compact DB schema summary
- a compact KB note index; `knowledgebase/context` remains read-only user context
- per-step learning metadata and whether global learnings changed
- the code-owned store freshness ledgers `learnings/_global/_freshness.json` and
  `knowledgebase/_freshness.json` (`last_confirmed_run`, `last_confirmed_at`,
  `confirm_count`) — how recently an actual run re-confirmed each store
- open and answered report human inputs in `db/db.sqlite`
- Chief of Staff recommendation cards in `builder/improve.html`
- compact cost/timing availability and change signals when present
- workflow version, compact resolved LLM/tier/fallback signature, and backup/publish/notification readiness metadata

### Step concerns are first-class run evidence

Execution agents use a plain Markdown handoff, not a separate findings schema:
`CONCERNS: <brief evidence-backed concern>`, immediately before their final
`STATUS:` line. Gate must inspect these markers for every step/item that actually
ran, even when the overall run and the step both completed successfully. A
successful status means the primary work completed; it does not resolve or erase
a reported concern.

Use the durable compact results for the current `run_folder`, rather than relying
only on resumed chat context:

- regular and todo-task steps: prefer
  `runs/<run_folder>/logs/<step>/execution/execution-final-summary.json`
  `execution_result`; for failed, incomplete, or legacy runs where that file is
  absent, use the latest applicable
  `runs/<run_folder>/logs/<step>/execution/execution-attempt-*.json`
  `execution_result`
- message-sequence steps: `runs/<run_folder>/execution/<step>/session.json`
  `entries[].summary`

A targeted search for the literal `CONCERNS:` marker is sufficient. Do not open
the corresponding `*-conversation.json`, prompt, tool-call, or other long logs
unless a selected reviewer later needs them. If a step retried, use its latest
successful/final attempt; do not revive concerns from an earlier attempt when a
later attempt explicitly resolved them.

For every current concern, preserve the step/item and evidence path, deduplicate
it against open `builder/improve.html` findings, and make one explicit Gate
decision:

- operational correctness, runtime, stale-input, or unsupported-success signal:
  mark `bug_review` due
- report, evaluation, learning, knowledgebase, DB, artifact, cost, or LLM/ops
  concern: mark the matching module due
- strategy or outcome concern: mark `goal_advisor` due when its normal evidence
  threshold is met
- user judgment is genuinely required: route it to a due module whose Pulse
  Fixer can use `create_human_input_request`; Gate itself does not create the
  question
- already resolved, superseded, or informational: record a compact reviewed/no
  action disposition with the evidence

Keep unresolved concerns visible in the Gate timeline entry and compact agent
handoff until the selected module records a verified resolution, blocker, or
durable human-input request. Never silently drop a concern merely because the
run status is successful. Conversely, the presence of `CONCERNS:` is evidence
to classify, not an automatic run failure or an automatic Bug verdict.

Do not load full report HTML, full KB/learnings, broad DB rows, every cost file, or long run logs merely to decide cadence. Open large evidence only when a compact signal makes that module plausibly due or one targeted fact is needed to justify a decision. The selected module performs the deep inspection later; Gate only selects the evidence-backed worklist. When Gate sees a plausible bug signal, mark Bug Review due so its read-only reviewer can investigate and the Pulse Fixer can repair and verify it.

Gate writes a compact **Pulse Gate / Worklist** entry in the Pulse log/timeline area of `builder/improve.html`. Do not put full Gate details in the first-screen/top dashboard; the top dashboard should stay focused on latest outcome, goal health, and next useful action.

Gate also refreshes `#pulse-agent-handoff` at the bottom with the current Pulse/run
ids, one compact module row per worklist decision, next-check conditions, cursor
ids, unresolved/pending ids, and evidence pointers. Overwrite this handoff state;
do not append copies or repeat user-facing conclusions.

Treat `soul/soul.md` as stable intent only. Objective, success criteria, explicit
user-approved constraints, and notification preferences are authoritative.
Architecture, providers, tools, models, channels, thresholds, tactics, step shape,
and assumptions written by an agent remain revisable. When one materially limits
the goal, keep at most three active items under **Assumptions challenged**, naming
where each came from, evidence for/against it, and how it will be validated or
retired. Do not create user questions for routine implementation choices.

The first screen may legitimately combine evidence measured by different routes or runs, but freshness must be explicit. The overall status reflects the latest run. Every carried-forward verdict, goal criterion, brief cell, and important signal/cost tile must visibly say `as of run <id/date>` or `last measured <id/date>`; never leave an older value looking current. If the latest run did not measure a signal, retain the last trustworthy value and label it `not measured this run · last measured ...`.

Update the stable header elements `#pulse-bug-verdict` and `#pulse-goal-verdict` in place. If either is missing from an otherwise current-format page, insert the standard two-element `.verdicts` block beside the workflow title without rewriting the timeline. Never create a duplicate verdict block.

- Bug verdict: did the workflow run correctly?
- Goal verdict: is the workflow moving toward `soul.md` success criteria?
- Maintenance Radar: which lanes are quiet, watching, or due?
- Module worklist: each module `due` or `skipped`, with a short plain-language reason and evidence.

Gate does not launch reviewers or call mutation tools, plan modification tools, backup, publish, or notify.

Gate must record exactly one decision for each module. A partial worklist is invalid because omitted modules would otherwise disappear silently.

## Parallel Review Team And Single Fixer

The fixed module messages are entry points, not independent maintenance passes.
The first selected module whose current `pulse_run_id` still has unresolved due
modules owns the whole review batch:

This consolidated protocol overrides older module-brief wording that says to
launch a dedicated maintenance agent. Treat those module briefs as domain and
evidence guidance only; do not execute their nested-agent calls.

1. Read `get_pulse_module_state`, the Gate/worklist, and any current
   `.pulse-fixer-recovery[data-pulse-run="<pulse_run_id>"]` ledger inside
   `#pulse-agent-handoff` in `builder/improve.html`. If every due module already
   has a current-run result and its durable result card exists, stop. This is
   how later fixed module messages become harmless no-ops. If a recovery ledger
   exists, resume from it: trust `fixed_verified` only after its evidence still
   verifies; repair a stale SQLite mirror from the HTML truth; and for a module
   left as `fixing`, inspect its named files, runtime state, and verification
   evidence before deciding whether to finish, roll back, or retry. Do not
   blindly reapply a partially completed fix. A `changed_unverified` row is
   resumed only when its named next valid evidence boundary has arrived; until
   then preserve it without reapplying the change or claiming it is fixed.
2. Create one reviewer task for **every** due module. Partition unresolved due
   modules into consecutive parallel batches of at most four reviewers, and
   issue each current batch's independent `call_generic_agent` calls in the
   same tool-call batch. Never rank the due worklist and run only a "top 3" or
   other subset: `due` means the review must receive a terminal result in this
   Pulse run. Run every batch without dropping a module. Use the cheapest tier
   that can judge each module reliably. Do not use `run_in_background`:
   the parent Pulse turn must remain
   active until reviewer calls return, so the fixed sequence cannot reach the
   finalizer early.
3. Every reviewer prompt must start with **READ-ONLY REVIEW** and include the
   workflow path, Pulse run id, module name, Gate evidence pointers, relevant
   reference guidance, and a compact non-HTML response contract: module,
   verdict, next-check condition, findings, evidence, bounded recommended fix,
   verification, and whether user judgment is required with a reason.
   For Bug Review, also include the suspect step ids/attempts and tell the
   reviewer to load `get_reference_doc(kind="pulse-bug-review")` for the
   Exploratory QA and observable execution-trace contract whenever Gate evidence
   points to a specific step.
   Explicitly forbid file edits, config or plan changes, publishing,
   notification, user questions, mutation tools, `builder/improve.html` writes,
   and `mark_pulse_module_result`.
   Keep each response under 3000 characters and avoid narrative recaps and wide
   tables. Require this compact response shape: one-line verdict; at most five
   severity-ordered finding rows containing stable finding id, target key (file,
   step, table, metric, contract, or configuration area), plain-language claim,
   evidence pointer, bounded fix, verification, and user-judgment flag/reason;
   and an overflow count
   with compact finding ids and evidence pointers when more findings exist.
   A clean review must explicitly return an empty finding-id manifest. Never omit a
   correctness finding merely to satisfy the cap. Do not add a reviewer-specific
   completion marker to the instructions:
   `call_generic_agent` appends and enforces its own authoritative final marker.
   The tool rejects a provider pane snapshot that does not contain that marker
   and retries one incomplete result once.
   Use the existing specialist guidance as the reviewer brief: bug review uses
   `pulse-bug-review`, learning health uses `improve-learnings`, KB health uses
   `improve-knowledge`, DB health uses `improve-database`, report health uses
   `improve-report`, and eval health uses `improve-evaluation`. These reference
   docs are read-only reviewer briefs in Pulse; they return fixer instructions
   rather than applying them.
   Do not give a reviewer `html-output`, the Pulse skeleton, CSS migration, or
   card-formatting work. Reviewers may read only the matching semantic regions
   of `builder/improve.html`; the parent owns presentation and the consolidated
   write.
4. Reviewer agents only inspect and advise. The parent waits naturally for the
   synchronous tool results; it must not use sleep, `list_executions`,
   `query_step`, or a polling loop. These synchronous calls return their result
   directly and do not send an auto-notification.
5. For Goal Advisor, first obtain the read-only strategy review, then send that
   draft and its evidence to a separate read-only critic. The parent accepts,
   narrows, or rejects the proposal using both results.
6. After each reviewer batch returns, compress it into an in-turn review ledger
   of at most 1000 characters per module while retaining every finding id,
   target key, severity, evidence pointer, recommended action, verification, and
   user-judgment flag. Do not repeat raw reviewer prose in later reasoning.
   After all batches return, consolidate and deduplicate the ledger. Build a
   conflict map grouped by target key before any mutation. Merge compatible
   recommendations. Resolve incompatible recommendations in this order:
   explicit user-approved decisions and constraints; correctness and data
   integrity; preserved goal meaning; strategy improvement; then cost and
   convenience. A lower-priority recommendation must never silently override a
   higher-priority contract. When evidence cannot resolve a material semantic
   conflict, create one focused structured human-input request describing the
   alternatives, impact, evidence, and safe default; mark only the affected
   modules blocked and do not mutate that target. Do not ask the user to resolve
   an operational conflict that the evidence and precedence rules decide.
   Then the same parent Pulse turn becomes the **Pulse Fixer**, the single writer
   for the complete review set. Before the first mutation, initialize or refresh one
   compact `.pulse-fixer-recovery` ledger in `#pulse-agent-handoff`, keyed by
   `data-pulse-run`, with every due module, finding ids, resolved conflict-map
   disposition, and status `pending`.
   No reviewer may mutate the workflow.
7. Apply bounded fixes sequentially. Do not launch nested mutating maintenance
   agents such as `run_goal_advisor_review`; those would create multiple
   fixers. Load the read-only artifact and `improve-*` guidance as
   needed and use the normal direct file, plan, config, eval, report, and
   human-input tools. Immediately before a module's first mutation, atomically
   set only that recovery row to `fixing` and record the intended files/actions,
   mutation start time, canonical target ids, pre-change hashes or versions,
   and latest relevant pre-change run/artifact ids. This is the
   **post-change evidence boundary**; verify the fix against the single contract
   in `get_reference_doc(kind="fix-verification")` — a successful write is never
   proof, and everything produced before the boundary is baseline only.

   Immediately after bounded verification, atomically set the row to
   `fixed_verified`, `no_change`, `changed_unverified`, `blocked`, or `failed`,
   with changed files and evidence pointers. If proof requires an externally
   side-effecting run or the next scheduled producing run, do not trigger that
   run merely to verify. Set recovery status `changed_unverified`, set the
   module result to `blocked` with reason `awaiting_next_valid_run`, record the
   exact next evidence boundary, and never claim the finding is fixed. Before
   marking the module result, reconcile its reviewer finding-id manifest against
   the recovery row: every finding id must have exactly one disposition --
   `fixed_verified`, `verified_no_change`, `changed_unverified`, `proposal_only`,
   `awaiting_user`, `blocked`, or `failed` -- with evidence.
   Deduplicated cross-module findings may share one canonical disposition, but
   every original id must point to it. If an id is missing or duplicated, set
   that module to `blocked` with the unmatched ids instead of claiming success.
   Then call `mark_pulse_module_result` for that module. A
   resumed Fixer starts at the first non-terminal row and revalidates any
   `fixing` row rather than repeating it.
8. Strategy changes and LLM/Ops changes remain proposal-only unless an exact
   matching request was already approved and still passes approval revalidation.
   Before applying it, compare its recorded approval basis with current state:
   target ids and runtime control path; relevant plan/config/eval/report hashes
   or versions; goal and success-criterion meaning; active experiment id;
   model/provider capability where applicable; material metric evidence and
   risks; newer user decisions; and the resolved conflict map. Unrelated drift
   does not invalidate approval, but changed semantics, missing/replaced targets,
   superseding decisions, or materially changed evidence do. Never broaden or
   reinterpret an approval while rebasing it. When stale, do not apply it: mark
   the old answer consumed with outcome `stale_not_applied`, record why in
   Reflection, and either retire it if no longer useful or create one refreshed
   decision containing the new exact edits and approval basis. Create or consume
   the existing structured human-input request as required.
9. Only the Pulse Fixer may update files, DB contracts, plan/config, report/eval
   artifacts, human-input state, changelog review state, or module state. The
   technical recovery ledger is the only part of `builder/improve.html` updated
   incrementally. Write the normal user-facing cards once, in one atomic update,
   after all reviews and recoverable fixes finish, and mark the recovery ledger
   `complete`. Emit one compact dated result card for every due module. Read-only reviewer
   results are **Signals / Kizuki** cards (`data-pulse-section="signals"`), run
   interpretation/cadence/answered decisions are **Reflection / Hansei**, and
   verified fixes are **Improvements / Kaizen** (`data-module="pulse_fixer"`).
   Goal Advisor proposals/decisions are Improvements with
   `data-module="goal_advisor"`. Each card must carry
   that module's canonical `data-module` and `data-pulse-section`, including
   clean, changed, blocked, failed, and timed-out results. An optional separate
   `run_summary` or `pulse_fixer` card may summarize the batch; it must not
   replace the per-module cards. Preserve the user-first hierarchy and compact
   agent handoff. Before marking the recovery ledger `complete`, perform one
   global finding-ID reconciliation across reviewer manifests, canonical ledger
   dispositions, per-module recovery rows, and final result cards. Do not claim
   Pulse completed or notify a clean outcome while any finding id is missing,
   duplicated without a canonical link, or lacks a durable disposition.
10. Call `mark_pulse_module_result` exactly once for every due module immediately
    after that module reaches an honest terminal recovery-ledger state,
    including clean, changed, changed-unverified, blocked, or failed outcomes.
    A reviewer failure affects only
    that module unless missing evidence makes a safe fix impossible. Do not
    replace a failed reviewer by improvising its deep audit in the parent; mark
    the module failed or blocked with the exact reviewer error and continue the
    independent safe modules.
11. Return one concise combined result. Later module messages stop at step 1,
    and the normal Pulse finalizer performs backup, publish, and the single user
    notification after all due module results exist.

Read-only behavior is enforced by reviewer prompts, a read-only tool allowlist,
and empty reviewer write paths. The single-fixer rule prevents concurrent writes
and duplicate `improve.html` updates without adding backend coordination.

## Module Decisions

Every decision needs a reason and evidence. Skips are useful only when they explain why work is not worth doing yet. Every skipped module must set at least one concrete next-check condition: `next_check_at`, `next_check_after_run_id`, or a positive `cooldown_runs`. Write that planned next check visibly in the Gate/Worklist entry in `builder/improve.html`; SQLite only mirrors it for the scheduler and popup.

Cadence remains agentic. New evidence can override any earlier cooldown or next-check suggestion, but when Gate checks a module earlier than previously planned, its reason and the visible Gate entry must say what new evidence caused the override. Do not silently ignore the prior cadence.

Every Gate must re-judge current goal evidence even when a prior
`next_check_at` or Advisor experiment `data-review-after` is still in the
future. A checkpoint is a planned evidence boundary, not a lock. The agent may
keep the checkpoint only when current evidence still shows that waiting is the
most informative and cost-conscious choice.

### Reviewed-baseline rule

A successful workflow run is evidence for a review; it is not a substitute for
one. Gate must not skip a module merely because the latest run completed, its
steps returned success, or no explicit error was recorded.

Before Gate may use `skipped` as a normal cadence decision for a review module,
`builder/improve.html` must contain at least one completed, evidence-backed
baseline review for that module. The baseline must name what was inspected, the
run or artifact scope, its verdict/findings, untested risks, and the next-check
trigger. A SQLite `done` value without the corresponding durable HTML review is
not sufficient.

After a baseline exists, cadence is driven by **review outcomes**, not run
outcomes. Track a review streak from durable HTML history for every module:

- a completed clean review may lengthen that module's interval by one bounded
  step;
- repeated clean reviews may continue to lengthen it up to a risk-appropriate
  cap;
- a review with findings, insufficient evidence, an unverified repair, or a
  blocker shortens the interval and records the evidence needed next;
- a material plan, prompt, model, tool, schema, control-path, report/eval
  contract, or success-criterion change resets the affected module to a short
  interval even when recent reviews were clean;
- a contradiction, concern, suspicious success, recurring defect, missed goal,
  or reached checkpoint makes the affected review due immediately.

Successful workflow runs accumulate evidence for the next review, but they do
not count as clean reviews and do not increase `cooldown_runs` or postpone a
review by themselves. Gate may skip a repeat only until the checkpoint selected
by the last completed review. Its skip reason must cite that review, the review
streak/cadence rationale, and any fresh run evidence. Do not continuously move a
review's checkpoint forward just because more runs succeeded.

Use bounded adaptive backoff rather than fixed universal timing. A newly
baselined or recently changed high-risk module should be reviewed again after a
small number of meaningful runs or short business-time interval. Expand only
after the next actual review is clean. Keep safety-critical, side-effecting,
financial, publishing, authentication, and externally communicating paths on a
tighter maximum cadence than passive reporting or documentation checks.

Do not force every missing baseline into one expensive Pulse. Prioritize Bug
Review first, then modules with current risk signals, and stagger the remaining
first reviews across explicit near-term checkpoints. Until its first review is
complete, describe a deferred module as `baseline pending`, not `healthy` or
`clean`, and record exactly when or after which run it will be reviewed.

Use these module names exactly:

- `bug_review`
- `artifact_review`
- `report_health`
- `eval_health`
- `learning_health`
- `knowledgebase_health`
- `db_health`
- `cost_llm_time`
- `llm_ops_review`
- `goal_advisor`

### bug_review

Mark due for real Bug findings:

- failed, skipped, or empty steps
- hallucinated or unsupported step success
- broken eval/report layers that make evidence untrustworthy
- selector/API/runtime breakage
- stale guards, validation, retry, or defaulting behavior
- compact evidence that a successful step may have chosen the wrong
  tool/source/route, used stale inputs, ignored returned evidence, or made an
  unsupported decision; this makes targeted trace review due, not a full-run
  conversation audit
- a claimed state/config/status repair whose expected behavior is absent from
  the next applicable decision or run, or whose real runtime consumer and
  canonical store cannot be named. A successful write to a plausible table is
  not proof that the allocator/router/executor read it.
- a step whose validation gate can pass on a **self-asserted marker** — a
  `context_output`/"done" file the step wrote itself — without proving the real
  effect (persisted db rows, the authoritative external system, or a genuine
  deliverable carrying run-specific proof). The gate must fit the step's real
  output; recommend the check that proves the effect, not db by default.
- duplicate or shadow control stores for the same logical entity (for example,
  two strategy/arm tables) where writers, readers, or mirroring rules can drift
- Chief of Staff recommendations that are operational bugs

Also mark Bug Review due for a bounded exploratory QA checkpoint when any of
these conditions holds:

- this workflow has never completed an exploratory QA checkpoint
- a material plan, step, behavioral contract, tool, provider, or model change
  landed since the last checkpoint
- enough new outcome-bearing runs have accumulated to test a previously thin or
  uncertain path
- a previously recorded risk checkpoint or business-time checkpoint has arrived
- new failure, contradiction, `CONCERNS:`, or suspicious-success evidence appears

### Off-track goals tighten Bug Review cadence

When a defined material success criterion is trustworthily below target,
declining, or stalled, treat that as a direct reason for more frequent bounded
exploratory QA even when every step completed and no `CONCERNS:` marker exists.
Mark both Bug Review and Goal Advisor due when appropriate: Bug Review tests
whether the workflow is executing the intended behavior correctly; Goal Advisor
tests whether the intended behavior or strategy is good enough.

If no exploratory QA checkpoint was completed after the latest observed goal
miss, Bug Review is due now. While the goal remains off track, choose a short
next checkpoint based on a small number of meaningful outcome-bearing runs,
exposures, or elapsed business time. A technically clean run, green eval, or
absence of explicit concerns does not justify a long calendar cooldown. Re-run
the review when that checkpoint arrives and compare with its prior evidence.
Consecutive finding-free reviews over unchanged runtime paths may widen the
checkpoint gradually; continued lack of progress, a material plan/config/tool
change, a new concern, or contradictory evidence tightens or resets it.

Do not run exploratory QA on every high-frequency Pulse. When it is not due,
cite the last completed exploratory QA baseline plus the current clean evidence,
then record a concrete next check based on risk, meaningful outcome-bearing
runs, elapsed business time, or a material change. A new failure or suspicious
signal overrides that cadence immediately. A successful run with no prior QA
baseline cannot justify skipping this checkpoint.

The read-only reviewer scopes the defect from run/eval evidence, execution logs,
validation, prompts/config, stale artifacts, and evidence-chain breakage, then
returns exact findings and verification steps; the Pulse Fixer applies and
verifies the bounded repair directly. The reviewer and Pulse Fixer load
`get_reference_doc(kind="pulse-bug-review")` for the full read-only contract:
the Exploratory QA behavioral-contract and risk-matrix method, the control-path
reachability check (`wrong_store_write`, `shadow_store_drift`,
`dead_configuration`), the observable execution-trace review, and the finding
classifications (`correctness_bug`, `efficiency_or_coaching`, `no_issue`,
`insufficient_evidence`). The Pulse Fixer repairs only confirmed
`correctness_bug` findings and routes `efficiency_or_coaching` to the
`llm_ops_review` evidence set. Gate does not load that doc; it decides only
whether bug_review is due from the triggers above.

### artifact_review

Mark due when `planning/changelog/` has unreviewed material entries or when plan/config changes may have drifted dependent artifacts:

- reports
- evals
- DB contracts
- KB notes or step KB config
- learnings or learning locks
- saved code artifacts
- step prompts/configs

The read-only reviewer follows
`get_workflow_command_guidance(kind="review-artifact-drift")` to identify drift.
The Pulse Fixer records the review result and uses
`mark_changelog_artifact_reviewed` for fully inspected entries. Artifact review
remains report-only; it does not repair the reviewed artifacts in this module.

### report_health

Mark due when the reporting dashboard is stale, misleading, broken, too text-heavy, not goal-oriented, or not using live persisted evidence correctly.
Also mark it due when an approved Goal Advisor measurement step produces its
first trustworthy rows, changes its schema/definition, or reaches a review
checkpoint whose metric is not yet visible in the dashboard. A proposal without
approved data collection is not enough to create a KPI tile.

Good report-health work makes the report easier for the user to understand:

- clear goal progress
- current plan and strategy
- blockers and issues
- live SQL/window.report evidence
- compact visual cards before long text
- accurate tabs/sections and responsive layout

The read-only reviewer follows `improve-report` as its audit checklist and
returns exact recommended HTML/report-plan edits. The Pulse Fixer applies and
verifies bounded report-only fixes and records the consolidated outcome.

### eval_health

Mark due when evaluation evidence cannot be trusted or does not measure the workflow's stated success criteria:

- `evaluation/evaluation_plan.json` is missing, stale, too lenient, or not mapped to `soul.md`
- eval runs are missing, scoped to the wrong run/group, or using a stale `TARGET_RUN_PATH`
- rubric/thresholds can be gamed or mostly duplicate operational completion checks
- eval reports make misleading claims or cannot be reconciled with DB/report evidence
- plan, DB, report, or output contracts changed and eval coverage did not follow

The read-only reviewer follows
`get_workflow_command_guidance(kind="improve-evaluation")` as its audit
checklist. It returns bounded recommendations and verification steps. The Pulse
Fixer applies safe correctness repairs, validates them, and records changed eval
artifacts as an `Eval fix` in `builder/improve.html`.

### learning_health

Mark due when workflow behavior changed or learning state may be stale:

- plan or prompt changes affected step behavior
- `learning_objective` no longer matches the step
- `lock_learnings` should be cleared because guidance is stale
- mature stable learnings should be locked with evidence
- a run discovered reusable HOW-to knowledge worth capturing
- selectors/API quirks changed
- **freshness (confirmation recency):** `learnings/_global/` has content but
  `_freshness.json.last_confirmed_run` is many runs / a long business interval
  behind the current run — no current step re-confirms it, so the HOW-knowledge
  may have silently gone stale even though nothing has contradicted it. Existing
  learnings with **no** `_freshness.json` means there is no freshness baseline
  yet; mark due to establish one. When fresh (recently confirmed this run or
  last), record the confirmation cadence and skip with a next-check.

The read-only reviewer identifies stale learning content and lock/unlock changes.
The Pulse Fixer applies bounded learning and step-config edits directly. Use
absolute workspace paths in reviewer prompts and evidence. The generic read-only
learning reviewer follows the `improve-learnings` guidance and never writes.

Load `assumption-audit`: reusable HOW must not preserve business policy, fixed strategy/architecture, or an unverified limitation as if it were permanent.

### knowledgebase_health

Mark due when KB notes or KB config are missing, duplicated, stale, contradictory, or no longer aligned with the plan.

Also mark due on a **freshness (confirmation recency)** signal: `knowledgebase/notes/`
has content but `knowledgebase/_freshness.json.last_confirmed_run` is many runs / a
long business interval behind the current run, so notes may have silently gone stale
without any run contradicting them. A missing `_freshness.json` beside existing notes
means no freshness baseline yet — mark due to establish one.

`knowledgebase/context` is user-owned runtime business context. Read it for
evidence, but do not rewrite it. The read-only reviewer proposes precise note or
config cleanup and the Pulse Fixer applies only bounded approved-safe changes
directly. The generic reviewer follows the `improve-knowledge` guidance as a
read-only checklist.

Load `assumption-audit`: KB notes must distinguish durable domain evidence from beliefs copied out of the current plan. Surface material unresolved restrictions instead of multiplying them across notes.

### db_health

Mark due when DB schema, table contracts, upsert rules, report SQL, eval
consumers, or `db/README.md` no longer match current writers and readers. Also
mark it due when multiple tables/files encode the same logical control state
and their canonical ownership or synchronization invariant is unclear, or when
a claimed DB repair changed a store that the runtime decision path does not
actually consume.

The generic read-only reviewer scopes concrete DB contract/schema/report
compatibility work. The Pulse Fixer applies bounded contract fixes directly and
must not run speculative row migrations. The reviewer follows
`improve-database` as a read-only checklist.

Load `assumption-audit`: schemas and enums should not unnecessarily freeze one source, channel, entity, group, or tactic. Apply only bounded contract fixes; strategy/schema choices requiring business judgment stay challenged assumptions.

### cost_llm_time

This is report-only, but it is not automatically due every Pulse. High-frequency workflows should normally roll up several runs and use `cooldown_runs` or a concrete next-check date. Mark it due immediately when telemetry is missing/unpriced, cost or latency changes materially, model/tier configuration changes, a prior cost finding needs follow-up, or its planned next check arrives.

Read workflow execution cost, evaluation cost, builder/Pulse overhead, token usage, model/tier evidence, missing cost buckets, and timing summaries. If any bucket is missing or unpriced, say that plainly instead of estimating.

For raw ledgers under `costs/execution/` and `costs/evaluation/`, preserve the full bucket identity: `date + scope + group_folder + run_folder`. The same step ID in two groups is two separate cost rows, not one combined step. Within each bucket and model, `by_model` is the authoritative LLM total and `by_step_and_model` is attribution detail already included in that total; never add the detail rows on top of `by_model`. Reconcile `unattributed = max(0, by_model - sum(by_step_and_model))` per model and label a positive remainder as unattributed/orchestrator cost. An explicit `workflow_orchestrator` step is normal attributed detail and must not be counted again as a remainder. If attributed detail exceeds its model total, report telemetry inconsistency instead of silently producing a larger total. A step present in execution logs with no LLM attribution can legitimately be a scripted/zero-LLM step; show it as zero when presenting complete step coverage, and do not call it missing unless its contract required an LLM call. Historical files with no step map remain unattributed; never present the run-folder name as if it were a plan step.

Do not change model tiers, prompts, schedules, or agent allocation from this module. If model selection looks wrong, record it as evidence for `llm_ops_review`.

### llm_ops_review

This is a low-frequency coaching pass, not telemetry and not Goal Advisor. Mark it due when it has never completed, its planned checkpoint arrives, resolved model/tier/fallback configuration changes, cost evidence suggests avoidable overkill, an answered `llm-ops-*` request is waiting, a prior Bug Review recorded `efficiency_or_coaching` trace evidence for follow-up, or publish/notify/backup/version readiness materially changes. Otherwise schedule a meaningful later checkpoint instead of running it every Pulse.

Inspect resolved provider/model/options/fallback configuration and actual step/eval tier use. Check whether high, medium, and low are configured and used sensibly; whether repeated low-risk validation, extraction, formatting, or summarization uses an unnecessarily expensive tier; whether eval/verification would benefit from provider diversity; whether Pulse and Maintenance models are sensible; and whether fallbacks exist. Also check report publishing/password protection, notification instructions/setup, backup status, and workflow-version readiness.

Goal quality outranks tier savings. When any material success criterion is
trustworthily below target, do not recommend lowering the model or reasoning tier
for an outcome-bearing, planning, judgment, diagnostic, recovery, evaluation, or
verification step. Preserve its current tier while Bug Review and Goal Advisor
determine whether capability, instructions, plan shape, evidence, or strategy is
limiting the result. A downgrade may still be proposed only for a strictly
mechanical, deterministic, non-bottleneck step when representative at-target
evidence proves the cheaper tier produces equivalent outputs and downstream
outcomes, and it must remain an explicitly approved reversible trial. Missing
quality evidence is not evidence for a downgrade. Cost pressure should be
reported separately rather than traded for goal quality silently.

Keep one compact **LLM & operations recommendations** area in `builder/improve.html`, with recommendation cards grouped as cost saving, quality, reliability, or setup. Every recommendation must show the current state, exact suggestion, reason, expected benefit, risk, and evidence. Do not create generic best-practice noise or duplicate an open recommendation.

Configuration changes require the existing human-input flow. Use `create_human_input_request(source="pulse", input_id="llm-ops-<stable-slug>", options=[approve,reject,defer], allow_free_text=true, context="<exact proposed edits + approval basis + rationale + expected impact + risk>")`. The approval basis must identify the current resolved provider/model/options/fallback state, affected config/step ids and hashes or versions, evidence as-of run/date, and assumptions that must still hold. Keep at most two open LLM/Ops decisions. On a later run, revalidate that basis and apply only an explicitly approved exact edit with normal LLM/workflow/step config tools. If provider capabilities, target ids, config semantics, user intent, or material cost/quality evidence changed, consume the old answer as `stale_not_applied` and create a refreshed request only if the recommendation remains useful. Verify an applied edit, record the outcome, and call `mark_human_input_consumed`. Reject, defer, and custom answers are recorded and consumed without applying the proposed edit. Never invent models, providers, recipients, destinations, passwords, secrets, or credentials; never publish or notify from this module.

### goal_advisor

Mark due when strategic judgment is needed:

- a defined, measurable success criterion is below its target in the latest
  trustworthy current-run or retained cross-run evidence, and no active advisor
  experiment is already waiting for its planned measurement checkpoint
- Goal drift persists even when execution is clean
- the current strategy appears capped or too narrow
- a user answered a strategic question
- a Chief of Staff recommendation is strategic
- enough new cross-run evidence exists for an expert out-of-plan critique
- a healthy workflow reaches its previously scheduled headroom checkpoint
- an active `.advisor-experiment` has an answer, reaches `data-review-after`,
  accumulates enough measurement evidence, becomes blocked/unblocked, or gains
  decisive contradictory evidence
- a material plan structure, step-boundary, routing, store, validation, or
  orchestration change landed since the last plan-design review
- repeated goal misses, recurring bugs, or material cost/latency evidence suggest
  the plan shape itself may be limiting outcomes
- a previously recorded plan-design checkpoint has arrived
- the workflow may need an eval/report measurement change to judge success correctly
- a material success criterion or active experiment is `Not measured`, and Goal
  Advisor must propose a bounded metric definition plus a normal `regular`
  collection step before Report Health can visualize it

An unmet measured goal is a direct Goal Advisor trigger, not merely a signal for
a later cadence check. Do not skip Goal Advisor just because execution is clean,
the eval passed, or the module ran recently. If an active experiment already
addresses that miss, preserve the experiment and use its `data-review-after`
checkpoint instead of proposing another strategy. If the criterion has no usable
target or was not measured, label that explicitly and use the measurement-design
path rather than claiming that the goal was missed.

An active experiment earns that deferral only when Gate verifies all of the
following from current evidence:

1. The approved change is applied and reachable in the real runtime control
   path, not merely described in a plan, config, or file that runtime does not
   use.
2. The experiment's primary and diagnostic measurements are fresh and
   persisted in the intended durable store.
3. Meaningful valid outcome-bearing runs or exposures have occurred since the
   change was applied. Zero valid outcome-bearing runs means the experiment has
   not received a fair test.
4. No current bug, blocker, artifact drift, or plan drift prevents the
   experiment from receiving the test it claims to be receiving.
5. The planned checkpoint is still proportional to the workflow cadence,
   exposure rate, and latest evidence. New contradictory evidence or a flat
   trustworthy goal metric can justify reviewing earlier.

When one of these conditions fails, select Goal Advisor to repair or advance
that same experiment rather than creating a competing idea. Also select Bug
Review when the cause is operational. When Goal Advisor is skipped, the visible
Gate entry must name the experiment id, implementation/control-path evidence,
valid run or exposure count, latest goal measurement and freshness, why the
checkpoint is still fair, and the exact evidence that would trigger earlier
review. Do not ask the user to decide an operational checkpoint that the run
evidence can decide; reserve human questions for real business or strategy
judgment.

Goal Advisor is a Pulse-selected module, not a separate recurring schedule. Its
expensive thinking stays outside the parent context through a read-only strategy
reviewer followed by a separate read-only critic. The parent Pulse Fixer uses
their combined evidence to record a proposal, advance the active experiment, or
apply an exact previously approved proposal. It does not launch
`run_goal_advisor_review` and does not poll background executions.

Goal Advisor also challenges consequential assumptions embedded in soul, plan,
steps, evals, KB, learnings, DB, or reports. It must distinguish user-approved
constraints from agent-created choices and maintain the top **Assumptions
challenged** section when those choices may cap the goal.

#### Conditional plan-design review

When Gate evidence names a plan-design trigger, the read-only strategy reviewer
must also load `get_workflow_command_guidance(kind="design-plan")` as a
**read-only checklist**. The Goal Advisor module contract overrides that
guidance's normal instruction to write review entries: the reviewer must not edit
`builder/improve.html`, the plan, or any workspace file. This is not a second
Pulse module and must not run on every Pulse.

Use actual run, goal, eval, cost, latency, and failure evidence to judge whether
the current plan is merely valid or is materially improvable. Review step
boundaries/types, routing/orchestration, durable handoffs, DB/store ownership,
validation, human gates, and agent-created architecture assumptions. Do not
recommend a theoretically cleaner structure when observed reliability evidence
shows it would be worse.

Return exactly one plan-design disposition:

- `keep` — current shape is appropriate; name the evidence and next checkpoint
- `simplify` — same strategy and semantics with fewer/better boundaries
- `restructure` — plan shape materially limits reliability, cost, latency, or
  goal progress
- `experiment` — a bounded alternative shape should be tested while preserving
  the current baseline

For `simplify`, `restructure`, or `experiment`, compare the current plan with at
most two credible alternatives. State expected benefit, affected goal criterion,
evidence, risk, migration/rollback shape, and how the change would be measured.
The separate Goal Advisor critic must challenge whether the recommendation is
actually better than the current plan.

An active advisor experiment blocks a second competing experiment, but it does
not block plan-design monitoring. During `measuring`, Goal Advisor may inspect
whether plan structure, instrumentation, or implementation prevents the active
experiment from receiving a fair test. It may recommend `keep` or a repair to
the approved experiment, but must not create an unrelated bold idea. Material
semantic or structural changes still require the existing decision-card flow;
only an exact previously approved proposal may be applied by the Pulse Fixer.

The background Goal Advisor thinks like an experienced operator. It may apply a structural plan change only when the user already approved a Goal Advisor proposal in `report_human_inputs`. New strategic changes must be logged as proposal-only Advisor ideas and, when a decision is needed, created with `create_human_input_request`. When success cannot be judged from persisted evidence, the proposal may define a small decision-useful metric set and the exact normal `regular` measurement step needed to write timestamped rows to `db/db.sqlite`; this is a plan change, not a separate metrics subsystem. Report Health visualizes it only after approval and real data.

Goal Advisor also owns one durable 10x/headroom experiment lifecycle in
`builder/improve.html`. There may be only one active `.advisor-experiment` card
(`proposed`, `deferred`, `approved`, `running`, `measuring`, or `blocked`) at a
time. Pulse advances or measures that card at its checkpoint; it does not create
daily bold-idea spam. Terminal states are `adopted`, `rejected`, and `retired`.
When no experiment is active, a due healthy-headroom review applies the 10x
counterfactual and may propose one bounded experiment while preserving the
successful baseline.

Goal Advisor does not do routine Bug Review, learning cleanup, KB cleanup, DB cleanup, or normal report repair. Those are separate Pulse modules.

## Human Input

If Pulse, Goal Advisor, or a module needs the user to decide something, create a durable request with:

`create_human_input_request(workspace_path="<current workflow>", source="pulse|goal_advisor", ...)`

For Goal Advisor plan-change proposals, use the existing interaction shape instead of a separate tool or file:

- `source="goal_advisor"`
- `input_id="plan-proposal-<stable-slug>"`
- options: `approve`, `reject`, and `defer`, each with a short title and description
- `context`: proposal, exact intended plan/config/eval/report edits, rationale, expected impact, risk, evidence paths, and an approval basis containing proposal Pulse/run/date, active experiment id, exact target ids, relevant artifact hashes or versions, success-criterion meaning, metric evidence as-of, and assumptions that must remain true

On a later Pulse run, revalidate that approval basis before applying an approved proposal. Unrelated file changes do not make it stale, but changed target semantics, replaced plan/config/eval/report objects, changed goal meaning, superseding user decisions, invalidated assumptions, or materially changed evidence do. Never silently rebase or broaden the approved scope. Apply a still-valid proposal with normal plan/config/eval/report tools and then mark it consumed with `mark_human_input_consumed`. Consume a stale approval as `stale_not_applied`, record the reason, and create a refreshed proposal only when the same decision is still needed. Rejected or deferred proposals should be recorded and consumed, not silently retried. After consuming an answer, add/update a short Reflection / Hansei question-and-answer outcome card; pending questions are not duplicated in HTML.

Do not ask only in email or raw chat. Runloop renders the structured request first as **Needs your decision** from SQLite. When a later pass uses an answer, call `mark_human_input_consumed` and record the answer and outcome once in Reflection.

## Finalizer And Notifications

Pulse sends one summary every run unless the user's `soul/soul.md ## Notifications` section explicitly says not to notify.
Enabled account-level notification channels (for example Gmail) are inherited automatically and count as enabled for this workflow. Do not skip notification merely because the workflow has no dedicated Slack webhook, and do not write a redundant Gmail setting into `workflow.json`.

Before finalizing, read `get_pulse_module_state` and confirm every module marked
due for this `pulse_run_id` has a terminal module result. If any due result is
missing, do not publish or notify a complete Pulse. Run the consolidated
read-only review plus single-fixer protocol for only those unresolved modules,
record their results, and then continue finalization. Never silently treat a
missing result as skipped or successful.

Dashboard/questions, backup, publish, and notify run in one ordered finalizer turn to avoid four repeated context loads. Before and after each command, call `mark_pulse_final_command_result` so the Pulse popup shows `waiting`, `running`, `done`, `skipped`, `blocked`, `failed`, or scheduler-recorded `timed_out` instead of static labels. The scheduler treats any command left waiting/running after the turn as failed rather than pretending it completed.

The dashboard command writes `builder/card.health.html`, creates any needed `report_human_inputs` rows, and keeps `builder/improve.html` aligned with those asks. Backup skips its actual operation only when its source-hash check proves the exact current state is already backed up. Publish skips when disabled, unverified, or already current; it never performs a first/verifying publish unattended. Notify runs last and mainly delivers the already-recorded state, even when an earlier final command failed.

The notify turn should include:

- Bug and Goal state
- modules that ran and modules that skipped
- important changes/fixes/proposals
- user questions created
- backup/publish status
- dashboard URL when publish is live
- cost/time summary, including missing or unpriced buckets

Backup protection must be stated plainly. Read `backup/status.json` and the
configured destinations, not only whether the latest Git command succeeded. If
status is `local_only`, or no verified destination is off-device, include a
prominent **Backup risk: local only** warning: the checkpoint helps undo an edit
but will be lost with the laptop. Never describe this state as healthy,
protected, or fully backed up. Recommend configuring at least one verified
remote Git or object-storage destination. Keep the warning in every Pulse
notification until off-device protection is verified; deduplicate any matching
dashboard recommendation or human-input request rather than creating a new one
each run.

When Gmail/email is available, default to rich email: set `email_subject`, `email_html`, and plain `email_body` on the same `notify_user` call. Use `email_to` only when the user's preference replaces the default To recipient. Use `email_cc` only when requested.
For Slack, default to the backend-owned rich rendering on that same call: set `slack_title`, a factual `slack_color`, compact `slack_fields`, relevant `slack_sections`, and `slack_footer`. Never read a webhook secret or post Block Kit JSON directly.

## Style

Write for the user first:

- takeaway first
- short labeled details after
- evidence paths last
- no long semicolon chains
- no compressed internal jargon unless also explained in plain language
- visual cards and chips before long text

Never invent values, trends, costs, or eval results. If evidence is missing, say exactly what is missing and why that matters.
