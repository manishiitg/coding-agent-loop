# Durable submit acknowledgement: file-ack P0 + pane fast-confirm P1

**Status: all five providers implemented 2026-09-19/20. P1
demotion is OUT of scope by decision — pane fast-confirm stays
where it is; only the durable-ack P0 rolls out per provider.
Live P0 green for codex/pi/muse/Claude; live server↔chat e2e
PASS for the same four (probe: /tmp/durable-e2e/probe.py).
Cursor is implemented + unit-green, live P0/e2e blocked on login
quota till 9/21/2026. 2026-09-20 fix: durability receipts are
now consumed on every ingestion path including durable restore
(see "Restore-path receipt consumption" below).**

## Problem

A tmux `send-keys` exit 0 only proves tmux accepted the keystroke. Every
CLI adapter therefore confirms submission by string-matching the pane
(draft cleared, activity started, queued text visible). When the pane
*looks* wrong — redraw races, version drift in spinner/progress strings,
ghost/suggestion placeholders, compaction banners, deep scrollback —
the adapters return `failed to submit live input to <provider>` and
`mcpagent` surfaces that straight to the user instead of queueing
(`mcpagent/agent/message_delivery.go`: failed tmux submission is
returned to the caller). False negatives become user-visible errors;
the natural user reaction (retype and resend) creates duplicates.

Meanwhile the pane is also the only fast signal. The provider's own
rollout/transcript/marker file is authoritative for *what* the model
received but lags (milliseconds idle, unbounded while busy), and it
cannot see unsubmitted drafts, modals, trust prompts, or compaction.
Neither signal alone is sufficient.

## Probe evidence (2026-09-19, codex-cli 0.155.0, gpt-5.6-luna)

Hands-on TUI probe in an isolated tmux server, adapter-exact
load-buffer/paste-buffer/Enter sequence, scratch workdir:

* No rollout file exists before the workspace trust prompt is
  accepted. Trust stays pane-only.
* Rollout rows for a turn: `session_meta` (cwd, cli_version,
  session_id), `task_started`, `turn_context` (turn_id),
  `response_item` user row with the **exact message text**,
  assistant row with `phase=final_answer`, `token_usage_record` +
  `token_count`, terminal `task_complete {turn_id,
  last_agent_message, duration_ms}`.
* Idle submit to user row in file: ~0.2s warm, ~1s cold (first turn).
  Short turn to `task_complete`: ~4-5s.
* Mid-turn steer: native queue confirmed
  (`Messages to be submitted after next tool call` + `↳ <text>`).
  The queued user row landed in the **same turn** (no new
  `task_started`) **17s after Enter**, folded into the extended
  turn; one `task_complete` closed it; the model obeyed.
* Mid-turn pane reproduced the contract regression case: a
  `Planning ... (esc to interrupt)` indicator above a
  ready-looking `Ask Codex to do anything` footer.
* Chromes to keep ignoring: update banner, `model: loading`
  splash, per-turn `done <time>` lines.

Consequence: a file-only submit confirmation with an 8s budget
would have falsely failed a steer that worked perfectly. For
busy input the pane queue state is the fast confirmation and
the file is the eventual durability proof.

## Design

### Three-state fast outcome

Phase 1 (pane, existing budgets, resubmit Enters only here):

1. **Submitted** (draft cleared + activity/new turn) → success
   immediately. No file wait on the caller path.
2. **Queued** (native queue text visible) → held, not submitted.
   Observe-only from here: never send keys into a queued
   composer (Enter/Esc semantics on queued state are
   version-dependent).
3. **No signal** (draft sitting, no transition, no queue) →
   fall through to phase 2.

### Phase 2: observe-only file arbiter (per-provider budget)

Runs only when phase 1 does not report Submitted. Polls the
provider file for the user row matching this send (offset
snapshot taken before paste + exact/prefix text match, so an
identical earlier message cannot confirm a new one):

* Row appears → success. The pane misread (drift, blind
  scraper, slow TUI) is logged as provider-drift signal.
* Budget expires with the pane still positively showing the
  message queued → **accepted-but-unflushed**: held by the
  CLI, durability unconfirmed. Not an error; reconciled by
  the turn's terminal marker / next-turn validation.
