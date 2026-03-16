package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"

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

	// Add human_questions tool
	humanQuestionsTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "human_questions",
			Description: "Ask the user multiple specific questions and get individual answers for each. Use this when you need structured, targeted feedback on several different topics at once. Each question gets its own text input field. The user can also provide optional general feedback. Returns a JSON object with answers keyed by question ID and optional general_feedback.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"unique_id": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for this questions request. Always generate a UUID.",
					},
					"questions": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":        "string",
									"description": "Unique identifier for this question (e.g., 'q1', 'q2').",
								},
								"question": map[string]interface{}{
									"type":        "string",
									"description": "The question text to display to the user.",
								},
							},
							"required": []string{"id", "question"},
						},
						"minItems":    2,
						"maxItems":    8,
						"description": "Array of 2-8 questions to ask the user. Each question has an id and question text.",
					},
				},
				"required": []string{"unique_id", "questions"},
			}),
		},
	}
	humanTools = append(humanTools, humanQuestionsTool)

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
	executors["human_questions"] = handleHumanQuestions

	return executors
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

	// Create feedback request (automatically sends notifications via notification manager)
	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, messageForUser, "", buttonOptions); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}

// handleHumanQuestions handles the human_questions tool execution
func handleHumanQuestions(ctx context.Context, args map[string]interface{}) (string, error) {
	uniqueID, ok := args["unique_id"].(string)
	if !ok || uniqueID == "" {
		return "", fmt.Errorf("unique_id is required and must be a string")
	}

	// Extract questions array
	questionsRaw, ok := args["questions"].([]interface{})
	if !ok || len(questionsRaw) < 2 {
		return "", fmt.Errorf("questions is required and must contain at least 2 items")
	}
	if len(questionsRaw) > 8 {
		return "", fmt.Errorf("questions must contain at most 8 items")
	}

	var questions []map[string]string
	for _, qRaw := range questionsRaw {
		qMap, ok := qRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each question must be an object with id and question fields")
		}
		id, _ := qMap["id"].(string)
		question, _ := qMap["question"].(string)
		if id == "" || question == "" {
			return "", fmt.Errorf("each question must have non-empty id and question fields")
		}
		questions = append(questions, map[string]string{"id": id, "question": question})
	}

	// Emit blocking_human_questions event so the frontend renders the structured questions UI
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		emitter.EmitBlockingHumanQuestions(uniqueID, questions)
	}

	// Build notification message for Slack (summarize all questions)
	notificationMsg := "Please answer the following questions:\n"
	for i, q := range questions {
		notificationMsg += fmt.Sprintf("%d. %s\n", i+1, q["question"])
	}

	// Get global feedback store — reuse same mechanism, response will be JSON
	feedbackStore := GetHumanFeedbackStore()

	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, notificationMsg, "", nil); err != nil {
		return "", fmt.Errorf("failed to create questions request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user answers: %w", err)
	}

	// Validate that response is valid JSON (frontend sends structured JSON)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		// If not JSON, wrap it as general feedback
		result := map[string]interface{}{
			"answers":          map[string]string{},
			"general_feedback": response,
		}
		jsonBytes, _ := json.Marshal(result)
		return string(jsonBytes), nil
	}

	return response, nil
}

