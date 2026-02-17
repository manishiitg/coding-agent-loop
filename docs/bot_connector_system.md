# Bot Connector System

A platform-agnostic bot framework that allows users to interact with the agent system from messaging platforms (Slack, Discord, Telegram, WhatsApp). Users @mention the bot, it analyzes the request, gets confirmation, starts a session, and streams results back to the conversation thread.

## Architecture Overview

```
Slack / Discord / Telegram / WhatsApp
        |
        v
  BotConnector interface (per-platform)
        |
        v
  BotConversationManager (platform-agnostic orchestrator)
        |
        +-> Analysis LLM call (determine needed servers/skills/sub-agents)
        +-> Confirmation message (buttons in thread)
        +-> startSessionInternal() (reuses handleQuery core logic)
        +-> Event subscriber -> filtered updates back to thread
        +-> Human feedback -> routed to thread (no 2-min delay)
```

### Key Components

| Component | File | Purpose |
|-----------|------|---------|
| `BotConnector` | `services/bot_connector.go` | Per-platform interface (Slack, Discord, etc.) |
| `BotConversationManager` | `services/bot_connector.go` | Platform-agnostic orchestrator |
| `BotAnalyzer` | `services/bot_analysis.go` | Lightweight LLM call to determine capabilities |
| `BotEventFilter` | `services/bot_event_filter.go` | 3-tier event filter for thread updates |
| `BotEventSubscriberAdapter` | `bot_event_adapter.go` | Bridges EventStore to BotEventSubscriber interface |
| `startSessionInternal` | `bot_session_starter.go` | Starts agent sessions programmatically |
| Bot routes | `bot_routes.go` | REST API for config, sessions, history |

---

## BotConnector Interface

```go
type BotConnector interface {
    NotificationConnector // embeds: Name(), IsEnabled(), SendNotification()

    StartListening(ctx context.Context) error
    StopListening()
    SendThreadMessage(ctx context.Context, threadID ThreadID, message string) (string, error)
    SendThreadMessageWithBlocks(ctx context.Context, threadID ThreadID, message string, blocks []MessageBlock) (string, error)
    UpdateMessage(ctx context.Context, threadID ThreadID, messageID string, newText string) error
    GetThreadHistory(ctx context.Context, threadID ThreadID) ([]ThreadMessage, error)
    SetMessageHandler(handler BotMessageHandler)
    SetInteractionHandler(handler BotInteractionHandler)
    GetFormatter() MessageFormatter
}
```

Each platform implements this interface. The manager calls these methods without knowing the platform details.

### ThreadID

Uniquely identifies a conversation thread across platforms:

```go
type ThreadID struct {
    Platform  string // "slack", "discord", "telegram", "whatsapp"
    ChannelID string
    ThreadTS  string // platform-specific thread root ID
}
```

### MessageFormatter

Converts standard Markdown to platform-specific formatting:

```go
type MessageFormatter interface {
    FormatMessage(markdown string) string
    MaxMessageLength() int
    SplitLongMessage(text string) []string
}
```

| Platform | Bold | Code Block | Links | Max Length |
|----------|------|-----------|-------|-----------|
| Slack | `*text*` | ` ```code``` ` | `<url\|text>` | 4000 |
| Discord | `**text**` | ` ```lang\ncode``` ` | `[text](url)` | 2000 |
| Telegram | `*text*` / `<b>` | `<pre>code</pre>` | `<a href>` | 4096 |
| WhatsApp | `*text*` | ` ```code``` ` | Plain URL | 65536 |

---

## End-to-End Flows

### Flow 1: New @mention in a channel

