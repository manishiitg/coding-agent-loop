[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-099 — Updating a workflow's coding agent leaves live-input routing on the old provider

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented` — focused regression tests and build pass; live reverify pending |
| Last synchronized | `2026-08-12` |

- **Priority:** P0 — a user cannot continue an otherwise healthy live workflow-builder chat after changing its coding agent.
- **Owner:** retained coding-agent live-input routing and continuation metadata.

## Actual defect

The workflow setting was successfully changed from Claude Code to Codex and the
next turn created a real `mlp-codex-cli-*` tmux session. The chat's retained
continuation request still named Claude Code, however. Both `/live-input` and
the `/api/query` fallback called the same delivery helper, which preferred that
stale request field over the live terminal identity. It therefore attempted to
send to Claude, reported that no Claude tmux existed, and rejected the user's
message even though the Codex tmux was live.

This was a backend routing defect, not a failed workflow update or a frontend
submission defect.

## Implemented repair

1. A live tmux provider is now authoritative for direct terminal delivery. A
   stored model is reused only when its stored provider matches that tmux.
2. After workflow manifest and tier resolution, the continuation record is
   rewritten with the provider/model that actually launches the coding CLI.
   The request's original LLM config is copied rather than mutated.
3. If an older terminal cannot identify its provider, stored continuation data
   remains the compatibility fallback.

## Regression coverage

- A stale Claude continuation record plus a live Codex tmux delivers to Codex
  and discards the incompatible Claude model.
- Effective runtime synchronization updates both top-level provider/model and
  the stored primary LLM config without mutating the source request.
- Existing retained-terminal and workflow-continuation tests continue to pass.

## Verification

- Focused `cmd/server` provider-switch and continuation tests pass.
- `go build ./...` passes.
- Live UI re-verification requires the backend to be restarted with this code;
  the server was not restarted during the repair.

## Acceptance

- Change a workflow automation from Claude Code to Codex (or the reverse).
- Start/retain the new provider's terminal in the existing chat.
- A subsequent user message is delivered to the live provider without a 409,
  "Could not submit live input", or a provider-mismatch error.
