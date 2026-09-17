# Slack bot routes and threaded messages

Call `get_slack_bot_settings` before changing a route and again afterward to confirm the saved state. Use exact Slack channel IDs (for example `C1234567890`), never display names. The tools are bound to the current workflow or Work project; they cannot manage another target.

`create_slack_bot_route(channel_id, bot_grant)` creates a route; `update_slack_bot_route_permission(channel_id, bot_grant)` changes its grant; `remove_slack_bot_route(channel_id)` revokes it. `test_slack_bot_connection` checks the connector. Global enablement and credentials remain operator configuration in Setup > Bots. Use `open_workspace_view(view="bots")` when the user needs that visual surface.

## Set up the Slack bot

When asked to connect Slack or configure a route, inspect `get_slack_bot_settings` first. Distinguish enabled/configured settings from a successful connection test; never say connected merely because `enabled` and `bot_mode` are true.

1. Open `open_workspace_view(view="bots")`. The main Bots screen contains separate Slack and WhatsApp cards. Routes for the current workflow/project, Add controls, grants, and blocked-email Options live inside each card. Open on the Slack card shows shared connection settings only.
2. Guide the operator to create a Slack app, install it in the workspace, and obtain its Bot User OAuth Token (`xoxb-`). Enable Socket Mode and obtain an App-Level Token (`xapp-`) with `connections:write`. Subscribe to `app_mention`, `message.channels`, and `message.groups`. Grant `app_mentions:read`, `channels:history`, `groups:history`, `chat:write`, `chat:write.public`, `reactions:write`, `users:read`, and `users:read.email`; add `files:read`/`files:write` only if attachments are needed. Reinstall the app after changing scopes.
3. The operator enters both tokens directly in Slack settings and turns on Enable Slack bot. There is one switch, not a separate Bot Mode choice. Test Connection saves changed settings before testing bot authentication and the app token's Socket Mode access. It does not require a default channel or post a test message. A failed test is not a connection. Do not request tokens in chat or attempt to configure them through workspace files.
4. Invite the bot to the desired channel. Obtain the exact channel ID from Slack's channel details, then create a route for the current workflow/project with `create_slack_bot_route`. Channel IDs belong only to routes; there is no global/default channel. Ask which grant is intended if unclear: `run` executes existing work; `owner` can author it. Confirm with `get_slack_bot_settings` after the change.
5. Everyone in a routed channel is allowed by default. To exclude users, set that route's `blocked_emails`; use `[]` to clear the list. Do not create a global Allowed Users list. The same app may serve different channels, each with its own workflow/project destination.

Notify controls outbound workflow notifications and human feedback. Bots controls conversations and channel routing. Gmail account settings have their own Setup > Gmail section and are not part of Bots.

A grant authorizes the bot integration itself: `run` gives runtime/read-only authority; `owner` gives authoring authority. The Slack sender is audit metadata and never supplies execution permissions. Only an authenticated interactive owner may create, upgrade, downgrade, or revoke a grant. A Slack-origin session cannot manage grants even if its bot has an `owner` grant. Read-only sessions receive inspection tools, not mutation tools. Permission changes invalidate running bot work and are rechecked on every turn.

Workflow destinations contain `workflow_id` and `workspace_path`. Product destinations contain `profile_id`, `conversation_key`, and `workspace_path`. A Work project route must preserve `profile_id=work` and `conversation_key=<projectId>` and must not acquire a workflow ID. The backend binds the product owner; never edit `workspace_user_id`.

## Scripted and agentic threaded sends

Use `send_slack_message(route_id, message, idempotency_key, thread_ref?)` through the existing authenticated MCP bridge. `route_id` is the exact configured channel route ID. The message limit is 3000 characters per logical send. Both grants may send; sending never confers permission to manage grants.

Outside a Slack-origin invocation, omitting `thread_ref` starts a top-level message. The response includes `channel_id`, `message_ts`, and an opaque `thread_ref`. Supply that reference for replies. Slack-origin runs and their children automatically inherit the originating thread when the reference is omitted.

For cross-step delivery, write the returned `thread_ref` to the scripted step's declared context output. A downstream step declares that file in `context_dependencies`, reads its normal positional input, and passes the reference to `send_slack_message`. Use the existing `MCP_AUTH` bridge; never place bot/app tokens in `SECRET_*`, scripts, prompts, outputs, or environment variables.

Reuse the same idempotency key for retries of one logical send. Use a different key for a different message. If the service reports an uncertain delivery outcome, inspect Slack before sending again: a new key could duplicate a message already accepted by Slack. Arbitrary channels and cross-route references are rejected, and a revoked route cannot send.

## Deterministic listeners

A saved route may include a `trigger`: `human_message` or `trusted_app`, an optional text `contains` filter, and exact trusted `app_id`/`bot_id` values for app messages. Only top-level messages trigger automation; edits, thread replies, and this bot's own messages do not. A workflow trigger fixes `group_names` plus either `route_selections` or `step_id` in owner configuration. The backend validates these against the saved plan on save and dispatch. Event content cannot change the target, grant, groups, or selected routes.

Events are stored as untrusted data. Preserve normal approvals for high-risk actions. Send progress and the final result/RCA with `send_slack_message` into the inherited source thread. Slack workflow runs use server-bound immutable `iteration-N-slack-<id>` folders and must never use or rotate Builder's `iteration-0`.

One global Slack connector may serve multiple channels. Each channel has one workflow or project destination. Everyone in that channel can invoke its bot grant without an AgentWorks login. Owners may set `blocked_emails` on create/update route tools; `[]` clears exclusions. Exclusions apply to human messages and approvals. With exclusions configured, an unverifiable email is blocked; trusted configured app triggers retain their source-identity checks. Sends are one-shot; the backend never automatically retries an uncertain Slack post.
