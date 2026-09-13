# Incident investigation and coordination workflow

## Intake and declaration

Accept monitoring/incident webhooks, CI/deployment escalation, Slack bot requests, and authorized manual declaration. Validate the source, deduplicate, resolve service/environment and ownership, and correlate without discarding original signals. Create one canonical incident ID and link external provider IDs and the Slack thread.

Apply the customer's severity matrix from observed user impact, scope, criticality, SLO/error-budget state, duration, and workaround. Record the evidence and policy revision. If required data is missing, mark severity provisional and page/escalate according to policy.

## Investigation

Build a timestamped timeline from alerts, metrics, logs, traces, deployments, flags, configuration/IaC, dependency health, prior related incidents, and operator statements. Preserve source clocks and note skew.

For each hypothesis record claim, scope, supporting evidence, contradicting evidence, next discriminating check, confidence, owner, and disposition. Prefer checks that safely separate hypotheses. A recent deploy is a candidate until rollback, comparison, or direct evidence supports causation.

## Coordination and status

Assign configured incident roles and escalation targets. Generate updates from durable state: current impact, affected scope, confirmed facts, current hypotheses labeled as such, actions/results, risks, next checkpoint, and timestamp. Post through the approved destination and retain delivery/edit receipts. Slack questions or decisions update the durable record with actor and message reference.

Handoff to Governed Remediation and Recovery for concrete operational changes. Investigation remains live through mitigation and a configured stability window. Resolve only when impact and health criteria pass, outstanding risks are recorded, stakeholders receive the final status, and follow-up ownership is established.

## Acceptance cases

Exercise duplicate/multi-source alerts, uncertain severity, out-of-order events, clock skew, missing telemetry, noisy alerts, third-party failure, suspected deployment regression, security-sensitive evidence, ownership gap, Slack initiation, update delivery failure, escalation timeout, false recovery, recurrence during stability window, and clean resolution. Confirm hypotheses never become facts without evidence.
