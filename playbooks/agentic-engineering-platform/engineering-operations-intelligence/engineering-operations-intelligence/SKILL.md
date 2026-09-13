---
name: engineering-operations-intelligence
description: Build one small-team AgentWorks workflow that connects engineering data, calculates delivery, quality, and reliability intelligence, and produces recurring evidence-backed reviews with tracked actions.
---

# Engineering Operations Intelligence

## Outcome

Create one compact workflow that turns authorized engineering-system data into trusted team metrics, evidence-backed findings, recurring reviews, and tracked improvement actions.

## When to use

Use when a small engineering team wants a shared view of delivery, quality, releases, and reliability without operating a separate data platform and review workflow. Start on demand and add ingestion triggers or a review schedule after the complete route succeeds.

## Discovery and user direction

Inspect the current workflow, goals, metrics, configuration, capabilities, stores, reports, and triggers. Default to one workflow for one small team; split only for incompatible access or lifecycle boundaries. Summarize reuse and gaps, ask focused questions about unresolved scope, definitions, targets, ownership, cadence, and approvals, and record the answers. Installation is guidance, not approval.

## Required inputs

Resolve team/repository/service scope, authorized sources, history and freshness, identity and workflow-state mappings, metric definitions and exclusions, goals or targets, review audience/cadence, action owners, retention, restricted fields, and approval/delivery policy.

## Plan and AgentWorks tools

Use scripted steps to ingest and normalize source records, calculate reproducible metric snapshots, check data quality, freeze review periods, and persist reports. Use a message sequence to investigate supported changes and draft bounded actions. Keep mapping, publication, notification, or work-creation decisions asynchronous: save the proposal with `create_human_input_request`, then let a separate route validate and apply an approved decision. Prove on demand before enabling schedules or webhooks.

## Knowledge and persistence

Store source identities, mappings, sync state, metric definitions/snapshots, findings, reviews, decisions, actions, outcomes, and delivery receipts in durable tables. Keep customer definitions and verified source quirks in scoped KB context with source links; never rely on chat memory.

## Validation and reporting

Validate authorization, freshness, completeness, identity, deduplication, metric reproducibility, comparable windows, evidence for each finding, review completeness, and action receipts. The dashboard shows source health, delivery/quality/reliability trends, limitations, findings, pending decisions, owners, outcomes, and historical reviews without individual ranking.

## Guardrails

Default sources to read-only. Do not infer mappings silently, treat missing data as zero, compare incompatible populations, claim causation from correlation, rank individuals, expose restricted content, invent targets, or publish/create work without configured authorization.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, small-team workflow boundaries, activation, and Pulse focus.
- [Operations data model](../references/operations-data-model.md): shared identity, lineage, and metric governance.
- [Data foundation](references/data-foundation.md): ingestion, normalization, reconciliation, and freshness.
- [Metrics and findings](references/metrics-and-findings.md): governed calculation and evidence-backed investigation.
- [Recurring review](references/recurring-review.md): review, asynchronous decisions, delivery, and tracked actions.
- [Examples](examples/source-map.json): begin with the fictional source map, metric policy, and review policy in this folder.
- [Catalog metadata](playbook.json): setup and optional tool recommendations.

## Completion contract

Return the installed playbook and policy revisions, customer direction, scope and source resolution, data-quality status, metric snapshots and findings, dashboard/review locations, pending or completed decisions/actions, schedule or webhook receipts when enabled, and unresolved limitations.