```
1. User types "@agent research competitor pricing" in #general
2. SlackService receives AppMentionEvent
   -> Strips @mention -> text = "research competitor pricing"
   -> ThreadTS = message timestamp (new thread root)
   -> Calls BotConversationManager.HandleIncomingMessage()
3. BotConversationManager:
   a) Creates BotSession (status: "analyzing")
   b) Posts to thread: "Analyzing your request..."
   c) Calls analyzer.Analyze() with user text + capabilities
4. Analysis LLM returns structured JSON:
   {
     summary: "Research competitor pricing using browser",
     required_servers: ["browser"],
     delegation_mode: "plan",
     needs_browser: true,
     confidence: 0.85
   }
5. Posts confirmation with buttons:
   "Here's what I'll set up:
    Mode: Multi-Agent Chat
    MCP Servers: browser
    Browser access: Yes
    [Confirm] [Cancel]"
6. User clicks [Confirm]
7. confirmAndStart():
   a) Builds QueryRequest from AnalysisResult
   b) Calls startSessionInternal()
   c) Starts BotEventFilter for streaming updates
   d) Status: "running"
8. Filtered events stream to thread:
   "Started session..."
   "Running: browser_navigate..." (batched every 12s)
9. Session completes:
   "Session completed."
   Status: "completed"
```

### Flow 2: @mention in an existing thread (with context)

```
1. User is in a thread discussing pricing with colleagues
   User types "@agent can you summarize this and create action items"
2. No existing BotSession for this thread
   -> GetThreadHistory(channelID, threadTS)
   -> Returns full thread messages
3. Analysis LLM receives:
   - User request + thread context (all messages)
   - Available MCP servers, skills, sub-agents
4. Analysis incorporates thread context into rewritten_query:
   {
     summary: "Summarize pricing discussion and create action items",
     required_servers: ["workspace"],
     delegation_mode: "off",
     rewritten_query: "Summarize this discussion and create action items:
       [thread content incorporated]"
   }
5. Confirmation -> Run -> Results in SAME thread
```

### Flow 3: Follow-up in running session

```
1. User @mentions bot in a thread with an active session
2. BotConversationManager finds existing session (status: "running")
3. Message forwarded as follow-up to the ongoing session
4. Agent receives it and adjusts accordingly
```

### Flow 4: Human feedback during bot session

```
1. Agent needs user input during a bot session
2. HumanFeedbackStore checks BotSessionChecker
   -> Returns true (this is a bot session)
   -> Skips 2-minute delay, sends immediately to thread
3. User replies in thread
4. Reply routed through BotConversationManager to feedback store
5. Agent continues processing
```

### Smart Thread Routing

```
User @mentions bot in a thread
  -> Lookup threadKey in sessions map + DB
     |
    EXISTS?                  DOESN'T EXIST?
     |                            |
  Check status:                New analysis
  - "running" -> forward       -> confirmation
  - "awaiting_confirmation"    -> new session
    -> parse reply
  - "completed"/"failed"
    -> start fresh
```

---

## Analysis LLM

**File**: `services/bot_analysis.go`

The analysis LLM is a fast, cheap call (Haiku by default) that determines what capabilities are needed.

### Input

The analysis prompt receives:
1. Available MCP servers (from MCP config file)
2. Available skills (from `skills.DiscoverSkills()`)
3. Available sub-agent templates (from `subagents.DiscoverSubAgents()`)
4. Available presets (TODO: integration point)
5. Thread context (if tagged in existing thread)
6. The user's message

### Output

```go
type AnalysisResult struct {
    Summary           string   // "I'll research competitor pricing using browser"
    RequiredServers   []string // ["browser", "slack"]
    RequiredSkills    []string // ["market-research"]
    RequiredSubAgents []string // ["research-agent"]
    RequiredSecrets   []string // ["SLACK_TOKEN"]
    DelegationMode    string   // "plan" or "off"
    NeedsWorkspace    bool
    NeedsBrowser      bool
    MatchedPresetID   string   // if a preset closely matches
    MatchedPresetName string
    RewrittenQuery    string   // cleaned query with thread context
    Confidence        float64  // 0.0-1.0
}
```

### Configuration

