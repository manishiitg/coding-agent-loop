# Slack Feedback Integration

## 📋 Overview

The Slack Feedback Integration extends the human feedback system to send reminder notifications to Slack channels when users don't respond to feedback requests within 2 minutes. This reduces notification noise while ensuring important feedback requests don't go unnoticed. Users can respond directly in Slack threads, and their responses are automatically captured and submitted as feedback, enabling seamless human-in-the-loop workflows without requiring the UI to be open.

**Key Benefits:**
- Smart delayed notifications (only if no response after 2 minutes)
- Respond via Slack thread replies (no UI required)
- Automatic response capture and submission
- Works alongside existing UI feedback system
- Secure Socket Mode connection (no webhook required)
- Easy configuration through UI

---

## 📁 Key Files & Locations

| Component | File | Key Functions |
|-----------|------|---------------|
| **Slack Service** | [`slack_service.go`](../agent_go/cmd/server/services/slack_service.go) | `SendFeedbackNotification()`, `GetUniqueIDFromThread()`, `TestConnection()` |
| **API Routes** | [`slack_feedback_routes.go`](../agent_go/cmd/server/slack_feedback_routes.go) | Configuration and test endpoints |
| **Human Feedback Store** | [`human_feedback_store.go`](../agent_go/cmd/server/virtual-tools/human_feedback_store.go) | `CreateRequestWithSlack()` - implements 2-minute delayed notification |
| **Database Migration** | [`010_add_slack_feedback_config.sql`](../agent_go/pkg/database/migrations/010_add_slack_feedback_config.sql) | Slack config and message mapping tables |
| **Frontend UI** | [`SlackFeedbackConfig.tsx`](../frontend/src/components/settings/SlackFeedbackConfig.tsx) | Configuration component |
| **API Service** | [`api.ts`](../frontend/src/services/api.ts) | `getSlackFeedbackConfig()`, `updateSlackFeedbackConfig()`, `testSlackConnection()` |

---

## 🔄 How It Works

### System Flow

1. **Configuration**
   - User configures Slack credentials in UI (bot token, channel ID, signing secret)
   - Configuration saved to database
   - Slack service initialized with credentials

2. **Feedback Request**
   - LLM or orchestrator requests human feedback
   - Backend creates feedback request in `HumanFeedbackStore`
   - Feedback request appears immediately in UI
   - **Delayed Notification**: If Slack is enabled, system waits 2 minutes
   - If user hasn't responded after 2 minutes, Slack notification is sent as a reminder
   - Message includes question, context, and unique request ID

3. **User Response**
   - User sees notification in Slack channel
   - User replies in the thread
   - Slack Events API sends webhook to backend

4. **Response Processing**
   - Backend verifies webhook signature
   - Extracts unique ID from thread parent message
   - Submits reply text to `HumanFeedbackStore`
   - Execution continues with user's response

### Message Mapping

The system maintains a mapping between:
- **Unique ID** (feedback request identifier)
- **Slack Message Timestamp** (parent message)
- **Slack Channel ID**

This allows the system to match thread replies to the correct feedback request.

---

## 🚀 Setup Instructions

### Step 1: Create Slack App

1. Go to https://api.slack.com/apps
2. Click **"Create New App"** → **"From scratch"**
3. Enter app name (e.g., "MCP Agent Feedback")
4. Select your workspace
5. Click **"Create App"**

### Step 2: Configure Bot Token Scopes

1. Navigate to **"OAuth & Permissions"** in the sidebar
2. Scroll to **"Scopes"** → **"Bot Token Scopes"**
3. Add the following scopes:
   - `chat:write` - Post messages to channels
   - `channels:read` - Read channel information (optional, for validation)

4. Scroll up and click **"Install to Workspace"**
5. Authorize the app
6. Copy the **Bot User OAuth Token** (starts with `xoxb-`)

### Step 3: Enable Socket Mode and Get App Token