* Budget expires with no signal in either channel → real
  error with pane snapshot, as today.

The budget is named and env-tunable per provider (same pattern
as `CODING_SDK_TMUX_PROMPT_WAIT_SECONDS`-style settings);
unit tests use injected probes, never sleeps. Defaults:
codex 180s (raised from 60 after a mid-turn steer landed at
97s — the watch is secondary, so the generous budget only
delays false negatives), cursor 180s (generous: busy path
still unknown, revisit after live P0), pi/muse/Claude 60s;
all clamped 5–300. Worst-case caller block lands only on the
path that fails today in ~8s, and most of those become
successes.

### Background audit (success path)

After a phase-1 Submitted, a bounded background check logs
(never errors) if the user row never lands. Converts silent
drops into observable drift without touching caller latency.

### Chat ticks (WhatsApp-style receipts)

* `✓` single = HTTP fast ack (pane confirm). Data already
  exists in `LiveInputResponse`.
* `✓✓` double = async durable confirm via new SSE event
  `live_input_confirmed {message_id, outcome, proof_source,
  latency_ms}`.
* `✓+clock` = accepted-but-unflushed. `!` = failed.
* Scoped to tmux live-input rows
  (`source: 'coding_agent_live_input'`) only.
* SSE, not blocking HTTP: `liveCodingAgentInputTimeout` is
  15s and P0 requires ack not to wait behind the session
  lane. Frontend upgrades the row matched by
  `metadata.message_id` (same pattern as the HTTP ack);
  reconnects re-query pending ids.

### Certification mapping

* New P0: durable-ack (file-backed acceptance) per provider.
* Dropped by decision 2026-09-19: no P1 demotion. The pane
  fast-confirm (resubmit budgets, handoff latency) stays
  exactly where it is; this rollout only ADDS the durable-ack
  P0 per provider. Revisit only if a dedicated P1 gate
  (`run-coding-cli-p1.sh` or equivalent) ever exists.
* Stays P0 on the pane permanently: queue/modal/draft/
  trust/compaction vetoes — no file signal exists. Folded
  into the P0 tests as negative assertions.
* Cursor exception: async `store.db` commit disqualifies
  file-ack from the hot path; pane stays P0, file is
  post-hoc/tiebreak only.

## Provider order

1. **Codex** (this doc): best markers, oracle pattern
   half-exists, representative JSONL playbook. Done.
2. **Pi**: `picli_durable_ack.go` (marker-offset receipts,
   `PI_DURABLE_ACK_SECONDS` budget, `Steering:`-keyed queue
   check — the positional matcher has no Pi equivalent, so
   the verdict keys on the native queue banner plus
   draft-gone-and-active); `piMarkersAcknowledgeUserMessage`
   now delegates to the timestamp-returning `piUserAckMarker`
   (same match, single source); `CertDurableAck` registered;
   live P0 green (busy steer acked 8.0s, idle 44ms). Server
   dispatch + watch test added; frontend untouched (generic
   path). Done.
3. **Muse**: `musecli_durable_ack.go` (sequence-scoped
   receipts on the pooled entry under the existing pool lock,
   `MUSE_DURABLE_ACK_SECONDS` budget, `Queued input`-keyed
   queue check + activity fallback — `museTUIAtPrompt` stays
   true while streaming so it cannot serve as the busy
   signal); send path was fully pane-only, so this is the
   highest-value ack of the rollout; `CertDurableAck`
   registered; live P0 green (busy steer acked 1.2s, idle
   1.2s — Muse records intake same-second even when busy).
   Server dispatch + watch test added; frontend untouched.
   Done.
4. **Claude**: intake-row + queue-drain verdict over the
   tmux transcript (see "Claude durable-ack P0" below). Live
   P0 green (busy steer 8s, idle 300ms) + live e2e PASS
   (fast ack 1.2s, durable 27.7s). Done.
5. **Cursor**: full file-ack after all — the probe showed
   idle `store.db` commit in ~9.5s with a stable WAL-safe
   reader, so the "async commit" fear did not hold for the
   measured path. Ref-identity scoping (no blob timestamps).
   Implemented + unit-tested; live P0/e2e blocked on login
   quota till 9/21/2026. See "Cursor durable-ack P0" below.

