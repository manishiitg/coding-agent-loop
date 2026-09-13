---
name: basic-browser-setup
description: Set up browser QA in AgentWorks, save verified locators and application knowledge, configure a repeatable smoke test, and establish reporting. Use when onboarding an application or repairing its QA foundation.
---

# Basic Browser Setup

## Outcome

Create a reusable browser QA foundation with managed browser access, canonical locator helpers, a saved smoke test, video plus console/network evidence, durable application knowledge, and a live report.

## When to use

Use when onboarding an application or repairing an incomplete browser QA foundation. Preserve a compatible existing setup and an explicitly chosen Playwright alternative.

## Required inputs

Resolve the application and environment, authorized account roles, authentication approach, approved smoke expectation, test-data and cleanup boundaries, test project, recording policy, and reporting preference. Keep missing required access or behavior as an explicit blocker.

## Plan and AgentWorks tools

Inspect the existing workflow first. Use the managed browser for discovery and a scripted step for the repeatable smoke runner. Add agentic investigation or a readiness branch only when it protects a real judgment, retry, or downstream boundary. Follow the shared plan/tool guide and current `builder-reference` schemas.

## Knowledge and persistence

Save executable selectors in shared test helpers. Record verified selectors, authentication quirks, routes, and setup in application knowledgebase notes with code/evidence links. Persist profile/run data in the workflow DB and videos, console/network logs, screenshots, and traces under durable assets.

## Validation and reporting

Run the saved smoke test from a fresh context. Readiness requires approved assertions, complete machine-readable results, policy-required video and console/network evidence status, source/config revisions, and a verified live dashboard over durable data.

## Guardrails

Preserve user preferences, secrets boundaries, existing compatible configuration, and approved test behavior. Do not claim readiness from exploratory browsing alone or store secrets in the profile, knowledgebase, examples, or report.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step boundaries, platform tools, stores, and execution.
- [Evidence capture](../references/evidence-capture.md): video, console/network logs, redaction, storage, and dashboard behavior.
- [Setup and handoff](references/setup-and-handoff.md): builder wiring, KB persistence, reporting, and reuse.
- [Workflow and dashboard](references/workflow-and-dashboard.md): adaptable flow, branches, data, and Report setup.
- [Locators and Playwright](references/locators-and-playwright.md): discovery, helpers, recording, and runner setup.
- [Example profile](examples/browser-foundation.json): fictional output shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and profile versions, customer overrides, workflow and step IDs, KB and canonical test/helper locations, trial result, evidence/report locations, capability resolution, and unresolved blockers. Setup does not activate schedules or external notifications.
