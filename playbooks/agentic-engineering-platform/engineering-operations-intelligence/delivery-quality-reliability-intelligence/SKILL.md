---
name: delivery-quality-reliability-intelligence
description: Build evidence-backed AgentWorks intelligence over engineering delivery, quality, and reliability data. Use to identify bottlenecks, regressions, recurring risks, and supported improvement opportunities.
---

# Delivery, Quality, and Reliability Intelligence

## Outcome

Create versioned team and system-level metrics, trends, and evidence-backed findings that explain engineering flow, quality, and reliability without reducing people to activity scores.

## When to use

Use after Engineering Data Foundation is reconciled and fresh enough for the requested scope. Use it for operational decisions, improvement tracking, release risk, and recurring engineering-health analysis.

## Required inputs

Resolve teams/repositories/services, analysis window and comparison period, customer metric definitions, workflow-state mapping, business calendar, targets or thresholds, exclusions, minimum data quality, reporting audience, and decision ownership.

## Plan and AgentWorks tools

Use scripted steps for deterministic snapshots, metric calculations, cohorts, trend comparisons, and completeness checks. Use a message sequence to interpret supported changes, inspect source evidence, test alternative explanations, and propose bounded actions. Keep publication behind configured review.

## Knowledge and persistence

Store metric definitions, snapshots, dimensions, findings, evidence links, recommendations, and follow-up outcomes in durable tables. Keep customer definitions/preferences in KB context and verified interpretation rules in notes or learnings. Preserve links to source records rather than copying private content.

## Validation and reporting

Validate input freshness, expected scope, denominators, null/missing handling, metric reproducibility, comparison compatibility, and evidence for every finding. The dashboard exposes definitions, lineage, confidence, limitations, trends, and drill-downs.

## Guardrails

Do not rank individuals, use commits or lines of code as productivity, infer causation from correlation, hide missing data, compare incompatible teams/windows, or recommend consequential process changes without evidence and ownership.

## Read details when needed

- [Operations data model](../references/operations-data-model.md): shared entities, lineage, identity, and metric governance.
- [Intelligence workflow](references/intelligence-workflow.md): metrics, analysis, reporting, and acceptance cases.
- [Example metric policy](examples/metric-policy.json): fictional definitions and decision rules.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and data/metric policy revisions, scope and comparison windows, customer overrides, data-quality status, calculated indicators, evidence-backed findings, report locations, capability resolution, and unresolved limitations.
