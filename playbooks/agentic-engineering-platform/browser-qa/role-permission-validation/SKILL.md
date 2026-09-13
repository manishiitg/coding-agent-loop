---
name: role-permission-validation
description: Build AgentWorks browser validation for role-based page, action, and data permissions. Use when an application has multiple roles, tenancy boundaries, or restricted operations.
---

# Role and Permission Validation

## Outcome

Create a repeatable actor-by-resource-by-action permission matrix that verifies allowed behavior, denied behavior, direct navigation, and data isolation with durable evidence.

## When to use

Use after browser and authentication setup when customers have multiple roles, workspaces, tenants, ownership rules, or privileged operations. Skip dimensions the application does not support.

## Required inputs

Resolve authorized actors, role definitions, resource ownership states, allowed and denied actions, tenant boundaries, direct-URL expectations, safe test data, cleanup, and the approved source of permission truth.

## Plan and AgentWorks tools

Use scripted steps for the stable matrix runner and deterministic completeness checks. Use message sequences to investigate mismatches without redefining policy. Partition credentials and fixtures when roles require different security contexts; branch only when classifications lead to different actions.

## Knowledge and persistence

Keep actor secret references out of artifacts. Store verified non-secret role behavior and route constraints in application KB notes, canonical cases in test code, matrix results in DB tables, and attempt evidence under durable assets.

## Validation and reporting

Require a result for every expected matrix cell. Verify both UI absence/disablement and server-observable denial where applicable, including direct navigation and cross-tenant data isolation. The dashboard shows the role/action/resource matrix, expected versus observed policy, environment/build, evidence drill-down, gaps, and history.

## Guardrails

Do not infer permission policy from the current UI, use production identities without authorization, broaden a role, or perform destructive privileged actions without isolated fixtures. A hidden control alone does not prove access is denied.

## Read details when needed

- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step and platform-tool choices.
- [Evidence capture](../references/evidence-capture.md): durable, redacted attempt evidence.
- [Permission guide](references/permission-workflow.md): matrix design, execution, and acceptance cases.
- [Example matrix](examples/permission-matrix.json): fictional coverage shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and matrix revisions, customer overrides, expected and executed cell counts, findings, actual trial result, KB/test/evidence/report locations, capability resolution, and unresolved policy questions.