1. Navigate to **"Socket Mode"** in the sidebar
2. Toggle **"Enable Socket Mode"** to ON
3. Navigate to **"Basic Information"** → **"App-Level Tokens"**
4. Click **"Generate Token and Scopes"**
5. Enter token name (e.g., "Socket Mode Token")
6. Add scope: `connections:write`
7. Click **"Generate"** and copy the **App-Level Token** (starts with `xapp-`)

### Step 4: Enable Events API (Socket Mode)

1. Navigate to **"Event Subscriptions"** in the sidebar
2. Toggle **"Enable Events"** to ON
3. Under **"Subscribe to bot events"**, add:
   - `message.channels` - Receive message events in channels
4. Click **"Save Changes"**

**Note:** With Socket Mode, you don't need to configure a webhook URL. The system uses a WebSocket connection instead.

### Step 5: Get Channel ID

1. Open Slack in your browser
2. Navigate to the channel where you want notifications
3. Right-click the channel name → **"View channel details"**
4. Scroll down to find the **Channel ID** (starts with `C`)

### Step 6: Configure in UI

1. Open the application
2. Click the **Slack icon** in the sidebar (next to LLM config)
3. Enable **"Enable Slack Notifications"** (description: "Send Slack notifications if user doesn't respond within 2 minutes")
4. Enter:
   - **Bot Token**: `xoxb-...` (from Step 2)
   - **App Token**: `xapp-...` (from Step 3 - Socket Mode)
   - **Channel ID**: `C1234567890` (from Step 5)
5. Click **"Test Connection"** to verify
6. Click **"Save Configuration"**

---

## 🔌 API Endpoints

### Get Slack Configuration

**GET** `/api/human-feedback/slack/config`

**Response:**
```json
{
  "enabled": true,
  "channel_id": "C1234567890",
  "bot_token": "xoxb-...1234",  // Masked (last 4 chars only)
  "app_token": "xapp-...5678"   // Masked (last 4 chars only)
}
```

### Update Slack Configuration

**POST** `/api/human-feedback/slack/config`

**Request:**
```json
{
  "enabled": true,
  "bot_token": "xoxb-...",
  "app_token": "xapp-...",
  "channel_id": "C1234567890"
}
```

**Response:**
```json
{
  "enabled": true,
  "channel_id": "C1234567890"
}
```

**Note:** Tokens are not returned in the response for security reasons.

### Test Connection

**POST** `/api/human-feedback/slack/test`

**Response:**
```json
{
  "success": true,
  "message": "Slack connection test successful!"
}
```

### Get Test Connection Reply

**GET** `/api/human-feedback/slack/test/reply?test_id={test_id}`

Returns the reply received for a test connection (used for polling).

**Response:**
```json
{
  "test_id": "test-connection-1234567890",
  "reply": "Test reply from Slack",
  "received": true
}
```

**Note:** Returns 204 No Content if no reply has been received yet.

---

## 📊 Database Schema

### `slack_feedback_config` Table

Stores global Slack configuration (single row).

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (always 'slack_config') |
| `enabled` | BOOLEAN | Whether Slack notifications are enabled |
| `bot_token` | TEXT | Slack bot token (encrypted in production) |
| `app_token` | TEXT | App-level token for Socket Mode (encrypted in production) |
| `channel_id` | TEXT | Target Slack channel ID |
| `created_at` | DATETIME | Creation timestamp |
| `updated_at` | DATETIME | Last update timestamp |

### `slack_feedback_messages` Table

Maps Slack messages to feedback requests.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key |
| `unique_id` | TEXT | Maps to `HumanFeedbackRequest.UniqueID` |
| `slack_message_ts` | TEXT | Slack message timestamp |
| `slack_channel_id` | TEXT | Slack channel ID |
| `slack_thread_ts` | TEXT | Thread timestamp (same as message_ts for parent) |
| `created_at` | DATETIME | Creation timestamp |

---

## 🔒 Security

### Socket Mode Security

The system uses Slack's Socket Mode, which provides:
- **WebSocket Connection**: Real-time bidirectional communication
- **No Public Webhook Required**: No need to expose webhook endpoints
- **App-Level Token**: Separate token for Socket Mode connection
- **Automatic Reconnection**: Handles connection drops gracefully

### Token Storage