| Env Variable | Default | Description |
|---|---|---|
| `BOT_ANALYSIS_PROVIDER` | `anthropic` | LLM provider for analysis |
| `BOT_ANALYSIS_MODEL` | `claude-haiku-4-5-20251001` | Model for analysis (fast/cheap) |

The analysis LLM composes configs freely from available capabilities. It is NOT limited to matching presets -- presets serve as hints/shortcuts, but the LLM can pick any combination.

---

## Event Filtering Tiers

**File**: `services/bot_event_filter.go`

To avoid flooding rate-limited platforms (Slack: ~1 msg/sec), events are classified into 3 tiers:

### Tier 1 -- Send Immediately

| Event Type | Message |
|---|---|
| `agent_start` | "Agent started processing..." |
| `agent_end` | "Agent completed." |
| `agent_error` | "Agent encountered an error." |
| `delegation_start` | "Sub-agent started..." |
| `delegation_end` | "Sub-agent completed." |
| `blocking_human_feedback` | "Waiting for your input (see above)." |
| `blocking_human_questions` | "Waiting for your answers (see above)." |
| `conversation_error` | "An error occurred during processing." |

### Tier 2 -- Batched (every 12 seconds)

| Event Type | Format |
|---|---|
| `tool_call_start` | "Running: {tool_name}" |
| `tool_call_end` | "Completed: {tool_name}" |
| `unified_llm_completion` | First 200 chars of assistant text |
| `conversation_turn` | (included in batch) |

Batched items are deduplicated and limited to the last 5 unique entries.

### Tier 3 -- Skip

Everything else: streaming chunks, system prompts, cache events, performance events, debug data.

---

## Database Schema

**Migration**: `pkg/database/migrations/019_add_bot_connector_tables.sql`

### bot_connector_config

Stores per-platform configuration.

| Column | Type | Description |
|---|---|---|
| `id` | TEXT PK | Platform name ("slack", "discord", etc.) |
| `enabled` | BOOLEAN | Whether the connector is enabled |
| `bot_mode` | BOOLEAN | Full bot vs notification-only |
| `config_json` | TEXT | Platform-specific tokens/settings |
| `default_preset_id` | TEXT | Fallback preset when no match |
| `auto_confirm` | BOOLEAN | Skip confirmation step |
| `allowed_channels` | TEXT | JSON array of allowed channel IDs |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | |

### bot_sessions

One row per bot conversation thread.

| Column | Type | Description |
|---|---|---|
| `id` | TEXT PK | UUID |
| `platform` | TEXT | "slack", "discord", etc. |
| `channel_id` | TEXT | Platform channel ID |
| `thread_ts` | TEXT | Thread root timestamp |
| `session_id` | TEXT | Internal chat session ID (set after confirmation) |
| `user_id` | TEXT | Platform user ID |
| `user_name` | TEXT | |
| `query` | TEXT | Original user query |
| `status` | TEXT | analyzing / awaiting_confirmation / running / completed / failed |
| `analysis_json` | TEXT | AnalysisResult JSON |
| `preset_id` | TEXT | Matched preset ID |
| `config_json` | TEXT | Final QueryRequest config / thread context |
| `thread_context` | TEXT | Thread history JSON |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | |
| `completed_at` | DATETIME | |

Unique constraint: `(platform, channel_id, thread_ts)`

### bot_messages

All messages sent/received in a bot session.

| Column | Type | Description |
|---|---|---|
| `id` | TEXT PK | UUID |
| `bot_session_id` | TEXT FK | References bot_sessions(id) |
| `direction` | TEXT | "incoming" / "outgoing" |
| `message_type` | TEXT | user_request / analysis / confirmation / progress / result / human_feedback |
| `content` | TEXT | Message content |
| `platform_message_id` | TEXT | For message updates |
| `created_at` | DATETIME | |

---

## REST API

**File**: `bot_routes.go`

