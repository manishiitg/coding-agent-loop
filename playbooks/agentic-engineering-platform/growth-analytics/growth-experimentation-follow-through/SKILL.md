---
name: growth-experimentation-follow-through
description: Turn growth findings into prioritized experiments and verified outcomes in AgentWorks for an AI Growth Analyst. Use to generate hypotheses, create tracked actions, and confirm whether a shipped change actually improved its KPI.
---

# Growth Experimentation and Follow-Through

## Outcome

Create a prioritized experiment backlog from evidence-backed findings, tracked actions in the customer's tools, and verified readouts that confirm whether each shipped change moved its target KPI.

## When to use

Use after a Growth Analytics intelligence playbook (funnel, lifecycle, SEO, or AI visibility) produces findings worth acting on. Reuse an existing experimentation backlog or growth process when it already tracks hypotheses, owners, and outcomes. It does not ingest sources or define funnels, cohorts, keywords, or visibility prompts; it consumes intelligence findings as evidence.

## Discovery and user direction

Inspect the current workflow, goals, metrics, evidence, capabilities, stores, dashboards, and schedules before proposing changes. Summarize reusable foundations and gaps, ask focused questions for unresolved scope, prioritization, approvals, ownership, and success criteria, and record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve finding sources and confidence, hypothesis format, prioritization model, experiment design standards, action destinations and permissions, rollout and rollback ownership, KPI targets and readout windows, notification policy, and decision ownership.

## Plan and AgentWorks tools

Use scripted steps for backlog records, prioritization scoring, action creation, rollout tracking, KPI snapshots, and readout comparisons. Use a message sequence to draft hypotheses, challenge weak evidence, size expected impact, and judge readouts against pre-registered targets. Keep experiment launch and customer-facing changes behind human approval.

## Knowledge and persistence

Store hypotheses, prioritization inputs, experiment designs, actions, rollout states, KPI snapshots, readouts, and learnings in durable tables. Keep customer process preferences in KB context and verified playbooks-that-worked in notes or learnings. Link every readout to its experiment and evidence.

## Validation and reporting

Validate hypothesis-evidence linkage, pre-registered targets before launch, guardrail metrics, readout-window integrity, KPI reproducibility, and action delivery receipts. The dashboard exposes the backlog, active experiments, readouts with verdicts, guardrails, learnings, and history.

## Guardrails

Do not launch experiments or customer-facing changes without approval, move targets after observing results, declare wins from underpowered readouts, ignore guardrail regressions, or create actions in tools the customer did not authorize.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Growth data model](../references/growth-data-model.md): shared entities, identity, lineage, and metric governance.
- [Experimentation workflow](references/experimentation-workflow.md): hypotheses, prioritization, actions, readouts, and acceptance cases.
- [Example experiment backlog](examples/experiment-backlog.json): fictional hypothesis and readout shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and backlog policy revisions, prioritized hypotheses with evidence links, created actions with delivery receipts, readout verdicts against pre-registered targets, guardrail status, report locations, capability resolution, and unresolved blockers.
