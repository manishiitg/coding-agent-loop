---
name: funnel-conversion-intelligence
description: Build evidence-backed funnel and conversion analysis in AgentWorks for an AI Growth Analyst. Use to find where signup-to-purchase funnels drop, detect conversion changes, and attribute them to segments, pages, devices, or sources.
---

# Funnel and Conversion Intelligence

## Outcome

Create versioned funnels, conversion trends, and evidence-backed investigations that show where customers drop, which segment or surface caused a conversion change, and what session evidence supports it.

## When to use

Use after Growth Data Foundation is reconciled and fresh enough for the requested scope. Use it for acquisition quality, funnel drops, conversion regressions, launch readouts, and recurring conversion monitoring. It does not own lifecycle cohorts, activation scoring, or retention analysis; those belong to Activation and Retention Intelligence.

## Discovery and user direction

Inspect the current workflow, goals, metrics, source coverage, capabilities, stores, dashboards, and schedules before proposing changes. Summarize reusable foundations and gaps, ask focused questions for unresolved scope, definitions, segments, thresholds, ownership, and success criteria, and record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve funnels and step definitions, analysis and comparison windows, segments and dimensions, traffic and campaign scope, conversion targets or thresholds, session-replay consent and sampling policy, minimum data quality, reporting audience, and decision ownership.

## Plan and AgentWorks tools

Use scripted steps for deterministic funnel snapshots, conversion calculations, segment breakdowns, change detection, and completeness checks. Use a message sequence to attribute supported changes, inspect session evidence, test alternative explanations, and propose bounded next actions. Prove an on-demand investigation first, then recommend scheduled monitoring with explicit scope, cadence, timezone, and notification policy. Keep publication behind configured review.

## Knowledge and persistence

Store funnel definitions, snapshots, segments, investigations, session references, findings, recommendations, and follow-up outcomes in durable tables. Keep customer definitions/preferences in KB context and verified interpretation rules in notes or learnings. Preserve links to source records and sessions rather than copying private content.

## Validation and reporting

Validate input freshness, expected scope, denominators, segment compatibility, comparison windows, session-evidence linkage, and evidence for every finding. The dashboard exposes funnel steps, drop-offs, trends, segment breakdowns, confidence, limitations, and drill-downs into sessions.

## Guardrails

Do not infer causation from correlation, compare incompatible segments/windows, report a conversion change without its denominator, expose non-consented session content, or recommend consequential changes without evidence and ownership.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Growth data model](../references/growth-data-model.md): shared entities, identity, lineage, and metric governance.
- [Conversion intelligence workflow](references/conversion-intelligence-workflow.md): funnels, change detection, attribution, sessions, and acceptance cases.
- [Example conversion investigation](examples/conversion-investigation.json): fictional finding and evidence shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and funnel/metric policy revisions, scope and comparison windows, customer overrides, data-quality status, calculated funnels and trends, evidence-backed findings with session references, report locations, capability resolution, and unresolved limitations.
