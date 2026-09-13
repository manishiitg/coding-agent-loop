# CI and deployment failure triage workflow

## Intake and normalization

Accept manual runs, authenticated AgentWorks webhooks, or bounded polling. Validate the delivery before parsing provider fields. Map provider pipeline, workflow, run, attempt, stage, job, repository, commit, branch/ref, build/artifact, deployment, service, environment, actor, and timestamps into the shared contract. Reject or quarantine events outside configured scope. Link retries as attempts of one failure without overwriting earlier evidence.

## Evidence collection

Fetch only the required log slices, test summaries, annotations, artifacts, deployment events, recent change/config/IaC/flag revisions, runner health, dependency status, and target-environment health. Redact secrets and personal data before persistence or agent analysis. Preserve provider links and hashes; record truncation and unavailable evidence.

## Classification

Test evidence for source/build, dependency/package, test assertion, test infrastructure/flakiness, runner/capacity, credentials/permission, configuration, artifact, deployment strategy, target health, policy gate, cancellation/timeout, external provider, duplicate/stale event, or unknown. Record supporting and contradicting evidence. A familiar error string is a signature match, not proof.

Correlate recent changes and known incidents by exact identity and time window. If customer impact or service-health degradation meets policy, open/link an incident and hand off the evidence record.

## Actions

Allow an automatic rerun only for an approved transient class, within attempt/cooldown limits, against the same immutable identity, with no failed policy gate. Otherwise prepare the exact rerun, rollback, fix, or owner action for review. Persist action and external delivery receipts. A successful rerun updates disposition but retains the original failure.

## Acceptance cases

Exercise malformed/unauthorized/duplicate/stale webhooks, missing commit or environment, secret-bearing logs, known deterministic failure, approved transient failure, rerun exhaustion, cancellation, infrastructure outage, failed deployment with healthy service, unhealthy production deployment, provider API failure, Slack escalation, approval timeout, and notification failure. Confirm no false-green result or duplicate action.
