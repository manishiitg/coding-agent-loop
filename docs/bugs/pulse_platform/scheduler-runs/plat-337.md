[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-337 — Legacy scheduled workflows lost their action-tool identity

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented on main; live restart verification pending` |
| Last synchronized | `2026-09-21` |
| Priority | `P0 execution` |

## Symptom

Every action-capable tool in fresh `twitter-automation` schedule chats failed
with `<tool> requires an authenticated session`. This included
`execute_shell_command`, `agent_browser`, and `diff_patch_workspace_file`.
Read-only bridge discovery such as `get_api_spec` and `read_skill` still worked,
which made the failure look like several unrelated tool or provider outages.
The same boundary failure was present in other ownerless legacy schedules,
including `salesoutreach` and `websiteaeo`; this was a platform-wide legacy
schedule regression, not a Twitter-specific fault.

## Evidence

The affected workflow is stored at `Workflow/social-media`. Its legacy
`workflow.json` has neither `created_by` nor an `access` ownership block.
Scheduled session tool results consistently recorded the same authorization
failure, including session
`schedule-cron--5227790a_1789963736158053000` on 2026-09-21.

The failure began after commit `74c66ab301`, which correctly made the shared
tool-registration boundary require authenticated claims. The scheduler still
copied only `WorkflowManifest.CreatedBy` into `ScheduleContext.OwnerUserID`.
For a legacy manifest that value was empty, so `startSessionInternal` created an
internal `/api/query` request with no `UserClaims`; every action tool was then
correctly rejected by `bindToolExecutionContext`.

This was not a LinkedIn login failure and retrying tools could not repair it.

## Fix

`buildScheduleContext` now resolves a durable scheduled execution identity in
this order:

1. the manifest's `created_by` value;
2. the first recorded access owner for older partially backfilled manifests;
3. the configured local owner for an ownerless legacy manifest, **only in
   single-user mode**.

An ownerless multi-user workflow remains unresolved and blocked. The fix does
not weaken the shared authentication boundary, fabricate a multi-user owner,
or permit model-supplied identity.

## Regression coverage

`TestBuildScheduleContextThreadsOwnerUserID` now covers all four ownership
states: creator, access owner, ownerless single-user legacy workflow, and
ownerless multi-user workflow. The single-user case explicitly verifies that
the scheduler supplies a non-empty authenticated principal rather than relying
on the retired empty-string fallback.

## Verification

- focused scheduler and tool-execution-context tests pass;
- the focused scheduler/tool-boundary suite passes; the complete `cmd/server`
  run is currently red on the unrelated in-progress internet-share work
  (`manage_internet_share` is exposed but not yet declared in `product.yaml`),
  and one cross-process allocator test was flaky in the full run but passed on
  its immediate focused rerun;
- `git diff --check` passes;
- live schedule verification requires restarting the currently running Go
  server on the fixed commit, then triggering the smallest
  `twitter-automation` schedule action.
