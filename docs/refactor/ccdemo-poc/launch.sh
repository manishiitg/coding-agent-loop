#!/bin/bash
# Launch a REAL CLI in a fresh, dedicated test tmux session, the way the app's
# adapters do (picli_interactive_adapter.go / codexcli_interactive_adapter.go).
# We create our OWN session name (ccdemo-*), never touching the app's mlp-* live
# sessions. User is already authed; we inherit the environment.
set -u

SESSION="${1:-ccdemo-live}"
COLS=120
ROWS=36

# Pick the CLI: pi-dev (pi) preferred, else codex. INLINE mode for both.
PI_BIN_PATH="$(command -v pi || true)"
CODEX_BIN_PATH="$(command -v codex || true)"

if [ -n "$PI_BIN_PATH" ]; then
  CLI="pi"
  # Minimal launch, defaults (provider google / gemini per the adapter), inherit env.
  # pi's TUI is already inline (normal buffer), like the adapter runs it.
  CMD="$PI_BIN_PATH"
elif [ -n "$CODEX_BIN_PATH" ]; then
  CLI="codex"
  # codex inline mode == --no-alt-screen (matches buildCodexInteractiveArgs).
  CMD="$CODEX_BIN_PATH --no-alt-screen"
else
  echo "NO_CLI" >&2
  exit 1
fi

# Fresh session only — refuse to clobber an existing one or any mlp-* session.
case "$SESSION" in
  mlp-*) echo "REFUSING to use an mlp-* (app live) session name" >&2; exit 2;;
esac
tmux kill-session -t "$SESSION" 2>/dev/null || true

# new-session -d with an initial size (like tmuxsize.Args(): -x 120 -y 36).
tmux new-session -d -s "$SESSION" -x "$COLS" -y "$ROWS" "$CMD"

# Mirror the adapter's per-session options, plus the design's client-driven flip.
tmux set-option        -t "$SESSION" status off
tmux set-option        -t "$SESSION" remain-on-exit on
tmux set-option        -t "$SESSION" history-limit 20000
# The design's client-driven flip (NOT manual): window follows the latest client.
tmux set-window-option -t "$SESSION" window-size latest

echo "CLI=$CLI"
echo "SESSION=$SESSION"
echo "CMD=$CMD"
tmux display-message -p -t "$SESSION" 'WINDOW=#{window_width}x#{window_height}'
