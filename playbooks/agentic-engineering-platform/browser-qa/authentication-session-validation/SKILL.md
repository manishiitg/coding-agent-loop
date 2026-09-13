---
name: authentication-session-validation
description: Build repeatable AgentWorks browser validation for authentication and session behavior. Use for login, logout, MFA, recovery, expiry, refresh, and invalid-session coverage after browser setup.
---

# Authentication and Session Validation

## Outcome

Create repeatable, role-aware tests for the application's approved authentication and session lifecycle with durable results and attempt-scoped evidence.

## When to use

Use after Basic Browser Setup when authentication behavior needs coverage beyond establishing one working test session. Include only flows enabled and authorized for the customer environment.

## Required inputs

Resolve authentication methods, actor roles, approved expectations, account ownership, MFA/recovery channels, session lifetime rules, lockout limits, allowed state changes, evidence retention, and cleanup/reset procedures.

## Plan and AgentWorks tools

Use scripted steps for repeatable Playwright scenarios and deterministic persistence. Use a message sequence for evidence-based investigation. Add branches only for real outcome paths or configured review. Reuse selected secrets and never pass secret values through plan context.

## Knowledge and persistence

Keep secret references in workflow configuration. Store verified routes, session behavior, role requirements, and non-secret auth quirks in application KB notes. Store runs, cases, attempts, and artifact metadata in the DB and video plus redacted console/network evidence in durable assets.

## Validation and reporting

Require a terminal result for every expected auth scenario and actor. Verify fresh-context behavior, post-action state, required cleanup, source/build identity, and evidence status. The dashboard shows the scenario/actor matrix, environment and build, failure class, evidence status, and history without exposing credentials or tokens.

## Guardrails

Do not trigger unapproved lockouts, recovery messages, enrollment changes, account deletion, or production authentication experiments. Never persist passwords, cookies, tokens, MFA seeds/codes, storage state, or unrestricted network bodies.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step and platform-tool choices.
- [Evidence capture](../references/evidence-capture.md): durable, redacted attempt evidence.
- [Authentication guide](references/authentication-workflow.md): scope, flow, storage, and acceptance cases.
- [Example scenarios](examples/authentication-scenarios.json): fictional coverage shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and scenario revisions, customer overrides, actor/scenario coverage, actual trial results, KB/test/evidence/report locations, capability resolution, and unresolved or unsafe-to-test behavior.
