---
name: inbound-lead-to-meeting-review
description: Build a chat-led small-team inbound sales review with sourced qualification, optional account research, and approved follow-up drafts.
---

# Inbound Lead-to-Meeting Review

## Outcome

Move a real inbound enquiry toward a useful meeting through a qualified lead brief, optional account research, and an owner-reviewed follow-up. Track replies and bookings only from authorized current records; a draft is not a meeting.

## When to use

Use when distinct Crews or owners need a checked lead-to-follow-up handoff. A single Crew can handle a simple review in chat or on its own schedule.

## Discovery and user direction

Inspect the Workflow, existing Crews, selected skills, source access, prior contact, and policies. Propose a concrete route and first result. Selection saves guidance only. Record reviewed choices and actual evidence in `SETUP.json`.

## Required inputs

Resolve lead source and scope, offer and ideal-customer criteria, owner, routing and suppression rules, contact approval, useful-meeting definition, and baseline or baseline-first choice. An authorized export supports the manual first run.

## Plan and AgentWorks tools

Use distinct ready Lead Intake & Qualifier and Sales Follow-up Coordinator Crews; add Account Researcher only when justified. Reuse suitable Crews. After review, create missing ones with `create_crew` and stable idempotency keys; do not pass their local skills as global selections. Include schema fields and output path in each Crew step. Add blocking validator script steps before consumers. Pass only bounded validated artifacts through authorized attachments. A qualification recommendation of not-fit, contact-blocked, or duplicate-to-review stops outbound drafting.

## Knowledge and persistence

Save source/account scope, criteria, Crew IDs, run IDs, artifact paths, validator results, owner decisions, stable lead/action IDs, prior contact, and current meeting state. Re-read source state and deduplicate before any repeat action.

## Validation and reporting

Run the bundled validator on real artifacts and the invalid fixture. A malformed lead brief stops Research and Follow-up; invalid research cannot inform the draft. Verify source truth separately from JSON shape. The reporting dashboard shows qualified, needs-review, and stopped cases, draft state, source freshness, owner decisions, run cost, and actual replies/bookings only when observed. Test one manual route before recurrence.

## Guardrails

No Playbook selection sends a message, writes CRM, enrolls a sequence, or books a meeting. A draft requires owner review; sending requires a separately authorized route, current suppression and prior-contact checks, exact recipient/content approval, and a recorded provider result. Do not infer contact permission from a form submission, invent budget or intent, or turn a draft into a claimed meeting.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goal and route decisions.
- [Team and handoff contract](references/team-and-handoffs.md): Crew binding and validation.
- [Artifact validator](scripts/validate_sales_artifact.py): blocking structure and reference checks.
- [Qualification example](examples/lead-qualification-brief.json), [research example](examples/account-research-brief.json), [draft example](examples/sales-followup-draft.json), and [invalid qualification](examples/invalid-lead-qualification-brief.json): fictional fixtures.
- [Setup progress](SETUP.json): checks and evidence.

## Completion contract

Return applied version, owner choices, Crew IDs and setup state, source map, Workflow step IDs, validated handoffs, first manual run, reviewed draft or stopped disposition, action ledger, metric limits, activation state, and unresolved work.
