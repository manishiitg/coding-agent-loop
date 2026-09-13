---
name: governed-remediation-recovery
description: Build an AgentWorks workflow that prepares, approves, executes, and verifies operational remediation and rollback. Use for controlled incident mitigation and service recovery.
---

# Governed Remediation and Recovery

## Outcome

Create a controlled workflow that turns incident evidence into an exact remediation, validates risk and rollback, obtains required approval, executes through authorized tools, and verifies sustained service recovery.

## When to use

Use when a CI/deployment failure or incident has a supported corrective action. Use investigation-only mode when identity, authorization, rollback, or health evidence is insufficient.

## Discovery and user direction

Inspect the current workflow, goals, metrics, configuration, capabilities, stores, reports, and triggers before proposing changes. Summarize reusable design and gaps, then ask focused questions for unresolved customer choices such as scope, success, approvals, thresholds, ownership, and budgets. Record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve incident/failure identity, target resource/service/environment, current configuration and revision, allowed action/runbooks, access boundaries, blast radius, SLO/health checks, approval matrix, change window, execution path, rollback, stabilization window, and communication policy.

## Plan and AgentWorks tools

Use a message sequence to compare options and prepare an evidence-backed proposal. Persist the exact action, preflight, target state, risk, and rollback as pending review, then end preparation. A separate execution route validates the later durable approval and current target state before protected action, verification, and receipts. Carry review through AgentWorks UI or the configured Slack bot.

## Knowledge and persistence

Store proposals, target/config revisions, validation, risk, approvals, execution/rollback receipts, health samples, decisions, and outcomes in durable tables/assets. Keep approved runbooks and constraints in KB context; never convert an improvised command into an approved runbook silently.

## Validation and reporting

Require exact target and incident, fresh state, allowed action, complete command/diff, dry-run or preflight where supported, risk/blast radius, health and rollback checks, valid approval, and execution identity. The dashboard shows proposed/approved/executed state, owner, target, risk, timestamps, receipts, health recovery, rollback readiness/result, and history.

## Guardrails

Do not infer approval from silence or emoji, broaden target scope, execute stale proposals, bypass canonical deployment/IaC paths, run destructive commands without explicit policy, hide partial failure, retry unsafe actions blindly, or declare recovery before the stability contract passes.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Remediation workflow](references/remediation-workflow.md): proposal, approval, execution, rollback, and recovery checks.
- [Error webhook to recovery](../references/error-webhook-to-recovery.md): end-to-end trigger, basic RCA, resolution, and closure path.
- [Reliability event contract](../references/reliability-event-contract.md): shared action, evidence, and receipt fields.
- [Triggers, webhooks, and Slack](../references/triggers-webhooks-and-slack.md): authenticated starts and interactive approvals.
- [Example remediation](examples/remediation-record.json): fictional proposed action.
- [Catalog metadata](playbook.json): setup and optional recommendations.

## Completion contract

Return installed playbook/policy revisions, incident/target/config identity, considered and selected action, validations and risk, approval receipt, execution/rollback receipts, health/stability result, dashboard location, capability resolution, and limitations.
