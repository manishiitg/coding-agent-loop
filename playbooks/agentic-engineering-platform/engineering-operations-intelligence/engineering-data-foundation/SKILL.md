---
name: engineering-data-foundation
description: Build a source-linked engineering operations data foundation in AgentWorks. Use when issues, pull requests, CI, deployments, QA, security, performance, and incidents need trustworthy normalized analysis.
---

# Engineering Data Foundation

## Outcome

Create a durable, refreshable engineering data model that links work items, code changes, CI, builds, deployments, quality assessments, and incidents with source provenance and synchronization health.

## When to use

Use before engineering operations metrics, reviews, or cross-system automation. Reuse an existing governed warehouse/model when it provides equivalent identity, freshness, and provenance.

## Required inputs

Resolve organizations, teams, repositories, projects, environments, source systems and read scopes, history window, refresh policy, source-of-truth precedence, identity mappings, retention, timezone, metric vocabulary, and restricted fields.

## Plan and AgentWorks tools

Use scripted steps for API/SDK ingestion, pagination, normalization, idempotent upserts, cursor management, and reconciliation. Split sources only for different credentials, rate limits, or failure domains. Use a message sequence for bounded mapping ambiguities and documented data-quality investigation.

## Knowledge and persistence

Store source records, normalized entities/events, links, sync runs, cursors, and data-quality findings in declared DB tables. Keep customer definitions and exclusions in KB context; store verified source quirks in KB notes or narrow learnings. Never rely on builder chat memory.

## Validation and reporting

Validate source counts, freshness, cursor movement, stable identity, relationship coverage, deduplication, and reconciliation against source samples. The dashboard shows coverage, lag, failures, unmapped records, and lineage before exposing downstream metrics as trusted.

## Guardrails

Default integrations to read-only. Do not infer people or repository mappings silently, overwrite source history, expose private content, create individual productivity scores, or treat missing data as zero activity.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Operations data model](../references/operations-data-model.md): shared entities, lineage, identity, and metric governance.
- [Foundation workflow](references/data-foundation-workflow.md): ingestion, AgentWorks steps, persistence, and acceptance cases.
- [Example source map](examples/source-map.json): fictional integration and mapping shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and schema/mapping revisions, source/capability resolution, customer overrides, sync coverage and freshness, reconciliation result, DB/report locations, restricted or unmapped data, and unresolved blockers.
