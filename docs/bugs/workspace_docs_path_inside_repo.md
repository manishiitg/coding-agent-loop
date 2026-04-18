# Bug: workspace-docs Lives Inside Repo, LLM Can Reach Source Files

## Status: Open (design discussion — not yet implemented)

## Problem

When `run_server_with_logging.sh --with-workspace` runs, `workspace-docs/` is mounted from inside the project tree (`mcp-agent-builder-go/workspace-docs/`). The LLM sees absolute paths like `/Users/<user>/ai-work/mcp-agent-builder-go/workspace-docs/foo.md` when reading or writing files. From there it can — and does — traverse upward into sibling directories (`agent_go/`, `frontend/`, `mcpagent/`, etc.) and read project source files that have nothing to do with the user's workspace.

This both pollutes the agent's context with irrelevant code and creates a soft confidentiality issue for anyone running the binary against a workspace that should be sandboxed.

## Root Cause

`agent_go/run_server_with_logging.sh` lines 404–408:

```bash
# Always use local workspace-docs for native workspace (ignore Docker paths from .env)
WORKSPACE_DOCS_PATH="${SCRIPT_DIR}/../workspace-docs"
mkdir -p "$WORKSPACE_DOCS_PATH"
WORKSPACE_DOCS_PATH="$(cd "$WORKSPACE_DOCS_PATH" && pwd)"
export WORKSPACE_DOCS_PATH
```

The script unconditionally overwrites `WORKSPACE_DOCS_PATH`, throwing away anything the caller exported. The comment explains why: `.env` files in this project commonly contain Docker paths like `/app/workspace-docs` that would break native mode. The heavy-handed fix also blocks legitimate shell overrides.

The env var itself is already plumbed everywhere it needs to be (`pkg/workspace/execute_shell_command.go`, `pkg/orchestrator/base_orchestrator_folder_guard.go`, `pkg/workspace/diff_patch_workspace_file.go`, `pkg/fsutil/atomic.go`, `desktop/main.js`, `docker-compose.yml`, etc.). The shell script is the only place that ignores it.

## Impact

- LLM regularly reads project source files that aren't part of the user workspace.
- Workspace paths leak `/Users/<user>/ai-work/mcp-agent-builder-go/...` into agent context, making prompts non-portable across machines.
- Anyone wanting to keep notes outside the repo has to edit the script.

## Options Considered

### Option A: Env-var opt-in, keep current default
- Capture `WORKSPACE_DOCS_PATH` from the shell *before* `.env` is sourced; if non-empty, use it; otherwise fall back to `${SCRIPT_DIR}/../workspace-docs`.
- Pros: zero impact on existing setups; users who care opt in via `~/.zshrc`.
- Cons: default still has the original problem, so most users won't benefit.

### Option B: Change the default to an out-of-repo path
- Default to e.g. `~/Documents/mcp-agent-workspace` or `~/Library/Application Support/mcp-agent/workspace-docs`.
- Pros: solves the problem for everyone by default.
- Cons: existing content in `mcp-agent-builder-go/workspace-docs` becomes invisible until the user moves it or sets the env var back. We have a lot of existing content — a silent default flip would surprise people.

### Option C: Hybrid — auto-detect existing folder
- If `WORKSPACE_DOCS_PATH` is set → use it.
- Else if `${SCRIPT_DIR}/../workspace-docs` exists and is non-empty → use it (preserves current behavior).
- Else fall back to `~/Documents/mcp-agent-workspace`.
- Pros: no surprise migration; new installs get the safe default.
- Cons: the rule is harder to reason about; "why is my notes folder in two places" support questions.

## Open Questions

1. **Default location.** If we change it, which folder convention?
   - `~/Library/Application Support/mcp-agent/workspace-docs` (Apple convention, hidden)
   - `~/Documents/mcp-agent-workspace` (Finder-visible, iCloud-syncable)
   - `~/.local/share/mcp-agent/workspace-docs` (XDG, cross-platform-friendly)
   - `~/mcp-agent/workspace-docs` (simple, clutters home)
2. **Migration story.** Do we ship a one-shot migration script, or document the `mv` in the bug fix and let users run it manually?
3. **Symlink as bridge?** A symlink from `mcp-agent-builder-go/workspace-docs` → `~/...` would let both old and new code paths resolve to the same content during a transition. Worth supporting?
4. **Frontend / Electron.** `desktop/main.js` also reads `WORKSPACE_DOCS_PATH`. Confirm it picks up the same env var without script changes.
5. **Docker / production.** The current "ignore .env" hack exists because compose sets `/app/workspace-docs`. Need to verify the new logic doesn't regress prod by sourcing `WORKSPACE_DOCS_PATH=/app/...` from `.env` and using it on a developer Mac. (Capturing from shell env *before* `.env` source addresses this.)
6. **Path containment.** Even with workspace-docs moved out of the repo, the workspace tools still hand out absolute paths. Should we additionally enforce path containment in the workspace server (reject any `..` resolution that escapes `WORKSPACE_DOCS_PATH`) so that this class of bug can't recur?

## Affected Files

- `agent_go/run_server_with_logging.sh` — hardcoded path (lines 404–408), `.env` sourcing (lines 317–327)
- `agent_go/pkg/workspace/execute_shell_command.go` — consumes the env var
- `agent_go/pkg/workspace/diff_patch_workspace_file.go` — consumes the env var
- `agent_go/pkg/orchestrator/base_orchestrator_folder_guard.go` — folder containment guard
- `agent_go/pkg/fsutil/atomic.go` — file ops
- `desktop/main.js` — Electron-side workspace path
- `docker-compose.yml`, `docker-compose.prod.yml` — set the Docker `/app/workspace-docs` value
- `docs/core/native_workspace_mode.md` — needs doc update once decision is made

## Workaround (Until Decision Is Made)

None purely via env right now — the script overwrites `WORKSPACE_DOCS_PATH`. Manual workarounds:

- Edit `run_server_with_logging.sh` line 405 locally and point it at the desired path.
- Or symlink `mcp-agent-builder-go/workspace-docs` to a folder outside the repo (`ln -s ~/Documents/mcp-agent-workspace mcp-agent-builder-go/workspace-docs`). The script will still resolve to the in-repo path, but the actual storage lives elsewhere.
