# Auto-Improvement Framework

Workflows that get better over time, in a way you can trust, measure, and override.

## What this gives you

Most automated workflows ship as one-shot configurations. They run, they produce output, and any improvement happens through ad-hoc edits. Over months, the workflow either drifts (small fixes pile up without direction) or stalls (the team is afraid to change anything because they can't tell what's working).

This framework changes that. With it, every workflow:

- **Has a clear, numeric goal.** Not "produce good output" — actual metrics with targets, units, and direction.
- **Carries its trajectory.** A line chart per metric, across runs, that anyone on the team can read.
- **Logs every change as a decision.** Who changed what, when, and what metric it was meant to move.
- **Improves through experiments, not edits.** Each change is tested against a measurement window and either kept or reverted based on evidence.
- **Stays under human control.** You can see what the AI is doing, override any decision, edit any metric, and steer what gets tried next.

The result: a workflow you can hand to operators with a straight face, and a track record of improvement you can show stakeholders.

## Who uses this

| Role | What they get |
|---|---|
| **Workflow author** | A clear way to declare what success means and watch progress toward it |
| **Operator** | Confidence that the workflow's output is held to a standard, with alerts when it drifts |
| **Reviewer / stakeholder** | A readable trail of every decision the AI made, why, and whether it worked |
| **Compliance / audit** | An immutable, structured log of changes with traceable evidence |

## The big idea — three workflow types

Not every workflow improves the same way. A regression-test suite improves by catching new bugs. A creative content workflow improves by discovering what resonates. A regulatory audit improves by absorbing new rules.

The framework recognizes three distinct types and treats each one differently:

| | **Type 1: Deterministic** | **Type 2: Exploratory** | **Type 3: Context-accumulating** |
|---|---|---|---|
| **Nature** | Same steps every run; reliable execution | Plan structure unknown; discovered by iteration | Stable shell, growing business context |
| **Success** | "Executed correctly" | "Output is good" — fuzzy, evolves | "Followed our rules + good output" |
| **Goal of improvement** | Drift monitoring, regression catching | Discovering what works | Absorbing new business rules |
| **Examples** | QA suites, ETL pipelines, data scrubs | Creative content, early-stage workflows | Audits, lead-gen, trading, AWS optimization |

Most real workflows are Type 3 — a stable plan with rules that grow over time as the business learns.

A workflow declares its type once. The type can change as the workflow matures (a Type 2 exploratory workflow often graduates to Type 3 once the rules stabilize).

## How the framework targets real workflows

The framework was built against seven concrete workflow shapes. Six of seven are Type 3 in steady state.

| Workflow | Type | What "improvement" looks like |
|---|---|---|
| QA suite (regression / smoke / business-attack / security) | Deterministic | Catch new bugs, monitor flakiness, never silently remove a check |
| Optimize a website for AI discoverability | Exploratory | Discover which structural changes raise AI citation rates over weeks |
| Lead generation | Context-accumulating | Accumulate ICP rules; measure conversion at the right time horizon |
| Outbound conversation / lead conversion | Context-accumulating | Accumulate playbooks per persona; measure conversion honestly given the prospect's agency |
| Trading bot | Context-accumulating | Accumulate risk rules; track PnL over an honest evaluation window |
| Finance audit | Context-accumulating | Absorb regulatory interpretations; preserve forensic audit trail |
| AWS cost-optimized security | Context-accumulating | Accumulate org-specific policy; track delayed cost outcomes |

## What success looks like — quantified, not prosed

Today, a workflow's success is described in a free-form `success_criteria` string. The framework keeps that string for high-level intent but adds a structured layer: a `metrics.json` file at the workflow root that defines each metric the workflow is held to.

A metric carries:

- A unique identifier (e.g. `audit.accuracy`).
- A unit (`percent`, `usd`, `seconds`).
- A direction — higher better, or lower better.
- A mode — drive *toward* a target, or stay above a *floor* / below a *ceiling*.
- A source — where the value comes from each run: an eval step, telemetry, an external feed, or delayed ground truth.
- Optionally, an evaluation lag for outcome metrics that don't materialize immediately (a 30-day prediction can only be scored 30 days later).

Metrics can be grouped under a parent for navigation in the UI. There is **no rolled-up "main metric" percentage** — that path produces fictions, especially when metrics have different units or directions. Every metric is shown independently.

