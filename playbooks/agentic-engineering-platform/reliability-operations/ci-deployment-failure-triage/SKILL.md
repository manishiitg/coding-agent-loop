---
name: ci-deployment-failure-triage
description: Build an AgentWorks workflow that ingests, classifies, and routes CI and deployment failures with durable evidence. Use for pipeline failure triage, safe reruns, and owner notification.
---

# CI and Deployment Failure Triage

## Outcome

Create a repeatable workflow that receives CI/deployment failures, establishes exact change and environment identity, gathers evidence, classifies likely cause, performs policy-safe actions, and routes a concise finding to the right owner.

## When to use

Use for failed, cancelled, stuck, or unhealthy build, test, release, and deployment events. Escalate active customer impact into the Incident Investigation playbook rather than treating it only as pipeline failure.

## Required inputs

Resolve CI/CD providers, repositories/services/environments, event schemas, trigger and polling paths, pipeline/stage identity, log/artifact access, ownership, retry policy, failure taxonomy, severity/escalation rules, retention, notification destinations, and restricted-data policy.

## Plan and AgentWorks tools

Use an authenticated webhook trigger for event-driven starts and a scripted step to validate/deduplicate input, fetch artifacts, redact evidence, and persist identity. Use a message sequence to compare hypotheses. Branch deterministically to safe rerun, owner routing, incident escalation, or hold. Use the Slack bot for threaded investigation and configured decisions.

## Knowledge and persistence

Store failures, attempts, jobs/stages, changes, classifications, evidence pointers, known signatures, actions, and delivery receipts in durable tables/assets. Keep approved taxonomies and runbooks in KB context; promote a signature only after verification.

## Validation and reporting

Require exact provider, pipeline/run, stage/job, repository/commit/build, service/environment, source event, and evidence freshness. The dashboard shows failure class, affected stage/service, owner, action/status, repeat count, evidence, trigger receipt, and trends with provider/repository/environment filters.

## Guardrails

Do not expose secrets from logs, rerun deterministic failures repeatedly, deploy around a failed gate, invent a root cause, alter source or infrastructure without the configured review, or mark a pipeline green from an agent explanation.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Triage workflow](references/triage-workflow.md): evidence collection, classification, actions, and test cases.
- [Reliability event contract](../references/reliability-event-contract.md): shared identity, evidence, state, and metrics.
- [Triggers, webhooks, and Slack](../references/triggers-webhooks-and-slack.md): integration and interaction rules.
- [Example triage record](examples/triage-record.json): fictional durable result.
- [Catalog metadata](playbook.json): setup and optional recommendations.

## Completion contract

Return installed playbook/policy revisions, trigger and source mappings, failure identity/classification/confidence, evidence locations, action/escalation and receipts, dashboard location, Slack routing when configured, capability resolution, and limitations.
