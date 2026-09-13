---
name: browser-test-self-healing
description: Build an AgentWorks workflow that diagnoses a browser-test failure, proposes a test-only repair, verifies it in isolation, obtains the configured review, and updates canonical tests and application knowledge. Use for stale or flaky browser automation, not product defects.
---

# Browser Test Self-Healing

## Outcome

Create an auditable workflow that classifies a browser-test failure, proposes an eligible test-only repair, verifies it in isolation, obtains configured review, applies the exact approved change, and reruns canonical tests.

## When to use

Use for stale or flaky browser automation backed by an existing Browser QA foundation and preserved failure evidence. Product defects, environment blockers, uncertain behavior, and requested product changes remain outside automated healing.

## Discovery and user direction

Inspect the current workflow, goals, metrics, configuration, capabilities, stores, reports, and triggers before proposing changes. Summarize reusable design and gaps, then ask focused questions for unresolved customer choices such as scope, success, approvals, thresholds, ownership, and budgets. Record the answers as customer direction. Installation alone does not approve workflow changes or execution.

## Required inputs

Resolve the foundation and application knowledge, original finding and evidence, canonical test source and base revision, allowed test/config paths, affected journeys, approval policy, recording retention, and optional publication destination.

## Plan and AgentWorks tools

Use a preparation route to diagnose, propose, verify in isolation, persist `pending_review`, and end without changing canonical tests. A separate apply route validates the later decision, exact patch, and current base before applying once and rerunning canonical tests. Use a blocking human branch only for an explicitly attended run.

## Knowledge and persistence

Preserve the original finding, every candidate, verification attempt, decision, applied revision, and video, console/network, screenshot, or trace reference in durable tables/assets. Update knowledgebase notes only with verified application facts after canonical success; keep executable locators in shared test helpers.

## Validation and reporting

A healed result requires the approved patch to match the applied change and the intended assertions plus affected journeys to pass against the resulting source revision. The dashboard must expose original evidence, diff, attempts, recordings, approval provenance, final disposition, and history.

## Guardrails

Never weaken assertions, change approved expected values, remove coverage, modify product code, discard original evidence, or label an uncertain result healed. Stop when scope, authority, evidence, or verification is insufficient.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step boundaries, platform tools, stores, and execution.
- [Evidence capture](../references/evidence-capture.md): video, console/network logs, redaction, storage, and dashboard behavior.
- [Healing policy](references/healing-policy.md): eligibility, allowed changes, verification, and stopping rules.
- [AgentWorks workflow](references/workflow.md): adaptable steps, message sequences, branches, and recording.
- [Dashboard](references/dashboard.md): durable schema, layout, playback, and review handoff.
- [Example finding](examples/healing-finding.json): illustrative input only.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return the installed playbook and source versions, customer overrides, repair and finding IDs, candidate diff, verification and approval states, applied revision when any, KB/dashboard/evidence locations, capability resolution, and unresolved limitations.