## How a workflow improves — the experiment loop

Every change to a workflow is treated as an experiment, not an edit.

```
Hypothesis  →  Baseline  →  Intervention  →  Measurement  →  Conclusion  →  Next hypothesis
```

1. **Hypothesis.** "Adding rule X will reduce false-positive rate by ≥10pp." Pre-registered in writing before the change is applied.
2. **Baseline.** Recent run history is read; the metric's current behavior is captured.
3. **Intervention.** The change is applied — but with the prior state captured first, so revert is always possible.
4. **Measurement window.** Over the next N runs (workflow-configured), the metric is tracked.
5. **Conclusion.** When the window closes, a verdict is computed:
   - **Kept** — the change moved the metric in the right direction.
   - **Reverted** — the change made things worse and is rolled back automatically.
   - **Inconclusive** — the change didn't move the metric meaningfully.
   - **Extend** — the team needs more runs before deciding.
6. **Loop.** The next experiment is selected from underserved metrics, or paused if everything is in the green.

This design fixes the core problem with single-shot edits: a one-run improvement is mostly noise. The framework forces an honest measurement window before claiming success.

## Honest measurement — why we don't fake confidence

Three commitments that keep the trajectory chart honest:

- **The system computes verdicts, not the AI.** A separate evaluator agent reads the data and writes a one-paragraph rationale, but cannot silently decide its own experiments succeeded. The verdict is a deterministic heuristic.
- **The proposer is not the evaluator.** The agent that designed the experiment is not the agent that decides whether it worked. Fresh context, no incentive to mark its own work as a success.
- **We don't pretend to do statistics.** With small sample sizes (3–20 runs is realistic for most workflows), real significance testing is theatre. Conclusions use simple, transparent rules — "post-mean improved by ≥50% of expected magnitude AND ≥70% of post-runs beat baseline" — and the UI surfaces the sample size prominently with a low-confidence badge when N is small.

When the world changes mid-experiment — a model version updates, an upstream API shifts, the source data restructures — the system records that drift and downgrades displayed confidence. Conclusions on a high-drift window are flagged, not hidden.

## Decisions, captured

Every change writes a structured decision record. Together they form a readable history of how the workflow has improved (or not).

A decision record carries:

- When it was made and by whom (AI agent, human user, or automated system).
- Why — short prose rationale.
- What changed — the file paths affected.
- Which metric(s) it was meant to move.
- The experiment it opened (if any).
- For compliance workflows: the regulation it relates to and pointers to forensic evidence.

The existing `improve.md` prose log is preserved alongside this structured record — narrative is still useful, just not the only artifact.

## Type 3 — when business context is the workflow's intelligence

Type 3 workflows accumulate rules from the people who use them. Each rule is the captured wisdom of an operator, an analyst, or a domain expert.

The framework gives Type 3 workflows their own structured store — separate from the AI agent's audit trail because human-supplied context has different lifecycles and different write authority. Rules are never silently overwritten by an AI agent; they are user-owned content.

Crucially, every rule a user adds must be anchored to a metric. "We're adding this rule because we want to move risk.false_positive_rate" forces the team to think about whether each accumulated rule actually earns its keep. Rules that don't move their declared metric become visible — the framework calls these out as cargo-cult rules in the UI.

## You stay in control

The framework is transparent and overridable end-to-end. Every AI-produced artifact is visible, editable, and rejectable.

### Three oversight modes

Each workflow declares its oversight posture:

| Mode | Hypothesis approval | Conclusion approval | Recommended for |
|---|---|---|---|
| **Manual** | Required before measurement starts | Required before commit | Compliance, regulated workflows, deterministic suites |
| **Supervised** (default) | Automatic for low-risk changes; required for high-risk | Required when an experiment changes a metric definition or removes a rule | Most Type 3 business workflows |
| **Autonomous** | Automatic | Automatic; user reviews afterward | Exploratory creative work where iteration speed matters more than gates |

High-risk changes — redefining a metric, removing a user rule, restructuring the eval — always require approval, regardless of mode.

### What you can see

