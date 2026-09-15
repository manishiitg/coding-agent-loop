---
name: growth-data-foundation
description: Build a source-linked growth data foundation in AgentWorks for an AI Growth Analyst. Use when traffic, product, billing, and feedback data need trustworthy normalized analysis of acquisition, activation, retention, and revenue.
---

# Growth Data Foundation

## Outcome

Create a durable, refreshable customer data model that links anonymous visitors to users and accounts across traffic sources, product events, subscriptions and revenue, and feedback, with source provenance and synchronization health.

## When to use

Use before funnel, activation, retention, revenue, or experiment analysis. Reuse an existing governed warehouse or product-analytics project when it provides equivalent identity, event quality, freshness, and provenance. It does not define funnels, cohorts, experiments, or metric targets; downstream playbooks own their analysis.

## Discovery and user direction

Inspect the current workflow, data sources, goals, metrics, capabilities, stores, dashboards, and schedules before proposing changes. Summarize reusable foundations and gaps, ask focused questions for unresolved scope, privacy, ownership, freshness, and success criteria, and record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve product and pricing model, domains and apps, authorized analytics/billing/feedback sources and read scopes, event taxonomy owners, identity and account rules, history window, refresh policy, source-of-truth precedence, retention, timezone, KPI vocabulary, consent and privacy policy, and restricted fields.

## Plan and AgentWorks tools

Use scripted steps for API/SDK ingestion, pagination, event validation, identity stitching, idempotent upserts, cursor management, and reconciliation. Split sources only for different credentials, rate limits, or failure domains. Use a message sequence for bounded identity or taxonomy ambiguities and documented data-quality investigation. Prove an on-demand sync first, then recommend scheduled refresh with explicit cadence, timezone, overlap, and notification policy.

## Knowledge and persistence

Store source records, normalized visitors/users/accounts/events/subscriptions, identity links, sync runs, cursors, and data-quality findings in declared DB tables. Keep taxonomy decisions and exclusions in KB context; store verified source quirks in KB notes or narrow learnings. Never rely on builder chat memory.

## Validation and reporting

Validate source counts, event volumes, identity match rates, revenue reconciliation against billing samples, freshness, cursor movement, and deduplication. The dashboard shows coverage, lag, failures, unmapped identities, invalid events, and lineage before exposing downstream growth metrics as trusted.

## Guardrails

Default integrations to read-only. Do not silently merge identities, overwrite source history, store PII beyond consent, treat missing events as zero behavior, or backfill production analytics without approval.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Growth data model](../references/growth-data-model.md): shared entities, identity, lineage, and metric governance.
- [Foundation workflow](references/data-foundation-workflow.md): ingestion, AgentWorks steps, persistence, and acceptance cases.
- [Example source map](examples/source-map.json): fictional integration and mapping shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and schema/mapping revisions, source/capability resolution, customer overrides, identity rules, sync coverage and freshness, reconciliation result, DB/report locations, restricted or unmapped data, and unresolved blockers.
