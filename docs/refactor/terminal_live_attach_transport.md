# Design: Live-attach terminal transport (replace snapshot/replay mirror)

**Status:** Design — not started. Supersedes the snapshot/replay mirror.
**Author/driver:** terminal-stability refactor.
**Related:** `cli_live_input_unification.md` (input routing — stays valid; this doc is the *output/render* transport).

---

## 1. Why (the root cause)

The CLI runs inside a **detached** tmux session (`tmux new-session -d`, **no client ever attaches**). Because nothing is attached, the pane's live output is *reconstructed* for the browser **three separate times**:

1. **Adapter** `capture-pane` polling (200–250ms) → `StreamChunkTypeTerminal` snapshots.
2. **Backend** `pipe-pane` append-log + a screen-mode-aware prologue, repaint hacks, trim loop.
3. **Frontend** delta-vs-reset heuristics (`computeXtermWrite`), 3s re-probe polling, remount-on-switch, RIS reseed on resize.

Every terminal bug this cycle — litter, duplicated frames, stacked spinners, wrapped tables on resize, blank-on-resume — is a failure of that reconstruction. The fix is to **stop reconstructing** and let a real attached client + xterm render the live byte stream natively.

> Empirically confirmed by the end-to-end map (see "Reconstruction eliminated" below): ~10 distinct mechanisms exist solely to compensate for the detached session.

---

## 2. Target architecture

```
 tmux session (created -d, as today)            ── reused: creation/lifecycle/input
      │  PTY bytes
      ▼
 backend ATTACH STREAMER  (per SELECTED terminal)        ── NEW
   pty.Start("tmux attach -t <session>")  (read-only: we read PTY master, never write its stdin)
   resize: pty.Setsize(cols,rows)  → tmux client follows → clean redraw frame
      │  raw terminal bytes (live, in order, at the xterm's geometry)
      ▼
 WebSocket (binary, 1:1)                                  ── NEW
      ▼
 xterm.js  (AttachAddon writes the stream; FitAddon for size; SerializeAddon/capture for reconnect)

 INPUT (unchanged): chat live-input / debug keys → send-keys / paste-buffer  (xterm stays display-only)
```

**Core idea:** attach one real (read-only) tmux client per *selected* terminal, stream its PTY bytes over a WebSocket straight into xterm. xterm handles cursor motion, alt-screen, colors, spinners, wrapping natively — because it's receiving the actual terminal stream, in order, at a stable geometry. No log, no capture-poll, no delta/reset, no reseed.

Input keeps the **existing** `send-keys`/`paste-buffer` path (the CLI-live-input unification). xterm remains `disableStdin` — we never write to the attach client's stdin, which also sidesteps tmux prefix-key / interactive-client concerns.

---

## 3. Key decision: PTY-attach (primary) vs control-mode `-CC` (fallback)

| | PTY-attach (`tmux attach` in a PTY) | Control mode (`tmux -CC attach`) |
|---|---|---|
| Mechanism | Read the attached client's rendered PTY output | Parse a structured protocol (`%output`, `%layout-change`) |
| Bytes to xterm | The rendered terminal stream (what we want) | `%output` per-pane (also what we want) |
| Chrome/prefix | Tame with `status off` + single pane + **read-only** (never write its stdin) | None (protocol, not an interactive client) |
| Resize | `pty.Setsize` → client follows | size commands in protocol |
| Code we write | Minimal (ttyd is a near-blueprint; creack/pty does the PTY) | **A control-mode protocol parser** (no mature Go lib; iTerm2 is GPL → design-only) |
| OSS leverage | High (ttyd, GoTTY, creack/pty — all MIT) | Low (write our own parser) |

**Decision: PTY-attach is primary.** With our **single-viewer / single-pane** constraint and read-only attach, the usual reasons to prefer control mode (chrome, prefix, multi-pane, multi-client size negotiation) don't apply, and PTY-attach has far more MIT OSS to lean on. **Control-mode stays the documented fallback** if read-only PTY-attach taming proves leaky on some tmux version.

---

## 4. Constraints / assumptions (locked with product owner)

1. **Single viewer per terminal** → 1 attach client ↔ 1 WebSocket ↔ 1 xterm. No fanout, no multi-client size negotiation.
2. **xterm grid is the authoritative size.** The attach client / `pty.Setsize` simply match it.
3. **Live stream renders; `capture-pane` (or xterm `SerializeAddon`) is used only for (re)connect backfill** — never for live rendering.
4. **Platforms:** macOS + Linux first-class. **Windows via WSL2** (= the Linux path; tmux already required, so no new dependency). **Native Windows out of scope** — blocker is tmux (no native equivalent), not the PTY. Use a **portable PTY lib** (e.g. `aymanbagabas/go-pty`: creack/pty on Unix, ConPTY on Windows) so the PTY layer keeps ConPTY optionality cheaply.
5. **Attach only the SELECTED terminal.** A chat/session can have several terminals (`main:<session>`, `workflow-step:<…>`), each its own tmux session. Only the focused one gets a live attach+WS; the rail/others stay low-freq `capture-pane` thumbnails/metadata. Switch → detach old, attach new.
6. **Minimum tmux version** pinned + checked at startup (control-mode/attach behavior varies by version; we already cope with version variance today).

---

## 5. OSS leverage (all MIT unless noted)

