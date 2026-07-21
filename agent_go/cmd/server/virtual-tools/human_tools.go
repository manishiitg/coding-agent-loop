package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	mcpexecutor "github.com/manishiitg/mcpagent/executor"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// CreateHumanTools creates human interaction tools
func CreateHumanTools() []llmtypes.Tool {
	var humanTools []llmtypes.Tool

	// Add human_feedback tool
	humanFeedbackTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "human_feedback",
			Description: "Request urgent, short-lived input that only a human can provide, such as an OTP/2FA code, CAPTCHA completion, explicit approval, a subjective decision, private information, or an explicit test of the human-feedback channel. This tool pauses only the calling agent turn until the human answers directly in the AgentWorks UI. It never sends through notify_user, Gmail, workflow webhooks, or account-level notification connectors. Do not use it for an ordinary Builder/chat question, something another agent can answer, or something that may wait hours or days. Choose the shortest realistic timeout_seconds; use an expiry shown by the external service when available. The tool returns the human's response as text. Bridge-only coding CLIs invoking the HTTP endpoint through execute_shell_command must keep curl in the foreground and wait for the same call to return; never use nohup, append &, delegate/background it, write the result to a temporary file, poll for completion, or ask the user to send another message after responding. Do not set the shell timeout shorter than timeout_seconds. Cursor CLI has an approximately 60-second silent MCP-call ceiling, so Cursor agents must set timeout_seconds to at most 45 seconds and may retry after an explicit expiry only when the input is still required.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message_for_user": map[string]interface{}{
						"type":        "string",
						"description": "Message to display to the user requesting their feedback",
					},
					"unique_id": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for this feedback request. Always generate a UUID (e.g., '550e8400-e29b-41d4-a716-446655440000').",
					},
					"options": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Optional list of choices to present as buttons. When provided, the user clicks a button instead of typing. Use for multiple-choice questions (e.g. ['Option A: Use REST API', 'Option B: Use GraphQL', 'Option C: Use gRPC']). Omit for free-text input.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"minimum":     30,
						"maximum":     1800,
						"default":     300,
						"description": "How long to wait for the human before the request expires. Choose the shortest realistic duration. Defaults to 300 seconds and is bounded to 30-1800 seconds.",
					},
				},
				"required": []string{"unique_id", "message_for_user"},
			}),
		},
	}
	humanTools = append(humanTools, humanFeedbackTool)

	// The email_* fields are exposed only when Gmail is an enabled channel, so
	// the agent doesn't see email-specific knobs it can't use.
	notifyProps := map[string]interface{}{
		"message_for_user": map[string]interface{}{
			"type":        "string",
			"description": "Concise plain-text summary sent to every channel and used as the Slack/email fallback. When supplying rich Slack fields, make this the lead verdict rather than duplicating every detail.",
		},
		"slack_title": map[string]interface{}{
			"type":        "string",
			"maxLength":   150,
			"description": "Optional Block Kit header. Use by default for workflow, Pulse, Chief of Staff, Goal Advisor, and other structured summaries. The backend owns the webhook URL and renders this safely; never post to a webhook directly.",
		},
		"slack_color": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"neutral", "success", "warning", "danger"},
			"description": "Block Kit accent color chosen from the factual outcome: success only when healthy, warning for incomplete/blocked, danger for confirmed failure, otherwise neutral.",
		},
		"slack_fields": map[string]interface{}{
			"type":     "array",
			"maxItems": 10,
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"label": map[string]interface{}{"type": "string"},
					"value": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"label", "value"},
				"additionalProperties": false,
			},
			"description": "Optional compact Block Kit metric fields. Use for counts/statuses. Other channels ignore these fields.",
		},
		"slack_sections": map[string]interface{}{
			"type":     "array",
			"maxItems": 12,
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"heading": map[string]interface{}{"type": "string"},
					"body":    map[string]interface{}{"type": "string"},
				},
				"required":             []string{"heading", "body"},
				"additionalProperties": false,
			},
			"description": "Optional ordered Block Kit sections for changed areas, findings, issues, blockers, or next actions. Slack mrkdwn and <url|label> links are supported. Other channels ignore these fields.",
		},
		"slack_footer": map[string]interface{}{
			"type":        "string",
			"maxLength":   2000,
			"description": "Optional short Block Kit context/footer such as scope and date. Other channels ignore this field.",
		},
		"exclude_channels": map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "string", "enum": []string{"gmail", "slack", "whatsapp"}},
			"description": "Optional one-off override to SKIP account-level delivery channels for THIS notification only, by name (\"gmail\", \"slack\", \"whatsapp\"). The DURABLE per-workflow preference belongs in workflow.json notifications.exclude_channels and is applied automatically on every send — use this arg only for a one-time skip beyond that. Suppresses the channel for this send only; never changes the account-wide configuration. Omit to deliver to every enabled channel not already excluded by workflow.json. The always-on web UI and any configured workflow Slack webhook are unaffected.",
		},
	}
	if gmailEnabled() {
		notifyProps["email_subject"] = map[string]interface{}{
			"type":        "string",
			"description": "Custom subject line for the email rendering (Gmail). When Gmail is enabled, set this by default for workflow/Pulse/Goal Advisor notifications unless the user explicitly asked not to email. Other channels ignore this.",
		}
		notifyProps["email_body"] = map[string]interface{}{
			"type":        "string",
			"description": "PLAIN-TEXT email body (longer than message_for_user). When Gmail is enabled, set this as the plain fallback by default for workflow/Pulse/Goal Advisor notifications unless the user explicitly asked not to email. Do NOT put HTML here — for a formatted email use email_html. If omitted, message_for_user is the body. (HTML accidentally placed here is auto-detected and rendered, but email_html is correct.)",
		}
		notifyProps["email_to"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional Gmail To recipients that replace the configured default To recipient for this notification. Use only when the user's notification preference explicitly asks to send the email to different primary recipient(s). Addresses in Gmail's blocked recipients list are rejected. Other channels ignore this.",
		}
		notifyProps["email_cc"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional Gmail CC recipients. Addresses in Gmail's blocked recipients list are rejected. Other channels ignore this.",
		}
		notifyProps["email_attachments"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional. Absolute file paths on the server host to attach to the email (Gmail only).",
		}
		notifyProps["block_recipients"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional one-off email denylist for THIS notification (Gmail only). Addresses listed here are rejected as To or CC recipients, on top of BOTH the account-wide disallowed-recipients list AND the durable per-workflow denylist in workflow.json notifications.block_recipients — it can only block MORE, never unblock a globally-blocked address. Put addresses the workflow must never email in workflow.json notifications.block_recipients (applied automatically); use this arg only for a one-time block beyond that. If a blocked address is the resolved recipient, the email is skipped rather than sent elsewhere. Does not change any account-wide configuration; other channels ignore this.",
		}
		notifyProps["email_html"] = map[string]interface{}{
			"type":        "string",
			"description": "Rich HTML body for a designed/formatted email (Gmail only). When Gmail is enabled, set this by default for workflow/Pulse/Goal Advisor notifications unless the user explicitly asked not to email. MUST be EMAIL-SAFE: use INLINE styles only (a style attribute on each element). Gmail strips <style> blocks, <head>, <script>, and class-based CSS, so a full browser HTML document or a generated *.html report (e.g. pulse/org-pulse.html) arrives UNSTYLED — build a compact inline-styled summary and link to the full report instead of pasting it. message_for_user / email_body remain the plain-text fallback for clients that don't render HTML. Other channels ignore this.",
		}
		notifyProps["email_html_file"] = map[string]interface{}{
			"type":        "string",
			"description": "Optional. Absolute path to an .html file on the server host to use as the HTML email body (alternative to email_html). The file MUST be email-safe — INLINE styles only; Gmail strips <style>/<head>/class CSS, so a browser-oriented report file (e.g. pulse/org-pulse.html) renders UNSTYLED. Point this at an email-specific inline-styled file, not the full browser report. If the file can't be read, the tool returns an error so you can fix the path.",
		}
	}
	notifyUserTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "notify_user",
			Description: buildNotifyDescription(),
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": notifyProps,
				"required":   []string{"message_for_user"},
			}),
		},
	}
	humanTools = append(humanTools, notifyUserTool)

	return humanTools
}

