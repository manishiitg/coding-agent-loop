# Slack Connections (Per-Workflow and Per-Project Slack Apps)

Each workflow may talk through its own Slack app (bot token + app token
pair), or inherit the platform default app. Crew projects work the same
way: each project may use its own app or inherit the default. This
document describes the registry, the multi-listener runtime, and the
ownership rules.

## Model

```text
slack-config.json
  connections[]: [{ id, display_name, bot_token, app_token,
                    enabled, workspace_path, profile_id }]
  default_connection_id: "slack_001"

workflow.json capabilities (workflows)
  slack_connection_id: "slack_abc123" | "" (= inherit default)

project runtime manifest capabilities (crew projects)
  slack_connection_id: "slack_abc123" | "" (= inherit default)
```

- One connection = one Slack app = one Socket Mode websocket. Tokens are
  encrypted at rest (`operator:slack` AAD) and masked on every read.
- `workspace_path` scopes a connection to its owning workflow or product
  project (`profile_id` names the agent profile for product scopes).
  Empty means platform-managed. A lone scoped connection never
  auto-becomes the default; inheriting platform traffic is an explicit
  admin decision.
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
  else the route target's selection (workflow manifest or project runtime
  manifest), else the platform default.

## Ownership

| Action | Who |
|---|---|
| Create/update/delete/test a workflow-scoped connection | Owner of that workflow (create requires the scope; scope changes are admin-only) |
| Create/update/delete/test a project-scoped connection | Owner of that product project (same scope rules as workflows) |
| Create/update the default or any unscoped connection | Platform admin |
| Change the default connection | Platform admin |
| Flip the global Enable switch / bot mode | Platform admin (one-time platform step) |
| Select an app on a workflow (`slack_connection_id`) | Anyone who can edit the manifest; unknown IDs fail validation |
| Select an app on a project (`slack_connection_id`) | Product owner (reads need product access); unknown IDs fail validation |
| Delete a connection | Blocked while it is the default or still selected by any workflow or project |

Bot-route principals can never manage connections. WhatsApp turns are not
bot-route principals: they run as the paired owner (`bot_owner` — the bot is
linked as the owner's number, so there is no channel grant to hold) and pass
these gates as the owner would. All API responses carry masked tokens only.

## Bot principals (`bot_route` vs `bot_owner`)

- `bot_route` is stamped on Slack turns only, and only when the request
  carries a channel grant (`applyBotRouteClaims` in
  `agent_go/cmd/server/bot_session_starter.go`). It is a synthetic route
  identity: the Slack sender gets no authority beyond the grant, and
  `revalidateExecutionPrincipal` re-checks the live route config on every
  tool call (connector enabled, route present, explicit grant, destination
  unchanged, sender email allowed). Execution is pinned to `run` access.
- `bot_owner` is stamped on WhatsApp turns. The sender was authenticated at
  message ingress (pairing ownership, link codes, per-slug workflow access),
  so the turn executes as the paired owner on the owner's data with nothing
  further to revalidate — the same standing as the owner's own app turns.
- The shared tool boundary (`bindToolExecutionContext` in
  `agent_go/cmd/server/tool_execution_context.go`) admits exactly these two
  principals on bot-marked sessions. Anything else bound there is rejected
  as `session origin changed`: before `bot_owner` existed, the guard
  admitted only `bot_route`, which silently broke every tool call in every
  WhatsApp turn.

## Key files

| Area | File |
|---|---|
| Registry model, normalize, migrate, encrypt | `agent_go/cmd/server/services/slack_connections.go` |
| Runtime mux, children, routing | `agent_go/cmd/server/services/slack_service.go` |
| Connection threading, scoped helpers | `agent_go/cmd/server/services/bot_connector.go` |
| Connections API + permission gates | `agent_go/cmd/server/slack_connection_routes.go` |
| Manifest selection + validation | `agent_go/cmd/server/workflow_manifest*.go` |
| Project selection + product ownership | `agent_go/cmd/server/slack_connection_routes.go` (`productSlackConnectionID`, `requireProductSlackScopeOwner`) |
| Tool owner branches | `agent_go/cmd/server/slack_bot_tools.go` |
| Bots panel UI | `frontend/src/components/workflow/bots/SlackSetup.tsx`, `useWorkflowBots.ts` |
| Agent guidance | `agent_go/cmd/server/guidance/templates/system/slack-bot-routing.md` |
