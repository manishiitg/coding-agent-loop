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