// channelLabels maps connector Name() values to human-friendly labels used in
// the dynamic notify_user description.
var channelLabels = map[string]string{
	"slack":    "Slack",
	"whatsapp": "WhatsApp",
	"gmail":    "Gmail (email)",
}

var sendRichSlackIncomingWebhook = services.SendRichSlackIncomingWebhook

// buildNotifyDescription renders the notify_user description with the set of
// channels enabled when the tool list is built (per session/run), so the agent
// knows where its message will actually land. The always-on web UI connector is
// not framed as an external channel.
func buildNotifyDescription() string {
	base := "Send a non-blocking notification to the human. Use this for FYIs, progress updates, alerts, and completion notices when you do not need to wait for a reply. If the workflow has a Slack Incoming Webhook configured, this tool automatically sends a backend-owned rich Block Kit card there in addition to enabled account-level channels; even a plain message_for_user call receives the safe rich default. For workflow, Pulse, Chief of Staff, Goal Advisor, and other structured summaries, set slack_title, factual slack_color, compact slack_fields, relevant slack_sections, and slack_footer by default. Never access a SECRET_* webhook variable, construct a webhook payload in shell, post with curl, disable notify_user to avoid duplication, or ask for the URL after an encrypted webhook reference is configured—the backend exclusively owns delivery. If you need the human to answer before continuing, use human_feedback instead. Returns a JSON delivery result — status (delivered|partial|failed|no_recipient|no_channels_configured) plus delivered/skipped/failed channel lists. Report it honestly to the user: do NOT claim the message was sent if status is failed or no_channels_configured."

	var labels []string
	gmailOn := false
	if nm := services.GetNotificationManager(); nm != nil {
		for _, name := range nm.ListEnabledConnectors() {
			if name == "web_simulator" {
				continue
			}
			if name == "gmail" {
				gmailOn = true
			}
			if l, ok := channelLabels[name]; ok {
				labels = append(labels, l)
			} else {
				labels = append(labels, name)
			}
		}
	}

	if len(labels) == 0 {
		return base + " NOTE: No account-level channels (Slack bot/WhatsApp/Gmail) are currently enabled. The message still uses a workflow Slack webhook when one is configured; otherwise it appears only in the web UI."
	}
	desc := base + " Currently enabled delivery channels: " + strings.Join(labels, ", ") + ". The message is delivered to all enabled channels — you do not choose which."
	if gmailOn {
		desc += " Gmail is enabled, so email_subject, email_body, email_to, email_cc, email_html, email_html_file, and email_attachments are available for the email rendering (other channels ignore these). For workflow, Pulse, org pulse, and Goal Advisor notifications, treat email as the default rich rendering: set email_subject, email_html, and plain email_body on the same notify_user call unless the user's notification preference explicitly says not to email. Set email_to only when the user's preference asks to replace the configured default To recipient; set email_cc only when the preference asks for CC recipients. Keep email_body plain text as the fallback."
	}
	return desc
}

