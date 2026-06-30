# Live-Attach Terminal: App vs PoC Demo — Debug Handoff

**Status:** ✅ **RESOLVED** — commit `d1ec56d6` "Fix live attach initial repaint
duplication". Root cause + fix below; the rest of this doc is the original investigation
(kept for context). The PoC at `docs/refactor/ccdemo-poc/` remains the reference oracle.

## RESOLUTION (commit `d1ec56d6`)
**Root cause:** the app's control-mode attach starts **lazily** (on the first browser
subscribe), so tmux's initial full-screen repaint — emitted whenever a control client
attaches — reached the browser *right after* the capture-pane backfill → the same screen
drawn twice → duplicate lines even without resize. The PoC never hit this because its
attach runs at server startup, before any browser, so that repaint broadcasts to zero
subscribers and is dropped.
**Fix:** on first subscribe, warm the attach with no subscriber and **drain tmux's initial
repaint** (`liveAttachInitialDrainDelay` 180 ms / 750 ms cap; `drained` chan +
`waitInitialDrain`/`markInitialDrainComplete`) BEFORE adding the viewer's channel —
replicating the PoC's warm-up. Reordered `handleTerminalStream` to subscribe→backfill→
writer. Added `setSize` dedup (only `resize-window` when the grid actually changed). New
test `TestLiveAttachManagerDrainsInitialAttachBeforeSubscriber`. Builds + live-attach
tests pass. (This is exactly the "backfill + attach initial %output = double" lead flagged
in §6 below.)

---

(Original investigation, pre-fix:) The standalone PoC demo rendered tmux CLIs perfectly;
the real app, using the same approach, showed duplicated lines/stacked frames even without
resizing. This doc captured everything tried.

Branch: `terminal-live-attach-phase1`

---

## 1. What we're building

A live terminal transport that mirrors a **server-side tmux session** (detached,
`new-session -d`) into a browser **xterm.js** pane via **tmux control mode**
(`tmux -CC attach`) over a WebSocket. The backend parses `%output` and streams the raw
pane bytes; the browser writes them to xterm. This replaces an older snapshot/replay
(capture-pane polling) transport.

## 2. The two implementations

### A. PoC demo — WORKS PERFECTLY (the reference)
Location: `/private/tmp/claude-501/-Users-mipl-ai-work/a86ff7c1-184a-484a-90e1-e3bb2d10bdd7/scratchpad/ccdemo/`
- `main.go` — standalone Go server: `tmux -CC attach` under a creack/pty PTY, a
  `bufio.Scanner` reads control-mode lines, decodes `%output` octal escapes, broadcasts
  decoded pane bytes to WS viewers. Backfill = `capture-pane -e -S -1000` prefixed with
  `\x1b[H\x1b[2J` and `\n`→`\r\n`. Resize = `resize-window -x C -y R`.
- `static/index.html` — xterm.js page. `new Terminal({fontSize:13, scrollback:20000})`,
  FitAddon, writes WS bytes to xterm, resize via `term.onResize` + debounced
  `window 'resize'`.
- Runs as two instances:
  - `:8742` → session `ccdemo-live`, `pi --model google/gemini-3.5-flash` (exact app model)
  - `:8743` → session `ccdemo-claude`, `claude` (Claude Code)
- **Result: both CLIs stream smoothly, resize cleanly on the fly, render complex ASCII —
  flawless.**

### B. Real app — BROKEN (duplicate lines, stacking, even at rest)
- Backend: `agent_go/cmd/server/terminal_live_attach.go`
  - `liveAttachManager` / `liveAttachStream`: one `tmux -CC attach` per session
    (subscribe/unsubscribe; designed for 1 viewer/session).
  - `handleTerminalStream` (WS handler `GET /api/terminals/{id}/stream`): backfill via
    `liveAttachBackfill` (capture-pane, same shape as demo), then streams broadcast
    channel; reader loop: binary→send-keys, JSON→resize/input/key.
  - `setSize`: now `resize-window` only (pty.Setsize removed).
  - Parser: `agent_go/internal/liveattach/parser.go` (`DecodeOutput`, `ClassifyLine`).