## Codex change list

SDK (`multi-llm-provider-go`):

* [x] `pkg/adapters/codexcli`: pre-paste rollout offset
  snapshot per send (`codexcli_durable_ack.go`); phase-2
  user-row watcher, observe-only, `CODEX_DURABLE_ACK_SECONDS`
  budget (default 180, clamped 5–300); `accepted-but-unflushed`
  outcome; message-text keyed queue check
  (`codexPaneShowsQueuedMessage`) because the positional
  matcher goes blind under the repainted ready footer.
* [x] Deterministic unit tests (`codexcli_durable_ack_test.go`):
  offset/timestamp scoping, malformed-line tolerance, long
  prefix match, poll confirmed/unflushed/failed/cancel,
  receipt stash/peek/cap/TTL.
* [x] Live P0: `TestCodexCLIRealDurableAckContract`
  (busy steer durably acked in 8.1s behind an 8s slow tool,
  steer obeyed; idle follow-up acked in 0.3s); `CertDurableAck`
  registered P0 via `SupportsDurableAck`.

Server (`mcp-agent-builder-go/agent_go` + `mcpagent`):

* [x] `llmtypes.DurableAck` envelope; `llmproviders` +
  `mcpagent/llm` await wrappers (same pattern as `Send*`).
* [x] Bounded watcher after fast ack
  (`live_input_durable.go`), spawned from the single
  `recordLiveCodingAgentUserMessage` choke point so all
  delivery paths (running agent, durable session,
  cold-retained, BG steers) are covered; emits
  `live_input_confirmed` SSE via the event store (replay
  and polling fallback included, no new endpoint needed);
  `QueryResponse.message_id` added for the query path.

Frontend (`mcp-agent-builder-go/frontend`):

* [x] Receipt `confirmation` stage in `liveInputReceipt.ts`
  + pure upgrade/parse/stamp helpers + tests.
* [x] Tick render in `UserMessageEvent.tsx` (✓/✓✓/✓…/!)
  + tests.
* [x] Upgrade handler at the top of `processEventsResponse`
  (covers SSE, polling, reconnect replay) + suppressed-echo
  identity transfer so query-steered rows can match their
  receipt.
* [x] 2026-09-20: receipt consumption on every remaining
  ingestion path — `splitLiveInputConfirmations` /
  `resolveLiveInputConfirmations` helpers wired into durable
  restore (`sessionRestore.ts`, incl. live-tail identity
  transfer onto content-only durable rows), workflow
  hydration (`WorkflowLayout.tsx`), and older-page backfill
  (`ChatArea.tsx`); receipts never enter any timeline. See
  "Restore-path receipt consumption" below.

## Open questions

* Decided: budgets are per-provider, env-tunable, clamped
  5–300: codex 180 (raised from 60 after the live e2e saw
  a mid-turn steer land at 97s — the watch is secondary,
  so the generous budget only delays false negatives),
  cursor 180 (revisit after live P0), pi/muse/Claude 60.
* Decided: `live_input_confirmed` reuses the event-store
  `PollingEvent` envelope — SSE, polling fallback, and
  reconnect replay all carry it with no new endpoint.
* Remaining: accepted-but-unflushed is terminal per watch
  in v1 — if the row lands after the budget, nothing
  promotes the tick to `✓✓`. Options: extend the watch on
  unflushed, or promote on the turn's terminal marker
  (an answered turn proves intake).

## Live server↔chat e2e (2026-09-19)

Harness: isolated workspace API (:18081, temp docs dir) +
agent server (:18943); per provider: real `/api/query`
turn → poll `can_steer` → POST live-input steer →
assert `sent_to_cli` fast ack → poll `?since=0` history
for `live_input_confirmed` → assert the same bytes on
the SSE stream. Probe: `/tmp/durable-e2e/probe.py`.

* [x] codex-cli / gpt-5.6-luna: fast ack 1.3s,
  durable `confirmed` at 54s, proof = session rollout
  jsonl, SSE wire verified, wire shape matches
  `readLiveInputConfirmation` field-for-field.
* [x] pi-cli / google/gemini-3.8-flash: fast ack 6.3s,
  durable `confirmed` at 6.3s, proof = markers.json,
  SSE wire verified.