// gmailEnabled reports whether the Gmail connector is currently an enabled
// delivery channel.
func gmailEnabled() bool {
	if nm := services.GetNotificationManager(); nm != nil {
		for _, n := range nm.ListEnabledConnectors() {
			if n == "gmail" {
				return true
			}
		}
	}
	return false
}

// htmlTagRe deterministically matches a real HTML start/end tag: "<" immediately
// followed by a known tag name (optional "/" for a closing tag), then a tag
// delimiter (space, "/", or ">"). Requiring the name right after "<" — with no
// space — means prose like "a < b", "score <3", or "x<y" never matches, while
// "<p>", "<div ...>", "<br/>", "</body>", "<!doctype html>" do.
var htmlTagRe = regexp.MustCompile(`(?is)<(?:!doctype\s+html|/?(?:html|head|body|div|span|table|thead|tbody|tr|td|th|p|h[1-6]|br|hr|ul|ol|li|a|img|strong|em|b|i|u|style|center|font|pre|blockquote))[\s/>]`)

// looksLikeHTML reports whether a string contains real HTML markup. Used to
// rescue HTML an agent placed in email_body (plain) instead of email_html.
func looksLikeHTML(s string) bool {
	return htmlTagRe.MatchString(s)
}

// gmailContentFromArgs builds the per-channel Gmail content from notify_user
// tool args, or (nil, nil) if no email-specific fields were provided. Returns an
// error (surfaced to the agent) when a referenced file can't be read.
func gmailContentFromArgs(args map[string]interface{}) (*services.GmailContent, error) {
	subject, _ := args["email_subject"].(string)
	body, _ := args["email_body"].(string)
	html, _ := args["email_html"].(string)
	cc := emailListFromArg(args["email_cc"])

	// email_html_file: absolute path to an .html file on the server host; its
	// contents become the HTML body (an alternative to inline email_html).
	if hf, _ := args["email_html_file"].(string); strings.TrimSpace(hf) != "" {
		data, err := os.ReadFile(strings.TrimSpace(hf))
		if err != nil {
			return nil, fmt.Errorf("email_html_file %q could not be read: %w", strings.TrimSpace(hf), err)
		}
		html = string(data)
	}

	// Robustness: agents frequently drop HTML into email_body (the plain field).
	// If email_body clearly contains HTML and no explicit email_html was given,
	// treat it as the HTML body so it renders instead of showing raw markup; the
	// plain-text fallback then falls back to message_for_user.
	if strings.TrimSpace(html) == "" && looksLikeHTML(body) {
		html = body
		body = ""
	}

	var attachments []string
	if raw, ok := args["email_attachments"].([]interface{}); ok {
		for _, a := range raw {
			if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
				attachments = append(attachments, strings.TrimSpace(s))
			}
		}
	}
	if strings.TrimSpace(subject) == "" && strings.TrimSpace(body) == "" && strings.TrimSpace(html) == "" && len(attachments) == 0 && len(cc) == 0 {
		return nil, nil
	}
	return &services.GmailContent{
		Subject:     strings.TrimSpace(subject),
		CC:          cc,
		Body:        body,
		HTMLBody:    html,
		Attachments: attachments,
	}, nil
}

