# Slack Feedback Integration

## 📋 Overview

The Slack Feedback Integration extends the human feedback system to send notifications to Slack channels when feedback is requested. Users can respond directly in Slack threads, and their responses are automatically captured and submitted as feedback, enabling seamless human-in-the-loop workflows without requiring the UI to be open.

**Key Benefits:**
- Receive feedback requests in Slack channels
- Respond via Slack thread replies (no UI required)
- Automatic response capture and submission
- Works alongside existing UI feedback system
- Secure webhook verification
- Easy configuration through UI

---

## 📁 Key Files & Locations

| Component | File | Key Functions |
|-----------|------|---------------|
| **Slack Service** | [`slack_service.go`](file:///Users/mipl/ai-work/mcp-agent/agent_go/cmd/server/virtual-tools/slack_service.go) | `SendFeedbackNotification()`, `GetUniqueIDFromThread()`, `TestConnection()` |
| **API Routes** | [`slack_feedback_routes.go`](file:///Users/mipl/ai-work/mcp-agent/agent_go/cmd/server/slack_feedback_routes.go) | Configuration and webhook endpoints |
| **Database Migration** | [`010_add_slack_feedback_config.sql`](file:///Users/mipl/ai-work/mcp-agent/agent_go/pkg/database/migrations/010_add_slack_feedback_config.sql) | Slack config and message mapping tables |
| **Frontend UI** | [`SlackFeedbackConfig.tsx`](file:///Users/mipl/ai-work/mcp-agent/frontend/src/components/settings/SlackFeedbackConfig.tsx) | Configuration component |
| **API Service** | [`api.ts`](file:///Users/mipl/ai-work/mcp-agent/frontend/src/services/api.ts) | `getSlackFeedbackConfig()`, `updateSlackFeedbackConfig()`, `testSlackConnection()` |

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
   - If Slack is enabled, notification sent to configured Slack channel
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

### Step 3: Enable Events API

1. Navigate to **"Event Subscriptions"** in the sidebar
2. Toggle **"Enable Events"** to ON
3. Set **Request URL** to:
   ```
   https://your-domain.com/api/human-feedback/slack/webhook
   ```
4. Slack will send a verification challenge - the backend handles this automatically
5. Under **"Subscribe to bot events"**, add:
   - `message.channels` - Receive message events in channels

6. Click **"Save Changes"**

### Step 4: Get Signing Secret

1. Navigate to **"Basic Information"** in the sidebar
2. Scroll to **"App Credentials"**
3. Copy the **Signing Secret**

### Step 5: Get Channel ID

1. Open Slack in your browser
2. Navigate to the channel where you want notifications
3. Right-click the channel name → **"View channel details"**
4. Scroll down to find the **Channel ID** (starts with `C`)

### Step 6: Configure in UI

1. Open the application
2. Click the **Slack icon** in the sidebar (next to LLM config)
3. Enable **"Enable Slack Notifications"**
4. Enter:
   - **Bot Token**: `xoxb-...` (from Step 2)
   - **Channel ID**: `C1234567890` (from Step 5)
   - **Signing Secret**: `...` (from Step 4)
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
  "bot_token": "xoxb-...1234"  // Masked (last 4 chars only)
}
```

### Update Slack Configuration

**POST** `/api/human-feedback/slack/config`

**Request:**
```json
{
  "enabled": true,
  "bot_token": "xoxb-...",
  "channel_id": "C1234567890",
  "signing_secret": "..."
}
```

**Response:**
```json
{
  "enabled": true,
  "channel_id": "C1234567890"
}
```

### Test Connection

**POST** `/api/human-feedback/slack/test`

**Response:**
```json
{
  "success": true,
  "message": "Slack connection test successful!"
}
```

### Webhook Endpoint (Slack Events API)

**POST** `/api/human-feedback/slack/webhook`

This endpoint is called by Slack Events API. It handles:
- URL verification challenges
- Message events (thread replies)
- Signature verification

**Note:** This endpoint is not meant to be called directly by users.

---

## 📊 Database Schema

### `slack_feedback_config` Table

Stores global Slack configuration (single row).

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (always 'slack_config') |
| `enabled` | BOOLEAN | Whether Slack notifications are enabled |
| `bot_token` | TEXT | Slack bot token (encrypted in production) |
| `channel_id` | TEXT | Target Slack channel ID |
| `signing_secret` | TEXT | Slack signing secret for webhook verification |
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

### Webhook Signature Verification

All webhook requests from Slack are verified using HMAC-SHA256:

1. Extract `X-Slack-Signature` and `X-Slack-Request-Timestamp` headers
2. Check timestamp (prevent replay attacks - max 5 minutes old)
3. Create signature base string: `v0:{timestamp}:{request_body}`
4. Compute HMAC with signing secret
5. Compare with provided signature

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
2. Notification sent to Slack (if enabled)
3. UI also shows feedback request
4. User can respond via Slack thread OR UI
5. First response received is used

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
2. Notification sent to Slack (if enabled)
3. UI shows feedback request
4. User responds in Slack thread
5. Response submitted to feedback store
6. Orchestrator continues with response

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

### Webhook Not Receiving Events

**Issue:** Thread replies not being captured

**Solutions:**
- Verify webhook URL is correct in Slack app settings
- Check Events API is enabled
- Verify `message.channels` event is subscribed
- Check webhook URL is publicly accessible
- Review server logs for webhook errors
- Verify signing secret is correct

### Messages Not Appearing in Slack

**Issue:** Notifications not sent to Slack

**Solutions:**
- Check Slack is enabled in configuration
- Verify bot token is valid
- Check channel ID is correct
- Ensure bot is member of the channel
- Review backend logs for Slack API errors

### Signature Verification Fails

**Issue:** "unauthorized" errors in webhook

**Solutions:**
- Verify signing secret matches Slack app settings
- Check request timestamp is within 5 minutes
- Ensure request body is not modified
- Verify webhook URL is correct

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
feedbackStore.CreateRequest(uniqueID, message)

// Slack notification sent asynchronously (non-blocking)
go func() {
    slackService.SendFeedbackNotification(ctx, uniqueID, message, context)
}()
```

### Tool Execution

In `handleHumanFeedback()`:
- Creates feedback request
- Sends Slack notification (if enabled)
- Waits for response (from UI or Slack)

### Orchestrator Feedback

In `RequestHumanFeedback()`, `RequestYesNoFeedback()`, `RequestMultipleChoiceFeedback()`:
- Emits blocking feedback event
- Sends Slack notification (if enabled)
- Waits for response

---

## 🎨 UI Components

### SlackFeedbackConfig Component

Located at: `frontend/src/components/settings/SlackFeedbackConfig.tsx`

**Features:**
- Enable/disable toggle
- Bot token input (password field with show/hide)
- Channel ID input
- Signing secret input (password field)
- Test connection button
- Save configuration button
- Setup instructions
- Error/success message display

**Access:**
- Sidebar → Slack icon (💬)
- Opens modal for configuration

---

## 📖 Related Documentation

- [Human Feedback Tool](file:///Users/mipl/ai-work/mcp-agent/docs/human_feedback_tool.md) - Core human feedback system
- [Workflow Orchestrator](file:///Users/mipl/ai-work/mcp-agent/docs/workflow_orchestrator.md) - Uses human feedback for approvals

---

## 🔮 Future Enhancements

Potential improvements:
- Multiple Slack channels (per workflow/session)
- Rich interactive elements (buttons, dropdowns)
- Response validation and formatting
- Notification preferences (per user/team)
- Message templates (customizable formats)
- Auto-close threads after response
- Reminder notifications for pending feedback
- Email connector integration
- WhatsApp connector integration

---

## ✅ Quick Checklist

**Setup Checklist:**
- [ ] Slack app created
- [ ] Bot token scopes configured (`chat:write`, `channels:read`)
- [ ] Events API enabled
- [ ] `message.channels` event subscribed
- [ ] Webhook URL configured
- [ ] Bot token copied
- [ ] Channel ID obtained
- [ ] Signing secret copied
- [ ] Configuration saved in UI
- [ ] Connection test successful
- [ ] Test feedback request sent
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

