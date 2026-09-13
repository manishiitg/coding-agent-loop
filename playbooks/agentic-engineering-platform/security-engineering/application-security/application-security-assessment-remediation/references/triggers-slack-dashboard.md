# AppSec triggers, Slack, and dashboard

## Trigger bindings

Use AgentWorks API triggers to bind authenticated CI, source-control, scanner, vulnerability-management, deployment, or disclosure events to fixed saved routes/groups. Common bindings include:

- pull request or push → selected code/dependency/secret/configuration checks;
- deployment → authorized dynamic browser/API checks for that immutable build;
- new scanner/vulnerability event → normalization and finding validation;
- fix deployed → exact finding retest;
- risk-acceptance expiry → review/reopen route.

Configure through the supported Builder/Setup surface. Record trigger name, authentication mode, source/event/schema, route/groups, allowed variables, environment mapping, delivery ID, and mapping revision. The first scripted step validates signature/secret, payload version, tenant/repository/service/environment, commit/build/deployment, event time, and authorization before using event fields.

Webhook payload data cannot expand scope, choose arbitrary plan paths/tools, supply secrets, approve testing/remediation, or interpolate into commands. Deduplicate retries by stable delivery/event ID and make downstream writes idempotent. Test unauthorized, malformed, duplicate, stale, out-of-order, and busy deliveries plus source redelivery behavior.

## Slack bot

Route approved security channels to the AppSec workspace with only the required skills, MCPs, secrets, and code-execution policy. Use a finding or assessment thread for triage questions, evidence links, fix status, and supported blocking decisions. Link every thread/message used for a decision to the durable finding/action and actor.

Slack is not the finding store. Build replies from durable state, restrict sensitive evidence links, and never paste credentials, exploit-sensitive artifacts, or unrelated customer data. Do not treat emoji, silence, or uncorrelated replies as authorization or risk acceptance. One-way Slack webhooks/notifications may publish approved status but cannot answer blocking decisions.

## Dashboard data and views

Build a restricted report over the same durable records used for completeness and decisions. Include:

- **Overview:** assessment state, selected route coverage, clean/blocked/incomplete status, open findings by customer severity and confidence, SLA/age, and source freshness.
- **Findings:** filters for application/service, environment, repository, route/category, severity, confidence, status, owner, age/SLA, and recurrence; drill into safe reproduction, restricted evidence, duplicates, decisions, and history.
- **Remediation:** issue/PR, fix revision, validation, reviewer, deployment, retest, rollback, and verified/reopened state.
- **Risk:** accepted scope, approver, rationale, compensating controls, residual severity, expiry, and review state.
- **Runs and integrations:** trigger receipt/source identity, expected/executed/blocked checks, tool/config versions, errors, delivery receipts, Slack thread, and immutable build/deployment.

Do not expose restricted assets through public report links. Use role-appropriate access and redacted previews. A chart count must drill into the exact records behind it. Validate static report structure and the live AgentWorks Report view.

## CI decision

Derive pass/fail/needs-review from the customer's policy only after expected route/check completeness, exact tested identity, and evidence availability pass. Missing, timed-out, cancelled, stale, or mismatched results cannot pass. Return a machine-readable decision plus dashboard/evidence links; CI must enforce that semantic decision rather than treating workflow completion as security success.