func emailListFromArg(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return normalizeNotifyEmailList(values)
	case []string:
		return normalizeNotifyEmailList(v)
	case string:
		return normalizeNotifyEmailList([]string{v})
	default:
		return nil
	}
}

// stringListFromArg reads an array-or-string tool argument into a trimmed,
// lowercased, de-duplicated slice. Used for simple token lists such as
// exclude_channels ("gmail", "slack", "whatsapp") where email-style splitting
// isn't needed.
func stringListFromArg(raw interface{}) []string {
	var values []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case []string:
		values = v
	case string:
		values = []string{v}
	default:
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		token := strings.ToLower(strings.TrimSpace(raw))
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func normalizeNotifyEmailList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			email := strings.ToLower(strings.TrimSpace(part))
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			out = append(out, email)
		}
	}
	return out
}

// GetToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human_tools"
}

// IsHumanToolCategory checks the single canonical category used by tool
// registration, workflow configuration, filtering, and context injection.
func IsHumanToolCategory(category string) bool {
	return strings.TrimSpace(category) == GetHumanToolCategory()
}

// WorkshopHumanToolNames is the SINGLE SOURCE OF TRUTH for which human tools a
// workflow-builder / workshop / run agent may use. The workshop allow-list
// (GetToolsForWorkshopMode) derives its human tools from here, and these are all
// registered by createCustomTools(workflowMode=true) — so the allow-list can never
// drift from what's actually registered (the drift that made notify_user invisible).
//
// human_feedback is available for explicit channel tests and truly urgent,
// short-lived human-only input; ordinary builder questions stay in chat.
// notify_user is the non-blocking outbound push (Slack/WhatsApp/Gmail).
// create_human_input_request and
// mark_human_input_consumed are non-blocking Pulse/report questions stored in
// the workflow-local db/db.sqlite.
func WorkshopHumanToolNames() []string {
	return []string{"human_feedback", "notify_user", "create_human_input_request", "mark_human_input_consumed"}
}

