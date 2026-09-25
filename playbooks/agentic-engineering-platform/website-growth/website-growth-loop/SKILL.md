---
name: website-growth-loop
description: Build a chat-led, multi-Crew Website Growth Automation for a newly launched company site, with source-linked opportunities, measured progress, and reviewed handoffs.
---

# Website Growth Loop

## Outcome

Coordinate Crews to find traffic opportunities, prepare improvements, and measure the owner's visitor goal.

## When to use

Use for a recently launched website whose owner wants sustained relevant traffic. A public site and owner context are enough for a first manual run; connected search or analytics data improves measurement later. Use a Crew schedule if only one specialist repeats one task.

## Discovery and user direction

Inspect the Automation, goals, Crews, skills, integrations, and access. Propose Crew reuse or creation, handoffs, cost, permissions, and blockers. Ask for material decisions and record answers. Selection alone does not authorize creation or runs.

Track setup in `SETUP.json` beside this skill. Verify each check before adding its ID to `completed_steps`; save a source or artifact reference under `evidence[id]`. Preserve existing progress and report blockers.

## Required inputs

Resolve site, offer, audience, market, visitor action, crawl scope, owner, metric, baseline decision, and publication boundary. Search Console and analytics are optional initially; never invent history.

## Plan and AgentWorks tools

Start with two distinct Crew roles: Website Growth Starter as strategist and Search Opportunity Mapper or SEO Analyst as search specialist. Reuse suitable authorized Crews. After the concrete team proposal is reviewed, use `create_crew` for missing specialists with stable idempotency keys; bind existing Crews with internal triggers and read-only attachments. Add Crew steps and deterministic artifact validation to the Workflow plan. Add Content Brief Writer and Traffic & Engagement Analyst only when their work and data are useful. Test a manual route before proposing paused recurrence.

## Knowledge and persistence

Save owner decisions, site scope, metrics, Crew bindings, source-linked results, and measurement windows. Pass bounded artifacts between Crews.

## Validation and reporting

Verify two distinct authorized Crew IDs, current skills and access, a schema-valid strategist brief, a search handoff, and one bounded manual test. The dashboard shows action status, source links, baseline availability, traffic or conversion readings with denominators, confidence, Crew run IDs, cost, and unresolved blockers.

## Guardrails

Do not infer indexing from a public fetch, fabricate keyword volume or traffic, blend Search Console clicks with analytics sessions, or promise rankings. Keep CMS publication, outreach, paid spend, and recurring triggers behind separate review and authorization. Recheck Crew access before each run.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goal, route, and schedule decisions.
- [Team and handoff contract](references/team-and-handoffs.md): exact roles, setup checks, artifacts, and run proof.
- [Example run record](examples/website-growth-run.json): fictional evidence shape.
- [Setup progress](SETUP.json): nine checks, saved evidence, and completed IDs.
- [Catalog metadata](playbook.json): roster, inputs, and recommendations.

## Completion contract

Return the applied Playbook version, owner choices, actual Crew IDs, access and setup state, Workflow step IDs, validated handoffs, first run and dashboard links, metric/baseline limits, paused or manual activation choice, and unresolved work.
