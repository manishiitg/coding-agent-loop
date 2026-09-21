[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-343 — Workflow switch briefly rendered the previous workflow transcript

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented; focused regression tests green; deployment pending` |
| Last synchronized | `2026-09-21` |
| Priority | `P0 chat correctness / navigation` |

## Report

After selecting another workflow, the header changed immediately but the chat
pane continued showing the old workflow's messages while the new workflow's
session and tab were being resolved. On a slow server this made unrelated
messages appear to belong to the newly selected workflow.

## Cause

Workflow selection is synchronous, but `openWorkflowPresetPage` awaits active
sessions, running-workflow lookup, and tab restoration before activating the
destination tab. The chat renderer treated the still-active source tab as valid
during this interval and had no ownership check between the selected workflow
and the active workflow tab.

## Fix

The workflow surface now requires the active tab's `presetQueryId` to match the
currently selected workflow. A mismatch renders the existing loading surface
immediately, even if the old tab has content. Once destination activation is
atomic and the IDs match, the destination transcript renders normally.

This does not clear, merge, or mutate either workflow's events; it only prevents
the source workflow from being displayed under the destination workflow's UI.

## Verification

- Resolver regression coverage verifies that source content cannot override a
  workflow-ownership mismatch.
- Existing workflow surface resolver coverage remains green.
- Deployment and production navigation verification remain pending.
