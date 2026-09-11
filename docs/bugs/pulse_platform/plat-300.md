[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-300 — Gmail send-only default and Google Workspace (Drive/Sheets/Docs/Slides/Calendar) connections

| Coordination | Value |
|---|---|
| Assigned agent | Claude Code |
| Ticket state | Implemented and deployed (confida) — no live Drive/Sheets connection exercised yet |
| Last synchronized | 2026-09-08 |
| Priority | P2 platform capability |

## Problem

The Gmail channel requested `gmail.readonly` by default even though every
existing use (Pulse and workflow `notify_user`) only ever sends, and a
send-only login could not report itself as authenticated or name its own
address — `gmail.users.getProfile` needs a read scope, so a send-only token
showed "Address not known yet" and, on the gog backend, read as needing
reconnect. Separately, whichever CLI backend actually serves a deployment
(`gws` or `gog`) was hardcoded as "gws" in the settings UI, so a gog-backed
deployment (gog is the only backend that supports anything beyond Gmail) saw
a permanently wrong "gws not installed" status. Beyond Gmail, there was no way
for a connection to authorize Drive, Sheets, Docs, Slides, or Calendar, and no
agent-facing way to use them even if there were.

## Contract and implementation

- **Send-only default, opt-in read.** `GmailConnection.AllowReadAccess`
  (default false) requests `gmail.send` + `userinfo.email` only; checking
  "Also allow reading this mailbox" in the UI adds `gmail.readonly`. Fixed at
  consent time, like every scope grant here — widening it means reconnecting.
- **Identity for a send-only token.** Both backends now resolve identity via
  Google's `tokeninfo` endpoint (`googleTokenInfo`) instead of
  `gmail.users.getProfile`: tokeninfo returns the granted scopes and, when
  `userinfo.email` was granted (always), the address — and it works
  regardless of read access. `computeAuthStatusGog` and `computeAuthStatus`'s
  server-managed-token branch both use it, with `getProfile` kept only as a
  fallback (tokeninfo unreachable, or the `--account`/`--client` path with no
  raw token to introspect).
- **Backend-aware UI labels.** `GmailAuthStatus.Backend` ("gws"/"gog") is set
  by whichever path computed the status; the settings UI reads it instead of
  hardcoding "gws" / `@googleworkspace/cli` in the Connection card and the
  "no account connected" banner.
- **Google Workspace service grants.** `GmailConnection.Services
  []GoogleServiceGrant` (`{service, write}`) lets a connection additionally
  request Drive, Sheets, Docs, Slides, or Calendar, each independently
  read-only (default) or read+write. The scope catalog
  (`services.GoogleServiceCatalog()`) is the single source of the OAuth scope
  URIs per service/level; a new `GET /api/human-feedback/gmail/service-catalog`
  endpoint keeps the UI's checkbox list from drifting from it. Set on create
  only — like `AllowReadAccess`, changing it means removing and re-adding the
  connection.
- **`google_workspace_cli` agent tool.** Rather than a bespoke tool per
  Drive/Sheets/etc. operation, the agent gets one tool that passes its chosen
  `gog` arguments straight through (`services.RunGoogleCLI`). The server
  resolves the target connection (default or named), injects its access
  token, rejects `--access-token`/`--account`/`--client`/`--home` if the
  caller tries to set them, and appends `--readonly` whenever the connection's
  grant for that service is read-only — enforcement is gogcli's own flag, not
  argument sniffing. The token never appears in tool-call arguments or the
  agent's shell. Independent of `GmailConfig.UseGogBackend`: that flag only
  picks which backend serves Gmail send/status, but Drive/Sheets/etc. have no
  `gws` equivalent at all, so this tool always resolves to `gog`.

## Verification

- [x] `go test ./agent_go/cmd/server/...` passes (server, services,
      virtual-tools packages), including new
      `TestComputeAuthStatusGogSendOnlyTokenIsAuthenticatedAndNamedViaTokenInfo`
      and `TestComputeAuthStatusGogReadOnlyTokenIsAuthenticatedButLacksSendScope`.
- [x] `go build ./agent_go/...` and frontend `tsc --noEmit` both clean.
- [x] Deployed to confida (`confida-c36503a-...`); `/api/health` reports
      healthy post-deploy.
- [x] confida's `gmail-config.json` confirmed to carry `use_gog_backend: true`
      across a redeploy (data is separate from the release).
- [ ] A live connection has not yet been created with a Drive/Sheets/etc.
      grant, so `google_workspace_cli` has not been exercised end-to-end
      against a real Google account.
- [ ] Existing pre-feature connections (e.g. confida's
      `manish.prakash@excellencetechnologies.com`) still need reconnecting to
      pick up any new service grants — expected, not a bug, matching how
      `AllowReadAccess` already behaves.

## Deployment

Shipped as part of confida's `agent_go` + frontend bundle
(`deploy/cf/deploy-cf.sh`, remote-main mode). Not evaluated
against RTS or Dominion — those deployments are managed separately.
