---
name: application-security-assessment-remediation
description: Build an authorized AgentWorks AppSec workflow covering scoped web, API, code, dependency, secret, and configuration assessment through reviewed remediation and verified closure. Use for application security and bounded pentesting.
---

# Application Security Assessment and Remediation

## Outcome

Create one governed AppSec workflow that inventories authorized attack surface, runs applicable assessment routes, validates findings, prepares fixes, obtains required decisions, and verifies remediation against the deployed result.

## When to use

Use for authorized application assessment, CI security gates, bounded pentests, vulnerability intake, and remediation. Select only relevant routes. Browser assessment may reuse verified Browser QA setup; code-only work does not require it.

## Required inputs

Resolve written scope, targets/environments/repositories/APIs, rules of engagement, allowed techniques and side effects, credentials/test data, rate/time limits, stop conditions, control/severity policy, source and deployment identity, evidence restrictions, owners, approval/risk-acceptance policy, and retest/closure requirements.

## Plan and AgentWorks tools

Use webhook/manual/Slack intake and a scripted scope gate. Route to browser/API dynamic checks, code analysis, or dependency/secret/configuration checks. Use scripted steps for repeatable tools, normalization, policy checks, fix validation, and retest; use a message sequence for attack-surface reasoning and evidence-based triage. Branch to dismiss, remediate, accept risk, escalate, or hold. Require configured decisions before intrusive tests, external writes, and protected changes.

## Knowledge and persistence

Store scopes, targets, runs, checks, observations, findings, evidence, decisions, fixes, deployments, retests, risk acceptances, and delivery receipts in durable tables/assets. Keep approved policies and non-sensitive verified facts in scoped KB notes. Keep exploit-sensitive details restricted.

## Validation and reporting

Require exact authorized target/build/commit, complete selected-check inventory, evidence freshness, reproducibility, confidence, customer severity, owner, and terminal disposition. The dashboard shows route coverage, findings by severity/confidence/status, age/SLA, target/owner, evidence access, fix PR/deployment, retest, risk-acceptance expiry, and history.

## Guardrails

Never exceed scope, infer production permission, perform credential attacks, denial of service, persistence, destructive payloads, or data exfiltration, expose secrets, treat scanner output as confirmed, weaken tests to clear findings, auto-accept risk, or close without deployed retest evidence.

## Read details when needed

- [Workflow design and outcomes](../../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Scope and authorization](references/scope-and-authorization.md): rules of engagement and scope gate.
- [Assessment routes](references/assessment-routes.md): choose browser, API, code, dependency, secret, and configuration paths.
- [Findings and remediation](references/findings-remediation-verification.md): validate, fix, approve, deploy, and retest.
- [Triggers, Slack, and dashboard](references/triggers-slack-dashboard.md): workflow integrations and reporting.
- [Recommended tools and skills](references/recommended-tools-and-skills.md): select MCPs, CLIs, and reviewed public skills without duplicating capability.
- [Example policy](examples/application-security-policy.json): fictional setup contract.
- [Catalog metadata](playbook.json): setup and optional recommendations.

## Completion contract

Return installed playbook/policy revisions, authorization and selected routes, target/source/deployment identities, coverage/results, findings/evidence/review, remediation/decision/deployment/retest receipts, risk acceptances, dashboard location, capability resolution, and limitations.
