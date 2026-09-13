---
name: example-playbook
description: Guide the AgentWorks builder in creating a reusable example capability. Use when the customer requests the example outcome or needs its existing workflow repaired.
---

# Example Playbook

## Outcome

State the durable workflow capability and the result the customer receives.

## When to use

State positive triggers, scope, and any important exclusion.

## Required inputs

List the minimum information, access, policies, and existing artifacts the builder must resolve. Treat unavailable required inputs as explicit blockers.

## Plan and AgentWorks tools

Explain how to compose this into the plan: important scripted steps, message sequences, routes, branches, schedules, or approvals; existing steps to reuse; and AgentWorks capabilities to resolve. Preserve the user's process and link to detailed guidance.

## Knowledge and persistence

State what belongs in workflow context, knowledgebase notes, learnings, database tables, and durable assets.

## Validation and reporting

Define machine-checkable readiness and failure behavior. Describe the dashboard's primary status/metrics, filters, evidence drill-down, history or trends, and incomplete/restricted states. Back it with durable workflow data.

## Guardrails

List the few constraints that must survive customer adaptation.

## Read details when needed

- [Workflow design and outcomes](../../agentic-engineering-platform/references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [Implementation guide](references/implementation.md): topic-specific decisions and examples.
- [Example output](examples/output.json): illustrative shape only.
- [Catalog metadata](playbook.json): UI presentation and optional tool recommendations.

## Completion contract

Return the installed workflow and source versions, material customer overrides, trial result, artifact/report locations, and unresolved blockers.
