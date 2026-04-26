# Auto-Improvement Framework

A framework for building workflows that improve over time in a measurable, auditable way.

The framework is type-aware: it recognizes that not every workflow improves the same way, and forces design choices about eval, success, metrics, and decision-tracking to flow from the workflow's actual nature rather than from one-size-fits-all defaults.

---

## Problem

Today, workflow improvement relies on a free-form `success_criteria` string, an `evaluation/` subtree that produces opaque scores, and a `builder/improve.md` log of prose decisions. Three things are missing:

1. **A quantified goal.** "What are we trying to drive?" answered with prose, not numbers.
2. **A trajectory.** Eval scores exist per run but are not surfaced as time series tied to declared goals.
3. **A decision audit trail.** `improve.md` carries narrative but is not structured, so we can't ask "which rule was added to move which metric, when?"

Without these, every improvement cycle restarts from scratch, and we cannot tell whether our changes are actually moving the dial.

The framework adds these three layers without breaking existing artifacts.

---

## Workflow Types

Eval, success, metrics, and decision-tracking work differently for different kinds of workflows. The framework recognizes three types and treats them as a first-class field on the workflow.

| | **Type 1: Deterministic** | **Type 2: Exploratory** | **Type 3: Context-accumulating** |
|---|---|---|---|
| **Nature** | Same steps every run; reliable execution | Plan structure is unknown; discovered by iteration | Stable shell, growing business context |
| **Success** | "Executed correctly" | "Output is good" (fuzzy, evolves) | "Followed business rules + good output" |
| **Eval** | Hard checks (existence, value match) | LLM-judge or human review | Hard checks + rule conformance + human review |
| **Metrics shape** | SLOs (floor, not target): reliability, cost, latency, freshness | Optional, often deferred | **Required, load-bearing.** SLOs + rule-conformance + outcome metrics |
| **"Improve over time" means** | Drift monitoring, regression catching | Discovering what works; restructuring plan | Absorbing human input as rules; structure stable |
| **Plan stability** | Frozen — changes are rare and risky | Mutates frequently | Stable shell |
| **Primary artifact** | Run history (was it green?) | `decisions.jsonl` (the journey) | `metrics.json` + `context/` store |
| **`/improve-*` value** | Low — mostly /monitor + /repair | High — heavy user | Medium, with required metric-targeting |
| **Examples** | check-form-26as | linkedin, social-media, trading | gst-audit-excellence, citymall workflows |

A workflow declares its type explicitly. The type can change as the workflow matures — common trajectory is Type 2 → Type 1 as the plan stabilizes; Type 3 elements bolt on later as business context grows.

---

## Type Definitions in Detail

### Type 1 — Deterministic

**What it is.** A workflow whose steps and structure are fully known. The author has decided exactly what should happen and in what order. The job is to execute that plan reliably across many runs, in the face of flaky upstream systems, source data drift, and infrastructure noise.

**Signals you're in Type 1:**
- The same input shape produces the same expected output shape every time.
- Step-by-step prose can describe the workflow without "and then we figure out…" hedging.
- "It worked yesterday but failed today" is a real concern.
- Re-running with the same inputs should give the same outputs (modulo source-data updates).

**Improvement model.** Improve = reduce drift, catch regressions, lower cost, lower latency. **Not** restructure the plan. Restructuring a working deterministic workflow is high-risk; the optimizer should warn before any plan-shape change.

**Eval shape.** Hard, mechanical checks: file exists, schema matches, value equals expected, hash matches, count is in range. No fuzziness. If a check is fuzzy ("this output looks reasonable"), the workflow is not really Type 1.

**Metric shape.** SLOs, not targets. You're not driving a number up — you're keeping a number above a floor (success rate, freshness) or below a ceiling (cost, latency). Trajectory views show degradation, not progress.

**When you're wrong about being Type 1.** If the workflow is actually exploratory but mislabeled as deterministic, the framework will block useful improvements as "high-risk plan changes." Fix by re-labeling, not by overriding gates.

**Examples in the repo:** `check-form-26as-xspaces`, `confida-login`, any login-and-fetch workflow.

### Type 2 — Exploratory

**What it is.** A workflow whose right structure is unknown. The author has a goal but doesn't yet know which steps, in which order, with which prompts, will produce good output. Improvement is the act of discovering that structure by running, observing, and changing.

**Signals you're in Type 2:**
- "Output is good" is fuzzy; reasonable people would judge differently.
- The plan changed three times last week.
- LLM-judge or human review is the only honest eval today.
- You can't yet say "the metric is X" because you don't know what to measure.

**Improvement model.** Improve = restructure plan, refine prompts, add or remove steps, change eval. The whole workflow is in flux. The decision log is the primary artifact because it captures the journey of figuring out what works.

**Eval shape.** Starts as LLM-judge or human review against rubrics. As patterns stabilize, hard checks emerge for the parts that have settled.

