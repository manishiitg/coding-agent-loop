---
name: release-pr-quality-gate
description: Build an AgentWorks release or pull-request browser quality gate over saved suites and exact build identity. Use when a merge, deployment, or promotion needs an auditable QA decision.
---

# Release and PR Quality Gate

## Outcome

Create an auditable decision workflow that binds an exact change/build to required Browser QA suites, evaluates completeness and policy, and publishes `pass`, `fail`, or `needs_review` with evidence.

## When to use

Use after the required suites have passed independently and can run unattended against a uniquely identified preview or release candidate. Use scheduled monitoring for time-based deployed-health checks.

## Required inputs

Resolve trigger type, repository/change/build/environment identity, required suites and groups, route selections, blocking and review rules, timeout/cancellation behavior, evidence retention, duplicate-run policy, and authorized status/report destinations.

## Plan and AgentWorks tools

Use deterministic preparation and finalization for identity, expected-suite set, completeness, and verdict derivation. Run existing suite routes rather than copying their tests. Use a human branch only for an explicit `needs_review` policy. Configure authenticated API triggers through the supported Setup surface and integrations.

## Knowledge and persistence

Persist gate, suite-run, decision, evidence, and delivery receipts keyed by exact change/build identity and policy revision. Keep canonical tests in their source packages and reusable application facts in KB notes. Never treat attempted delivery as a published status.

## Validation and reporting

Require terminal results for every required suite/group, exact tested identity, valid evidence, and a deterministic policy decision. Missing, cancelled, stale, or mismatched results cannot pass. The dashboard shows the gate timeline, suite drill-down, tested identity, evidence, review state, history, and actual delivery receipt.

## Guardrails

Do not test repository HEAD when a different build is under review, reuse results from another commit, weaken policy after failures, auto-approve review states, or post external statuses without configured authorization. Preserve reruns as separate attempts.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): plan, execution, and persistence choices.
- [Evidence capture](../references/evidence-capture.md): gate evidence provenance.
- [Release gate guide](references/release-gate-workflow.md): trigger, aggregation, verdict, and delivery.
- [Example gate policy](examples/release-gate-policy.json): fictional policy shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and policy revisions, trigger and identity mapping, required suite/group set, customer overrides, controlled trial result, report and delivery locations/receipts, capability resolution, and unresolved blockers.
