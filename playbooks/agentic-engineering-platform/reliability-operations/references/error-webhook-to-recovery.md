# Error webhook to recovery

This is the default Reliability Ops route when an external reliability system detects an error. Examples include monitoring, error tracking, incident management, CI, deployment, logging, and cloud-health systems.

## 1. Attach the source

Create an AgentWorks API trigger bound to the saved reliability route and environment groups. Configure the external system to send its event to that endpoint using supported signed or bearer authentication. Map provider event type/version, event and delivery IDs, service, environment, severity, timestamps, error/alert identity, source link, and safe evidence references.

Test one controlled event. Invalid authentication, missing identity, unsupported event versions, stale events, and out-of-scope services stop before investigation.

## 2. Receive and group errors

The first scripted step validates and normalizes the delivery, deduplicates retries, and correlates the error with an existing failure or incident. Group only through configured fingerprints and evidence such as exact service, environment, exception/signature, deployment, trace, and time window. Preserve every original event and increment occurrence counts.

## 3. Perform basic RCA

Collect bounded evidence around the event: error details, logs, metrics, traces, recent deployments/commits/configuration/feature flags, dependency health, related CI results, topology, and similar verified incidents.

Produce:

- observed symptom and affected scope;
- customer/SLO impact and current severity;
- likely cause category and candidate component/change;
- supporting and contradicting evidence;
- confidence and important unknowns;
- next discriminating check;
- matching approved runbook, when any.

Basic RCA is a working diagnosis. Label it `confirmed` only when direct evidence or a verification experiment supports causation. Otherwise use `probable`, `possible`, `unknown`, or `insufficient_data`.

## 4. Resolve or escalate

Choose one policy outcome:

- **Observe/deduplicate:** retain and monitor a known non-actionable repeat.
- **Automatic runbook:** execute only a pre-approved, reversible, tightly scoped action whose target and preconditions match exactly.
- **Approval required:** prepare the exact action, risk, checks, and rollback for AgentWorks UI or correlated Slack approval.
- **Escalate:** create/link an incident and notify the configured owner when impact, uncertainty, risk, or failed recovery meets policy.
- **Hold:** preserve evidence when access or identity is insufficient.

Never construct commands from untrusted webhook or Slack fields. Re-query current resource/configuration identity immediately before action.

## 5. Verify and close

Run action-specific checks plus the service stability contract. Verify error recurrence, health/SLOs, latency, saturation, dependencies, deployment/configuration identity, and rollback readiness throughout the configured window. A successful command does not prove recovery.

Update the durable record, dashboard, Slack thread, and authorized source system with RCA status, action, verification, and receipts. Close or resolve the external issue only when policy permits; otherwise prepare the update for review. Link unresolved or recurring cases to deeper incident investigation and post-incident actions.

## Required dashboard path

Show event receipt → correlation → RCA → decision → action/approval → verification → final disposition. Support filters for source, service, environment, severity, cause category, confidence, owner, action, and status, with restricted evidence drill-down and recurrence history.

## Acceptance cases

Test valid event, invalid signature, duplicate retry, burst of the same error, similar but unrelated errors, stale event, missing service mapping, probable deployment cause, dependency failure, known safe runbook, approval rejection/timeout, unauthorized Slack response, action failure, successful action without health recovery, rollback, verified recovery, source update failure, and recurrence.