- **ttyd (MIT)** — near-blueprint for PTY→WS→xterm (incl. readonly); borrow its WS message framing (input/resize/output). Not drop-in (no session/cost/workflow integration).
- **creack/pty / aymanbagabas/go-pty (MIT)** — Go PTY primitive; `pty.Start`, `Setsize`. go-pty for the portability hedge.
- **xterm addons (MIT)** — **AttachAddon** (wire xterm ⇄ WS), **FitAddon** (size, already used), **SerializeAddon** (snapshot xterm buffer for reconnect).
- **GoTTY (MIT)** — Go-specific reference for the WS↔PTY relay; documents tmux for shared sessions.
- **WebSocket lib** — `coder/websocket` or `gorilla/websocket`.
- **iTerm2 (GPL-2)** — gold-standard *design* reference for control-mode (fallback). Study, **do not copy**.
- **node-pty / WeTTY** — conceptual only (Node).

---

## 6. Phased migration (feature-flagged, parallel to the current transport)

The current snapshot/replay transport stays the **default** until the new one is proven. Flag: `RUNLOOP_TERMINAL_LIVE_ATTACH` (off by default).

- **Phase 0 — Spike (throwaway):** stand up ttyd-style PoC: `pty.Start("tmux attach -t <existing session>")` → WS → a bare xterm. Confirm clean render of a live pi-cli and codex pane, resize via `Setsize`, on macOS + Linux. Validates the core assumption before touching product code.
- **Phase 1 — Backend attach streamer + WS endpoint (behind flag):** per-selected-terminal attach streamer (portable-pty), read-only; `GET/WS /api/terminals/{id}/stream`; lifecycle (open on select, close on deselect/disconnect); `pty.Setsize` on resize; reconnect backfill via `capture-pane` seed then live. Reuse `tmuxsize`, session registry, `tmuxexec`.
- **Phase 2 — Frontend xterm over WS (behind flag):** xterm + AttachAddon bound to the WS; FitAddon → `Setsize`; reconnect → SerializeAddon/capture seed. Keep `disableStdin`. Behind the flag, side-by-side with the existing polling path.
- **Phase 3 — Input + selection + rail:** confirm existing `send-keys`/`paste-buffer` input works unchanged with an attached session; switch = detach/attach; rail thumbnails via low-freq capture.
- **Phase 4 — Verify all cases** (flag on, internal): new chat (pi-cli + codex), resume, busy steer, idle follow-up, completed→new turn, terminal switch, server restart/reconnect, workflow-step terminals, resize/wide-table. macOS + Linux + WSL.
- **Phase 5 — Flip default + soak.**
- **Phase 6 — Delete the old transport:** remove `terminal_pipe_recorder.go`, adapter capture-poll streaming, `terminalReplay.ts` + `applyContent` delta/reset, the 3s re-probe + dual-source merge + `ResetForResize`/repaint hacks, `[SPINNER_DEBUG]`.

Each phase ships independently; the flag means a regression can't reach users mid-build.

---

## 7. What gets replaced vs reused (from the end-to-end map)

**Replace / delete:**
- `agent_go/cmd/server/terminal_pipe_recorder.go` (entire byte-mirror: prologue, pipe-pane, `forceTerminalPaneRepaint`, trim, `ResetForResize`).
- Adapter snapshot streaming + capture-poll bodies: `streamCodex/PiTerminalSnapshot`, `captureCodex/PiPaneForDisplay`, the `waitFor…` capture loops (snapshot portions only).
- `frontend/src/components/terminalReplay.ts` + `applyContent` delta/reset branches; the 3s re-probe + rail/detail polling; dual-source merge (`terminal_routes.go` detail `:218-259`); resize reseed/repaint.
- `[SPINNER_DEBUG]` / `inspectTerminalPaneRuntimeStats` geometry diagnostics.

**Reuse (transport-agnostic):**
- tmux **session creation/lifecycle** (`startCodex/PiTmuxSession`, `remain-on-exit`, kill, owner→tmux registry). `window-size manual` likely flips to client-driven.
- `coding_agent_contract.go` capability registry + `coding_agent_live_input.go` dispatch (add a transport flag).
- **Input path** (`send-keys`/`paste-buffer`, `/input`, `/key`, `tmuxKeyName`).
- `internal/terminals/store.go` Snapshot model + `terminal_id` keying (`main:`/`workflow-step:`) + `Active/State/Status` + `SessionHasBusyCodingTmux` + live-input routing.
- `internal/tmuxsize`, `tmuxexec`, `tmuxlaunch`.
- xterm shell in `TerminalCenter.tsx` (FitAddon, `key={terminal_id}` remount, theme) — fed by the stream instead of polls.
- `services/api.ts` `resize`/`size-hint`/`input`/`key`; `useSessionTerminals` presence (useful at connect).

---

## 8. Risks / open questions

1. **Read-only PTY-attach leakage:** does a never-written-to `tmux attach` render 100% cleanly with `status off` + single pane on all target tmux versions? (Phase 0 answers this; control-mode is the fallback.)
2. **Reconnect backfill fidelity:** capture-pane vs xterm SerializeAddon for restoring the exact pre-disconnect screen, then seamless resume of the live stream without a doubled frame.
3. **tmux version variance** across brew (macOS) / distro (Linux) / WSL — pin a minimum + startup check.
4. **WS lifecycle** under the existing auth/session model (the app's API auth, session ownership).
5. **Selection churn cost:** attach/detach on every terminal switch — ensure it's cheap (it is: attach is fast; rail stays capture-based).
6. **Workflow-step terminals** that complete/exit (`remain-on-exit`) — attach to a dead-but-retained pane shows the final frame; confirm that's the desired read-only behavior.

---

## 9. Recommended stack (summary)

`go-pty` (portable PTY) + read-only `tmux attach` per selected terminal + `coder/websocket` + xterm `AttachAddon`/`FitAddon`/`SerializeAddon`; ttyd as the blueprint; control-mode `-CC` as the documented fallback. Phased behind `RUNLOOP_TERMINAL_LIVE_ATTACH`, current transport stays default until Phase 5.
