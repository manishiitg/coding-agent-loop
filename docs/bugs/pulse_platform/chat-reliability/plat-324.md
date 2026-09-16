[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-324 — open Work chat tabs must retain their conversation across reloads and deployments

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented and deployed; reload/rebind live-verified, context-dependent follow-up matrix remains` |
| Last synchronized | `2026-09-16` |

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
