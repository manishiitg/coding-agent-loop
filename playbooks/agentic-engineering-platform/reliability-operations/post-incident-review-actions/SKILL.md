---
name: post-incident-review-actions
description: Build an AgentWorks workflow for evidence-backed post-incident review and verified follow-up actions. Use after incident stabilization to learn, assign improvements, and track completion.
---

# Post-Incident Review and Actions

## Outcome

Create a blameless, evidence-backed review that reconstructs impact and response, separates contributing conditions from unsupported claims, creates governed follow-up work, and verifies actions through completion.

## When to use

Use after the incident meets its recovery/stability contract or for an approved retrospective of a near miss. Keep unresolved operational work in the active incident workflow.

## Required inputs

Resolve incident/evidence records, review policy and audience, impact and metric definitions, timeline, participating roles, remediation outcomes, sensitive-data rules, review/approval process, action taxonomy, owners/dates, issue destination, verification requirements, and publication destination.

## Plan and AgentWorks tools

Use scripted steps to freeze the incident snapshot, calculate response milestones, check completeness, create approved work items, and synchronize action status. Use a message sequence for causal/contributing-factor analysis and draft review. Branch for reviewer edit/approve/defer. Use the Slack bot for review discussion and action queries.

## Knowledge and persistence

Store immutable review snapshots, timeline, impact, contributing factors, detection/response analysis, decisions, learnings, actions, owners, external issue IDs, verification, and publication receipts. Promote reusable learning into scoped KB notes with source links and review status.

## Validation and reporting

Require incident and evidence revision, recovery outcome, sourced timeline, impact basis, explicit unknowns, reviewed contributing factors, and owner/verification contract for every accepted action. The dashboard shows review state, milestones, factors, action owner/due/status, overdue risk, verification evidence, recurrence links, and historical themes.

## Guardrails

Do not blame or rank individuals, fabricate certainty, rewrite the incident record, expose restricted evidence, create issues or publish externally without configured authorization, accept vague actions without an owner/outcome, or mark work complete from issue status alone.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Review workflow](references/post-incident-workflow.md): snapshot, analysis, publication, and action verification.
- [Reliability event contract](../references/reliability-event-contract.md): shared evidence, state, action, and metric definitions.
- [Triggers, webhooks, and Slack](../references/triggers-webhooks-and-slack.md): follow-up events and collaboration rules.
- [Example review](examples/post-incident-review.json): fictional review record.
- [Catalog metadata](playbook.json): setup and optional recommendations.

## Completion contract

Return installed playbook/policy revisions, incident and snapshot identity, approved review and publication receipt, sourced findings/unknowns, created actions and external receipts, verification status, KB updates, dashboard location, capability resolution, and limitations.