### Connector Management

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/bot/connectors` | List all connectors + status |
| `GET` | `/api/bot/connectors/{platform}` | Get config for a platform |
| `POST` | `/api/bot/connectors/{platform}` | Save/update connector config |
| `POST` | `/api/bot/connectors/{platform}/test` | Test connection |

### Session Management

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/bot/sessions` | List bot sessions (paginated, `?limit=&offset=&status=&platform=`) |
| `GET` | `/api/bot/sessions/{id}` | Get session details |
| `POST` | `/api/bot/sessions/{id}/stop` | Stop a running session |
| `GET` | `/api/bot/sessions/{id}/messages` | Get session messages |

---

## Slack App Setup

### Required Scopes

Add these OAuth scopes to your Slack app (in addition to existing ones):

- `app_mentions:read` -- receive @mention events
- `channels:history` -- read thread history (usually already present)
- `chat:write` -- send messages (usually already present)

### Event Subscriptions

Add to your Slack app's Event Subscriptions:

- `app_mention` -- triggered when bot is @mentioned

This is in addition to the existing `message.channels` subscription used for notification replies.

### Socket Mode

No changes needed -- the existing Socket Mode connection handles both notification replies and @mention events. The `SlackService` routes events to the appropriate handler based on type.

### Enable Bot Mode

Set `bot_mode: true` for the Slack connector config via the API or DB:

```bash
curl -X POST http://localhost:8080/api/bot/connectors/slack \
  -H "Content-Type: application/json" \
  -d '{"bot_mode": true, "auto_confirm": false}'
```

Or update the `bot_connector_config` table:
```sql
INSERT INTO bot_connector_config (id, enabled, bot_mode, auto_confirm)
VALUES ('slack', 1, 1, 0)
ON CONFLICT(id) DO UPDATE SET bot_mode = 1, enabled = 1;
```

---

## Human Feedback Integration

When a bot session triggers a human feedback request:

1. `HumanFeedbackStore.CreateRequestWithSlack()` checks `BotSessionCheckerFunc`
2. If the session is a bot session -> skips the 2-minute delay
3. The feedback question is sent immediately to the platform thread
4. The user replies in the thread
5. The reply is routed through `BotConversationManager` to the feedback store
6. The agent continues processing

This is seamless -- the user never leaves the thread.

---

## Import Cycle Avoidance

The `services` package cannot import `internal/events` (the EventStore). This is solved with two abstractions:

1. **`BotEventSubscriber` interface** (in `services/bot_connector.go`): abstracts event subscription with `SubscribeBot(sessionID) -> (chan, unsubscribe)`

2. **`BotEventSubscriberAdapter`** (in `bot_event_adapter.go`): bridges `EventStore` to `BotEventSubscriber`, converting `Event` to `BotEventData` in a goroutine

3. **`SessionStartFunc`** (in `services/bot_connector.go`): callback type for starting sessions, injected by server layer

These are wired in `server.go` during startup.

---

## Adding a New Platform

To add support for Discord, Telegram, or WhatsApp:

1. **Create `services/{platform}_bot_service.go`** implementing `BotConnector`:
   - `StartListening()` -- connect to platform API (WebSocket, polling, webhook)
   - `SendThreadMessage()` -- send a message in a thread
   - `GetThreadHistory()` -- fetch messages from a thread
   - `GetFormatter()` -- return a `MessageFormatter` for the platform

2. **Implement `MessageFormatter`** for the platform's markup syntax

3. **Add a `bot_connector_config` row** for the platform:
   ```sql
   INSERT INTO bot_connector_config (id, enabled, bot_mode) VALUES ('{platform}', 1, 1);
   ```

4. **Register in `server.go`** startup:
   ```go
   if platformBotEnabled {
       platformService := services.NewPlatformBotService(...)
       botManager.RegisterConnector(platformService)
   }
   ```

5. **Add routes** if platform-specific config is needed (the generic `/api/bot/connectors/{platform}` routes already work)

The `BotConversationManager` handles the rest -- analysis, confirmation, session management, event filtering -- identically across all platforms.
