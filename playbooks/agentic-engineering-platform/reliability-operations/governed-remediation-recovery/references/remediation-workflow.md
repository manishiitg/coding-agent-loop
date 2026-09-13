# Governed remediation and recovery workflow

## Prepare the action

Read the canonical incident/failure record and re-query current target/configuration state. Define the exact action, mechanism, immutable target identity, parameters or diff, expected effect, evidence, alternatives, blast radius, dependencies, duration, capacity/availability impact, security/compliance impact, validation, rollout, stop conditions, rollback, and owner.

Prefer a customer-approved runbook, deployment pipeline, feature-flag system, or IaC change. If no runbook applies, label the proposal novel and require its configured review. Never interpolate untrusted webhook or Slack text directly into a shell command, resource selector, query, or tool argument.

## Validate and approve

Perform read-only preflight and dry-run/plan checks where supported. Confirm the target revision still matches, access is scoped, dependencies and quotas permit the action, rollback is available, evidence is fresh, and current impact justifies the risk.

Present the concrete command/diff/parameters, target, incident, expected impact, risk, validation result, rollout, health checks, stop conditions, and rollback before the approval branch. Bind approval to the proposal digest and actor. Edits invalidate the prior approval. Timeout, ambiguity, or missing authority means defer/hold.

## Execute and recover

Execute once through the authorized integration and record provider request/change/deployment identity plus partial results. Prevent concurrent conflicting actions. Check immediate safety signals and then the configured stabilization window. Compare service health, customer impact, capacity, errors, latency, saturation, and SLOs against the pre-action baseline.

Trigger the approved rollback path when stop conditions fire; record rollback approval rules and outcome. If rollback fails or evidence is inconclusive, keep the incident open and escalate. Recovery requires both impact and technical health criteria, not a successful command exit.

## Acceptance cases

Exercise stale target, insufficient authorization, changed proposal after approval, approval timeout/rejection, conflicting action, dry-run failure, partial provider response, action timeout, immediate success with health regression, successful recovery, rollback success/failure, repeated webhook, Slack approval by unauthorized actor, and missing verification data. Confirm no action executes twice or outside scope.
