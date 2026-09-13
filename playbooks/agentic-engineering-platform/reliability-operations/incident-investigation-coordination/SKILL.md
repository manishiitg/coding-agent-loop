---
name: incident-investigation-coordination
description: Build an AgentWorks workflow for evidence-based incident intake, investigation, coordination, and status. Use for service degradation, outages, and operational incident response.
---

# Incident Investigation and Coordination

## Outcome

Create a governed incident workflow that receives reliability errors, correlates signals, performs basic evidence-backed RCA, establishes impact and severity, coordinates owners, and produces current status without inventing causes.

## When to use

Use for alerts, customer-impact reports, deployment regressions, or manually declared incidents. Use lighter failure triage when there is no service impact and no incident policy trigger.

## Required inputs

Resolve services/environments, incident taxonomy and severity policy, SLOs, monitoring/log/trace/deployment/topology sources, on-call and incident roles, trigger mappings, Slack channels, update cadence/audience, evidence access, escalation rules, and closure criteria.

## Plan and AgentWorks tools

Use webhook/manual/Slack intake, then a scripted step for validation, deduplication, incident identity, timeline, and evidence queries. Use a message sequence for hypotheses and impact analysis. Branch for escalation, remediation proposal, status update, or insufficient data. Keep one Slack thread linked to the durable incident record.

## Knowledge and persistence

Store incidents, signals, affected services, severity decisions, timeline events, hypotheses, evidence, roles, updates, decisions, linked changes, and action receipts in durable tables/assets. Keep approved runbooks, ownership, topology, and definitions in scoped KB context.

## Validation and reporting

Require current incident identity, status/severity basis, affected scope, signal/evidence freshness, role ownership, timeline provenance, and explicit unknowns. The dashboard shows active incidents, impact, severity/status, service/environment, owner, elapsed milestones, hypotheses, recent evidence/actions, update freshness, and history.

## Guardrails

Do not infer causation from correlation, expose restricted evidence, silently change severity, close from one recovered metric, issue contradictory status updates, let Slack replace durable records, or execute remediation without its required approval and validation.

## Read details when needed

- [Incident workflow](references/incident-workflow.md): intake, investigation, coordination, updates, and acceptance cases.
- [Error webhook to recovery](../references/error-webhook-to-recovery.md): default external-error, RCA, resolution, and verification route.
- [Reliability event contract](../references/reliability-event-contract.md): shared identity, evidence, state, and metrics.
- [Triggers, webhooks, and Slack](../references/triggers-webhooks-and-slack.md): event intake and collaboration rules.
- [Example incident record](examples/incident-record.json): fictional durable state.
- [Catalog metadata](playbook.json): setup and optional recommendations.

## Completion contract

Return installed playbook/policy revisions, incident and correlated trigger identities, impact/severity/status, basic RCA with confidence/unknowns, timeline/hypotheses/evidence, roles and Slack thread when configured, actions/decisions, dashboard location, capability resolution, and limitations.
