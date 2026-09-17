[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-324 — open Work/Crew chat tabs must retain their conversation across reloads and deployments

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `deployed to RTS; health verified; user live chat acceptance pending` |
| Last synchronized | `2026-09-17` |
| Latest regression fix | `eb93a5972` deployed — shared queue ownership for human-decision and generated report chat actions |
| Previous deployed regression fix | `1e87e0186` — retain live CLI finals across stale hydration |

## Post-incident assessment — 2026-09-17

The broad reliability change `1979a25ef` was too large for one rollout: it
changed 58 files and added about 3,400 lines across persistence, submission
receipts, queue ownership, native recovery, multi-user isolation and background
notifications. It introduced two confirmed regressions:

- workflow-scoped sessions could not recover their project after a backend
  restart because the new durable fallback began with an empty workspace path;
- conversation snapshot reconciliation used the shorter incoming runtime
  snapshot as the merge base, allowing prior history blocks to be appended
  again.

Two other incidents were pre-existing defects exposed during this rollout:

- the ChatInput live-delivery effect had bypassed the shared queue worker since
  August. The new durable idempotency journal made that race visible as a 409
  instead of allowing a possible second delivery. For the observed Strategic
  Review action, the first request was durably accepted and the conflicting
  second attempt was rejected;
- the background completion retry sweep predated this change and did not honor
  `suppress_auto_notification`, so it could revive a deliberately suppressed
  child completion.

The architectural failure is split message ownership. Browser storage, React
effects, the queue worker, `/api/query`, `/live-input`, in-memory session maps
and durable history were each able to make independent routing or persistence
decisions. Durability was added around that structure before every entry point
shared one immutable submission lifecycle.

The targeted corrections have fail-before/pass-after coverage and are deployed,
but service health is not proof of end-to-end chat correctness. This ticket
remains open until live acceptance covers two users, multiple tabs, idle and
running turns, browser reload, backend restart and deployment. Known remaining
limits are process-local conversation locks, no general exactly-once guarantee
after an uncertain provider response, and already-corrupted duplicate history
that has deliberately not been rewritten.

- **Priority:** P0 — a follow-up sent from an already-open Work chat reached a
  fresh agent conversation. The visible transcript remained in the tab, but the
  agent had no knowledge of it and asked the user to restate prior context.
- **Category:** Chat Reliability.
- **Owner:** Work tab hydration, product-conversation registry binding, and the
  profile chat submission boundary.

## Regression

Work restores its tabs from browser storage after a page refresh. Those tabs
retain their application session id and visible conversation, but project
preparation only hydrated messages and runtime selection. It did not rebind the
tab's saved session to the server-owned product conversation registry before
enabling the composer.

The send route treats the registry as authoritative. After a deployment or
server restart, a stale/shared registry key could therefore resolve to a newer
or empty session even though the open tab displayed an older conversation. The
message was delivered, but to a fresh provider conversation with no prior
context.

Legacy Work tabs make this worse because they used the project id itself as the
conversation key, the same key as the permanent Builder launch surface.

The production transcript supplied direct evidence: after 191 messages in the
same visible chat, the 2026-09-16 follow-up was answered with “I don't have
prior context … this looks like the start of a new session.”

## Fix

Released in `b4beba011` (`Preserve Work chat resume across deployments`). Work
project preparation now blocks readiness while it rebinds every retained tab's
saved session, and upgrades legacy project keys to tab-specific keys. The
profile query boundary also compares each authenticated `X-Session-ID` with the
tab-specific registry record and switches back to the verified project session
when they drift. This backend guard covers tabs that remain open while a new
release is deployed.

Custom chat titles also became visually detached from resumed conversations.
The title remained in the transcript and the older dated index row, but a
next-day resume created a newer index row with an empty title. Recent chats
then preferred that newer row and fell back to the latest user message. Chat
titles now follow the durable session id across dated files; listing repairs
already-stale rows, saving inherits the session title, and renaming updates
every indexed copy of the session.

## Required contract

1. An open Work chat tab can survive any number of frontend refreshes and
   backend deployments without being closed or reopened.
2. Before its composer becomes ready, the tab's durable application session is
   rebound to its server-owned product conversation key.
   The server repeats this verified comparison at every send so a tab that
   remained open during a deployment is protected without loading new
   frontend code first.
3. Each retained chat has a tab-specific conversation key. Legacy project-key
   tabs are upgraded without changing their saved session.
4. If the saved conversation cannot be verified, sending must fail visibly;
   the platform must never silently start a fresh agent conversation.
5. The next message resumes the same provider-native conversation when the
   provider supports native resume, and otherwise receives the saved transcript
   through the established cross-provider fallback.

## Acceptance

- Open multiple Work chat tabs, complete at least one turn in each, deploy a new
  server/frontend release, refresh the page, and send a context-dependent
  follow-up from every tab. Each agent must answer from its own prior context.
- Repeat after a backend-only restart and after closing/reopening the browser.
- Verify legacy project-key tabs are upgraded to distinct keys and do not merge
  with Builder or with one another.
- Verify an unverifiable saved session blocks readiness with an explicit error
  instead of accepting a context-free turn.

## Verification

- Focused frontend tests: 33 passed, covering Work key migration, tab-store
  hydration and profile submission payloads.
- Focused server tests passed, including the verified Work-only rebind guard.
- Chat-history regression tests cover title inheritance across dated resume
  paths and self-repair of existing untitled index rows.
- Production release `b4beba0-20260916081750` deployed successfully; public
  health and frontend returned 200.
- Browser verification reopened the affected 191-message chat and then reloaded
  the page. The same named tab and full transcript remained active after the
  reload. No message was sent into the user's production chat during this
  verification, so the multi-tab context-dependent follow-up matrix remains.

## 2026-09-16 multiline paste follow-up

Three concurrently open Work tabs were reported as cross-wiring a pasted
message. Production request and provider logs disproved cross-tab routing: the
paste was delivered to the selected `faceexpression` application session, while
`prodissue` was independently executing in its own session. The actual failure
was below the Work tab boundary. Claude Code converts bracketed multiline and
large pastes into an attachment asynchronously, and the retained-input adapter
pressed Enter before that conversion settled. Claude therefore received an
empty turn while the paste remained in its editor.

The provider fix is `multi-llm-provider-go` commit `8e97d8c` (`Wait for Claude
multiline paste before submit`). Multiline or large retained input now waits for
the Claude prompt/attachment to settle before Enter. A settlement failure is
returned to the caller instead of being reported as successful delivery; short
single-line steering keeps the immediate path. Focused and full Claude adapter
tests pass. Cursor and Pi already wait for a visible draft before submit, Muse
waits for and expands its pasted-content attachment, and Codex verifies the
rollout/prompt transition with resubmission; their relevant adapter suites pass.
Production release `8623c20-20260916120839` deployed successfully and public
health returned healthy. A non-destructive live multiline send remains to be
verified.

The same incident also exposed a frontend isolation weakness even though the
captured request was routed correctly: Crew reused one mounted `ChatArea` and
`ChatInput` while changing its `tabId`. Drafts lived durably per tab, but local
paste, picker, upload and submit closures could briefly retain the preceding
tab during a rapid switch. Crew now remounts the chat/composer at each tab
boundary. Before unmount, a pending debounced draft is flushed to its owning
tab, so switching cannot lose the draft or carry it into the next composer.
AgentWorks' workflow chat host uses the same tab-keyed boundary and the same
shared draft-flush safeguard.

## 2026-09-16 resumed-tab transcript regression

Opening the saved `gptlive1` chat exposed a second regression in another open
Crew tab. A later message in `faceexpression` reached the correct application
session, Claude completed it, and the server emitted both the transcript chunk
and structured completion. The raw terminal showed the full turn, while the
formatted conversation stopped at the preceding answer.

Two frontend lifecycle choices combined to cause the loss:

1. The tab-isolation follow-up above keyed the entire `ChatArea`. Resuming or
   switching a chat therefore unmounted the observer that owns every open
   session's SSE connections and foreground catch-up loops. Composer isolation
   only requires a hard boundary around `ChatInput`; the session observer must
   remain mounted across tab switches.
2. `hydrateTabEvents` painted durable history as soon as that request resolved,
   then projected the same history a second time when the live event request
   completed. The first projection marked the optimistic user row as restored;
   the second projection intentionally discards previously restored trace. If
   the live window was empty or delayed, the second paint erased the new user
   row and answer from Chat even though the provider terminal retained them.

The correction keys only the shared `ChatInput`, preserving per-tab draft,
paste, upload and submit state without restarting `ChatArea`. Durable history
and the live window are now reconciled once, so a stale history response cannot
temporarily mark and then discard the current retained turn. A regression test
resolves history before an empty live window and verifies that the optimistic
retained message remains present through the final commit. The focused suite
passes 30 tests and TypeScript compilation passes.

Production read-only verification after that frontend release showed the
already completed turn still disappeared after a server restart. The retained
Claude conversation was intact, but its Work transcript had never been updated.
Production logs showed both `LIVE INPUT` and `RETAINED_TURN` for the correct
session with no intervening `CHAT_HISTORY` write.

The retained persistence code had two project-scope errors. The live-input
writer searched global history because it omitted the Work workspace path, and
the completion-time native transcript reconciler hardcoded the legacy
`default` owner even when the active session belonged to another user. Both
lookups therefore missed the project-owned file without reporting a delivery
failure. Live-input persistence now uses the session's resolved workspace, and
native reconciliation carries the authenticated session owner through its
read, path resolution and index update. The resume HTTP path passes its
authenticated owner through the same function. Regression coverage uses a
non-default user and `_users/<id>/Chats/Work/projects/<project>` transcript,
verifying both the resumed human append and native assistant reconciliation.

## 2026-09-16 consecutive-message completion regression

Production reproduction in the `ci/cd` Work chat isolated the remaining
multiple-message failure. The user submitted a second message nine seconds
after the first, while Claude was still answering. Both messages reached the
same Claude session, and the native transcript contains complete assistant
answers to both. The application event stream stopped after the first answer,
however, leaving the second answer visible only in Terminal. Chat rendered an
empty Agent row followed by a misplaced `Previous conversation` divider.

The retained-turn lifecycle stored later execution ids as aliases of the
currently running turn. The first completion therefore marked every accepted
message complete, canceled the terminal observer, and removed the session's
tracking state. This assumption is invalid for coding CLIs: input submitted
while busy is queued as a distinct provider turn with its own completion.
Additionally, the warm MCP session's event bridge can close after the first
answer even though the retained CLI immediately continues with queued input.

Retained executions are now kept in submission order. One structured
completion settles only the active execution; if another accepted message is
pending, it becomes the active execution, the session remains busy, and a new
retained-terminal observer follows that response through its own completion.
Provider failure still fails the active and pending executions together.
Regression coverage submits two retained messages before the first completion
and verifies that the first completion promotes the second, reattaches an
observer, and does not settle the session until the second completion arrives.
The formatted chat also suppresses the `conversation_resumed` lifecycle marker;
older pages remain available through the transcript's existing top pagination
control, without a misleading divider below the newest restored answer.

## 2026-09-16 Cursor live-publication regression

The `rts-pr-reviweer` Cursor session produced the complete “Here are the
links” answer in Cursor's native SQLite transcript and Terminal. Native
transcript catch-up subsequently merged it into the durable Work conversation,
so a refresh restored the answer, but the already-open formatted Chat remained
blank. Production logs showed several Cursor completions with
`final_response_missing` followed by successful native transcript merges.

The catch-up path previously updated only the conversation file and its index.
It now also publishes each newly recovered assistant message to the session's
live EventStore using the existing whole-message transcript event contract.
Recovered messages therefore appear immediately in an open Chat and remain
available through SSE backfill, while the event is deliberately not a second
completion signal that could settle the next queued turn. Exact assistant-text
occurrence counts across durable history and existing main-agent events prevent
duplicate rows, including repeated sync attempts and replies that already
arrived through the normal live stream. This provider-neutral backstop applies
to Claude Code, Codex, Cursor, Pi, and Muse native transcript recovery.

Production verification of the first release exposed the restart variant: the
durable conversation was already current, so reconciliation returned early
while the restored UI-event trace still lacked several final replies. An
unchanged reconciliation now compares the recent canonical conversation tail
with the live EventStore and republishes only missing assistant rows. This makes
refresh and deployment recovery repair answers that were persisted before the
new server process started, as well as answers recovered during the current
process.

Live verification then exposed a final timing race in the same Cursor path.
The reconciliation scheduler stopped as soon as any transcript change was
observed, even when that change contained only progress, and its three retries
ended about two seconds after completion. Cursor can flush the final assistant
message later than that. The scheduler now continues a bounded series of
reconciliations for roughly fifteen seconds after completion, even when an
earlier pass recovered progress. The existing occurrence-count deduplication
makes those follow-up reads idempotent while ensuring the delayed final is
published to Chat and the session reaches its ready state.

## 2026-09-16 final-message flash regression

A later Cursor completion exposed a frontend-only race: the final answer was
visible from the live transcript chunk, disappeared when the stream settled,
and returned only after durable history caught up or the page was refreshed.
The Cursor terminal and durable provider transcript both contained the answer.

The first completion hydration correctly merged the raw live completion into
the formatted timeline and marked it as persisted trace. A second hydration
that had started against older durable history treated every marked trace event
as a generated restore row and discarded it. That rule was too broad: synthetic
`restored-*` conversation carriers have generated timestamps, while marked
transport events retain stable backend IDs and real timestamps.

Repeated hydration now drops only synthetic restored carriers. Raw traced
events remain in the merge and deduplicate by their stable IDs, so an older
history response cannot remove a live final while provider persistence catches
up. The regression test performs two hydrations against stale history, with the
second live window empty, and verifies that the Cursor completion remains.

### Production verification

- Commit `2f387fa37` (`Keep reconciling delayed CLI final replies`) was pushed
  to `main` and deployed in release `2f387fa-20260916174454`.
- In session `c7d58080-0058-4f6f-a394-feea651f405b`, Terminal completed with
  “Done. Local output is now just one line…” while formatted Chat initially
  stopped at the preceding progress row. Transcript reconciliation then
  published the omitted final, Chat displayed the complete answer, and the
  session changed from running to ready.
- Production `/api/health` reported `healthy`, an idle drain, and all agent,
  workspace, and gateway services remained active after the release swap.
- Focused native-transcript recovery and live-publication tests pass. The
  full server suite retains its unrelated existing Claude model-discovery
  expectation failure.
- Commit `1e87e0186` (`Keep live CLI finals across stale hydration`) was pushed
  to `main` and deployed in RTS release `1e87e01-20260916183335`.
- The frontend regression suite performs two stale-history hydrations, with an
  empty live window on the second pass, and verifies that the stable-ID Cursor
  final remains visible. All 19 focused session-restore/product-fallback tests
  and TypeScript compilation passed.
- The release activated after draining the active turn without interruption.
  The public health endpoint then reported `healthy`, zero active sessions and
  an idle drain. The later `bdcbb00-20260916184549` release also contains this
  correction.


## 2026-09-17 architecture and multi-user/multi-tab hardening

The repeated incidents exposed shared lifecycle gaps: selected-tab state could
influence delayed submissions, stale saved-history snapshots could replace newer
answers, CLI delivery could precede durable acceptance, and recovery demand could
be lost while another reconciliation was running. The earlier production
verification above applies to earlier releases, not to this new fix set.

Implemented locally:

- Capture account, source tab, session and submission identity through delayed
  input, queue, Builder handoff and response callbacks. Resolve workflow settings
  from the owning tab; never use selection as a delivery destination.
- Namespace persisted tabs, drafts and queues by workspace and user. Invalidate
  old-account callbacks across login, OAuth, refresh, logout and browser windows.
- Protect composer drafts with store-owned revisions across remounts. Consume
  submitted attachments only after acceptance, preserving newer edits.
- Process queues independently of the selected chat; retain rejected messages,
  share manual/background delivery locks and reuse stable submission receipts.
- Reject stale hydration using session/account guards and canonical revisions.
  Serialize transcript read/merge/write within the backend process and preserve
  newer persisted answers and structured metadata.
- Persist interactive acceptance journals before dispatch. Return explicit
  uncertainty for ambiguous delivery instead of automatically resending through
  another endpoint. Fail visibly when established Work continuation cannot
  verify the requested conversation.
- Persist owner-scoped native recovery demand, retain demand arriving during
  reconciliation, and retry unresolved native flushes on restart and every
  30 seconds through a cancellable four-worker scanner.
- Remove view-owned global SSE teardown and automatic schedule/webhook chat-tab
  discovery. Explicit access to those runs remains available. The separate
  agent's auto-notification work was preserved.

### Local verification and release status

- 147 frontend tests across 15 files passed; TypeScript `npx tsc -b` passed.
- Combined Go suites passed for conversation/profile identity, history writers,
  native recovery, acceptance journals, live input and session ownership,
  including existing auto-notification ownership tests.
- `git diff --check` passed.
- No commit, deployment or live provider/browser restart matrix was performed
  for these changes. Keep live acceptance pending: two users with multiple
  tabs, reload/deployment during delivery, and context-dependent follow-ups
  through each supported CLI must verify both visible history and provider context.
- Transcript locking is process-local; concurrent writing replicas still need
  storage-level coordination. Deployment must preserve the AgentWorks state root
  containing acceptance/recovery journals. Native adapters lack application
  turn IDs, so unresolved delivery remains explicit rather than claiming
  exactly-once provider execution.

See the [implementation report](../../../audits/chat-reliability-implementation-2026-09-17.md)
for the complete change mapping, validation logs and remaining architecture limits.


### RTS deployment — 2026-09-17

- Pushed implementation commit `1979a25ef1176d406f887785b355581374449ffb`
  to main and deployed release `1979a25-20260917063441` through the standard
  RTS server-side build and idle-drain activation procedure.
- Recorded sibling revisions: mcpagent `0220d4294b1fab3168611ed1c2c4263d005c281b`;
  multi-llm-provider-go `5e625eb6219be2fc5a7622989af1e18d3d0c28ed`.
- Native Linux backend and production frontend builds passed. Release asset
  checks and bundle limits passed (JS gzip 975.43 kB, below the 1030 kB limit).
  The deploy's Landlock overlap smoke test skipped because its config directory
  was not writable; it is not recorded as verified by this release.
- Confirmed current release symlink and source revisions, all three user
  services active, public health healthy with zero active sessions and idle
  drain, and frontend HTTP 200 at 06:40 UTC.
- The service's persistent config root is writable and lives outside releases;
  native recovery markers survive release replacement. Acceptance journals live
  in the persistent user-scoped workspace chat_history/submissions directory.
- User will perform live chat testing. Multi-tab/provider continuity acceptance
  remains pending; successful deployment and health do not close this ticket.


### 2026-09-17 post-deployment live-input regression — hotfix

The user reported an RTS `404 Session not found` at 12:54 IST and a separate
local workflow live-input failure. RTS logs show a successful normal Work query
and retained follow-up in `product-6a648cdb-fc00-43cc-b1db-2e200c9b9c7a`, followed
at 07:24:50 UTC by live-input to unrecognized browser session
`67007965-e580-4093-8268-0b0b67d86135`. The latter was rejected before dispatch
or journal acceptance. The exact browser transition remains unverified.

Code review also confirmed the new cold journal project resolver only requested
history with an empty workspace path, which misses workflow-scoped transcripts.
This is a regression/gap in commit `1979a25ef`; earlier passing injected-resolver
tests did not cover real workflow discovery after cache loss.

Follow-up work requires retained input to target a session known to the server,
and real owner-checked workflow history discovery after restart. Live acceptance
remains open; the deployed hardening did not yet satisfy the complete contract.


Hotfix implementation: retained live-input now requires the exact session to
appear in the server session cache; provisional/cold sessions use the ordinary
request carrying workspace and continuation context. Backend project recovery
now discovers workflow-scoped and private Work transcripts from disk or the
workspace API, verifies owner/session/path, and rehydrates the project binding.
Missing optional directories do not block recovery; ambiguous project matches
fail closed. Terminal authorization is unchanged.

Validation: 69 frontend tests across six files and TypeScript passed. New backend
regressions invoke the live-input handler with empty project caches and an actual
saved workflow transcript, verify restored journal attribution, reject another
owner, and cover workspace-API-only discovery. Focused journal/live-input tests
passed. These checks cover the previously missed cold-workflow lookup, not the
unobserved browser transition that produced the RTS provisional UUID.


Hotfix deployed to RTS as `4e2d78b-20260917073436` on 2026-09-17. Verified
current release, all three services active, public health healthy/idle and
frontend HTTP 200 at 07:39 UTC. The Linux backend and production frontend builds,
asset checks and bundle limits passed. The existing Landlock overlap smoke test
again skipped due to its config-directory permission issue. Live user acceptance
remains pending; this deployment did not replay or resend the user's failed input.


### 2026-09-17 duplicate exchange after live-input — confirmed, unresolved

User screenshot shows `did you trigger webhooks for it` twice. RTS logs show
one live-input delivery and one HTTP 200 at 07:49:35–07:49:37 UTC for
`c7d58080-0058-4f6f-a394-feea651f405b` (Workflow/rtsprreviweer).
Read-only inspection of that saved transcript found the exact human row and
its same adjacent assistant exchange repeated at different history offsets;
the copy count grew between reads while background processing continued.
This is persisted duplication, not merely an optimistic UI bubble. No duplicate
provider dispatch of that exact input was found in the inspected log.

History reconciliation is the suspected writer boundary: cumulative/native
snapshot merging can preserve earlier repeated blocks, and the continuation
merge appends an entire incoming segment when strict prefix/suffix matching
fails. Exact responsible save sequence still needs a deterministic reproducer.
No history deletion, production transcript repair or new fix is claimed here.


### Duplicate snapshot merge and suppressed-child retry — reproduced

A private replay of the affected production history established the snapshot
writer failure: with the saved prefix containing the reported message once,
merging a 114-message source snapshot using the old reversed ordering produced
two copies; merging the subsequent 126-message source produced three. Keeping
canonical saved history as the base retained one copy through the 114/126/132
snapshot sequence. The runtime source snapshots themselves each contained the
message once. A sanitized regression covers the same long-history/short-snapshot
shape without committing user conversation data.

A separate real-notification defect was identified: retry sweeping requeued
completed children marked suppress_auto_notification, even after producer-side
suppression. Production trace contains full-run, suppressed verify-and-repair
child, then Basic PR review completion prompts. The child caused an extra
actual model turn; it is distinct from persisted duplicate blocks.

The correction is deployed and its focused regression tests pass. Existing
corrupt history has not been deleted or automatically deduplicated, because
text-only deletion would also remove legitimate repeated turns.


Duplicate follow-up locally verified: both snapshot writers now preserve canonical
order and full tool content identity. Regression tests cover repeated saves,
retained tails, opaque metadata and distinct numeric tool payloads. Suppression
is enforced by retry, delay, batch, steer and atomic claim paths. Focused combined
snapshot/native/journal/notification Go tests pass. Existing duplicated rows are
preserved pending a separate verified repair; distinct enclosing run and step
notifications remain separate under current policy.

The Recent/Schedules/Bots/Webhooks filter header now uses panel-width container
queries: labels disappear at 560px and counts at 320px, with icon tooltips and
accessible button names retained. Chrome computed-style checks at 700/530/300px,
five existing filter tests and TypeScript passed.


2026-09-17 report/human-decision 409 follow-up: production recorded a successful
/api/query delivery at 08:05:04 UTC, immediately followed by a conflicting
/live-input attempt for the same session. The durable receipt confirms the
Strategic Review question was delivered; its original project was empty while
the live-input path resolved Workflow/rtslatency. The server correctly rejected
the second use of the same submission ID with different project metadata.

The ChatInput live queue effect bypassed the shared queue controller: when the
idle worker started streaming before its response settled, the effect could
remove and resubmit the same still-pending message. It now uses
sendQueuedChatMessage, sharing the worker's lock and durable receipt, retaining
entries until acceptance, respecting queue errors, and delivering separate human
actions separately. Both human-decision questions and generated report buttons
use sendWorkflowMessageToChat; neither entry point is removed or disabled.

Validation: 43 tests passed across queue ownership, human-decision context,
shared report dispatch, dashboard request IDs and live routing; TypeScript
passed. Added race cases for both entry points and preservation of later actions.
Live authenticated acceptance still needs the user's test.

The preceding duplicate-history/responsive release is now live on RTS as
4ad83b2-20260917080551 (includes b90b02639). Agent, workspace and gateway services
are active and local API health is healthy. Existing duplicate history remains
preserved; the fixes prevent the identified new duplicates.


Report queue follow-up deployed to RTS as eb93a59-20260917081444 at
2026-09-17 08:19 UTC. The current release symlink was verified, all three user
services (agent/workspace/gateway) are active, and the public /api/health is
healthy. The additional report-frame/bootstrap/decision-refresh checks passed
(13 tests), for 56 focused frontend tests overall; production frontend build,
release assets and bundle budget passed. Existing non-blocking Go lint warnings
remain; the unrelated Landlock deployment smoke skipped because its configured
GOG_HOME could not be created. No authenticated production actions were replayed.
User acceptance remains pending after a browser refresh: one decision question
and one generated report chat action during an active turn should each deliver
once to the intended interactive conversation with context intact.

### Work chat rename before first transcript

RTS reproduced a timing gap for a newly created Work tab. The frontend had a
valid product session ID and allowed rename immediately, while the backend
rename endpoint required the first conversation file to already be discoverable.
The initial request therefore returned `Session not found`; after the running
turn saved its transcript, the same rename succeeded. Production timestamps
show the first transcript at 08:34:55 UTC and the successful `latency` title at
08:37:06 UTC.

The backend now accepts an early rename only for the authenticated user's
private `Chats/Work/projects/...` scope. It records the pending title in that
project's durable chat index without fabricating a transcript. The first
conversation save adopts the indexed title into the transcript, then replaces
the pending index row with the completed metadata. Conversation and index locks
keep a simultaneous first-save/rename race ordered. Workflow-scoped chats retain
their existing author verification and do not use this fallback.

Regression coverage exercises rename before the first save, adoption by the
first transcript, completed index metadata, and a subsequent normal rename.
Focused chat-history, snapshot and submission tests pass. Deployment and live
acceptance are pending.
