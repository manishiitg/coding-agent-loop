[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-336 — Workflow Automation Chats omitted scheduled conversations

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented on main; deployment pending` |
| Last synchronized | `2026-09-21` |
| Priority | `P1 visibility` |

## Symptom

In the `twitter-automation` workflow (stored at `Workflow/social-media`), the
Automation → Chats section did not show the latest schedule conversations even
though those runs had completed and their transcripts were available elsewhere.

## Evidence

The data was not lost. The workflow had fresh conversation and resume files for
three schedule sessions completed between 09:22 and 09:25 IST on 2026-09-21:

- `schedule-cron--d4007648_1789962659658760000`
- `schedule-cron--4128e261_1789962659658374000`
- `schedule-queued--654c48e6_1789962779626804000`

All three sessions were also present in
`Workflow/social-media/builder/conversation/chat-index.json`, with message
counts and previews. Persistence and indexing were therefore healthy.

## Root cause

The workflow Automation hub rendered `WorkflowPreviousChatsPanel` with
`recentOnly`. `PreviousChatHistoryPanel` interprets that combination as a
`kind=chat` API request unless `includeAutomationChats` is also enabled. The
Crew Automation hub already passed `includeAutomationChats`; the workflow hub
did not. As a result, schedule, webhook, and bot transcripts were deliberately
filtered out before the Chats section rendered them.

## Fix

Commit `b812cdb48` enables `includeAutomationChats` for the workflow hub's
unified Chats index. It still keeps the Schedules and Triggers sections as the
run-centric views with status and delivery metadata; Chats now supplies the
chronological conversation index the label promises.

## Verification

- Added a regression that returns a newer `schedule-cron--…` transcript from
  the unfiltered history request and an ordinary Builder conversation from the
  `kind=chat` request, then verifies both appear in the unified Chats list.
- Added a workflow-layout wiring assertion for
  `includeAutomationChats={chatOnly}`.
- Focused frontend suite: 17/17 tests pass.
- `git diff --check` passes.
- The full frontend pre-commit build is currently blocked by a pre-existing
  TypeScript error in unchanged file
  `GlobalActivityMonitor.dropdown.test.tsx:124`; this ticket did not alter that
  file or its types.

Deployment and live verification in the `twitter-automation` Chats section
remain pending.
