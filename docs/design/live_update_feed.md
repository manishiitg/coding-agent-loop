# Live update feed: one SSE per tab instead of timers

Status: proposed (2026-09-24). Owner: TBD.

## Problem

With one user idle on Dominion, the browser makes 150–220 API calls every
5 minutes. Almost all of them are timers that ask "did anything change?" and
get "no":

| Endpoint | Caller | Interval (after the 2026-09-24 cuts) |
|---|---|---|
| `GET /api/header-summary` | `GlobalActivityMonitor.tsx:219`, `useChatStore` safety poll | 10s, plus a 60s safety poll |
| `GET /api/workflow/pulse-module-state` (252 KB) | `WorkspaceViewHost.tsx` | 30s while the Pulse view is open |
| `GET /api/workflow/plan-changelog?limit=1` | `usePlanData.ts` | 30s while the Plan canvas is visible |
| `GET /api/org-dashboard/notifications` | `GlobalActivityButton`, `WorkflowActivityButton` | 30s each |
| `GET /api/report-human-inputs[/aggregate]` | `usePendingDecisionCount`, the activity buttons | 30s each |
| `GET /api/scheduler/config` | `useGlobalSchedulerPaused.ts` | 30s |
| `GET /api/browser/sessions`, `/api/wp/api/processes`, `/api/wp/api/browser/processes` | `RuntimeHealthControl.tsx` | 30s, even in a hidden tab |

This design has two costs:

- **Slow networks.** On a lossy link, polling fills the connection and delays
  the requests the user is waiting on. On 2026-09-24 page reloads took 5
  minutes.
- **Slow updates.** A change shows up only on the next timer tick, 10–30s
  later.

## Proposal

The server keeps **one** Server-Sent Events connection per browser tab. Over
it the server pushes small **change notices**. A notice says what changed and
where. It carries no data. Each panel keeps its existing fetch and calls it
only when a relevant notice arrives.

```
browser tab ──GET /api/live (SSE)──> agent server
                                      ▲
   writers ── livefeed.Publish(kind, workflow) ──┘
```

Chat sessions keep their own streams (`/api/sessions/{id}/events/stream`)
for now. Folding them into this connection is Phase 4.

### Wire format

```
event: change
data: {"kind":"pulse_state","workflow":"Workflow/tectonic-usa-day-trading"}

event: resync          # on connect, and after any dropped notices
data: {}

: heartbeat            # every 15s (Cloudflare closes idle streams at 100s)
```

These are the kinds, and the refetch each one triggers:

| kind | Scope | Client refetches |
|---|---|---|
| `sessions` | user | `header-summary` |
| `schedules` | global | `header-summary` (schedule summary) |
| `plan` | workflow | plan and changelog head (only while the canvas is visible) |
| `pulse_state` | workflow | `pulse-module-state` (only while the Pulse view is open) |
| `notifications` | workflow | org-dashboard notifications |
| `human_inputs` | workflow | `report-human-inputs` and the aggregate |
| `scheduler_config` | global | `scheduler/config` |
| `browser_sessions` | global | `browser/sessions` |

A notice never contains the changed data. The client always refetches through
the existing endpoint, which applies its own access rules. This keeps the
feed small and keeps one source of truth for every shape.

### Server

**Package `agent_go/internal/livefeed`.** It is importable from both
`cmd/server` and `pkg/...`, so there is no import cycle.

- `Publish(kind string, workflow string)`: non-blocking and safe from any
  goroutine. If there is no subscriber it does nothing.
- Each subscriber has a small buffered channel.
  - **Coalescing:** a burst of writes, such as a Pulse run updating 20 rows,
    becomes one notice per `(kind, workflow)`. The connection loop flushes
    pending notices every 250ms.
  - **Overflow:** if the buffer fills, the subscriber is marked dirty and
    gets one `resync` instead of notices being silently dropped.
- Everything runs in one process. Each deployment runs one agent server, so
  no Redis or other broker is needed.

**Route `GET /api/live`.**

- **Auth:** the existing `AuthMiddleware`. `?token=` is already supported for
  EventSource.
- **Filtering:** it drops workflow-scoped notices for workflows the user
  cannot see.
  - The check uses `workflowAccessForWorkspacePath`, cached per connection.
  - The cache is dropped when `manifestMutationGeneration` changes.
  - `sessions` notices go to every connection; `header-summary` already
    filters the sessions on refetch.
- **Headers:** the same as `handleSSEStream`: no write deadline and
  `X-Accel-Buffering: no`.

**Where the server publishes.** File:line references come from the
2026-09-24 survey.

