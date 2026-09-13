---
name: flaky-test-detection-stabilization
description: Build AgentWorks browser-test flake detection and reviewed stabilization using repeated isolated attempts and evidence comparison. Use when identical test inputs produce inconsistent outcomes.
---

# Flaky-Test Detection and Stabilization

## Outcome

Create an evidence-backed workflow that detects inconsistent browser-test behavior, separates likely test, application, and environment causes, and verifies any authorized stabilization without hiding failures.

## When to use

Use when the same test and approved expectation produce intermittent outcomes. Run before self-healing when instability is suspected; use self-healing only after evidence supports an eligible test-automation defect.

## Discovery and user direction

Inspect the current workflow, goals, metrics, configuration, capabilities, stores, reports, and triggers before proposing changes. Summarize reusable design and gaps, then ask focused questions for unresolved customer choices such as scope, success, approvals, thresholds, ownership, and budgets. Record the answers as customer direction. Installation alone does not approve workflow changes or execution. Default to one small-team workflow; split only for incompatible access or lifecycle boundaries.

## Required inputs

Resolve the canonical test/source revision, exact build and environment, fixtures, browser profile, repetition and concurrency policy, classification thresholds, allowed stabilization paths, related cases, evidence retention, and review policy.

## Plan and AgentWorks tools

Use scripted steps for repeated isolated runs, completeness checks, evidence hashing, statistics, candidate verification, and approved application. Use a message sequence for cross-attempt diagnosis. Persist verified candidates as pending review, then use a separate action route to validate the later approval and apply the exact stabilization.

## Knowledge and persistence

Preserve every attempt and its video, console/network logs, trace, result, timing, and provenance. Store classifications, candidates, decisions, and stability checks in durable tables/assets. Update KB facts only after verified stabilization; canonical changes remain in test source.

## Validation and reporting

Require all planned attempts or an explicit incomplete state. Compare identical inputs, isolate concurrency and order effects, retain failing and passing evidence, and rerun after any approved change. The dashboard shows test-level flake rate/confidence, attempt outcomes and environment, diagnosis, stabilization state, evidence, and history. A later pass does not erase earlier failures.

## Guardrails

Do not automatically quarantine, skip, weaken assertions, inflate waits/timeouts, or label a product race as a test defect. Limit repetitions and storage, preserve original evidence, and require configured review before canonical changes.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step and platform-tool choices.
- [Evidence capture](../references/evidence-capture.md): comparable attempt evidence.
- [Flake guide](references/flake-workflow.md): experiment design, classification, and stabilization.
- [Example investigation](examples/flake-investigation.json): fictional run shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook, test/build/profile revisions, experiment policy, completed attempts, classification and confidence, proposed/applied change and approval when any, post-change stability result, evidence/report locations, and unresolved causes.
