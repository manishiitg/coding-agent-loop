# Bots: who a turn runs as

Status: proposed 2026-09-26. Related: `bot_destination_scope.md`.

## Rule

A bot turn runs as a person only when the platform proves who that person is
and nobody else is reading. Otherwise it runs as the destination in Run mode.

| Where the message comes from | Runs as | Mode |
|---|---|---|
| WhatsApp (the paired user's own chats) | the paired user | from that user's access: owner/editor → full, reader → Run |
| Slack channel, private channel, group DM | the route (the workflow or crew) | always Run |
| Slack DM with the bot (new) | the AgentWorks user matched to the sender | from that user's access, same as WhatsApp |

Mode always comes from access, never from the platform or a saved setting:
`readOnlyForRequest(access)` → `resolveWorkflowChatPolicy` already turns write
access into Builder and read access into Run. The only new question per
platform is **whose access**.

## Today (checked in code, 2026-09-26)

**WhatsApp already follows the rule.** The pairing is the user's own number;
only their self-chat and chats they bound with a link code are accepted
(`isAllowedDM`). Turns carry `Provider: "bot_owner"` with the paired user's id
(`applyBotRouteClaims`), and `conversationTargetAccess` answers from that
user's access, so a workflow reader gets Run and an owner gets full.

- Gap: a crew someone shared with the user (reader) is not offered on WhatsApp.
  `checkWhatsAppWorkflowAccess` resolves crews only under the paired user's
  own tree. Under this rule it should run in Run mode.
- Extra phones (`phone-2`, …) run as the paired user. They are that user's
  devices by pairing, so this is correct.

**Slack channels already run in Run mode.** `revalidateExecutionPrincipal`
forces `BotGrant = "run"` and `Access = read`. A crew route runs in its owner's
namespace (`ResourceOwnerID`), and a workflow route runs as a synthetic
`bot-…` principal. The sender is only an audit actor and an email allow/block
list.

**Slack DMs do not work at all.** The app subscribes to `app_mention`,
`message.channels` and `message.groups`, not `message.im`, and
`handleSocketModeMessage` drops every top-level message that isn't a thread
reply. A DM never reaches a turn.

## Slack DMs as the sender

### Identity

Slack gives the sender's user id; `users.info` gives the profile email
(`users:read.email`). Matching that email to a `users.json` record is what
maps a DM to an AgentWorks user. Email alone is only as strong as the Slack
workspace's email control, so a DM runs as a user only when all of these hold:

1. The sender is a full member of the app's own Slack team: not a guest
   (`is_restricted`, `is_ultra_restricted`), not an external Slack Connect
   user (`is_stranger`, or a different `team_id`), and not a bot.
2. The email matches exactly one `users.json` record that is not disabled.
3. The conversation is a 1:1 DM (`channel_type: "im"`, a `D…` id). A group DM
   (`mpim`) is a group: Run mode, like a channel.

If any check fails, the DM falls back to the channel rule (the route in Run
mode) when the bot has one, or the bot says who it can't identify.

### Destination

- **Its own bot** (a workflow's or crew's dedicated app): the DM is for that
  workflow or crew. The mapped user's access to it decides the mode; no
  access means a refusal, not a Run-mode fallback.
- **The shared bot**: a DM names no destination. Phase 1 replies that DMs work
  with a workflow's or crew's own bot. A later phase can reuse WhatsApp's
  `@slug` picker, which already lists only what the user can access.

### Principal

A DM turn uses a new provider, `bot_user`: `UserID` = the mapped user,
`Provider` = `bot_user`, and the Slack user id as the audit actor. It is
revalidated at the query boundary and on every tool call, like `bot_route`:

- the connection still exists and is still the destination's own app;
- the Slack user still maps to the same AgentWorks user (email changed,
  account disabled or removed → refuse);
- the DM channel still belongs to that Slack user.

Access comes from `conversationTargetAccess` with the mapped user's claims.
It is not stored on the session, so a demotion applies on the next turn and
the next tool call.

### Chats

Each DM thread (or the DM itself, when the user doesn't thread) is its own
chat in the crew, as channel threads are today
(`slackThreadConversationKey`). It shows in the user's own chat list, owned by
the user, not the crew owner. In a workflow, the DM runs in that user's own
Builder or Run chat.

### Setup

The app needs the `message.im` event and the `im:history` scope; Save & test
reports them as missing, like the other scopes. The dry run gains a DM case.

## Crew turns in Slack: what they get

This is separate from identity. These are the gaps the 2026-09-26 crew report
found. They apply to channel turns (Run mode) and DM turns alike:

1. **Thread history on the first turn.** The crew path skips the history
   that workflow bot turns get (`buildQueryWithThreadHistory`); follow-ups get
   catch-up already.
2. **The Slack read tool.** Crew turns register the `slack` CLI only when not
   read-only (`agent_profile_runtime.go`), so a Run-mode Slack turn can't read
   its own thread. Register it read-only, scoped to the arrival app and thread.
3. **MCP servers (Notion etc.).** Missing in crew Slack turns; root cause
   under investigation.
4. **Who is asking.** The prompt gets the sender's Slack name, and their
   email when it maps to an AgentWorks user.

## Tests

Dry-run cases (`bot_dry_run_test.go`), from what the UI saves:

- channel mention by the crew owner → Run mode, owner namespace;
- DM by the crew owner to the crew's own bot → full mode, owner's chat;
- DM by a reader of a shared crew → Run mode, reader's chat;
- DM by a user with no access → refused;
- DM by a guest, an external user, or an unmatched email → not run as a user;
- group DM → Run mode;
- DM to the shared bot → pointer reply;
- a user demoted between turns → the next turn is Run mode.
