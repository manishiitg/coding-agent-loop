Define what success means for this workflow before optimization.

Write to `builder/improve.html` - the single durable log. For the log/HTML format, one-time migration from legacy review files, and close-out rules, follow `get_reference_doc(kind="review-improve-log")` and `get_reference_doc(kind="html-output")`.

Either bootstrap a confirmed Goal contract or audit the existing Goal, eval coverage, and Pulse setup.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read `workflow.json`. Note any existing `oversight_mode` and `post_run_monitor`.
2. Read `builder/improve.html` if present. Note recent timeline entries, open findings, answered decisions, and archive rows. Do not treat it as the Goal source.
3. Read `soul/soul.md` to extract the workflow's objective and success criteria.
4. Read `planning/plan.json` to understand steps, structure, and whether the plan is frozen, ratcheting, or in flux.
5. Read `evaluation/evaluation_plan.json` if present to understand what currently measures success.
6. Read `runs/` to see how mature the workflow is.

STEP 0 - DETECT SETUP STATE
- FRESH SETUP: `soul/soul.md` lacks a clear objective or checkable success criteria. Proceed to STEP 0.5.
- REVIEW EXISTING: `soul/soul.md` already has a clear objective and checkable success criteria. Skip to STEP 5.

STEP 0.5 - CONFIRM THE GOAL WITH THE USER
The Goal Advisor loop evaluates strategy against this goal, so a vague or stale goal makes the loop aimless. Establish a real, user-confirmed goal before classifying or scheduling anything.

- Show the user the objective and each success criterion read from `soul.md`.
- Ask: "Is this still what success means here? Anything to change, add, or drop?"
- Push for checkable criteria: a threshold, observable outcome, required artifact, or explicit quality bar.
- If `soul.md` has no success criteria, or they are vague/placeholder, stop and ask directly: "What does success look like for this workflow - what checkable outcomes tell you it is working?"
- Write the confirmed objective and success criteria back to `soul.md`. It is the single source of truth rendered by Runloop's Goal / Ikigai tab and used by the Goal verdict.
- Keep only stable intent in `soul.md`: objective, checkable success criteria, and any constraint the user explicitly confirms as non-negotiable. Do not copy architecture, step design, provider/tool choices, implementation details, references, historical decisions, or agent-inferred assumptions into it. Those belong in the plan/config/changelog/learnings/knowledgebase and remain open to improvement.
- If existing `soul.md` contains architecture or an agent-made assumption, do not silently treat it as authoritative. Surface the ambiguity as an assumption to challenge; preserve the current implementation in its proper artifact and ask only when deciding whether it is a genuine user constraint.

Only once the goal is confirmed, continue.

STEP 1 - CLASSIFY THE OPERATING MODEL
Walk the user through a primary type plus optional secondary traits. Real workflows mix types; do not force a single enum.

Ask the user to confirm:
- Primary type:
  - `deterministic_harden_first`: known plan/output; improve reliability, validation, and locking.
  - `exploratory_goal_optimization`: goal known but best plan unknown; improve by experiments, eval evidence, and Goal Advisor plan changes.
  - `business_context_accumulating`: workflow improves by remembering user rules, preferences, examples, account/domain context.
  - `compliance_audit`: correctness, evidence, traceability, and conservative change control matter most.
  - `human_review_production`: workflow prepares drafts/options for human approval; improve approval rate and reduce edit burden.
  - `monitoring_alerting`: workflow watches events/thresholds and escalates; improve false positives/negatives and alert latency.
  - `research_synthesis`: workflow gathers uncertain external info and produces grounded judgment; improve source quality and unsupported-claim checks.
  - `creative_generative`: subjective output quality and preference fit matter most; improve through feedback and examples.
- Secondary traits: any additional types that materially constrain improvement.

Then map the confirmed type/traits onto the internal axes:
- Plan stability: `mutable`, `ratchet`, or `frozen`.
- Runtime mode: `single` or `dual`.
- Business context accumulation: `accumulating` or `none`.
- Improvement cadence: daily / weekly / per-incident / quarterly / never.