- Frontend: `frontend/src/components/TerminalCenter.tsx`
  - `LiveAttachXtermPaneInner` (~line 2359): xterm `convertEol:false`,
    `disableStdin:true`, scrollback 20000, FitAddon, **manual** `term.write`,
    `ResizeObserver`-based fit, reconnect-on-close. `key={stableLiveAttachId}`.
  - Render branch (~line 4944): synthetic→`StructuredTerminalView`,
    else `stableLiveAttachId`→`LiveAttachXtermPane`, else empty placeholder.
  - `stableLiveAttachId`: a **debounced** mount id (~line 3886) so transient selection
    flicker doesn't unmount the pane.
- **Result: same content block renders 2+ times; "Working…" spinners stack; content
  mis-wraps. Happens DURING NORMAL STREAMING (no resize), and worse on resize.**

## 3. Symptoms (app)
- **Duplicate lines without resize**: e.g. a whole "STEP 4 — BACK UP FINAL STATE…" block
  appears twice, with different wrapping. ← strongest clue: the *live stream* is being
  rendered more than once, OR backfill overlaps the stream.
- On resize: multiple stacked spinners, mis-wrapped/garbled tables.
- Demo never shows any of this.

## 4. RULED OUT (with evidence)
| Hypothesis | Test | Result |
|---|---|---|
| Stale frontend build / cache | Deleted **all** `node_modules`, reinstalled, restarted `--only-frontend` | App still broken → **not stale** |
| xterm.js version | App is `@xterm/xterm@6.0.0`; switched **demo** to 6.0.0 too | Demo still perfect → **not the version** |
| Model / thinking level | Ran demo on `(google) gemini-3.5-flash • medium`, 1.0M ctx (exact app match) | Demo still perfect → **not the model** |
| Scrollback / content length | Generated 200-line output in demo, then resized | Demo still perfect → **not content length** |
| Write method (AttachAddon vs manual) | Switched demo to manual `term.write` | Demo still perfect → **not the write method** |
| Old capture-pane polling interfering | Gated then ripped out the legacy polls (selected-probe, rail-probe excl. selected, selected-detail, manual-refresh) | App still broken |
| Feature flag / dual-path | Ripped out `RUNLOOP_TERMINAL_LIVE_ATTACH` flag + `XtermTerminalPane`; app is always live-attach for tmux | App still broken |

## 5. NOT yet ruled out (prime suspects)
1. **CLI config/extensions**: the app launches CLIs with extra config the demo lacks —
   e.g. the live gemini workflow runs `gemini --model auto --admin-policy
   …/restrict-tools.toml`; `mcpagent/agent/agent.go:2226,3354` append a system prompt
   "running inside Pi CLI with built-in tools disabled. Use the MCP …" + MCP servers.
   The demo's CLIs are **bare**. *Plan: run the app's exact CLI through the demo's
   transport, OR replicate the config in the demo, to rule this out.*
2. **Backend transport code** (`terminal_live_attach.go`) differs subtly from the demo's
   `main.go`. Look hard at: the shared-attach manager (subscribe/unsubscribe vs demo's
   simpler hub), the broadcast buffer **drop-for-slow-viewer** path
   (`liveAttachSubBuffer`, ~line 271 "Slow viewer: drop"), the scanner buffer sizing,
   and whether **the backfill is being sent more than once** or **the WS reconnects**
   (each reconnect re-backfills → duplicate blocks).
3. **Frontend** (`LiveAttachXtermPane`) differs from `index.html`: React re-renders, the
   debounced remount, the flex/`[&_.xterm]:h-full` container, `disableStdin`.

## 6. STRONGEST lead for "duplicate WITHOUT resize"
A duplicated block during steady streaming almost always means one of:
- the WS **reconnects** and the backend **re-runs the backfill** (capture-pane snapshot
  appended on top of the live stream), or
- **two deliveries** of the same `%output` (double subscribe / two viewers / scanner
  re-emitting), or
