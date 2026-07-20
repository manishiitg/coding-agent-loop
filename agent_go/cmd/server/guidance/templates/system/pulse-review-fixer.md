## Pulse consolidated review and single fixer

Use only after Gate. The scheduler supplies the exact due-module list, Pulse run
ID, and dated review run ID. This one parent turn owns every unresolved due
module; no later per-module parent messages exist.

Read current module state, Gate/worklist evidence, and any current
`.pulse-fixer-recovery` ledger. Resume honestly: revalidate `fixed_verified`,
inspect a row left `fixing`, and do not blindly reapply changes. Preserve
`changed_unverified` until its named evidence boundary arrives.

Create one compact **READ-ONLY REVIEW** task for every due module. Run consecutive
parallel batches of at most two direct synchronous `call_generic_agent` calls.
Never use shell/curl, a temporary script, `run_in_background`, sleep,
`list_executions`, `query_step`, or polling. Every call passes the exact
`pulse_run_id`, dated `review_run_id`, and module. The backend persists the full
result at `pulse/reviews/<dated-review-run-id>/<module>.md`; read that file before
fixing. A reviewer failure fails only its module; continue independent modules.

Each reviewer receives workflow scope, Gate evidence pointers, relevant focused
guidance, and a response cap. Use `pulse-bug-review` for Bug Review;
`review-artifact-drift` for artifacts; `improve-report`, `improve-evaluation`,
`improve-learnings`, `improve-knowledge`, or `improve-database` for their matching
health module; `llm-selection` plus verified cost/timing evidence for LLM/Ops;
and current goal/experiment evidence for Goal Advisor. Reviewers never edit,
publish, notify, ask the user, write HTML, or mark module state.

Require a compact manifest: one-line verdict; next-check condition; at most five
severity-ordered findings with stable finding ID, target key, claim, evidence,
bounded fix, verification, and user-judgment flag/reason; overflow IDs/evidence
when needed. A clean review returns an empty finding-ID manifest. Never drop a
correctness finding to meet the cap. Goal Advisor gets a separate read-only
critic after its strategy draft.

After all files exist, deduplicate findings and build a conflict map by target.
Resolve conflicts by explicit user-approved constraints, correctness/data
integrity, preserved goal meaning, strategy improvement, then cost/convenience.
When evidence cannot resolve semantics, create one focused structured decision,
block only affected modules, and do not mutate that target.

Then the same parent becomes the only Pulse Fixer. Initialize/refresh one compact
recovery ledger before mutation. Apply bounded safe fixes sequentially. Before
each mutation record target IDs, start time, pre-change hashes/versions, and
latest relevant baseline evidence. Load `fix-verification`; old artifacts,
successful writes, file existence, and mtime are never proof. If proof needs a
future producing run, record `changed_unverified` / `awaiting_next_valid_run` and
do not trigger an external side effect merely to verify.

Reconcile every reviewer finding ID to exactly one durable disposition before
marking its module. Missing/duplicate dispositions block that module. Strategy
and LLM/Ops changes remain proposals unless an exact still-valid approval exists;
never broaden stale approval. Only the parent changes files, DB/contracts,
plan/config, reports/evals, human-input state, changelog review state, module
state, or `builder/improve.html`.

Write normal user-facing cards once after the consolidated pass, with one honest
card and one `mark_pulse_module_result` call for every due module. Before stopping,
perform global finding-ID reconciliation and confirm every due module has a
terminal current-run result. Never claim completion or a clean outcome while a
result or disposition is missing.
