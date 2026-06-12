package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/common"

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
			Description: "Use this tool when you need to get human input, confirmation, or feedback. This tool will pause execution until the user provides input via the UI. You can present multiple options as buttons for the user to choose from, or use free-text input. The tool returns the user's response as text. Ideal for asking clarifying questions, presenting choices, requesting confirmation, or any situation requiring human decision-making.",
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
			"description": "Message to send to the user",
		},
	}
	if gmailEnabled() {
		notifyProps["email_subject"] = map[string]interface{}{
			"type":        "string",
			"description": "Optional. Custom subject line for the email rendering (Gmail). Other channels ignore this.",
		}
		notifyProps["email_body"] = map[string]interface{}{
			"type":        "string",
			"description": "Optional. Email-specific body, which can be longer/richer than message_for_user. If omitted, message_for_user is used as the email body.",
		}
		notifyProps["email_attachments"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional. Absolute file paths on the server host to attach to the email (Gmail only).",
		}
	}
	notifyUserTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "notify_via_bot",
			Description: buildNotifyDescription(),
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": notifyProps,
				"required":   []string{"message_for_user"},
			}),
		},
	}
	humanTools = append(humanTools, notifyUserTool)

	// submit_human_answer — used by a builder agent to resolve a human_input
	// step that was routed into this chat session (via run_workflow). The chat
	// message from the workflow will include the request_id to pass here.
	submitHumanAnswerTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "submit_human_answer",
			Description: "Resolve a pending workflow decision. Use this ONLY in response to a [WORKFLOW_HUMAN_INPUT], [WORKFLOW_HUMAN_FEEDBACK], or [WORKFLOW_ROUTING] message from a workflow you launched. Pass the request_id from that message and the answer. For human_input yes/no steps, answer with 'yes' or 'no'. For multiple-choice, answer with 'option0', 'option1', ... (or the exact option text). For text or human_feedback prompts, pass the user's free-text answer. For routing steps, answer with the exact route_id (or route name) from the message. The workflow resumes as soon as you call this.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"request_id": map[string]interface{}{
						"type":        "string",
						"description": "The request_id from the [WORKFLOW_HUMAN_INPUT] message (e.g. 'human_input_step_2_1714171234567').",
					},
					"answer": map[string]interface{}{
						"type":        "string",
						"description": "The answer to submit. Format depends on the response type given in the workflow message.",
					},
				},
				"required": []string{"request_id", "answer"},
			}),
		},
	}
	humanTools = append(humanTools, submitHumanAnswerTool)

	return humanTools
}

// channelLabels maps connector Name() values to human-friendly labels used in
// the dynamic notify_via_bot description.
var channelLabels = map[string]string{
	"slack":    "Slack",
	"whatsapp": "WhatsApp",
	"gmail":    "Gmail (email)",
}

// buildNotifyDescription renders the notify_via_bot description with the set of
// channels enabled when the tool list is built (per session/run), so the agent
// knows where its message will actually land. The always-on web UI connector is
// not framed as an external channel.
func buildNotifyDescription() string {
	base := "Send a non-blocking notification to the human. Use this for FYIs, progress updates, alerts, and completion notices when you do not need to wait for a reply. If you need the human to answer before continuing, use human_feedback instead."

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
		return base + " NOTE: No external channels (Slack/WhatsApp/Gmail) are currently enabled, so the message only appears in the web UI."
	}
	desc := base + " Currently enabled delivery channels: " + strings.Join(labels, ", ") + ". The message is delivered to all enabled channels — you do not choose which."
	if gmailOn {
		desc += " Gmail is enabled, so you may optionally set email_subject, email_body, and email_attachments for the email rendering (other channels ignore these)."
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

// gmailContentFromArgs builds the per-channel Gmail content from notify_via_bot
// tool args, or nil if no email-specific fields were provided.
func gmailContentFromArgs(args map[string]interface{}) *services.GmailContent {
	subject, _ := args["email_subject"].(string)
	body, _ := args["email_body"].(string)
	var attachments []string
	if raw, ok := args["email_attachments"].([]interface{}); ok {
		for _, a := range raw {
			if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
				attachments = append(attachments, strings.TrimSpace(s))
			}
		}
	}
	if strings.TrimSpace(subject) == "" && strings.TrimSpace(body) == "" && len(attachments) == 0 {
		return nil
	}
	return &services.GmailContent{
		Subject:     strings.TrimSpace(subject),
		Body:        body,
		Attachments: attachments,
	}
}

// GetToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human"
}

// CreateHumanToolExecutors creates the execution functions for human tools
func CreateHumanToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["human_feedback"] = handleHumanFeedback
	executors["notify_via_bot"] = handleNotifyUser
	executors["submit_human_answer"] = handleSubmitHumanAnswer

	return executors
}