// CreateHumanToolExecutors creates the execution functions for human tools
func CreateHumanToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["human_feedback"] = handleHumanFeedback
	executors["notify_user"] = handleNotifyUser

	return executors
}

func handleNotifyUser(ctx context.Context, args map[string]interface{}) (string, error) {
	messageForUser, _ := args["message_for_user"].(string)
	messageForUser = strings.TrimSpace(messageForUser)
	if messageForUser == "" {
		return "", fmt.Errorf("message_for_user is required")
	}

	notificationManager := services.GetNotificationManager()
	if notificationManager == nil {
		return "", fmt.Errorf("notification manager not available")
	}

	dest := NotificationDestinationFromContext(ctx)
	if to := emailListFromArg(args["email_to"]); len(to) > 0 {
		if dest == nil {
			dest = &services.NotificationDestination{}
		}
		dest.Gmail = &services.GmailDest{Email: strings.Join(to, ", ")}
	}
	// Optional one-off email denylist for this send, unioned with both the
	// account-wide blocked list and the per-workflow workflow.json
	// notifications.block_recipients already carried on dest.Gmail.
	if blocked := emailListFromArg(args["block_recipients"]); len(blocked) > 0 {
		if dest == nil {
			dest = &services.NotificationDestination{}
		}
		if dest.Gmail == nil {
			dest.Gmail = &services.GmailDest{}
		}
		dest.Gmail.BlockedRecipients = append(dest.Gmail.BlockedRecipients, blocked...)
	}
	gc, err := gmailContentFromArgs(args)
	if err != nil {
		return "", err // e.g. email_html_file not found — feed the problem back to the agent
	}
	if gc != nil {
		if dest == nil {
			dest = &services.NotificationDestination{}
		}
		dest.Content = &services.NotificationContent{Gmail: gc}
	}
	slackContent, err := slackContentFromArgs(args)
	if err != nil {
		return "", err
	}

	// Per-workflow channel opt-out. The durable preference comes from workflow.json
	// notifications.exclude_channels (carried on dest.ExcludeChannels); the optional
	// exclude_channels arg adds a one-off skip for this send. Both are unioned.
	excludeChannels := stringListFromArg(args["exclude_channels"])
	if dest != nil && len(dest.ExcludeChannels) > 0 {
		excludeChannels = append(excludeChannels, dest.ExcludeChannels...)
	}

	// Synchronous send so we can report real per-channel delivery to the agent
	// (and so the send isn't killed when this turn's context is canceled).
	results := notificationManager.SendUserNotificationSync(ctx, messageForUser, "", dest, excludeChannels...)
	if dest != nil && dest.SlackWebhook != nil {
		msgID, sendErr := sendRichSlackIncomingWebhook(ctx, dest.SlackWebhook.URL, messageForUser, slackContent)
		result := services.ConnectorResult{
			Channel: "slack_webhook",
			OK:      sendErr == nil,
			MsgID:   msgID,
		}
		if sendErr != nil {
			result.Err = sendErr.Error()
		}
		results = append(results, result)
	}

	delivered := []string{}
	skipped := []string{}
	failed := map[string]string{}
	for _, r := range results {
		switch {
		case !r.OK:
			failed[r.Channel] = r.Err
		case r.MsgID == "":
			skipped = append(skipped, r.Channel) // connector had no destination for this recipient
		default:
			delivered = append(delivered, r.Channel)
		}
	}

	var status string
	switch {
	case len(results) == 0:
		status = "no_channels_configured" // nothing connected; not delivered anywhere
	case len(delivered) == 0 && len(failed) == 0:
		status = "no_recipient" // all connectors skipped (no destination resolved)
	case len(delivered) == 0:
		status = "failed"
	case len(failed) > 0:
		status = "partial"
	default:
		status = "delivered"
	}

	result := map[string]interface{}{
		"status":    status,
		"delivered": delivered,
		"skipped":   skipped,
		"failed":    failed,
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func slackContentFromArgs(args map[string]interface{}) (services.SlackWebhookContent, error) {
	content := services.SlackWebhookContent{}
	content.Title, _ = args["slack_title"].(string)
	content.Color, _ = args["slack_color"].(string)
	content.Footer, _ = args["slack_footer"].(string)

	if rawFields, ok := args["slack_fields"].([]interface{}); ok {
		for i, raw := range rawFields {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				return content, fmt.Errorf("slack_fields[%d] must be an object", i)
			}
			label, _ := entry["label"].(string)
			value, _ := entry["value"].(string)
			content.Fields = append(content.Fields, services.SlackWebhookField{Label: label, Value: value})
		}
	}
	if rawSections, ok := args["slack_sections"].([]interface{}); ok {
		for i, raw := range rawSections {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				return content, fmt.Errorf("slack_sections[%d] must be an object", i)
			}
			heading, _ := entry["heading"].(string)
			body, _ := entry["body"].(string)
			content.Sections = append(content.Sections, services.SlackWebhookSection{Heading: heading, Body: body})
		}
	}
	return content, nil
}

