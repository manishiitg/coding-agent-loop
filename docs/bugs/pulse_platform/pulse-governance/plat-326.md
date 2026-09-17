[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-326 — Collapse Pulse persistence to reviews, issues and human decisions

| Coordination | Value |
|---|---|
| Assigned agent | Unassigned |
| Ticket state | `recommended; explicitly deferred — do not implement yet` |
| Last synchronized | `2026-09-17` |
| Priority | `P2 simplification / migration` |

## Problem

Pulse currently spreads one product lifecycle across roughly twenty-five
workflow-local SQLite tables: module state and audits, review notes and focus
history, recovery, finding details and events, fix attempts and verifications,
impact records, command state, telemetry and Activity projections. Much of this
is bookkeeping for intermediate states rather than user-facing product truth.
It increases tool count, prompt complexity, synchronization risk and the chance
that Pulse spends more effort maintaining its own records than improving the
workflow.

The intended product lifecycle is deliberately smaller:

```text
review concludes -> issue reported/open -> action taken -> issue closed
```

There is no separate verification lifecycle. If Pulse cannot complete the fix,
the issue remains open. A later review may add current evidence, but should not
create a second semantic issue.

## Recommended target model

### `pulse_reviews`

```text
id
pulse_run_id
reviewer_type
status
summary
evidence
created_at
```

One row records the user-readable result of Plan Drift, Technical,
Architecture or Strategic Review. Activity is derived directly from these rows;
Pulse must not maintain a second notification-authored copy of the result.

### `pulse_issues`

```text
id
review_id
description
evidence
status          -- open | closed
action_taken    -- empty while open; actual completed action when closed
created_at
updated_at
```

Do not add `recommended_action`, `outcome`, verification status, monitoring
status, fix attempts or a general issue-event ledger. When the same root cause
is encountered again, update the canonical issue's evidence and timestamp.

### `pulse_decisions`

```text
id
issue_id
question
options
status          -- pending | answered
answer
created_at
answered_at
```

Every Pulse decision must belong to an open issue. Answering a decision does
not itself claim that work was performed:

- approval leaves the issue open until the approved action is applied; then
  `action_taken` is recorded and the issue closes;
- rejection records the choice as `action_taken` and closes the issue;
- an unanswered decision remains pending and its issue remains open.

This replaces Pulse-specific use of the much broader
`report_human_inputs`/event/apply-contract lifecycle. General workflow
`human_input` steps remain outside this migration.

## Migration plan

Implement only in a dedicated migration change, not opportunistically during
other Pulse work.

1. Back up every workflow database and introduce a new workflow contract/schema
   version.
2. Create the three target tables alongside the existing tables.
3. Backfill reviews from `pulse_module_audit`:
   `module -> reviewer_type`, `result -> status`, `reason -> summary`, retained
   evidence -> `evidence`, and `recorded_at -> created_at`.
4. Backfill one canonical issue by merging `run_concerns`,
   `pulse_finding_details` and the latest useful lifecycle record. Collapse all
   unresolved intermediate states to `open`; collapse resolved, rejected and
   successfully changed states to `closed`. Copy the best truthful completed
   repair/resolution description to `action_taken`; discard verification-only
   history.
5. Backfill Pulse-owned pending and answered rows from `report_human_inputs`
   into `pulse_decisions`, preserving their link to the canonical issue.
6. Validate before cutover: every latest reviewer result is present, every
   active public `PUL-*` identity exists exactly once, every pending decision is
   visible, and every closed issue has a non-empty `action_taken`.
7. Switch `record_pulse_result`, finding writes, decision writes, Gate reads,
   Pulse UI and Activity to the new model. Prefer one terminal review write that
   atomically stores its review and issue changes.
8. Stop legacy writes, retain old tables read-only for one release/rollback
   window, compare old and new UI results, then remove legacy tables and tools.

## Tables expected to retire from Pulse ownership

The migration should explicitly assess and normally remove or move out of the
workflow Pulse model:

- `pulse_module_state`, `pulse_module_audit`, `pulse_review_notes`,
  `pulse_review_focus_state`, `pulse_review_focus_history`,
  `pulse_module_result_history`, `pulse_review_recovery`, `pulse_review_log`;
- `run_concerns`, `pulse_finding_details`, `pulse_finding_events`,
  `pulse_fix_attempts`, `pulse_fix_attempt_findings`,
  `pulse_fix_verifications`;
- Pulse-specific copies of human-decision event, claim and apply-contract state;
- Pulse command, context, shadow-signal and agent telemetry tables when their
  data belongs to the scheduler or platform telemetry instead;
- intervention/impact tables unless a separately approved product requirement
  demonstrates information that cannot be represented by the review, issue or
  action taken.

`org_dashboard_notifications` may remain for ordinary run notifications, but
Pulse Activity must read/project from `pulse_reviews` rather than storing a
second independently authored Pulse summary.

## Acceptance criteria

- A complete Pulse pass can be explained entirely from the three target tables.
- Reviewer results, open issues, closed actions and pending/answered decisions
  match the pre-migration UI on representative workflow databases.
- The runtime has no verification/monitoring/fix-attempt state machine for
  Pulse issues.
- Reporting an issue creates/updates one `open` row. Completing the fix writes
  `action_taken` and closes it. Failure to fix leaves it open.
- Activity displays the stored review summary without a publish/update tool or
  duplicate prose record.
- Migration is reversible during the rollback window and never silently drops
  an active issue or pending human decision.

## Explicit deferral

This ticket records the recommended direction only. Do not begin the schema,
tool, prompt or UI migration until it is separately prioritized. Current Pulse
storage remains authoritative in the meantime.
