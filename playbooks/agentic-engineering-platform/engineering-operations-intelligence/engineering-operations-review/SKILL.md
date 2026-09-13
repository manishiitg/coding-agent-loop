---
name: engineering-operations-review
description: Build a recurring AgentWorks engineering operations review from governed delivery, quality, and reliability intelligence. Use for daily, weekly, monthly, or leadership review workflows with tracked actions.
---

# Engineering Operations Review

## Outcome

Create a repeatable engineering review that explains material changes, highlights supported risks and bottlenecks, tracks prior actions, and prepares audience-appropriate recommendations and delivery.

## When to use

Use after governed engineering metrics and findings are available. Run on demand first; add a schedule only after the same scope, review, report, and delivery behavior succeeds unattended.

## Required inputs

Resolve review cadence/period, audience, team/repository/service scope, comparison window, required sections, significance rules, action owners, prior-review follow-up policy, sensitive-data rules, delivery destinations, notification conditions, and approval policy.

## Plan and AgentWorks tools

Use a scripted step to freeze the period snapshot, expected sections, data-quality status, and prior actions. Use one message sequence to verify material findings, explain evidence and limits, and draft actions. Use a human branch before external delivery or issue creation when required, followed by deterministic delivery and receipt persistence.

## Knowledge and persistence

Store review snapshots, sections, findings, recommendations, decisions, actions, follow-ups, and delivery receipts in durable tables. Keep audience preferences and definitions in KB context. Reports link to governed metrics and source evidence without copying restricted content.

## Validation and reporting

Require all expected sections, current data-quality status, comparison provenance, material-change evidence, prior-action accounting, owners for accepted actions, and delivery receipts when applicable. The dashboard preserves historical reviews and recommendation outcomes.

## Guardrails

Do not invent explanations, shame or rank individuals, omit negative trends, resend unchanged alerts, publish restricted data, create work items without configured authorization, or present stale/incomplete data as current health.

## Read details when needed

- [Operations data model](../references/operations-data-model.md): identity, lineage, and metric governance.
- [Review workflow](references/review-workflow.md): snapshot, synthesis, approval, delivery, and acceptance cases.
- [Example review policy](examples/review-policy.json): fictional cadence and content contract.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and review-policy revisions, period/scope/audience, customer overrides, data-quality status, review/report location, proposed and accepted actions, approval/delivery receipts, schedule when any, capability resolution, and limitations.