// handleHumanFeedback handles the human_feedback tool execution
func handleHumanFeedback(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters - message_for_user is optional, use default if missing
	messageForUser, ok := args["message_for_user"].(string)
	if !ok || messageForUser == "" {
		messageForUser = "Please provide your feedback here..."
	}

	uniqueID, ok := args["unique_id"].(string)
	uniqueID = strings.TrimSpace(uniqueID)
	if !ok || uniqueID == "" {
		return "", fmt.Errorf("unique_id is required and must be a string")
	}
	waitTimeout := humanFeedbackTimeoutFromArgs(args)

	// Extract optional options array
	var options []string
	if optionsRaw, ok := args["options"].([]interface{}); ok {
		for _, opt := range optionsRaw {
			if s, ok := opt.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}

	// Get global feedback store
	feedbackStore := GetHumanFeedbackStore()

	// Register the request before emitting UI/notification events so an immediate
	// Electron response can never race the store registration.
	expiryContext := fmt.Sprintf("This request expires in %d seconds.", int(waitTimeout/time.Second))
	sessionID, _ := ctx.Value(BGAgentSessionIDKey).(string)
	if err := feedbackStore.CreatePendingRequest(
		uniqueID,
		messageForUser,
		expiryContext,
		sessionID,
		options,
		len(options) == 0,
		waitTimeout,
	); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Emit blocking_human_feedback so the frontend renders a direct response UI.
	// The expiry is informational here; the store owns the authoritative timer.
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		hasOptions := len(options) > 0
		emitter.EmitBlockingHumanFeedback(uniqueID, messageForUser, expiryContext, hasOptions, "", "", options...)
	}

	// Wait only for the bounded duration selected by the agent.
	response, err := feedbackStore.WaitForResponse(uniqueID, waitTimeout)
	if err != nil {
		return "", fmt.Errorf("human feedback request %s expired after %s: %w", uniqueID, waitTimeout, err)
	}

	return response, nil
}

const (
	defaultHumanFeedbackTimeout = 5 * time.Minute
	minHumanFeedbackTimeout     = 30 * time.Second
	maxHumanFeedbackTimeout     = 30 * time.Minute
)

