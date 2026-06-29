# ccdemo — tmux control-mode live-attach PoC (reference implementation)

**Keep this. It is critical.** This is the minimal, *proven-correct* reference for the
live-attach terminal transport: it mirrors a server-side **tmux** session into a browser
**xterm.js** pane via **tmux control mode** (`tmux -CC attach`) over a WebSocket —
streaming and resize, flawlessly, for real CLIs (pi, claude-code, codex).

The production app's live-attach
(`agent_go/cmd/server/terminal_live_attach.go` + `agent_go/internal/liveattach/parser.go`
+ `frontend/src/components/TerminalCenter.tsx` → `LiveAttachXtermPane`) is meant to behave
**identically**. When the app diverges (e.g. duplicated lines, stacked frames), this demo
is the oracle — diff the app against it. See `../live_attach_app_vs_demo_debug.md` for the
ongoing app-vs-demo investigation.

## Architecture
```
tmux session (detached)  ──(tmux -CC attach, run under a PTY)──►  ccdemo (Go server)
                                                                   │  parse %output, octal-decode
                                                                   ▼
   browser xterm.js  ◄──────────── WebSocket: raw pane bytes ──────┘
      • input :  keystrokes  → WS (binary) → tmux send-keys -H
      • resize:  FitAddon    → WS (JSON)   → tmux resize-window -x C -y R
      • (re)connect: one capture-pane backfill, then the live %output stream
```
Key files: `main.go` + `listen.go` (server, `package main`), `static/index.html` (xterm
page), `launch.sh` / `stop.sh` (helpers).

## Build
```
cd docs/refactor/ccdemo-poc
go build -o ccdemo .
```
Deps (`go build` fetches them): `github.com/creack/pty`, `github.com/gorilla/websocket`.
Requires **tmux ≥ 2.9** (for `window-size`).

## Run
1. Start a CLI in a dedicated tmux session (never reuse the app's `mlp-*` sessions):
   ```
   tmux new-session -d -s ccdemo-live -x 120 -y 36 "pi --model google/gemini-3.5-flash"
   tmux set-option        -t ccdemo-live status off
   tmux set-option        -t ccdemo-live remain-on-exit on
   tmux set-window-option -t ccdemo-live window-size latest
   ```
   `./launch.sh [session]` automates this for pi/codex. For claude-code use `"claude"`.
2. Serve it:
   ```
   ./ccdemo -s ccdemo-live -addr 127.0.0.1:8742
   ```
3. Open **http://127.0.0.1:8742/** — type to the CLI, scroll for history, resize the window.

Run several instances on different ports/sessions (e.g. `-s ccdemo-claude -addr 127.0.0.1:8743`).
Stop with `./stop.sh`.

### GOTCHA
ccdemo does **not** auto-reattach. If you kill/recreate the tmux session, **restart the
ccdemo server**, or you'll only see the backfill on reload (no live stream).

## Use it as an oracle against the app
Point ccdemo at the app's *real* tmux session (with the app backend **off**, so there is
no competing `-CC` client on that session) to isolate transport bugs from CLI/config:
```
./ccdemo -s <mlp-… app session> -addr 127.0.0.1:8744   # http://127.0.0.1:8744/
```
- Clean here **but** broken in the app ⇒ the app's **transport** is the bug (not CLI/config).
- Broken here too ⇒ it's the CLI/config.

## Flags
- `-s <session>`   tmux session to attach (default `ccdemo-live`)
- `-addr <addr>`   listen address (default `127.0.0.1:0` = random free port)
- `-cols` / `-rows` initial control-client size (default 120 × 36)