- Bot tokens are stored in the database
- Tokens are masked in API responses (only last 4 characters shown)
- **Recommendation:** Encrypt tokens in production environments

---

## 🎯 Usage Examples

### LLM Tool Call with Slack

When an LLM calls the `human_feedback` tool:

```json
{
  "tool": "human_feedback",
  "arguments": {
    "message_for_user": "Please approve this database migration",
    "unique_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

**What happens:**
1. Feedback request created in backend
2. Feedback request appears immediately in UI
3. If user responds within 2 minutes → no Slack notification sent
4. If user doesn't respond within 2 minutes → Slack notification sent as reminder
5. User can respond via Slack thread OR UI
6. First response received is used

### Orchestrator Feedback with Slack

When orchestrator requests feedback:

```go
approved, feedback, err := orchestrator.RequestHumanFeedback(
    ctx,
    "approval_123",
    "Please review the generated plan",
    "Plan includes 5 steps...",
    sessionID,
    workflowID,
)
```

**What happens:**
1. `BlockingHumanFeedbackEvent` emitted
2. Feedback request appears immediately in UI
3. If user responds within 2 minutes → no Slack notification sent
4. If user doesn't respond within 2 minutes → Slack notification sent as reminder
5. User responds in Slack thread or UI
6. Response submitted to feedback store
7. Orchestrator continues with response

---

## 🐛 Troubleshooting

### Connection Test Fails

**Issue:** "Connection test failed"

**Solutions:**
- Verify bot token is correct (starts with `xoxb-`)
- Check bot has `chat:write` scope
- Verify channel ID is correct (starts with `C`)
- Ensure bot is installed to workspace
- Check bot has access to the channel

### Socket Mode Not Receiving Events

**Issue:** Thread replies not being captured

**Solutions:**
- Verify Socket Mode is enabled in Slack app settings
- Check Events API is enabled
- Verify `message.channels` event is subscribed
- Ensure App-Level Token has `connections:write` scope
- Check bot is member of the channel (type `/invite @YourBotName`)
- Verify bot has `channels:history` scope (required to receive messages)
- Review server logs for Socket Mode connection errors
- Check that App Token is correct (starts with `xapp-`)

### Messages Not Appearing in Slack

**Issue:** Notifications not sent to Slack

**Solutions:**
- **Note**: Slack notifications are only sent if user doesn't respond within 2 minutes
- If you respond quickly in UI, Slack notification won't be sent (this is expected behavior)
- Check Slack is enabled in configuration
- Verify bot token is valid
- Check channel ID is correct
- Ensure bot is member of the channel
- Review backend logs for Slack API errors
- Wait 2+ minutes without responding to test delayed notification

### Socket Mode Connection Issues

**Issue:** Socket Mode connection fails or disconnects

**Solutions:**
- Verify App-Level Token is correct (starts with `xapp-`)
- Check App-Level Token has `connections:write` scope
- Ensure Bot Token is valid (starts with `xoxb-`)
- Review server logs for connection errors
- Check network connectivity to Slack servers
- Verify Socket Mode is enabled in Slack app settings

---

## 🔧 Configuration

### Environment Variables

No environment variables required. Configuration is stored in the database and managed through the UI.

### Frontend Configuration

Access configuration via:
- **Sidebar** → Slack icon (next to LLM config)
- Or programmatically via API endpoints

### Backend Configuration

Configuration is loaded from database on server start:
- Stored in `slack_feedback_config` table
- Loaded by `SlackService` on initialization
- Can be updated via API without restart

---

## 📝 Message Format

Slack notifications use rich message blocks:

```
🤔 Human Feedback Required