* [x] muse-cli / muse-spark-1.3-contributor: fast ack
  1.2s, durable `confirmed` at 1.2s, proof = session
  jsonl, SSE wire verified.

Findings (evidence-backed, not yet fixed):

* Codex 60s default budget was too tight for mid-turn
  steers. One run timed out (`failed` after 60s) while
  the rollout recorded the message at 97s — a false
  negative, delivery had succeeded. Fixed: default
  raised to 180 (`codexDurableAckBudgetDefault` +
  `TestCodexDurableAckBudget`).
* `can_steer` went true before the CLI's interactive
  session registered: a muse steer at t+4s 409'd with
  `delivery_uncertain` ("no active Muse interactive
  session registered"). Fixed: `canSteerSession` now
  requires transport registration via per-provider
  `InteractiveSessionRegistered` (codex/pi/muse —
  all three race the same way, muse just lost it).
  Idle foreground-session completion keeps the old
  transport-agnostic liveness (`foregroundTurnAlive`)
  so a registering turn never reads as idle.
* Dropped idea (2026-09-19): collapsing query/steer
  into one async accept-then-deliver sender. Rejected:
  uncertainty is irreducible on short timescales
  (transcript lag ~60–100s), so server retry can't
  check-before-resend quickly; attempt-once-report-truth
  stays the control-plane contract.

## Claude durable-ack P0 (2026-09-19)

Live probe (haiku, persistent tmux): intake row at +3.2s,
mid-turn steer `enqueue` row at +500ms with full text.
Two drain paths found live:

* steer during text generation: `dequeue` then its `user`
  row (~7s).
* steer during a tool call: `remove` with full text, NO
  user row ever — the CLI folds the text into a
  system-reminder with the tool result. An
  instruction-styled steer was then rejected by the model
  as injection-like even though delivery was confirmed;
  benign follow-up phrasing is obeyed (proven by the
  pre-existing queue test, re-run green).

Verdict rule: `confirmed` = user row OR queue drain
(remove with our text, or dequeue after our enqueue —
dequeue rows are contentless so the match is positional).
`accepted_but_unflushed` = enqueue with no drain at
budget expiry. Budget `CLAUDE_DURABLE_ACK_SECONDS`,
default 60 (flush observed at 7–8s; a steer held past a
long turn reports unflushed, truthfully).

* [x] `claudecode_durable_ack.go`: arbiter + stash/peek +
  `AwaitClaudeInputDurable` + `InteractiveSessionRegistered`
  (mirrors the send-path owner registry).
* [x] Send-path stash in `SendClaudeCodeInput`.
* [x] Root `AwaitClaudeInputDurable`, contract flag,
  `CertDurableAck` entry + `TestClaudeCodeRealDurableAckContract`.
* [x] mcpagent wrapper, server watcher switch, steer-gate
  readiness case for `claude-code`.
* [x] Live P0: busy steer confirmed in 8s (model obeyed),
  idle follow-up in 300ms.
* [x] Collateral: two server tests asserted total event
  count 1 where the watch now races a second event;
  they filter to `user_message` rows. `canSteer` precondition
  tests simulate the registered transport via hook.
* [x] Live server↔chat e2e (2026-09-19): fast ack 1.2s
  `sent_to_cli` → durable `confirmed` at 27.7s, proof =
  session transcript, SSE wire verified, mid matches.

## Cursor durable-ack P0 (2026-09-19, live-unverified)

Proof is the session `store.db` (`~/.cursor/chats/<md5(cwd)>/
<agentId>/store.db`): blobs carry no per-message
timestamps, so ref-identity scopes one send — the pre-send
snapshot of latest-root refs plus exact `<user_query>`
text. Confirmed = user_query row under a new ref.
Unflushed = pane still shows our text in the native
follow-ups queue at budget expiry (matcher shaped by
helpers + fixtures, NOT yet by a live pane).

Probe evidence (auto model, persistent tmux): idle steer
committed its row in ~9.5s; reader stable. Busy path
UNKNOWN — login free-quota exhausted mid-probe (resets
9/21/2026, no CURSOR_API_KEY). Budget
`CURSOR_DURABLE_ACK_SECONDS` default 180 (generous:
secondary + unknown busy path; revisit after live P0).

* [x] `cursorcli_durable_ack.go`: arbiter + stash/peek +
  `AwaitCursorInputDurable` + `InteractiveSessionRegistered`.
* [x] Send-path stash in `SendCursorInteractiveInput`.
* [x] Root export, contract flag, `CertDurableAck` entry +
  `TestCursorCLIRealDurableAckContract`, mcpagent wrapper,
  server watcher + steer-gate cases + routing test.
* [x] Unit tests green (real-schema sqlite fixtures).
* [ ] LIVE (blocked on quota till 9/21): SDK P0 then e2e:
  `go test ./pkg/adapters/cursorcli/ -run
  TestCursorCLIRealDurableAckContract -coding-cli-p0-live
  -count=1 -v` from `multi-llm-provider-go`, then the
  server↔chat probe (`/tmp/durable-e2e/probe.py` with a
  `("cursor-cli", "auto")` entry) against a rebuilt
  isolated stack. Also confirm the queued-followups pane
  shape and tune the 180s default + 90s idle bound.

## Restore-path receipt consumption (2026-09-20)

Live symptom (desktop app, muse-cli): a steered message showed
no tick, and the chat rendered `Unknown Event Type:
live_input_confirmed` with the full receipt JSON. Backend proof
was healthy (durable `confirmed` in 1.2s).

Root cause: receipt consumption existed only in the live path
(`processEventsResponse`). Every restore/hydration path
appended the raw event window, so after any restore the receipt
itself entered the timeline as an unknown row, and the rebuilt
rows — synthesized from role/content-only durable carriers —
lost the tick upgrade (no `message_id` to match).

Fix (frontend, uncommitted — git frozen while a second agent
works the tree):

* `splitLiveInputConfirmations` /
  `resolveLiveInputConfirmations` in `liveInputReceipt.ts`:
  one choke point that filters receipts out of a window and
  applies them to the rows they name.
* Wired into durable restore (`sessionRestore.ts`: replace,
  live-tail append, existing-tab refresh),
  `transferLiveTailInputIdentity` copies `message_id` +
  delivery facts from volatile live-tail user rows onto
  same-content durable rows (plus any terminal verdict the
  donor already carries), so ticks survive restores.
* Same treatment for workflow hydration
  (`WorkflowLayout.tsx`) and older-page backfill
  (`ChatArea.tsx`), which shared the raw-append gap.

Tests: 3 regression tests in `sessionRestore.test.ts` (real
muse-cli wire shape; failed pre-fix, green post-fix) + a
helper contract test in `liveInputReceipt.test.ts`. Focused
suites 40/40 green, `tsc -b` + eslint clean. Full frontend
suite: 10 failures in 5 unrelated files, all pre-existing
(providers panel, webhooks feed, notifications, history-fetch
args, workflow classification).

## Transcript-view ticks (2026-09-20)

Follow-up gap found live: the terminal transcript renders user
rows itself (`UserTranscriptMessage` in
`TerminalEventTranscript.tsx`) and never received the tick UI —
`DeliveryTick` lived only in `UserMessageEvent.tsx`, which the
transcript does not use for user rows. Log-verified on session
`6de60b72…`: "ok" opened a muse-cli turn 09:55:43, "just
testing" steered live 09:55:47 (HTTP 200 in 253ms), muse
recorded `runtime.user_intent.accepted` — full chain healthy,
nothing rendered.

Fix: `DeliveryTick` + `deliveryTickState`/`deliveryTickTitle`
extracted to shared modules (`DeliveryTick.tsx`,
`deliveryTickState.ts`; `UserMessageEvent.tsx` re-imports,
behavior unchanged), transcript user rows now take `metadata`
and render the tick beside the timestamp in both plain and
collapsible variants. Plain rows render byte-identical to
before. Contract change, surfaced: the old transcript test
asserted timestamp-only rows for accepted delivery statuses;
it now asserts the fast tick (the `sending` case still asserts
timestamp-only). 3 new tests in
`TerminalEventTranscript.deliveryTick.test.tsx` (failed
pre-fix); focused suites 50/50 green, `tsc -b` + eslint clean.