**Metric shape.** Often empty. Forcing `metrics.json` here is premature commitment — you commit to driving the wrong number. As the workflow stabilizes, real metrics emerge from observed patterns; that's the moment to add them, not before.

**When you're wrong about being Type 2.** If you've actually figured out the structure but keep relabeling as exploratory to avoid the discipline, the workflow never graduates and never picks up Type 1's reliability tooling. Common bad sign: same plan shape unchanged for many runs but workflow_type still `exploratory`. Promote it.

**Examples in the repo:** `linkedin`, `social-media`, early iterations of any new workflow.

### Type 3 — Context-accumulating

**What it is.** A workflow whose plan shell is stable but whose business context grows continuously. Each user-supplied rule ("never recommend assets <6mo old", "always include the Q4 reporting clause", "this customer prefers tabular outputs") shapes future runs. The workflow's intelligence accumulates through a partnership between the agent (which executes) and the user (which steers).

**Signals you're in Type 3:**
- The workflow has been running for a while; the plan is mostly stable.
- The user keeps adding "and one more thing…" rules after each run.
- "Did the agent follow the user's preferences?" is a meaningful question.
- The same prompt would produce different correct answers for different users.

**Improvement model.** Improve = absorb human input as durable rules. The plan shell rarely changes; what changes is the rule set, the example library, and the metric definitions that gate rule conformance. **Metrics are load-bearing here**: every rule must declare which metric it's meant to move, otherwise context accumulation becomes complexity accumulation.

**Eval shape.** Hard checks + auto-derived rule-conformance checks (one per rule in `context/rules.md`) + human review on outputs.

**Metric shape.** Required, with hierarchy for grouping (no rollup math). Mix of SLOs (cost, latency), rule-conformance rates (per rule cluster), and outcome metrics (human approval, downstream signal).

**When you're wrong about being Type 3.** Workflows mislabeled as Type 3 when they're really Type 2 force premature metric definition that channels iteration into the wrong place. Workflows mislabeled as Type 1 when they're really Type 3 lose the context store entirely and the user's rules live in soul.md as ungovernable prose. Get the type right.

**Examples in the repo:** `gst-audit-excellence-technosoft`, mature `trading` workflows, `citymall-infra`.

---

## Patterns That Look Like New Types — But Aren't

Several workflow patterns feel distinct enough to deserve their own type. They don't. Each maps cleanly into one of the three existing types plus a configurable capability. This section documents the mappings so authors don't fork the typology.

### Adversarial / robustness-bound (e.g. `citymall-exploit-hacker`, security audits)

**What it looks like.** Inputs are crafted to break the workflow. "Pass" means catching attacks; the attack distribution shifts as adversaries adapt.

**Maps to:** **Type 2** (the attack patterns evolve, eval test set evolves) **or Type 3** (when the security team's accumulated detection rules form the workflow's business context).

