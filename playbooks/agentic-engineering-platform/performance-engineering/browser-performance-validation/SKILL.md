---
name: browser-performance-validation
description: Build repeatable AgentWorks browser performance validation with customer-defined budgets and comparable samples. Use for page, journey, Web Vital, resource, or timing regressions.
---

# Browser Performance Validation

## Outcome

Create repeatable browser performance measurements for approved pages and journeys, compare them with customer budgets or baselines, and retain enough provenance and diagnostics to explain regressions.

## When to use

Use after browser setup for performance budgets, trend tracking, or release checks. Treat results as environment-specific observations unless the customer provides a controlled benchmark environment.

## Required inputs

Resolve target pages/journeys, tested build, browser/device/network profile, cold/warm-cache policy, sample count, metrics, budgets or baseline, acceptable variance, evidence retention, and pass/fail policy.

## Plan and AgentWorks tools

Use scripted steps for controlled execution, metric collection, aggregation, comparison, and persistence. Use a message sequence to investigate supported regressions. Keep warmup and measured samples explicit; do not let an agent invent thresholds after seeing results.

## Knowledge and persistence

Store raw samples, aggregates, budgets, environment/profile identity, and comparison results in durable DB tables. Store traces, video, console/network logs, and exported profiles in durable assets. Put reusable application performance facts in KB notes only after verification.

## Validation and reporting

Require the configured number of valid samples and comparable provenance. The dashboard shows the customer's chosen aggregates, variance and budget status, filters for journey/browser/device/build/environment, and drill-down to samples, traces, evidence, missing metrics, invalid samples, and history. A partial measurement set cannot silently pass.

## Guardrails

Do not claim production performance from an uncontrolled test environment, mix incompatible device/network/cache profiles, change budgets to obtain a pass, or ignore functional failures. Redact sensitive URLs and network data before retention.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Performance measurement](../references/performance-measurement.md): shared provenance, sampling, budgets, and comparison rules.
- [Browser QA plan and tools](../../browser-qa/references/agentworks-plan-and-tools.md): browser step and platform-tool choices.
- [Evidence capture](../../browser-qa/references/evidence-capture.md): durable browser diagnostic artifacts.
- [Performance guide](references/performance-workflow.md): sampling, comparison, storage, and acceptance cases.
- [Example budget](examples/performance-budget.json): fictional policy shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and budget revisions, customer overrides, tested environment/build/profile, sample completeness, metric decisions, actual trial result, evidence/report locations, capability resolution, and limitations.
