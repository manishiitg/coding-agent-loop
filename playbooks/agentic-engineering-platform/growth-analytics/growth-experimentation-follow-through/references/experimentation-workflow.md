# Experimentation workflow

## Ground hypotheses in evidence

Every hypothesis records its source finding, evidence links, confidence, target KPI with pre-registered target and readout window, guardrail metrics, expected impact, cost, owner, and status. Reject hypotheses without a linked finding or with incompatible evidence; they return to intelligence as open questions, not to the backlog.

Version the prioritization model (impact, confidence, cost weights) and record every input so rankings are reproducible. Never reorder the backlog silently after results arrive.

## Adapt the plan

Use scripted steps for backlog records, prioritization scoring, approved action creation with delivery receipts, rollout-state tracking, KPI snapshots, and readout comparisons against pre-registered targets. Use a message sequence to draft hypotheses from findings, challenge weak evidence, size expected impact, and judge readouts including guardrail checks.

Use a human branch for experiment launches, customer-facing changes, and audience/segment exports. A launch requires the design, target, guardrails, rollout/rollback owner, and readout plan; anything missing stays a blocker. For unattended schedules, persist the proposal, leave it pending, and let a later authorized run consume the saved answer; never hold a blocking call open for a decision that may take hours or days.

For recurring operation, prove one manual hypothesis-to-readout cycle first, then configure scheduled backlog reviews or readout checks with explicit scope, cadence, timezone, and notification conditions.

## Validation and report

Validate hypothesis-evidence linkage, pre-registered targets frozen before launch, guardrail evaluation on every readout, readout-window integrity, KPI reproducibility from durable snapshots, action delivery receipts, and approval records for launches and exports. Verify that underpowered or inconclusive readouts stay visibly inconclusive.

Build a live experimentation dashboard showing the prioritized backlog, active experiments with rollout state, readouts with ship/iterate/kill verdicts, guardrail status, learning history, and follow-up actions. No readout appears trusted when its declared quality gate fails.

## Handoff

Intelligence playbooks consume readout verdicts and learnings as new evidence. Return backlog/policy versions, experiment records, verdicts with KPI deltas and confidence, guardrail outcomes, and recommended follow-ups. Do not require downstream agents to reconstruct experiment history from chat or tool logs.