| kind | Publish from |
|---|---|
| `plan` | `writePlanChangelogEntry` (planning_agent.go:1343) covers ~24 `logPlanChange` callers. Also `writePlanToFile` (:2671), `writePlanToWorkspace` (workflow.go:2082), `handlePrunePlanChangelog`, and `markChangelogArtifactReviewed`. The writers are centralized, and raw tools cannot write under `planning/` (planning_file_write_access.go). |
| `pulse_state` | About 20 writers in `pulse_worklist.go`, `pulse_final_commands.go`, `pulse_goal_work.go`, `pulse_schedule.go`, `pulse_fast_requests.go`, `pulse_fix_run.go` and `pulse_review_notes.go`. Add one `notePulseStateChanged(ws)` helper and call it after each commit. Also publish on `workflow.json` writes (`noteWorkspaceMutation` already sees them), because the response includes autonomy, focus areas and next pulse. |
| `sessions` | The `activeSessions` and `trackedWorkflowExecutions` mutators (server.go `trackActiveSession`, `updateSessionStatus`, `handleDismissSession`, `cleanupInactiveSessionsAt`, `handleQuery` start/finish; `workflow_execution_tracker.go` start/finish/cancel). **Not** `updateSessionActivity`, which runs on every event: publish only when a status changes. |
| `schedules` | `noteScheduleSummaryChange()` (header_summary_cache.go:19). It is already the invalidation point for this data. |
| `notifications` | `SendUserNotification` and `UpsertPulseResultActivity` (services/org_dashboard_connector.go). |
| `human_inputs` | `create`/`answer`/`dismiss`/`consumeReportHumanInput` (report_human_inputs.go), plus three outliers: `dismissDuplicateHumanInput`, `consumeLinkedPulseDecisionTx`, `activateApprovedAdvisorSpecialization`. |
| `scheduler_config` | `SaveSchedulerConfig` (scheduler_config_store.go:43). |
| `browser_sessions` | The `browser.SessionTracker` mutators (`Touch` on first sight only, `Remove*`, `Close*`, `Clear`). The client computes `age`/`idle` from timestamps instead of polling for them. |

**Writes the feed cannot see.** Coding CLIs and shell `/api/execute` write
straight to disk, and the workspace service's `/api/mutate` writes
`db.sqlite` without going through the agent server. The client covers these
three ways:

1. **Resync** on every (re)connect, and whenever the tab becomes visible
   again.
2. **Session completion.** Most of these writes happen during an agent turn.
   When a session reaches a terminal status, the server publishes
   `pulse_state`, `human_inputs` and `notifications` for that session's
   workflow. `ChatArea.tsx:2074` already does this in the client for
   `WORKFLOW_LOG_REFRESH_EVENT`.
3. **Safety poll.** A slow poll every 5 minutes, only while the tab is
   visible. It replaces the current 10–30s timers.

Workspace processes (`/api/processes`, `/api/browser/processes`) come from
`ps` snapshots in a separate service and have nothing to publish. They stay
polled, but only while the tab is visible and the health menu is open.

### Client

**`frontend/src/services/liveFeed.ts`** is a singleton.

- It opens the EventSource once, using the same token handling as
  `shared/session/sse.ts`.
- API: `subscribe(kind, workflow | null, onChange) → unsubscribe` and
  `onResync(cb)`. The hook form is `useLiveRefetch(kind, workflow, refetch,
  {enabled})`.
- **Reconnect:** exponential backoff, and a `resync` on reconnect.
- **Fallback:** if the stream cannot connect, it reports
  `status: 'polling'` and each hook falls back to its old interval. Nothing
  depends on the feed to work.

Each panel keeps its existing fetch and replaces `setInterval(fetch, N)` with
`useLiveRefetch(kind, workflow, fetch, {enabled: visible})`.

### Expected effect

| | Today | With the feed |
|---|---|---|
| Idle requests per tab | ~30/min | ~0.2/min (safety poll) + one open stream |
| Delay before a change shows | 10–30s | <1s |
| Pulse view while a run is working | 252 KB every 30s | 252 KB once per coalesced change |

### Connection budget

This adds one long-lived connection per tab.

- **Behind Cloudflare (HTTP/2):** all streams share one TCP connection, so
  this costs nothing.
- **Plain HTTP/1.1 (local dev, some self-hosted setups):** the browser cap of
  6 connections per origin already squeezes the per-session chat streams
  (`ChatArea.tsx:2397`). This adds one more. Phase 4 removes that pressure by
  carrying session events on this same connection.

## Phases

1. **Feed plus the biggest pollers.** `livefeed` package, `/api/live`, the
   client singleton, `sessions`/`schedules` (header-summary),
   `human_inputs`, `notifications`. This removes about 80% of the idle
   traffic.
2. **Workflow views.** `pulse_state` and `plan`. Drop the Pulse and canvas
   timers, and add publish-on-session-completion.
3. **The rest.** `scheduler_config` and `browser_sessions`. Gate
   `RuntimeHealthControl` on tab visibility.
4. **Optional.** Carry per-session chat events on the same connection
   (subscribe/unsubscribe per open chat over a small POST) and retire the
   per-session EventSources.

Each phase keeps the polling fallback. A phase is done only when the gateway
log on Dominion shows that endpoint's idle rate near zero.

## Testing

- **Unit:** `livefeed` coalescing, overflow→resync, and access filtering.
- **Live (e2e):** with a real Builder turn that adds a step, the canvas
  updates within 1s and has no timer running. The same for a Pulse run
  updating the Pulse view.
- **Traffic check:** after deploy, the per-endpoint rates in
  `/srv/dominion/logs/gateway.log` for an idle tab.

## Related finding (separate fix)

These read handlers accept any `workspace_path` and never check workflow
read access:

- `plan-changelog`
- `pulse-module-state`
- `org-dashboard/notifications`
- `report-human-inputs`
- `browser/sessions`

`requireWorkspacePath` only validates the path. They should call
`requireWorkflowVisible` (workflow_access.go:220). This should be tracked
separately from the feed.
