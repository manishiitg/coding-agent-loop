# Reliability event and evidence contract

Use one normalized contract across CI failures, deployment failures, incidents, remediation, and follow-up work. Keep provider-native identifiers alongside normalized fields.

## Identity and correlation

Every event records event/run ID, source and source event ID, trigger/delivery ID, received and source timestamps, service, component, environment, region, owner, severity, status, workflow/run, deployment/change/build/commit identity when applicable, and correlation keys. Preserve source revisions and links rather than copying unrestricted payloads.

Deduplicate by the provider's stable delivery/event identity. Correlation may group repeated symptoms, related pipeline stages, one deployment and its health alerts, or several signals for one incident. Retain the original events and the rule/evidence that created the group. Similar timing alone is not proof of causation.

## Evidence

Evidence records type, source, query or artifact reference, observation window, captured time, freshness, access classification, redaction status, and content hash when available. Typical evidence includes logs, metrics, traces, CI output, test results, deployment events, configuration/IaC diffs, feature flags, topology, alerts, health checks, screenshots, and operator observations.

Store large or restricted evidence as durable assets with scoped access. Store structured summaries and pointers in tables. Never persist credentials, authorization headers, tokens, private Slack content unrelated to the incident, or unrestricted payload dumps.

## State and decisions

Keep detection, triage, investigation, mitigation, resolution, and review as explicit timestamps/states. A state transition records actor, reason, evidence, and policy revision. Unknown, duplicate, blocked, false-positive, and insufficient-data remain first-class outcomes.

Hypotheses retain supporting and contradicting evidence, confidence, and disposition. Actions retain exact command/change/diff, target, risk, validation, approval requirement and receipt, executor, start/end, result, health verification, and rollback receipt. Human edits remain attributable.

## Metrics

Calculate acknowledgement, diagnosis, mitigation, recovery, and follow-up timing from recorded transitions using the customer's definitions. Preserve censored/incomplete intervals. Do not compare teams or services until scope, severity, working hours, and metric definitions are compatible.