- Active experiments: hypothesis, runs N/M, observed values so far, world-state snapshot.
- Decision feed: every change ever made, filterable by AI vs human, by metric, by date.
- Metric trajectory charts with experiment windows shaded and decision markers shown as vertical lines on the chart.
- Conclusion details: the system-computed verdict, which heuristic rule fired, the evaluator's rationale.
- For any AI-produced artifact: the prompt and model that produced it (behind a "show prompt" affordance).

### What you can override

- Edit any metric definition. Changes are versioned — the prior series is preserved on the chart.
- Add, edit, or disable any rule in the context store.
- Pause, abort, extend, or manually conclude any experiment.
- Reject a hypothesis before measurement starts, with optional reason.
- Pin a hypothesis as forbidden so the optimizer stops retrying it.
- Set focus — designate which metrics the optimizer should prioritize next.
- Edit the proposer's and evaluator's system prompts. This is the primary lever for changing how the AI thinks about your workflow.
- Hard guardrails: max concurrent experiments, max cost, blackout windows, forbidden file paths.

Every override is itself a decision record with `source: user`. Reading the audit trail filtered to user-source gives the full history of how you have steered the workflow.

## Trust calibration — being honest about confidence

Users repeatedly ask "but is this number real?" The framework answers honestly:

- **Sample size** is shown next to every conclusion. Small N gets a visible low-confidence badge.
- **World drift** indicators flag conclusions where the underlying environment changed materially during the measurement window.
- **The heuristic that fired** is shown — not as opaque AI output but as an explicit rule with the inputs that triggered it.
- **Proposer ≠ evaluator** is shown in the UI as a confirmation badge, so users can see independence is in effect.

The framework refuses to render conclusions as confident on weak evidence. A workflow with three runs and rapidly shifting source data will see its conclusions dimmed — not hidden, but visibly less trustworthy than a workflow with thirty stable runs.

## What this framework deliberately does not do

These are choices, not omissions. Documenting them so the team doesn't reintroduce them later.

| Anti-feature | Why we said no |
|---|---|
| Composite "main metric" rolled up from sub-metrics with weights | Weights are made up; rolling across units like `percent` and `days` requires normalization choices nobody trusts. The same false-precision pattern is what produced existing eval false positives. Hierarchy is for grouping only. |
| Predicted-impact field on decisions | Asking the AI to predict its own impact in advance produces confident, baseless numbers. |
| Silent attribution of single-run deltas to specific decisions | Single-run noise is too high. We measure deltas only inside an explicit experiment frame. |
| Auto-replacement of `success_criteria` with structured fields | The plan-level prose stays. Per-step success_criteria is deprecated. |
| Forced metric definitions for exploratory workflows | Premature commitment. Adopt metrics when patterns stabilize. |
| Optimizer that reads observed deltas to pick its next move without guardrails | Goodhart's law machine. The framework includes this loop *with* guardrails: separate proposer/evaluator, pre-registered hypotheses, pinning, focus controls, explicit cost budgets. |
| New workflow types for adversarial / compliance / forecast / multi-tenant | Not new types — these are configurable capabilities. The three types remain. |

## Configurable capabilities for special cases

Beyond the three workflow types, four capabilities cover edge cases without typology bloat:

- **Plan stability** — most workflows allow plan changes freely; some (regulated, security-critical) ratchet up only; some are frozen entirely.
- **Decision-log mutability** — most workflows allow corrections via supersede; compliance workflows enforce strict append-only.
- **Lag-evaluated metrics** — predictions and outcomes that materialize over weeks are first-class. Trading and forecast workflows depend on this.
- **Optional artifacts** — adversarial test corpus for security workflows; predictions log for forecast workflows; tenant-partitioned context for multi-tenant SaaS workflows.

A workflow opts into the capabilities it needs. Most don't need any.

## What it looks like for the user

A workflow author working in optimizer mode sees four new tools available to the AI and four new slash commands they can invoke directly:

- `/improve-setup-framework` — one-time setup. Classifies `workflow_type`, sets `oversight_mode`, proposes starter metrics. Run this first; the improvement commands redirect here if it hasn't been done.
- `/exp-abort` — revert and stop the active experiment.
- `/exp-extend` — give the experiment more runs before concluding.
- `/exp-conclude` — manually render a verdict (the override path).

