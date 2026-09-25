---
name: website-growth-loop
description: Build a chat-led, multi-Crew Website Growth Automation for a newly launched company site, with source-linked opportunities, measured progress, and reviewed handoffs.
---

# Website Growth Loop

## Outcome

Coordinate specialist Crews to find useful website traffic opportunities, prepare reviewable improvements, and measure progress against the owner's visitor goal.

## When to use

Use for a recently launched website whose owner wants sustained relevant traffic. A public site and owner context are enough for a first manual run; connected search or analytics data improves measurement later. Use a Crew schedule if only one specialist repeats one task.

## Discovery and user direction

Treat this Playbook as a proposal. Inspect the current Automation, goals, Crews, templates, skills, integrations, and access first. Show which agents can be reused, which must be created, their handoffs, cost and permissions, and what remains unknown. Ask only for material decisions; record the owner's answers. Selecting or installing the guidance is not approval to create agents or activate runs.

## Required inputs

Resolve the canonical site or page export, offer, audience, market, primary visitor action, site/crawl scope, named owner, goal metric, baseline or baseline-first decision, and publication boundary. Search Console and analytics are optional for the first plan; never invent traffic or ranking history.

## Plan and AgentWorks tools

Start with two distinct Crew roles: Website Growth Starter as strategist and Search Opportunity Mapper or SEO Analyst as search specialist. Reuse suitable authorized Crews. After the concrete team proposal is reviewed, use `create_crew` for missing specialists with stable idempotency keys; bind existing Crews with internal triggers and read-only attachments. Add Crew steps and deterministic artifact validation to the Workflow plan. Add Content Brief Writer and Traffic & Engagement Analyst only when their work and data are useful. Test a manual route before proposing paused recurrence.

## Knowledge and persistence

Keep owner decisions, site scope, audience, metric definitions, and Crew bindings in durable Workflow context. Persist source-linked briefs, opportunity lists, review decisions, shipped changes, and measurement windows as run artifacts. Pass only bounded artifacts between Crews.

## Validation and reporting

Verify two distinct authorized Crew IDs, current skills and access, a schema-valid strategist brief, a search handoff, and one bounded manual test. The dashboard shows action status, source links, baseline availability, traffic or conversion readings with denominators, confidence, Crew run IDs, cost, and unresolved blockers.

## Guardrails

Do not infer indexing from a public fetch, fabricate keyword volume or traffic, blend Search Console clicks with analytics sessions, or promise rankings. Keep CMS publication, outreach, paid spend, and recurring triggers behind separate review and authorization. Recheck Crew access before each run.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goal, route, and schedule decisions.
- [Team and handoff contract](references/team-and-handoffs.md): exact roles, setup checks, artifacts, and run proof.
- [Example run record](examples/website-growth-run.json): fictional evidence shape.
- [Catalog metadata](playbook.json): roster, inputs, and recommendations.

## Completion contract

Return the applied Playbook version, owner choices, actual Crew IDs, access and setup state, Workflow step IDs, validated handoffs, first run and dashboard links, metric/baseline limits, paused or manual activation choice, and unresolved work.
