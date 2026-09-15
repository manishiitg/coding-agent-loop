---
name: activation-retention-intelligence
description: Build evidence-backed activation, retention, and feature-adoption analysis in AgentWorks for an AI Growth Analyst. Use to find what makes customers successful, why cohorts retain or churn differently, and whether features move retention or revenue.
---

# Activation and Retention Intelligence

## Outcome

Create versioned activation, cohort-retention, and feature-adoption analysis that shows which behaviors predict customer success, why cohorts diverge, and how feature use and feedback relate to retention and revenue.

## When to use

Use after Growth Data Foundation is reconciled and fresh enough for the requested scope. Use it for onboarding improvement, churn investigation, feature launch readouts, pricing/packaging questions, and recurring lifecycle monitoring. It does not own acquisition funnels or conversion-change attribution; those belong to Funnel and Conversion Intelligence.

## Discovery and user direction

Inspect the current workflow, goals, metrics, source coverage, capabilities, stores, dashboards, and schedules before proposing changes. Summarize reusable foundations and gaps, ask focused questions for unresolved scope, definitions, cohorts, thresholds, ownership, and success criteria, and record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve activation definition, cohort keys and retention windows, feature and flag inventory, revenue linkage scope, feedback sources and access, comparison cohorts, targets or thresholds, minimum data quality, reporting audience, and decision ownership.

## Plan and AgentWorks tools

Use scripted steps for deterministic activation scoring, cohort retention curves, feature-adoption breakdowns, revenue joins, and completeness checks. Use a message sequence to interpret supported differences, correlate feedback themes, test alternative explanations, and propose bounded next actions. Prove an on-demand analysis first, then recommend scheduled monitoring with explicit scope, cadence, timezone, and notification policy. Keep publication behind configured review.

## Knowledge and persistence

Store lifecycle definitions, snapshots, cohorts, adoption analyses, feedback correlations, findings, recommendations, and follow-up outcomes in durable tables. Keep customer definitions/preferences in KB context and verified interpretation rules in notes or learnings. Preserve links to source records rather than copying private content.

## Validation and reporting

Validate input freshness, expected scope, cohort comparability, survivorship and censoring handling, revenue-join integrity, feedback sampling, and evidence for every finding. The dashboard exposes activation, retention curves, adoption, revenue impact, confidence, limitations, and drill-downs.

## Guardrails

Do not infer causation from correlation, compare incompatible cohorts/windows, report retention without its denominator and censoring rules, quote customers without consent basis, or recommend consequential changes without evidence and ownership.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Growth data model](../references/growth-data-model.md): shared entities, identity, lineage, and metric governance.
- [Lifecycle intelligence workflow](references/lifecycle-intelligence-workflow.md): activation, cohorts, adoption, feedback, and acceptance cases.
- [Example cohort analysis](examples/cohort-analysis.json): fictional finding and evidence shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and lifecycle/metric policy revisions, scope and comparison cohorts, customer overrides, data-quality status, calculated indicators, evidence-backed findings with feedback references, report locations, capability resolution, and unresolved limitations.