**Business-context capture has no slash command** — instead, the builder agent's system prompt teaches it to recognize when the user shares a business rule in conversation ("always X", "never X", regulatory clauses, ICP rules) and offer to persist it via the `capture_context` tool. This is the proactive path; rule capture is something the agent notices, not a ritual the user has to invoke.

The existing `/improve-*` commands precheck that `/improve-setup-framework` has been run. If the workflow lacks `workflow_type` or (for Type 1/3) lacks metrics, they redirect the user to `/improve-setup-framework` instead of bootstrapping inline. Setup is its own command because it's a meaningful conversation with the user; conflating it with improvement work bloats every improvement turn.

In the workflow folder, the framework adds:

- `metrics.json` at the root — the metric definitions.
- `builder/decisions.jsonl` — the structured decision log alongside the existing `improve.md` prose.
- `context/` — for Type 3 workflows: rules, examples, and clarifications from human users.
- `experiments/` — active and concluded experiments, with revertable diffs.

Every existing workflow keeps working unchanged. Adoption is opt-in, file by file.

## Adoption path

The framework ships in five phases. Each phase delivers value independently — there's no big-bang switchover.

1. **Charts + decisions feed.** No schema changes; charts existing eval scores over runs and surfaces a structured decision log alongside the prose log.
2. **Workflow type + oversight mode.** Authors declare what their workflow is and how much oversight they want. UI starts surfacing the type-aware behavior.
3. **Metrics + sources.** `metrics.json` becomes definable, with eval-step and telemetry sources. Charts now show metrics, not raw eval scores.
4. **Type 3 context capture.** The rules store and the `capture_context` tool land. The builder agent recognizes business rules in conversation and offers to persist them — no separate slash command is needed; rule accumulation becomes auditable.
5. **Experiment loop.** Every `/improve-*` command opens an experiment instead of applying immediately. Verdicts are system-computed; conclusions are evaluator-narrated; reverts are atomic.

Most teams will see immediate value at phase 1 (just the charts) and phase 4 (rule capture). The full experiment loop at phase 5 is where the framework's central promise — improvement you can trust — is fully realized.

## The migration story

Existing workflows are entirely unaffected on day zero:

- Workflow type defaults to *exploratory* if not declared.
- Oversight mode defaults to *supervised*.
- All new files (`metrics.json`, decisions log, context store, experiments) are opt-in.
- The existing `success_criteria` prose, `improve.md` log, and `evaluation/` subtree continue to work.

A team can adopt one capability at a time. Charts on day one. A single metric on day three. An experiment on day ten. Type 3 rule capture when the workflow accumulates business context worth recording.

## Concrete narrative — what a quarter of usage looks like

A finance audit team adopts the framework:

- **Week 1.** They declare the workflow as Type 3 with manual oversight. They define three metrics: `audit.accuracy`, `audit.coverage`, and `audit.cycle_time`. The chart starts showing values from last quarter's runs.
- **Week 3.** The workflow encounters a new GST clause. An auditor mentions the rule in chat ("for FY 2026, exclude reverse-charge entries from Section 17(5)"). The builder agent recognizes it as a durable rule, asks which metric it should move (the auditor picks `audit.accuracy`), and persists it via `capture_context`. The framework opens a follow-up experiment to validate the impact and waits five runs.
- **Week 5.** The experiment concludes: `audit.accuracy` rose from 87% to 94%; the rule is kept. The decision log records the rule, the auditor who added it, the regulation it references, and the experiment that validated it.
- **Week 7.** A second rule is added but the experiment concludes inconclusive — accuracy didn't move. The team decides to keep the rule anyway (compliance reasons) and the inconclusive verdict is logged honestly.
- **Quarter end.** Stakeholders see a chart: accuracy improved 7pp, coverage held its floor, cycle time stayed within budget. They see exactly which rules drove the gains and which didn't. The audit-trail is fully traceable.

That's the workflow the framework is designed to support.

## Related materials

For technical detail behind this framework:

- JSON Schemas for every artifact and tool input — `../../schemas/auto-improvement.schema.json`.
- Source code — see `agent_go/cmd/server/auto_improvement_*.go` and the related `decisions_log.go`, `metrics_runtime.go`, `context_store.go`, and `experiment_*.go` files.
- Adjacent workflow docs — `evaluation_system.md`, `learning_architecture.md`, `workflow_manifest_architecture.md`, `workflow_builder_commands_and_tools.md`.
