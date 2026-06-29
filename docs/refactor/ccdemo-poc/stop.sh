#!/bin/bash
# Stop the ccdemo server and kill ONLY our test tmux session (never mlp-*).
pkill -f 'ccdemo -s ccdemo-live' 2>/dev/null && echo "server stopped" || echo "server not running"
tmux kill-session -t ccdemo-live 2>/dev/null && echo "killed tmux session ccdemo-live" || echo "no ccdemo-live session"