Question:
[User's question text]

Context:
[Additional context if provided]

Reply to this message to provide feedback
Request ID: `550e8400-e29b-41d4-a716-446655440000`
```

The unique ID is included in the footer for tracking and mapping thread replies.

---

## 🔄 Integration Points

### Human Feedback Store

The Slack service integrates with `HumanFeedbackStore`:

```go
// When feedback request is created
feedbackStore.CreateRequestWithSlack(ctx, uniqueID, message, contextMsg, buttonOptions)

// System automatically:
// 1. Creates feedback request (appears in UI immediately)
// 2. Waits 2 minutes in background
// 3. Checks if user responded
// 4. If no response, sends Slack notification as reminder
```

### Delayed Notification Logic

The system implements smart delayed notifications:

1. **Immediate**: Feedback request appears in UI right away
2. **2-Minute Delay**: System waits 2 minutes before sending Slack notification
3. **Response Check**: Before sending, system checks if user already responded
4. **Conditional Send**: Only sends Slack notification if user hasn't responded
5. **Non-Blocking**: All notification logic runs asynchronously

### Tool Execution

In `handleHumanFeedback()`:
- Creates feedback request (appears in UI immediately)
- Starts 2-minute delayed notification timer
- Waits for response (from UI or Slack)
- If no response after 2 minutes, sends Slack reminder

### Orchestrator Feedback

In `RequestHumanFeedback()`, `RequestYesNoFeedback()`, `RequestMultipleChoiceFeedback()`:
- Emits blocking feedback event
- Feedback appears in UI immediately
- Starts 2-minute delayed notification timer
- If no response after 2 minutes, sends Slack reminder
- Waits for response

---

## 🎨 UI Components

### SlackFeedbackConfig Component

Located at: `frontend/src/components/settings/SlackFeedbackConfig.tsx`

**Features:**
- Enable/disable toggle (with description: "Send Slack notifications if user doesn't respond within 2 minutes")
- Bot token input (password field with show/hide)
- App token input (password field with show/hide) - for Socket Mode
- Channel ID input
- Test connection button
- Save configuration button
- Setup instructions
- Error/success message display

**Access:**
- Sidebar → Slack icon (💬)
- Opens modal for configuration

---

## 📖 Related Documentation

- [Human Feedback Tool](human_feedback_tool.md) - Core human feedback system
- [Workflow Orchestrator](workflow_orchestrator.md) - Uses human feedback for approvals

---

## 🔮 Future Enhancements

Potential improvements:
- Configurable delay time (currently fixed at 2 minutes)
- Multiple Slack channels (per workflow/session)
- Rich interactive elements (buttons, dropdowns)
- Response validation and formatting
- Notification preferences (per user/team)
- Message templates (customizable formats)
- Auto-close threads after response
- Multiple reminder notifications (escalation)
- Email connector integration
- WhatsApp connector integration

---

## ✅ Quick Checklist

**Setup Checklist:**
- [ ] Slack app created
- [ ] Bot token scopes configured (`chat:write`, `channels:read`, `channels:history`)
- [ ] Socket Mode enabled
- [ ] App-Level Token created with `connections:write` scope
- [ ] Events API enabled
- [ ] `message.channels` event subscribed
- [ ] Bot token copied
- [ ] App token copied
- [ ] Channel ID obtained
- [ ] Bot invited to channel (`/invite @YourBotName`)
- [ ] Configuration saved in UI
- [ ] Connection test successful
- [ ] Test feedback request sent (wait 2+ minutes without responding to test delayed notification)
- [ ] Thread reply captured successfully

---

## 📞 Support

For issues or questions:
1. Check server logs for errors
2. Verify Slack app configuration
3. Test connection via UI
4. Review webhook events in Slack app settings
5. Check database for configuration entries

---

**Last Updated:** 2025-01-27

## ⏰ Delayed Notification Behavior

The Slack integration uses a **smart delayed notification** strategy to reduce notification noise:

- **Immediate**: Feedback requests appear in the UI immediately when created
- **2-Minute Delay**: System waits 2 minutes before sending Slack notification
- **Response Check**: Before sending, system verifies if user has already responded
- **Conditional Send**: Slack notification is only sent if user hasn't responded within 2 minutes
- **Non-Blocking**: All notification logic runs asynchronously and doesn't block the main workflow

This approach ensures:
- Users who respond quickly don't receive unnecessary Slack notifications
- Users who don't respond get a helpful reminder after 2 minutes
- Reduced notification noise in Slack channels
- Better user experience with immediate UI feedback

