# API security testing

## Inventory and identity

Build the authorized endpoint inventory from customer specifications, gateway/routes, client traffic, and observed service metadata. Record host/service, environment, protocol/version, method/path or operation, authentication scheme, actor/role, object ownership/tenant, data classification, state-changing behavior, dependencies, expected controls, rate/abuse policy, source revision, and coverage source.

Unknown endpoints remain discovery gaps. Do not crawl or follow references into unapproved hosts, tenants, third parties, or administrative surfaces.

## Applicable checks

Select cases based on the API design and rules of engagement:

- missing, invalid, expired, replayed, or incorrectly scoped authentication;
- object, property, and function authorization across approved roles, owners, and tenants;
- request schema, type, size, encoding, content-type, and unexpected-field handling;
- bounded injection and unsafe downstream interpretation checks;
- pagination, filtering, exports, uploads/downloads, and sensitive-data minimization;
- safe rate, quota, concurrency, retry, idempotency, and resource-consumption behavior;
- CORS, cache, error, version/deprecation, webhook/callback, and transport controls;
- multi-step business rules, state transitions, limits, duplication, and race behavior when explicitly authorized.

Define expected status and invariant, safe request template, fixture, allowed mutation, cleanup, request/attempt ceiling, evidence, and stop conditions per case. Avoid load or race testing unless its concurrency and impact are specifically authorized.

## AgentWorks steps and evidence

Use scripted API clients or customer-selected scanners with fixed configuration. Keep credentials in AgentWorks secrets and partition actors. Capture sanitized request shape, status, relevant header/schema fields, response hash or minimal redacted excerpt, timing, correlation/trace ID, resulting state, cleanup, and server-side denial evidence when available. Do not persist authorization headers, tokens, unrestricted bodies, or unrelated customer data.

Use a message sequence to evaluate business-logic and cross-request findings only after deterministic results are stored. Distinguish client-visible denial from server-enforced authorization. A UI restriction alone does not validate an API control.

## Remediation retest

Retest the exact operation, actor, object/tenant relationship, and invariant against the deployed fix. Include a permitted control case so the fix does not break valid use. Run adjacent object/function paths and authorized bypass variants selected by the finding model. Confirm logs/alerts and data state when those controls are part of closure.

## Acceptance cases

Exercise undocumented endpoint, missing identity, allowed and denied actors, cross-owner/tenant request, invalid token, schema error, sensitive error body, rate-limit stop, duplicate/idempotent mutation, cleanup failure, dependency timeout, scanner error, finding duplicate, fixed denial with permitted success, and regression after deploy.
