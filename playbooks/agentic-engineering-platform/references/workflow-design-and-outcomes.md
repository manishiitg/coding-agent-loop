# Workflow design and outcomes

Use this check before adapting any playbook. Its purpose is to preserve the customer's operating model while making the resulting workflow measurable.

## Inspect the current system

Read the workflow objective and success criteria, then inspect the current plan, routes, triggers, schedules, integrations, permissions, durable data, report, and installed playbooks. Reuse established names, owners, environments, policies, and metrics. Identify what is verified, assumed, missing, or conflicting.

## Goals and metrics

Map the requested capability to an existing goal when one already covers the outcome. Recommend a new or revised goal only when the desired outcome is otherwise unowned or unmeasurable. Define:

- the outcome and decision it supports;
- success, leading, quality, reliability, cost, and safety indicators that matter for this playbook;
- baseline, target or decision threshold supplied by the customer;
- calculation, dimensions, evidence source, owner, and review cadence;
- incomplete, stale, or unavailable states.

Never invent a target after observing results. When no approved threshold exists, record a baseline and report the result as unrated until the customer chooses a policy. Reuse existing metrics when their definition, population, environment, and time window are compatible.

## Current or separate workflow

Prefer the current workflow when the capability shares its objective, owner, access boundary, environment, trigger or cadence, durable data, and lifecycle, and can be expressed as a coherent route or step group.

Recommend a separate workflow when it has an independent objective or owner; a materially different trigger, cadence, environment, tenant, credential or permission boundary; an approval or blast-radius boundary; distinct scaling, retention, deployment, or reliability needs; or a lifecycle that should succeed and fail independently.

Do not split merely because a playbook has several steps, and do not force unrelated operations into one workflow for convenience. For separate workflows, define the supported handoff, stable identifiers, data contract, permissions, retry/idempotency behavior, and reporting ownership. Do not copy secrets or rely on another workflow's private paths.

## Builder recommendation

Before structural changes, present:

1. the current objective, relevant goals, metrics, and workflow boundaries;
2. the recommended placement: extend the current workflow, create a route, or create a separate workflow;
3. the concrete reasons and tradeoffs;
4. proposed goal/metric additions or changes, with missing customer decisions;
5. the plan, data, reporting, trigger, integration, and permission changes that follow.

Proceed with the user's chosen structure. Record the decision and customer overrides in the installed playbook setup record.
