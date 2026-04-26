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

// GetToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human"
}

// CreateHumanToolExecutors creates the execution functions for human tools
func CreateHumanToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["human_feedback"] = handleHumanFeedback
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

	// Build a destination hint so connector resolvers can consult per-user
	// preferences. The userID is set on the context by the server when it
	// dispatches an agent or workflow run. Origin auto-detection (e.g.
	// route back to the originating Slack thread) is a follow-up — for now
	// we only carry the userID, which is enough for per-user prefs.
	var dest *services.NotificationDestination
	if uid, ok := ctx.Value(common.UserIDKey).(string); ok && uid != "" {
		dest = &services.NotificationDestination{UserID: uid}
	}

	// Create feedback request (automatically sends notifications via notification manager)
	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, messageForUser, "", buttonOptions, dest); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}
