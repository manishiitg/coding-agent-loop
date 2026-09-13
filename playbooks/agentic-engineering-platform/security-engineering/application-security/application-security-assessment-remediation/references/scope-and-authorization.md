# Scope and authorization

## Authorization contract

Create a versioned authorization record before discovery or testing. Record approver and source, validity window, customer/tenant, allowed applications, origins, APIs, repositories, services, cloud accounts, environments, IPs/regions when relevant, test identities/roles, source/build/deployment revisions, and third-party exclusions.

The rules of engagement define:

- passive and active techniques allowed for each target/environment;
- prohibited behavior, payload classes, endpoints, data, and dependencies;
- request/concurrency limits, testing windows, timeouts, and maximum attempts;
- allowed state changes, fixtures, cleanup, and recovery;
- evidence capture, classification, redaction, access, retention, and deletion;
- communication, emergency contact, stop conditions, and incident escalation;
- whether production, social engineering, credential testing, uploads, callbacks, or external services are authorized;
- which actions require a human decision and who may approve them.

An empty, ambiguous, expired, or conflicting field is not permission. Narrow the selected route or mark it blocked.

## Mandatory scope-gate step

Make the first executable plan step scripted and deterministic. It resolves the current authorization revision and target identity, verifies the trigger/request belongs to the allowed customer and environment, intersects requested checks with the allowlist, validates time/rate/state limits, confirms credentials and evidence paths, initializes expected check rows, and emits `ready`, `blocked`, or `decision_required`.

Every later scripted assessment receives the immutable scope ID and authorized check IDs. Re-check target/build and authorization before active testing and before remediation. A webhook payload, Slack message, scanner configuration, or model response cannot expand scope.

## Pentest modes

- **Passive:** inspect existing responses, metadata, configuration, code, and artifacts without intentionally changing application state.
- **Bounded active:** run specific allowlisted cases with unique fixtures, attempt/rate limits, cleanup, and evidence requirements.
- **Intrusive:** run only when the exact technique, target, window, side effects, stop/rollback plan, and approver are recorded. Unattended default is defer.

Credential attacks, denial of service, persistence, destructive payloads, and data exfiltration remain prohibited unless the customer's explicit engagement contract defines a separately controlled exercise. Generic playbook wording never grants that authorization.

## Stop behavior

Stop new probes and preserve current evidence when scope identity changes, rate/impact limits fire, unexpected sensitive data appears, cleanup fails, another tenant is reachable, service health degrades, credentials behave outside the assigned role, or the customer invokes the stop contact. Record the reason and notify the configured owner without continuing to “confirm” the issue.

## Acceptance cases

Test valid scope, expired approval, target alias mismatch, unauthorized production request, third-party redirect, missing build identity, allowed passive route, gated active route, prohibited technique, rate limit, unexpected state change, cleanup failure, sensitive-data discovery, stop request, and authorization revision during a run. Confirm no blocked check can yield a clean assessment.