- the **backfill cursor desync**: capture-pane backfill leaves the xterm cursor at the
  bottom, and the subsequent cursor-relative `%output` redraws append instead of
  overwrite (but the demo does the *same* backfill and is clean, so this alone
  shouldn't explain it — unless the app reconnects/re-backfills repeatedly).

**Recommended first check:** instrument how many times `handleTerminalStream` runs the
backfill and how many WS connects happen per terminal in the app vs the demo; and
count `term.write` calls per `%output` chunk. The demo connects **once** and stays.

## 7. Changes made this session (app, branch `terminal-live-attach-phase1`)
- Backend `terminal_live_attach.go`: `setSize` → `resize-window` only (removed
  `pty.Setsize`); `liveAttachEnabled()` → always true (flag removed);
  `newLiveAttachManagerIfEnabled` always builds (keeps tmux ≥2.9 guard).
- Frontend: capabilities **retry-with-backoff** + a `main.tsx` entry-point fetch
  (`initializeStores()` was dead code; capabilities never loaded reliably);
  **reverted** a bad cache-buster header that tripped CORS (`Cache-Control` not in
  `Access-Control-Allow-Headers`, 127.0.0.1 vs localhost); debounced `stableLiveAttachId`
  mount; ripped out legacy polling + `XtermTerminalPane` usage + the flag + debug
  scaffolding; experimented with removing then restoring `ResizeObserver` and
  `[&_.xterm]:h-full` (both restored — removing the observer **regressed** at-rest
  rendering because the app's React pane sizes after mount).
- `tsc` and `go build` both clean.

## 8. Suggested next steps for the reviewer
1. **Isolate transport vs CLI-config**: point the demo's `ccdemo` server at the app's
   *real* tmux session (`./ccdemo -s <mlp-… session> -addr …`) and view it — same CLI +
   config through the proven-clean transport. (Avoid two simultaneous `-CC` clients on
   one session; stop the app's attach first.) If it stacks → CLI/config; if clean →
   app transport code.
2. **Line-by-line diff** `terminal_live_attach.go` vs `ccdemo/main.go` (broadcast,
   backfill, subscribe/unsubscribe, scanner) and `LiveAttachXtermPane` vs
   `index.html`.
3. **Count duplicate writes**: log WS-connect count, backfill count, and per-chunk
   `term.write` count in the app; compare to the demo (which is 1 connect, 1 backfill).
4. Verify the app isn't opening **two** `/stream` WS for one terminal (e.g. rail +
   main), or re-subscribing.

## 9. How to run / reproduce the demo

The PoC source is committed at **`docs/refactor/ccdemo-poc/`** and has its own
**`README.md`**. **Keep it** — it is the proven-correct reference/oracle for this
transport. Key files: `main.go` + `listen.go` (the server, package main at root),
`static/index.html` (the xterm page), `launch.sh`/`stop.sh` (helpers).

### Build the demo server
```
cd docs/refactor/ccdemo-poc
go build -o ccdemo .          # main.go + listen.go are package main at the root
```
Deps (`go build` fetches them): `github.com/creack/pty`, `github.com/gorilla/websocket`.

### Run a CLI in a tmux session, then serve it
```
# pi on the app's exact model:
tmux new-session -d -s ccdemo-live -x 120 -y 36 "pi --model google/gemini-3.5-flash"
tmux set-option        -t ccdemo-live status off
tmux set-option        -t ccdemo-live remain-on-exit on
tmux set-window-option -t ccdemo-live window-size latest
./ccdemo -s ccdemo-live -addr 127.0.0.1:8742 &      # open http://127.0.0.1:8742/

# claude-code:
tmux new-session -d -s ccdemo-claude -x 120 -y 36 "claude"
tmux set-window-option -t ccdemo-claude window-size latest
./ccdemo -s ccdemo-claude -addr 127.0.0.1:8743 &    # open http://127.0.0.1:8743/
```
`./launch.sh [session]` is a helper that does the tmux setup for pi/codex automatically.

**GOTCHA:** ccdemo does NOT auto-reattach. If you kill/recreate the tmux session,
**restart the ccdemo server**, or it only shows the backfill on reload (no live stream).

### Config-vs-transport test — point the demo at the APP's real session
With the app backend OFF (so there's no competing `-CC` client on the session):
```
./ccdemo -s <mlp-… app session> -addr 127.0.0.1:8744 &   # open http://127.0.0.1:8744/
```
Clean here **and** duplicated in the app ⇒ app **transport** bug. Duplicated here too ⇒
CLI/**config**. (capture-pane reflects tmux's grid, which is never duplicated, so a clean
`:8744` means the app's render path is duplicating, not the session content.)

### The app
```
./run_server_with_logging.sh --only-frontend   # frontend (vite :51733) + Electron
```
Backend is separate on `:18743` and **must be running** for the live-attach WS. The
Electron renderer caches aggressively — for reliable comparison open a plain browser at
**http://localhost:51733/** instead.

### Servers running in this debug session
- `:8742` — pi `gemini-3.5-flash` (app's model)
- `:8743` — claude-code
- `:8744` — the app's real `mlp-claude-code-…` session through the demo transport