func humanFeedbackTimeoutFromArgs(args map[string]interface{}) time.Duration {
	raw, ok := args["timeout_seconds"]
	if !ok {
		return defaultHumanFeedbackTimeout
	}

	var seconds int64
	switch value := raw.(type) {
	case int:
		seconds = int64(value)
	case int32:
		seconds = int64(value)
	case int64:
		seconds = value
	case float32:
		seconds = int64(value)
	case float64:
		seconds = int64(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return defaultHumanFeedbackTimeout
		}
		seconds = parsed
	default:
		return defaultHumanFeedbackTimeout
	}

	minSeconds := int64(minHumanFeedbackTimeout / time.Second)
	maxSeconds := int64(maxHumanFeedbackTimeout / time.Second)
	if seconds < minSeconds {
		return minHumanFeedbackTimeout
	}
	if seconds > maxSeconds {
		return maxHumanFeedbackTimeout
	}
	return time.Duration(seconds) * time.Second
}

// NotificationDestinationFromContext returns the best notification destination
// hint available for the current tool execution context.
func NotificationDestinationFromContext(ctx context.Context) *services.NotificationDestination {
	var dest *services.NotificationDestination
	if explicit, ok := ctx.Value(BotNotificationDestinationKey).(*services.NotificationDestination); ok && explicit != nil {
		dest = cloneNotificationDestination(explicit)
	}
	// Coding-agent tools execute through a separate HTTP request context. The
	// bridge preserves the trusted session ID, but not arbitrary values from the
	// original agent context, so resolve the latest session destination here.
	sessionID, _ := ctx.Value(common.ChatSessionIDKey).(string)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = mcpexecutor.SessionIDFromContext(ctx)
	}
	if current := sessionNotificationDestination(sessionID); current != nil {
		dest = current
	}
	if uid, ok := ctx.Value(common.UserIDKey).(string); ok && uid != "" {
		if dest == nil {
			dest = &services.NotificationDestination{}
		}
		if dest.UserID == "" {
			dest.UserID = uid
		}
	}
	if notificationDestinationEmpty(dest) {
		return nil
	}
	return dest
}

func cloneNotificationDestination(dest *services.NotificationDestination) *services.NotificationDestination {
	if dest == nil {
		return nil
	}
	clone := &services.NotificationDestination{UserID: dest.UserID}
	if dest.Slack != nil {
		clone.Slack = &services.SlackDest{
			ChannelID: dest.Slack.ChannelID,
			ThreadTS:  dest.Slack.ThreadTS,
		}
	}
	if dest.SlackWebhook != nil {
		clone.SlackWebhook = &services.SlackWebhookDest{
			SecretName: dest.SlackWebhook.SecretName,
			URL:        dest.SlackWebhook.URL,
		}
	}
	if dest.WhatsApp != nil {
		clone.WhatsApp = &services.WhatsAppDest{
			ChannelID: dest.WhatsApp.ChannelID,
			PhoneE164: dest.WhatsApp.PhoneE164,
		}
	}
	if dest.Gmail != nil {
		clone.Gmail = &services.GmailDest{
			Email: dest.Gmail.Email,
		}
	}
	// Content is treated as read-only by connectors, so sharing the pointer is
	// safe and avoids a deep copy of attachment lists.
	clone.Content = dest.Content
	return clone
}

func notificationDestinationEmpty(dest *services.NotificationDestination) bool {
	return dest == nil ||
		(dest.UserID == "" &&
			(dest.Slack == nil || dest.Slack.ChannelID == "") &&
			(dest.SlackWebhook == nil || (dest.SlackWebhook.SecretName == "" && dest.SlackWebhook.URL == "")) &&
			(dest.WhatsApp == nil || (dest.WhatsApp.ChannelID == "" && dest.WhatsApp.PhoneE164 == "")) &&
			(dest.Gmail == nil || dest.Gmail.Email == ""))
}