**Why not its own type:** the adversarial nature lives entirely in the **eval inputs** (a maintained `red_corpus/` of attack examples) and a **plan-stability flag** (`ratchet` — added controls don't get silently removed). The improvement loop, decision log, and metric shape are unchanged.

**Configurable capability needed:** `red_corpus/` artifact + `plan_stability: ratchet`.

### Compliance / audit-trail-bound (e.g. `gst-audit-excellence`, regulatory filings)

**What it looks like.** Every change must be defensible to an external auditor. Improvements are ratchet-only; the decision log is forensic evidence.

**Maps to:** **Type 1** when the audit procedure is fully prescribed, **Type 3** when it absorbs ongoing regulatory interpretation as rules.

**Why not its own type:** the compliance-distinct behavior decomposes into existing knobs — `oversight_mode: manual` (already in the framework), `plan_stability: ratchet`, `decision_log_mutability: append_only_strict` (forbids edits even with audit trail), and optional `regulation_ref` / `evidence_paths` fields on decision records.

**Configurable capability needed:** `decision_log_mutability: append_only_strict` + optional decision-record fields.

### Forecast / prediction (e.g. `trading` for price/risk predictions)

**What it looks like.** The workflow makes predictions whose ground truth arrives later — sometimes days or months. The experiment loop's "next M runs" measurement window doesn't apply; you have to wait for reality.

**Maps to:** any of the three types, depending on whether the prediction logic is fixed (Type 1), being discovered (Type 2), or shaped by accumulated trader/analyst preferences (Type 3).

**Why not its own type:** the delayed-ground-truth nature is a **per-metric concern**, not a per-workflow concern. A trading workflow has both immediate metrics (cost, latency) and delayed metrics (forecast accuracy at 30d). Forcing a "Forecast" type would push that workflow into a corner where its non-delayed metrics don't fit.

**Configurable capability needed:** `evaluable_at_lag: "30d"` on individual metrics + `predictions_log.jsonl` artifact + experiment-loop pending-evaluation queue (generalizes the existing "extend window" mechanism).

### Multi-tenant / per-tenant variant (B2B SaaS, per-customer workflows)

**What it looks like.** A base workflow + N customer-specific overrides. Metrics need both aggregate and per-tenant views.

**Maps to:** **Type 3** with a partitioned context store. The base workflow lives at the workflow level; per-tenant rules live at `context/<tenant_id>/rules.md`.

**Why not its own type:** the tenant dimension is a **partition on existing artifacts**, not a different framework shape. Adding tenant-awareness to the framework before there's an actual multi-tenant workflow in the repo would be premature; the partition can be added when needed.

**Configurable capability needed:** `context/<tenant_id>/` partitioning + `tenant_id` dimension on metrics. Defer until first real multi-tenant workflow exists.

### Periodic / reporting (weekly KPI reports, monthly compliance digests)

**What it looks like.** Runs on a schedule, produces a report consumed by humans who skim it.

**Maps to:** **Type 1** if the report format is prescribed, **Type 2** if you're still figuring out what makes a good report.

**Why not its own type:** the reader-cognition concern is real but doesn't change framework defaults. It's a metric concern (add a "report quality" metric, sourced from human feedback) not a structural one.

**Configurable capability needed:** none beyond the existing framework.

### Pipeline / data-engineering (ETL, ML training pipelines, data scrubs)

**What it looks like.** Data flows through transformations; output is data, not action. Lineage, schema drift, and freshness matter.

**Maps to:** **Type 1**. The plan is fixed; success = data quality.

**Why not its own type:** data-quality concerns are metric-shape concerns (`source: lineage`, `source: schema_check`) within Type 1's SLO framing.

**Configurable capability needed:** lineage/schema metric sources. Optional.

### Generative / creative (content generation, design generation, code generation)

**What it looks like.** Output's value depends on novelty + quality + on-brand-ness. Mode collapse is a failure.

**Maps to:** **Type 2** until it stabilizes. Some generative workflows graduate to **Type 3** when the brand voice / style guide becomes accumulated context.

**Why not its own type:** novelty and quality are metric concerns within Type 2's exploratory frame. Diversity metrics (cluster spread, n-gram uniqueness) plug into the existing metrics.json.

**Configurable capability needed:** none beyond metric definitions.

### Triage / routing (ticket classification, alert routing, document categorization)

**What it looks like.** Classify input → route to the right handler.

**Maps to:** **Type 1** with classification metrics. Categories are stable; the job is accuracy.

**Why not its own type:** precision/recall per category are normal metrics. Nothing structural is different.

**Configurable capability needed:** none.

### Negotiation / multi-turn agent (sales outreach, support, scheduling)

**What it looks like.** Workflow interacts with an external party over multiple turns; success depends on the other party's actions.

**Maps to:** **edge case — flag, don't type.** The "run = unit of measurement" assumption that the experiment loop rests on doesn't cleanly hold when the environment has agency. A run can succeed for reasons unrelated to the agent's choices.

**Why not its own type:** the right home for this is a **separate research question** about whether the experiment loop generalizes to environments with agency, not a typology slot. Workflows of this shape can still use the framework with caveats; conclusions need extra skepticism.

**Configurable capability needed:** flagged in the UI's trust calibration as "interactive — conclusions account for environmental agency"; out of scope for the initial framework.

---

## Configurable Capabilities (Not New Types)

These are framework features that any workflow can opt into, regardless of its declared type. They're how the framework absorbs the variations above without typology bloat.

**Per-workflow flags** (in `workflow.json`):
- `plan_stability: mutable | ratchet | frozen` (default `mutable`) — `ratchet` allows additions but blocks silent removals; `frozen` blocks any plan change without explicit human approval. Used by adversarial and compliance workflows.
- `decision_log_mutability: append_only | append_only_strict` (default `append_only`) — `append_only_strict` forbids any edit, even corrective. Used by compliance.

**Optional artifacts** (workflows that need them, regardless of type):
- `red_corpus/` — versioned adversarial test inputs.
- `predictions_log.jsonl` — predictions waiting for ground truth.
- `context/<tenant_id>/` — tenant-partitioned context store.

**Per-metric fields** (in `metrics.json`):
- `evaluable_at_lag: "<duration>"` — declares the metric value isn't available until a lag elapses. The experiment-loop conclusion mechanism waits.
- `source: { type: "lineage" | "schema_check" | "delayed_ground_truth", ... }` — metric value comes from data infrastructure, not eval steps.

**Optional decision-record fields**:
- `regulation_ref` — for compliance workflows.
- `evidence_paths` — pointers to forensic evidence supporting the decision.

**Experiment-loop capability**:
- Pending-evaluation queue: experiments whose target metric has `evaluable_at_lag` wait for the lag to elapse before conclusion. This generalizes the existing "extend window" path; same machinery, different trigger.

The principle: when a workflow shape feels like it needs its own type, first check whether one of these capabilities accommodates it. Only fork the typology if multiple capabilities together still don't cover the case.

---

## The Artifacts

The framework adds three optional files alongside the existing workflow structure. None replaces an existing file.

```
<workflow>/
  workflow.json
  planning/
    plan.json              # add: workflow_type field at root
    metrics.json           # NEW (optional/required by type)
    step_config.json
  evaluation/              # unchanged
    evaluation_plan.json
    step_config.json
  builder/
    improve.md             # unchanged (prose log preserved)
    decisions.jsonl        # NEW (sidecar, structured)
  context/                 # NEW (Type 3 only)
    rules.md
    examples/
    clarifications.jsonl
  experiments/             # NEW (Types 2 and 3)
    active.json
    history.jsonl
    config.json
  soul/
    soul.md                # unchanged
```

### `workflow_type` (in `planning/plan.json`)

```json
{
  "workflow_type": "deterministic" | "exploratory" | "contextual",
  "objective": "...",
  "success_criteria": "...",
  "steps": [...]
}
```

The field is editable. UI, builder defaults, and `/improve-*` command behavior gate on this value.

### `planning/metrics.json`

A flat list of metric definitions with optional parent links for grouping. **No weighted rollup, no composite "main metric" math** — that path produces fictions.

```json
{
  "metrics": [
    {
      "id": "audit.accuracy",
      "label": "Findings accuracy vs source",
      "unit": "percent",
      "direction": "higher_better",
      "target": 95,
      "mode": "target",
      "source": { "type": "eval_step", "id": "eval-data-accuracy" }
    },
    {
      "id": "audit.coverage",
      "parent": "audit.quality",
      "unit": "percent",
      "direction": "higher_better",
      "mode": "slo",
      "floor": 90,
      "source": { "type": "eval_step", "id": "eval-retrieval" }
    },
    {
      "id": "cost_per_run",
      "unit": "usd",
      "direction": "lower_better",
      "mode": "slo",
      "ceiling": 0.50,
      "source": { "type": "telemetry", "field": "run.total_cost_usd" }
    },
    {
      "id": "human_approval_rate",
      "unit": "percent",
      "direction": "higher_better",
      "source": { "type": "external", "field": "feedback.approved_pct" }
    }
  ]
}
```

**Fields:**
- `id` — unique within the workflow.
- `unit` — `percent`, `usd`, `seconds`, `count`, `days`, etc.
- `direction` — `higher_better` | `lower_better`.
- `mode` — `target` (drive toward), `slo` (stay above floor / below ceiling).
- `target` / `floor` / `ceiling` — set by mode.
- `parent` — optional, for grouping in UI only. No rollup math.
- `source` — where the metric value comes from each run:
  - `eval_step` — taken from an existing eval step's score.
  - `telemetry` — taken from run telemetry (cost, latency).
  - `external` — supplied by an external feed (human feedback, downstream signal).

**Per-type required-ness:**

| Type | `metrics.json` |
|---|---|
| Deterministic | Optional but recommended (SLO mode preferred) |
| Exploratory | Optional, often empty until patterns stabilize |
| **Contextual** | **Required.** Each rule/decision is anchored to a metric. |

### `builder/decisions.jsonl`

Append-only structured decision log, sidecar to `improve.md` (prose log preserved). One JSON record per line.

```json
{"ts":"2026-04-26T12:00:00Z","source":"agent","trigger":"improve-eval","rationale":"single eval step gave 100/100 despite TDS=0 vs ₹2L in PDF","applied_changes":["evaluation/evaluation_plan.json","evaluation/step_config.json"],"target_metrics":["audit.accuracy"]}
{"ts":"2026-04-26T14:30:00Z","source":"user","trigger":"capture-context","rule_added":"never recommend assets with <6mo track record","applied_changes":["context/rules.md"],"target_metrics":["risk.false_positive_rate"]}
```

**Fields:**
- `ts` — ISO-8601 UTC.
- `source` — `agent` | `user`. Distinguishes builder/agent decisions from user-supplied context.
- `trigger` — slash command or other origin.
- `rationale` — short prose, optional.
- `applied_changes` — array of file paths that were modified.
- `target_metrics` — array of metric ids this decision is meant to move.

**No `expected_delta` and no `observed_delta` fields.** Predicting impact is hallucination-prone; auto-attributing observed deltas is unreliable due to single-run noise and multiple-decisions-per-window. Humans read the chart and judge causality; the system only enforces the upfront declaration.

**Per-type behavior:**
- Type 1 — `target_metrics` optional.
- Type 2 — `target_metrics` optional (metrics may not exist yet).
- **Type 3** — `target_metrics` is **required and non-empty**. `/capture-context` and `/improve-*` refuse to save without it.

### `context/` store (Type 3 only)

Accumulates human-supplied business context that shapes how the workflow reasons. Read on every run; injected into agent context.

- `context/rules.md` — append-mostly markdown, structured by section. Business rules, constraints, exceptions.
- `context/examples/` — concrete cases the user has provided as reference.
- `context/clarifications.jsonl` — user-side decisions, separate from `builder/decisions.jsonl`. Same schema, but `source: user` always, and `rule_added` / `clarification` fields capture what was added.

This layer is structurally separate from `builder/` because **agent self-improvement and user-supplied context have different lifecycles, different review processes, and different write authority.**

### `experiments/` store (Types 2 and 3)

The experiment loop's persistent state. See the Experiment Loop section below for full lifecycle.

- `experiments/active.json` — currently running experiment(s), one record each.
- `experiments/history.jsonl` — append-only log of concluded experiments.
- `experiments/config.json` — default sample sizes, conclusion thresholds, cooldowns.
- `experiments/proposer_prompt.md` — system prompt for the LLM that proposes experiments. User-editable.
- `experiments/evaluator_prompt.md` — system prompt for the LLM that concludes experiments. User-editable; intentionally distinct from the proposer.

---

## Experiment Loop

The framework treats every improvement as an experiment, not a one-shot edit. An experiment has an explicit hypothesis, a measurement window of N runs, an evidence-based conclusion, and a successor experiment chosen by what the conclusion revealed.

This addresses the single-run noise problem (which earlier was the reason for rejecting auto-attribution) by replacing silent attribution with **pre-registered hypotheses + windowed measurement**. The system never claims "this decision moved the metric" without an experiment frame around it.

Type 1 (deterministic) does not run experiments — its plan is frozen. Types 2 and 3 use experiments as their primary improvement mode.

### Lifecycle

```
HYPOTHESIS (pre-registered, written to active.json before intervention)
  ↓
BASELINE (taken from prior N runs already in history)
  ↓
INTERVENTION (apply change; capture revertable diff)
  ↓
MEASUREMENT WINDOW (next M runs, M = workflow-configured sample size)
  ↓
CONCLUSION (kept | reverted | inconclusive | extend)
  ↓
NEXT HYPOTHESIS
```

### Experiment Record

```json
{
  "id": "exp-2026-04-26-001",
  "status": "measuring",
  "hypothesis": "Adding rule 'never recommend assets <6mo old' will reduce risk.false_positive_rate by ≥10pp",
  "target_metrics": ["risk.false_positive_rate"],
  "expected_direction": "decrease",
  "expected_magnitude": 0.10,

  "baseline": {
    "window": "last_5_runs",
    "values": { "risk.false_positive_rate": [0.18, 0.21, 0.16, 0.19, 0.20] },
    "mean":   { "risk.false_positive_rate": 0.188 }
  },
  "intervention": {
    "trigger": "capture-context",
    "applied_changes": ["context/rules.md"],
    "revertable_diff": "<diff path>"
  },
  "measurement": {
    "target_runs": 5,
    "completed_runs": 3,
    "values": { "risk.false_positive_rate": [0.10, 0.08, 0.12] }
  },
  "world_state": {
    "started_at": { "model_versions": {...}, "mcp_versions": {...} },
    "concluded_at": null
  },
  "conclusion": null,
  "approvals": {
    "hypothesis_approved_by": "user|auto",
    "conclusion_approved_by": null
  },
  "linked_decisions": ["decisions.jsonl entry ids that this experiment owns"]
}
```

### Sample Size and Conclusion Rules

Sample size is workflow-dependent and declared per experiment. Defaults set in `experiments/config.json`:
- **Cheap, fast workflows** (low cost, short runtime): 10–20 runs.
- **Expensive, slow workflows**: 3–5 runs.

**Conclusions use heuristic rules, not pretend-statistics.** With small N, real significance testing is theatre. Default heuristics (tunable):
- **Kept**: post-mean improved in declared direction by ≥X% of declared expected magnitude AND ≥70% of post-runs individually beat baseline mean.
- **Reverted**: post-mean got worse, OR ≥50% of post-runs were worse than baseline-mean's worst.
- **Inconclusive**: post-mean within noise band of baseline. Requires explicit decision: extend window, accept change as neutral, or revert.
- **Extended**: not enough runs yet.

### Concurrent Experiments

Default: one active experiment per metric. Multiple experiments may run concurrently only if they target disjoint metric sets. For Type 3 batch rule additions ("rules-2026-04-26"), multiple rules can land in one experiment with per-rule breakdown captured for later analysis.

### LLM-as-Experimenter Guardrails

The agent that proposes is not the agent that concludes:
- **Proposer**: writes hypothesis, picks target metric, applies the intervention. Prompt at `experiments/proposer_prompt.md`.
- **Evaluator**: separate LLM call (or human). Reads only the pre-registered hypothesis and the measurement-window data. Prompt at `experiments/evaluator_prompt.md`. Does not see the proposer's reasoning.

The hypothesis is **pre-registered** — written to `active.json` before any post-intervention data is collected. Post-hoc redefinition of "what success looks like" is rejected by the evaluator.

### Confounds and Limitations (acknowledged in the UI)

- **World drift**: source data, MCP server behavior, model versions change during the measurement window. `world_state` snapshots flag large drift; conclusions get a confidence dim.
- **No interleaved control**: would require per-run toggling of the change. Out of scope.
- **Small N**: conclusions are heuristic. UI displays N and confidence prominently rather than hiding it.
- **Goodhart still applies**: the loop optimizes declared metrics. Proxy metrics get gamed. Periodic human review of metric definitions is the only durable defense.

### Loop Behavior

After conclusion, the next hypothesis is chosen by the optimizer (running on `/improve-continuously`'s slower schedule):
1. Prioritize under-served metrics (no recent experiments targeting them).
2. Prioritize metrics farthest from target / closest to SLO floor breach.
3. Avoid retrying recent failures within a cooldown window.
4. **Stop conditions**:
   - All metrics within target/floor band → loop pauses, monitors for drift.
   - N successive inconclusive experiments → loop pauses, asks user for direction.
   - Cost budget reached → loop pauses, asks user.

### Effect on `/improve-*` Commands

`/improve-*` commands no longer apply changes immediately. Instead they **open an experiment**: capture the hypothesis, apply the change as a revertable diff, start the measurement window. Until the experiment concludes, the change can be reverted automatically.

New commands:
- `/abort-experiment` — revert the active experiment now.
- `/extend-experiment` — extend measurement window.
- `/conclude-experiment` — manually render a verdict (overrides evaluator).

`/improve-continuously` becomes the loop runner.

---

## User Oversight and Control

The framework is transparent and overridable. The LLM proposes; the user disposes. Every LLM-produced artifact (hypothesis, conclusion, metric definition, rule) is visible, editable, and rejectable.

### Oversight Modes (per workflow)

Each workflow declares an oversight mode. Stored on `workflow.json` as `oversight_mode`.

| Mode | Hypothesis approval | Conclusion approval | Direct edits |
|---|---|---|---|
| **Manual** | Required before measurement starts | Required before commit | All edits free |
| **Supervised** (default) | Auto for low-risk, required for high-risk | Required for "kept" verdicts that change metric defs or remove rules | All edits free |
| **Autonomous** | Auto | Auto | User reviews afterward |

**High-risk** experiments are those that: redefine an existing metric, remove a user-supplied rule, change `evaluation_plan.json` structure, or change `success_criteria`. Configurable list in `experiments/config.json`.

Recommended defaults: Type 1 → Manual, Type 2 → Autonomous, Type 3 → Supervised.

### What the User Can See

Everything the LLM produces is inspectable in the UI:

- **Active experiments panel** — current hypothesis, target metric, runs N/M, real-time observed values, world-state snapshot.
- **Decision feed** — every entry in `builder/decisions.jsonl` and `context/clarifications.jsonl`, with source (agent/user), trigger, and target metrics.
- **Metric trajectory charts** — per metric, with experiment windows shaded and decision/rule markers as vertical lines.
- **Conclusion details** — the evaluator's full rationale, the pre-registered hypothesis it was checked against, the data window used, the heuristic that produced the verdict.
- **LLM prompt audit** — for any agent-produced artifact, the prompt and model used to produce it. Surfaced behind a "show prompt" affordance, not always visible.
- **Experiment history** — every concluded experiment, filterable by metric, verdict, or date.

### What the User Can Override

Direct controls (UI affordances writing back to the workflow files):

- **Edit metric definitions** — change `unit`, `direction`, `target`, `floor`, `source`. Edits are versioned: prior series preserved, new series begins, with a marker on the chart.
- **Add / disable / edit rules** in `context/rules.md`. Rule disabling triggers an experiment if the rule had a tracked target metric.
- **Pause / abort / extend / conclude** experiments. Manual conclusion overrides the evaluator and is logged as such.
- **Reject hypothesis** before measurement starts (Manual / Supervised modes). Optional rationale gets written into history with verdict `rejected_by_user`.
- **Edit decision records** — corrections only; original is preserved with `edited_at` and `edited_by` fields, edit history is appended.
- **Pin / forbid hypotheses** — tag a hypothesis as "do not retry" so the optimizer skips it.
- **Set focus** — designate which metrics the optimizer should prioritize next.
- **Edit proposer / evaluator prompts** at `experiments/proposer_prompt.md` and `experiments/evaluator_prompt.md`. Changes the LLM's reasoning style for future experiments.
- **Hard guardrails** in `experiments/config.json`: max concurrent experiments, max cost per experiment, blackout windows (e.g., no experiments during business hours), forbidden file paths.

### Audit Trail

Every override is itself a decision record:

```json
{"ts":"...","source":"user","trigger":"override","action":"abort_experiment","target":"exp-2026-04-26-001","rationale":"world drift makes this measurement window untrustworthy"}
{"ts":"...","source":"user","trigger":"override","action":"edit_metric","target":"audit.accuracy","change":"raised target from 90 to 95","rationale":"updated SLA"}
{"ts":"...","source":"user","trigger":"override","action":"reject_hypothesis","target":"exp-2026-04-26-002","rationale":"hypothesis is poorly specified; pin and forbid"}
```

The user's authority is logged with the same shape as the agent's authority. Reading `decisions.jsonl` with `source: user` filter gives the full history of human steering.

### Trust Calibration

The UI surfaces honesty signals so the user can decide how much to trust each conclusion:

- **N (sample size)** — shown prominently. Small N gets a visible "low confidence" badge.
- **World drift indicator** — if model/MCP/source versions changed materially during the window, the conclusion is flagged.
- **Heuristic used** — show which conclusion rule fired (kept/reverted/inconclusive) and why.
- **Proposer ≠ evaluator confirmation** — show that separation is in effect (otherwise warn).

These signals are visible by default. The framework refuses to render conclusions as confident if the underlying evidence is weak.

---

## Per-Type Detail

### Type 1 — Deterministic

The workflow's steps are known. The job is to execute reliably and catch drift.

**`success_criteria`** — prose, plan-level. Stable across the workflow's life.
**Eval** — hard checks: existence, exact match, schema validation. Pass/fail.
**Metrics** — SLO-mode. `floor` for things that should stay high (success rate), `ceiling` for things that should stay low (cost, latency). Alerts on degradation, not trajectory.
**Decisions log** — light. Mostly incident/repair entries when something drifts.
**`/improve-*` commands** — most are warning-gated or hidden. Restructuring a deterministic workflow that's working is high risk. Surface `/monitor` and `/repair` instead.
**UI** — SLO board (red/green tiles), regression timeline, alert list.

### Type 2 — Exploratory

The workflow's structure is being discovered. We don't know the right plan yet.

**`success_criteria`** — prose, plan-level, expected to evolve.
**Eval** — starts as LLM-judge or human review. Hard checks added as patterns emerge.
**Metrics** — `metrics.json` is **deferred**. Adopting metrics before patterns stabilize is premature commitment that channels iteration in a wrong direction. Add metrics later, when something stable enough to measure has emerged.
**Decisions log** — primary artifact. Captures hypothesis → outcome journey. This is where the value lives — the history of what was tried and why.
**`/improve-*` commands** — heavy users. Plan, eval, KB, learnings can all change.
**UI** — decision log + per-eval-step score chart + plan/eval version history (because they change).

### Type 3 — Context-accumulating

The workflow has a stable shell, but business context grows continuously. Each rule the user adds shapes future runs.

**`success_criteria`** — prose, plan-level. Mostly stable.
**Eval** — hard checks + rule conformance (auto-derived from `context/rules.md`) + human review.
**Metrics** — **required and load-bearing.** Without metrics, accumulating context is just accumulating complexity. Each new rule must declare what it's meant to move. Hierarchy comes back here for grouping/navigation only — no weighted rollup.
**Decisions log** — both `builder/decisions.jsonl` (agent) and `context/clarifications.jsonl` (user). Both require `target_metrics`.
**`/improve-*` commands** — agent self-improvement on plan/eval. Plus a new `/capture-context` for user-driven rule additions.
**UI** — rules viewer, rule-violation chart, decision log split by source, **metric coverage view** (under-served metrics, cargo-cult metrics).

---

## UI Surfaces (per type)

### Type 1 — SLO Board

- Tiles: one per SLO metric, current value vs floor/ceiling.
- Red/green status, regression timeline (when did we last cross the floor?).
- Alert list: active alerts, recent acknowledgments.
- Decisions feed at the bottom: mostly repair entries.

### Type 2 — Trajectory + Decisions

- Per-eval-step score chart over runs (zero schema change required — uses existing eval scores).
- Decision log timeline with markers on the chart.
- Plan/eval version history (because they change frequently).
- Optional metrics chart if `metrics.json` has been adopted.

### Type 3 — Metric Coverage + Rules

- **Goal tree view**: parent/child metrics, each plotted independently. No fake composite percentage.
- **Per-metric view**: chart over runs, decisions/rules targeting this metric annotated as markers.
- **Coverage matrix**: rows = metrics, columns = decisions/rules, cells = which decisions claim to move which metrics.
  - **Under-served metrics** (rows with no decisions): red flag — declared a goal you're not working toward.
  - **Cargo-cult metrics** (rows with many decisions, flat chart): red flag — rules aren't doing what people think.
- Rules viewer: `context/rules.md` rendered, each rule linked to its `clarifications.jsonl` entry.
- Decision log split by source (agent vs user).

---

## `/improve-*` Command Behavior (per type)

| Command | Type 1 | Type 2 | Type 3 |
|---|---|---|---|
| `/improve-workflow` | Warning-gated; encourages monitoring instead | Heavy use, may replan | Available, must declare `target_metrics` |
| `/improve-eval` | Light use; mostly add new checks | Heavy use, eval evolves | Available, must declare `target_metrics` |
| `/improve-kb` | Available | Available | Available |
| `/improve-learnings` | Available | Available | Available |
| `/improve-report` | Available | Available | Available |
| `/improve-continuously` | Optimizer schedule conservative | Optimizer schedule active | Optimizer schedule + context-review prompts |
| `/capture-context` (NEW) | N/A | N/A | **Primary user command.** Adds a rule/clarification, requires `target_metrics`. |
| `/monitor` (NEW) | **Primary command.** Surfaces SLO breaches and drift. | Available | Available |
| `/repair` (NEW) | Diagnose + fix on SLO breach | Available | Available |

All `/improve-*` commands write a `builder/decisions.jsonl` record automatically when they apply changes. This keeps the structured log honest without builder discipline.

---

## What This Framework Explicitly Does Not Do

These were considered and rejected. Documenting the rejections so they don't get reintroduced.

1. **No weighted rollup tree with composite "main metric" percentage.**
   Weights are made up; rolling up `percent` and `days` requires a normalization choice nobody trusts. This produced the existing 100/100 false-positive failure mode at the eval layer; a metric-tree rollup would reproduce it one layer up. Hierarchy is for grouping/navigation only.

2. **No `expected_delta` field on decisions.**
   Asking the agent to predict impact in advance produces confident, baseless numbers.

3. **No silent `observed_delta` auto-attribution.**
   Single-run deltas are noise-shaped. The framework's answer is the experiment loop: pre-registered hypothesis + N-run measurement window + heuristic verdict. Deltas are still measured — but always inside an explicit experiment frame, never silently attributed to a lone decision in run-stream noise.

4. **No replacement of `success_criteria` with structured fields.**
   `success_criteria` stays as plan-level prose. It's the workflow's north star statement; structuring it adds friction without value. Per-step `success_criteria` is deprecated and should be ignored.

5. **No replacement of `builder/improve.md`.**
   The prose log carries narrative that doesn't fit flat records. `decisions.jsonl` is a sidecar, not a replacement.

6. **No forced metric definition for Type 2 workflows.**
   Forcing metrics before patterns stabilize channels iteration into the wrong place.

7. **No LLM-judge optimizer reading observed deltas to choose next moves.**
   This is the textbook recipe for Goodharting. If introduced later, it needs guardrails (cap on eval-definition changes, frozen-metric windows, human sign-off on metric redefinitions) — none of which are in scope here.

---

## Implementation Order

Each step ships independently and is useful on its own. Land in this order:

1. **Chart existing eval scores over runs.** Zero schema change. Uses existing `/scores/evaluation/{group}/{date}.json` time series. Biggest signal-per-effort, validates the trajectory hypothesis before more infra is built.

2. **Add `decisions.jsonl` writer to `/improve-*` commands.** Each command appends a record when it applies changes. `target_metrics` optional at this stage. Decisions feed visible in UI.

3. **Add `workflow_type` field + `oversight_mode` field** to `planning/plan.json` and `workflow.json`. Default existing workflows to `exploratory` + `supervised`. Builder/UI starts gating on type and mode.

4. **Add optional `metrics.json` + `source` resolver.** Implement `eval_step` source first; `telemetry` and `external` later. Render markers from `decisions.jsonl` on the metric charts.

5. **Add the experiment loop scaffolding.** `experiments/active.json`, `history.jsonl`, `config.json`. `/improve-*` commands evolve from "apply" to "open experiment." `/abort-experiment`, `/extend-experiment`, `/conclude-experiment` commands. Proposer/evaluator prompt files with reasonable defaults.

6. **Add user-control affordances in UI**: edit metrics, edit rules, pause/abort/extend/conclude experiments, reject hypotheses, pin/forbid, set focus, edit proposer/evaluator prompts. Every override writes a `decisions.jsonl` record.

7. **Add `/capture-context` and the `context/` store** (Type 3 only). Enforce `target_metrics` on Type 3 decisions/clarifications.

8. **Add `/monitor` and `/repair`** for Type 1.

9. **Add metric coverage matrix view** for Type 3.

10. **Wire `/improve-continuously` to the experiment loop runner.** Stop conditions, cooldowns, focus selection.

Stop and reassess after step 4. Steps 1–4 deliver the auditable trajectory + decisions for Types 1 and 2 without requiring the experiment loop. Steps 5–6 add the loop and oversight. Steps 7–10 layer on Type 3, monitoring, and continuous improvement.

---

## Migration

Existing workflows are unaffected by default:
- `workflow_type` defaults to `exploratory` if absent.
- `metrics.json`, `decisions.jsonl`, `context/` are all opt-in.
- `success_criteria` and `builder/improve.md` keep working as today.

The deprecated step-level `success_criteria` field is ignored by the framework. Residual code reads remain (workflow.go, instructions.go) but should be cleaned up in a separate cleanup PR; the framework does not depend on them.

---

## Related Docs

- `evaluation_system.md` — how eval steps are run and scored today.
- `learning_architecture.md` — learnings/KB layer (separate from this framework).
- `workflow_manifest_architecture.md` — how `workflow.json` and `plan.json` are structured.
- `workflow_builder_commands_and_tools.md` — the existing `/improve-*` slash commands.
- `persistent_stores_design.md` — earlier design notes on db/, reports/, KB graph.
