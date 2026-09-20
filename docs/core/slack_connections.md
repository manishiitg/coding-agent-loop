# Slack Connections (Per-Workflow Slack Apps)

Each workflow may talk through its own Slack app (bot token + app token
pair), or inherit the platform default app. Profile projects always use the
platform default. This document describes the registry, the multi-listener
runtime, and the ownership rules.

## Model

```text
slack-config.json
  connections[]: [{ id, display_name, bot_token, app_token,
                    enabled, workspace_path }]
  default_connection_id: "slack_001"

workflow.json capabilities
  slack_connection_id: "slack_abc123" | "" (= inherit default)
```

- One connection = one Slack app = one Socket Mode websocket. Tokens are
  encrypted at rest (`operator:slack` AAD) and masked on every read.
- `workspace_path` scopes a connection to its owning workflow. Empty means
  platform-managed. A lone workflow-scoped connection never auto-becomes the
  default; inheriting platform traffic is an explicit admin decision.
- Pre-registry single-credential configs migrate on load into an unscoped
  `slack_001` "Default" connection. Legacy save shapes fold onto the
  default connection.
- An unknown, incomplete, or disabled selection fails the send. It never
  falls back to another identity: delivering from an unintended Slack app
  is worse than not delivering.

## Runtime

```text
Socket Mode listeners (one per enabled connection)
        |  inbound tagged with connection ID
        v
SlackService root (serves default) + children (one per extra connection)
        |  BotConnector / NotificationConnector
        v
BotConversationManager (connection rides ThreadID / BotIncomingMessage)
```

- The root instance (returned by `GetSlackService`, registered with the
  `BotConversationManager`) serves the default connection itself and fans
  out to one child `SlackService` per additional enabled connection.
- Children are owned by the root: created, token-synced, and stopped by
  `reconcileChildren`, which runs on every config reload. Children never
  touch the config file.
- `ThreadID.ConnectionID` (excluded from `Key()`, so session lookup is
  unaffected) carries the arrival connection through sessions, replies,
  reactions, and history reads. Reactions and channel names resolve through
  the optional `connectionScopedConnector` interface; other platforms keep
  the legacy channel-only behavior.
- Resolution order for a send: live bot execution's arrival connection,
  else the route workflow's manifest selection, else the platform default.

## Ownership

| Action | Who |
|---|---|
| Create/update/delete/test a workflow-scoped connection | Owner of that workflow (create requires the scope; scope changes are admin-only) |
| Create/update the default or any unscoped connection | Platform admin |
| Change the default connection | Platform admin |
| Flip the global Enable switch / bot mode | Platform admin (one-time platform step) |
| Select an app on a workflow (`slack_connection_id`) | Anyone who can edit the manifest; unknown IDs fail validation |
| Delete a connection | Blocked while it is the default or still selected by any workflow |

Bot-route principals can never manage connections. All API responses carry
masked tokens only.

## Key files

| Area | File |
|---|---|
| Registry model, normalize, migrate, encrypt | `agent_go/cmd/server/services/slack_connections.go` |
| Runtime mux, children, routing | `agent_go/cmd/server/services/slack_service.go` |
| Connection threading, scoped helpers | `agent_go/cmd/server/services/bot_connector.go` |
| Connections API + permission gates | `agent_go/cmd/server/slack_connection_routes.go` |
| Manifest selection + validation | `agent_go/cmd/server/workflow_manifest*.go` |
| Tool owner branch | `agent_go/cmd/server/slack_bot_tools.go` |
| Bots panel UI | `frontend/src/components/workflow/bots/SlackSetup.tsx`, `useWorkflowBots.ts` |
| Agent guidance | `agent_go/cmd/server/guidance/templates/system/slack-bot-routing.md` |
