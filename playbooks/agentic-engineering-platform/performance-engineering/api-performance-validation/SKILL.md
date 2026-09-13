---
name: api-performance-validation
description: Build AgentWorks API performance validation with authorized bounded load, customer-defined budgets, comparable samples, and durable diagnostics. Use for latency, throughput, error-rate, or capacity regression checks.
---

# API Performance Validation

## Outcome

Create a repeatable API performance workflow that exercises approved scenarios under bounded load, evaluates customer budgets, preserves comparable samples, and reports supported latency, throughput, reliability, and capacity findings.

## When to use

Use for service-level performance baselines, regressions, release checks, or controlled capacity experiments. Require explicit authorization before meaningful load; production is never assumed to be an acceptable target.

## Required inputs

Resolve endpoints and protocol, environment/build, authentication and secret references, scenarios/payload classes, test-data cleanup, load shape, worker location, connection behavior, sample duration, budgets/baseline, stop limits, observability access, evidence retention, and owner.

## Plan and AgentWorks tools

Use scripted steps for preflight, the saved load runner, deterministic parsing, aggregation, persistence, and policy decisions. Use a message sequence to correlate supported regressions with metrics/traces. Put high-load or state-changing phases behind configured human approval and fail closed on target mismatch.

## Knowledge and persistence

Store policies, profiles, scenarios, runs, samples, aggregates, errors, and findings in durable tables. Store sanitized runner output, request summaries, traces, and profiles in durable assets. Keep secret values and unrestricted bodies out of artifacts; add only verified non-sensitive service facts to KB notes.

## Validation and reporting

Require exact target/build/profile identity, planned sample completeness, functional response checks, error accounting, and valid measurements. The dashboard shows latency distribution, throughput, error rate, concurrency/rate, saturation, and budget status, with filters for route/profile/build/environment and drill-down to samples, errors, traces, history, and limitations. Partial or incomparable runs cannot pass.

## Guardrails

Do not load-test production by assumption, exceed approved rate/concurrency/duration, create uncontrolled data, ignore rate-limit signals, hide errors through averages, change budgets after observing results, or claim server capacity from client latency alone.

## Read details when needed

- [Performance measurement](../references/performance-measurement.md): shared provenance, sampling, budgets, and reporting.
- [API performance workflow](references/api-performance-workflow.md): scenarios, plan, safety, storage, and acceptance cases.
- [Example policy](examples/api-performance-policy.json): fictional measurement and load shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and policy/profile revisions, exact target/build, authorized load shape, customer overrides, planned/completed samples, functional and budget decisions, findings, evidence/report locations, capability resolution, and limitations.