Show your inference, reasoning, and alternatives considered. Ask the user to confirm.

STEP 2 - SEED builder/improve.html
If `builder/improve.html` does not exist yet, create it from the Starter HTML skeleton in `get_reference_doc(kind="review-improve-log-skeleton")` using `diff_patch_workspace_file`. If it exists, preserve its timeline.

Fill the skeleton:
- Header: workflow name, type/oversight chips, and verdict pills. With no runs yet, set Bug = "Not measured" and Goal = "Not measured" (warn).
- Add one dated **Reflection / Hansei** framework-setup card with `data-pulse-section="reflection"` and `data-module="run_summary"`. Summarize the confirmed operating model and why it fits; do not copy the Goal into the HTML.

Leave signal tiles, recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, and archive section in place. Do not invent values or runs.

STEP 3 - SET FRAMEWORK FIELDS IN workflow.json
These are the structured framework fields that drive behavior:
- `oversight_mode`: `manual`, `supervised`, or `autonomous`. Recommended defaults: deterministic or ratcheting workflow -> `manual`; exploratory -> `autonomous`; contextual/business-context -> `supervised`.
- `post_run_monitor`: `true` or `false`. Recommend `true` for workflows where a silently broken or drifting run would matter and is not watched live. Leave off for scratch, experimental, or interactive-only workflows where the extra per-run Pulse review is not worth it.

When turning `post_run_monitor` on, ask the user how they want to be notified. Default: the monitor sends one compact run summary after every run, and marks broke / recovered / new finding transitions clearly. If they want something else, capture it as a `## Notifications` section in `soul/soul.md`. If they accept the default, leave the section out.

STEP 4 - VERIFY EVAL COVERAGE
Read `evaluation/evaluation_plan.json` and compare it to `soul.md` (the coverage matrix, both ways):
- Does each important success criterion have an eval step measuring it?
- Does any eval step map to no criterion, or duplicate operational checks that Pulse Gate/Bug Review / `pre_validation` already own (missing artifacts, malformed output, skipped steps)? Flag those for retirement — eval measures the goal; Pulse owns operational quality.
- Do eval steps fail closed when inputs are missing (never pass on absent evidence)?
- Are eval thresholds aligned with the confirmed success criteria?

If important coverage is missing, record an open finding in `builder/improve.html` and suggest `/improve-evaluation` with a focused instruction. Do not create separate workflow measurement artifacts; that feature is currently disabled.

STEP 5 - REVIEW PATH
You are auditing existing setup, not bootstrapping. Walk through these checks and surface issues with proposed fixes. Apply nothing without user confirmation.

5.1 Goal contract sanity
- Does `soul/soul.md` contain only stable intent, checkable success criteria, and explicit user-confirmed constraints?
- Has architecture, step shape, provider choice, or an agent-inferred assumption leaked into the Goal and started constraining improvement?
- Are the objective and criteria still aligned with what the user actually wants?

5.2 Framework fields
- Verify `oversight_mode` matches the workflow's operating model.
- Check `post_run_monitor`. If the workflow is scheduled and silent breaks matter but it is off, recommend turning it on. If it is scratch/experimental and on, note the user can turn it off.

5.3 Goal and eval coverage
- Compare `soul.md` success criteria against `evaluation/evaluation_plan.json`.
- Flag success criteria with no eval coverage.
- Flag eval steps that always pass, fail open on missing inputs, or do not inspect consequential run evidence.

After STEP 5, record what you reviewed and recommended in `builder/improve.html` as a dated **Reflection / Hansei** Framework review entry (`data-pulse-section="reflection"`, `data-module="run_summary"`) so the audit trail survives the session.

If `builder/improve.html` is already long, compact it after the review:
- keep the current header/verdicts, latest 10-20 timeline entries, and all open findings in `builder/improve.html`
- move older resolved/no-action/superseded entries to `builder/improve-archive/YYYY-MM.html`
- leave an archive row with date range, entry count, unresolved findings, and one-line summary
