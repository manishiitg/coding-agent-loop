# Governed remediation and recovery workflow

## Prepare the action

Read the canonical incident/failure record and re-query current target/configuration state. Define the exact action, mechanism, immutable target identity, parameters or diff, expected effect, evidence, alternatives, blast radius, dependencies, duration, capacity/availability impact, security/compliance impact, validation, rollout, stop conditions, rollback, and owner.

Prefer a customer-approved runbook, deployment pipeline, feature-flag system, or IaC change. If no runbook applies, label the proposal novel and require its configured review. Never interpolate untrusted webhook or Slack text directly into a shell command, resource selector, query, or tool argument.

## Validate and queue review

Perform read-only preflight and dry-run/plan checks where supported. Confirm the target revision still matches, access is scoped, dependencies and quotas permit the action, rollback is available, evidence is fresh, and current impact justifies the risk.

Persist the concrete command/diff/parameters, target, incident, expected impact, risk, validation result, rollout, health checks, stop conditions, and rollback through `create_human_input_request` with approve/reject/defer options, an exact bounded apply contract, and evidence references. Bind the request to the proposal digest and actor, expose it in reporting, then end preparation without executing. Edits invalidate the prior approval. Ambiguity or missing authority means defer/hold.

## Execute and recover later

A separately started action route reads the durable decision and revalidates its actor, status, expiry, proposal digest, target revision, and current incident state. Missing, rejected, deferred, expired, or stale decisions remain held. Execute a valid approved action once through the authorized integration and record provider request/change/deployment identity plus partial results. Prevent concurrent conflicting actions. Check immediate safety signals and then the configured stabilization window. Compare service health, customer impact, capacity, errors, latency, saturation, and SLOs against the pre-action baseline.

Trigger the approved rollback path when stop conditions fire; record rollback approval rules and outcome. If rollback fails or evidence is inconclusive, keep the incident open and escalate. Recovery requires both impact and technical health criteria, not a successful command exit.

## Acceptance cases

Exercise stale target, insufficient authorization, changed proposal after approval, approval timeout/rejection, conflicting action, dry-run failure, partial provider response, action timeout, immediate success with health regression, successful recovery, rollback success/failure, repeated webhook, Slack approval by unauthorized actor, and missing verification data. Confirm no action executes twice or outside scope.