// handleSubmitHumanAnswer resolves a pending workflow human_input step by
// forwarding the answer to the HumanFeedbackStore, unblocking the workflow
// goroutine that's parked on WaitForResponse.
func handleSubmitHumanAnswer(ctx context.Context, args map[string]interface{}) (string, error) {
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		return "", fmt.Errorf("request_id is required")
	}
	answer, _ := args["answer"].(string)
	// Note: empty answer is allowed (e.g., "Approve" with no text for text-type steps).

	feedbackStore := GetHumanFeedbackStore()
	if err := feedbackStore.SubmitResponse(requestID, answer); err != nil {
		return "", fmt.Errorf("failed to submit answer: %w", err)
	}
	result := map[string]interface{}{
		"status":     "submitted",
		"request_id": requestID,
	}
	b, _ := json.Marshal(result)
	return string(b), nil
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
	if gc := gmailContentFromArgs(args); gc != nil {
		if dest == nil {
			dest = &services.NotificationDestination{}
		}
		dest.Content = &services.NotificationContent{Gmail: gc}
	}

	if err := notificationManager.SendUserNotification(ctx, messageForUser, "", dest); err != nil {
		return "", fmt.Errorf("failed to notify user: %w", err)
	}

	result := map[string]interface{}{
		"status": "sent",
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// handleHumanFeedback handles the human_feedback tool execution
func handleHumanFeedback(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters - message_for_user is optional, use default if missing
	messageForUser, ok := args["message_for_user"].(string)
	if !ok || messageForUser == "" {
		messageForUser = "Please provide your feedback here..."
	}

	uniqueID, ok := args["unique_id"].(string)
	if !ok {
		return "", fmt.Errorf("unique_id is required and must be a string")
	}

	// Extract optional options array
	var options []string
	if optionsRaw, ok := args["options"].([]interface{}); ok {
		for _, opt := range optionsRaw {
			if s, ok := opt.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}

	// Emit blocking_human_feedback event so the frontend renders the proper UI
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		hasOptions := len(options) > 0
		emitter.EmitBlockingHumanFeedback(uniqueID, messageForUser, "", hasOptions, "", "", options...)
	}

	// Build button options for notifications (Slack, etc.)
	var buttonOptions *services.ButtonOptions
	if len(options) > 0 {
		buttonOptions = &services.ButtonOptions{
			Options: options,
		}
	}

	// Get global feedback store
	feedbackStore := GetHumanFeedbackStore()

	sessionID, _ := ctx.Value(BGAgentSessionIDKey).(string)
	if sessionID != "" {
		if pc := GetParentChat(sessionID); pc != nil && pc.SessionID != "" && HasChatInjector() {
			if err := feedbackStore.CreateRequestWithoutNotification(uniqueID, messageForUser); err != nil {
				return "", fmt.Errorf("failed to create feedback request: %w", err)
			}

			var msg strings.Builder
			msg.WriteString("[WORKFLOW_HUMAN_FEEDBACK] The workflow you launched is waiting on a human_feedback tool call. ")
			msg.WriteString("If you already know the answer from the conversation so far, answer directly by calling submit_human_answer. ")
			msg.WriteString("Otherwise, ask the user for what you need, then submit their reply.\n\n")
			if pc.WorkflowPath != "" {
				msg.WriteString(fmt.Sprintf("Workflow: %s\n", pc.WorkflowPath))
			}
			if pc.GroupName != "" {
				msg.WriteString(fmt.Sprintf("Group: %s\n", pc.GroupName))
			}
			msg.WriteString(fmt.Sprintf("Request ID: %s\n", uniqueID))
			msg.WriteString(fmt.Sprintf("Question: %s\n", messageForUser))
			if len(options) > 0 {
				msg.WriteString("Options:\n")
				for i, opt := range options {
					msg.WriteString(fmt.Sprintf("  %d. %s\n", i, opt))
				}
				msg.WriteString("Submit the user's choice as the exact option text.\n")
			} else {
				msg.WriteString("Submit the user's free-text answer as the answer.\n")
			}

			if err := InjectChatMessage(ctx, pc.SessionID, pc.UserID, msg.String()); err != nil {
				return "", fmt.Errorf("failed to inject feedback into parent chat: %w", err)
			}

			response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
			if err != nil {
				return "", fmt.Errorf("failed to get user feedback: %w", err)
			}
			return response, nil
		}
	}

	dest := NotificationDestinationFromContext(ctx)

	// Create feedback request (automatically sends notifications via notification manager)
	if err := feedbackStore.CreateRequestWithNotification(ctx, uniqueID, messageForUser, "", buttonOptions, dest); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}

// NotificationDestinationFromContext returns the best notification destination
// hint available for the current tool execution context.
func NotificationDestinationFromContext(ctx context.Context) *services.NotificationDestination {
	var dest *services.NotificationDestination
	if explicit, ok := ctx.Value(BotNotificationDestinationKey).(*services.NotificationDestination); ok && explicit != nil {
		dest = cloneNotificationDestination(explicit)
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

func ScheduleHumanFeedbackNotification(ctx context.Context, requestID, message, contextMsg string, buttonOptions *services.ButtonOptions) {
	GetHumanFeedbackStore().ScheduleNotification(ctx, requestID, message, contextMsg, buttonOptions, NotificationDestinationFromContext(ctx))
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
			(dest.WhatsApp == nil || (dest.WhatsApp.ChannelID == "" && dest.WhatsApp.PhoneE164 == "")) &&
			(dest.Gmail == nil || dest.Gmail.Email == ""))
}
