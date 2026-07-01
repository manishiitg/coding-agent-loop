Define what success means for this workflow before optimization.

Write to `builder/improve.html` - the single durable log. For the log/HTML format, one-time migration from legacy review files, and close-out rules, follow `get_reference_doc(kind="review-improve-log")` and `get_reference_doc(kind="html-output")`.

Either bootstrap the auto-improvement framework (Workflow Profile + confirmed success criteria) or, if it already exists, audit the setup and surface issues.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read `workflow.json`. Note any existing `oversight_mode` and `post_run_monitor`.
2. Read `builder/improve.html` if present. Note any existing Workflow Profile block, recent timeline entries, open findings, and archive rows.
3. Read `soul/soul.md` to extract the workflow's objective and success criteria.
4. Read `planning/plan.json` to understand steps, structure, and whether the plan is frozen, ratcheting, or in flux.
5. Read `evaluation/evaluation_plan.json` if present to understand what currently measures success.
6. Read `runs/` to see how mature the workflow is.

STEP 0 - DETECT SETUP STATE
- FRESH SETUP: `builder/improve.html` has no Workflow Profile block. Proceed to STEP 0.5.
- REVIEW EXISTING: `builder/improve.html` already has a Workflow Profile block. Skip to STEP 5.

STEP 0.5 - CONFIRM THE GOAL WITH THE USER
The auto-improve loop optimizes toward this goal, so a vague or stale goal makes the loop aimless. Establish a real, user-confirmed goal before classifying or scheduling anything.

- Show the user the objective and each success criterion read from `soul.md`.
- Ask: "Is this still what success means here? Anything to change, add, or drop?"
- Push for checkable criteria: a threshold, observable outcome, required artifact, or explicit quality bar.
- If `soul.md` has no success criteria, or they are vague/placeholder, stop and ask directly: "What does success look like for this workflow - what checkable outcomes tell you it is working?"
- Write the confirmed objective and success criteria back to `soul.md`. It is the single source of truth that the goal card and Goal verdict read from.

Only once the goal is confirmed, continue.

STEP 1 - CLASSIFY THE WORKFLOW PROFILE
Walk the user through a primary type plus optional secondary traits. Real workflows mix types; do not force a single enum.

Ask the user to confirm:
- Primary type:
  - `deterministic_harden_first`: known plan/output; improve reliability, validation, and locking.
  - `exploratory_goal_optimization`: goal known but best plan unknown; improve by experiments, eval evidence, and replanning.
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
If `builder/improve.html` does not exist yet, create it from the Starter HTML skeleton in `get_reference_doc(kind="review-improve-log")` using `diff_patch_workspace_file`. If the file already exists, edit the goal card/profile in place; do not overwrite the timeline.

Fill the skeleton:
- Header: workflow name, type/oversight chips, and verdict pills. With no runs yet, set Bug = "Bug-free" and Goal = "Not yet measured" (warn).
- Goal card: one-line objective from `soul.md`, then one criterion row per success criterion. Until the first run, mark each criterion as not yet measured.
- Workflow Profile: a readable profile block with primary type, secondary traits, plan stability, runtime mode, business context, improvement cadence, and 3-5 behavioral implications the agent respects every turn.

Leave signal tiles, recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, and archive section in place. Do not invent values or runs.

STEP 3 - SET FRAMEWORK FIELDS IN workflow.json
These are the structured framework fields that drive behavior:
- `oversight_mode`: `manual`, `supervised`, or `autonomous`. Recommended defaults: deterministic or ratcheting workflow -> `manual`; exploratory -> `autonomous`; contextual/business-context -> `supervised`.
- `post_run_monitor`: `true` or `false`. Recommend `true` for workflows where a silently broken or drifting run would matter and is not watched live. Leave off for scratch, experimental, or interactive-only workflows where the extra per-run triage pass is not worth it.

When turning `post_run_monitor` on, ask the user how they want to be notified. Default: the monitor pings once only on a transition (broke / recovered / new finding) and is silent on steady runs. If they want something else, capture it as a `## Notifications` section in `soul/soul.md`. If they accept the default, leave the section out.

STEP 4 - VERIFY EVAL COVERAGE
Read `evaluation/evaluation_plan.json` and compare it to `soul.md`:
- Does each important success criterion have eval coverage?
- Does the eval also catch operational failures: missing artifacts, malformed output, unsupported claims, wrong tool use, skipped steps, and stale data?
- Are eval thresholds aligned with the confirmed success criteria?

If important coverage is missing, record an open finding in `builder/improve.html` and suggest `/improve-evaluation` with a focused instruction. Do not create separate workflow measurement artifacts; that feature is currently disabled.

STEP 5 - REVIEW PATH
You are auditing existing setup, not bootstrapping. Walk through these checks and surface issues with proposed fixes. Apply nothing without user confirmation.

5.1 Workflow Profile sanity
- Is the existing Workflow Profile block accurate given the current plan?
- Are primary type, secondary traits, and axes filled in with rationale?
- Are behavioral implications still relevant?

5.2 Framework fields
- Verify `oversight_mode` matches the profile.
- Check `post_run_monitor`. If the workflow is scheduled and silent breaks matter but it is off, recommend turning it on. If it is scratch/experimental and on, note the user can turn it off.

5.3 Goal and eval coverage
- Compare `soul.md` success criteria against `evaluation/evaluation_plan.json`.
- Flag success criteria with no eval coverage.
- Flag eval steps that always pass, fail open on missing inputs, or do not inspect consequential run evidence.

After STEP 5, record what you reviewed and recommended in `builder/improve.html` as a dated Framework review entry so the audit trail survives the session.

If `builder/improve.html` is already long, compact it after the review:
- keep the Workflow Profile, latest 10-20 timeline entries, and all open findings in `builder/improve.html`
- move older resolved/no-action/superseded entries to `builder/improve-archive/YYYY-MM.html`
- leave an archive row with date range, entry count, unresolved findings, and one-line summary
