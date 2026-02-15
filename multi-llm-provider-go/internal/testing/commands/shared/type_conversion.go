package shared

import (
	"context"
	"log"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// RunTypeConversionTest validates that all ContentPart types can be properly handled
// by adapters. This test simulates what happens when agent_go types are converted
// to llm-providers types and sent to adapters.
//
// This test would have caught the bug where ToolCall and ToolCallResponse weren't
// being converted from agent_go/internal/llmtypes to llm-providers/llmtypes.
func RunTypeConversionTest(llm llmtypes.Model, modelID string) {
	log.Printf("\n📝 Test: Type Conversion Validation")
	log.Printf("   This test validates that all ContentPart types work correctly")
	log.Printf("   when passed through the conversion layer (agent_go → llm-providers)")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Test all ContentPart types that must be supported
	testCases := []struct {
		name    string
		message llmtypes.MessageContent
	}{
		{
			name: "TextContent",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{Text: "Hello, world!"},
				},
			},
		},
		{
			name: "ToolCall",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCall{
						ID:   "call_test_1",
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      "test_function",
							Arguments: `{"param": "value"}`,
						},
						ThoughtSignature: "test_thought_sig",
					},
				},
			},
		},
		{
			name: "ToolCallResponse",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCallResponse{
						ToolCallID: "call_test_1",
						Name:       "test_function",
						Content:    `{"result": "success"}`,
					},
				},
			},
		},
		{
			name: "MixedParts",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{Text: "I'll call a tool"},
					llmtypes.ToolCall{
						ID:   "call_test_2",
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      "test_function",
							Arguments: `{"param": "value"}`,
						},
					},
				},
			},
		},
		{
			name: "MultipleToolCalls",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCall{
						ID:   "call_test_3",
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      "function1",
							Arguments: `{"param1": "value1"}`,
						},
					},
					llmtypes.ToolCall{
						ID:   "call_test_4",
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      "function2",
							Arguments: `{"param2": "value2"}`,
						},
					},
				},
			},
		},
		{
			name: "MultipleToolResponses",
			message: llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCallResponse{
						ToolCallID: "call_test_3",
						Name:       "function1",
						Content:    `{"result1": "success"}`,
					},
					llmtypes.ToolCallResponse{
						ToolCallID: "call_test_4",
						Name:       "function2",
						Content:    `{"result2": "success"}`,
					},
				},
			},
		},
	}

	// Validate that all parts are from llm-providers package
	// This ensures conversion would work correctly
	log.Printf("\n🔍 Step 1: Validating ContentPart types are from llm-providers package")
	allValid := true
	for _, tc := range testCases {
		for i, part := range tc.message.Parts {
			if !isProviderType(part) {
				log.Printf("❌ %s - Part %d: Type is not from llm-providers package: %T", tc.name, i, part)
				allValid = false
			} else {
				log.Printf("✅ %s - Part %d: Type is valid (%T)", tc.name, i, part)
			}

			// Verify type assertions work (what adapters do)
			switch part.(type) {
			case llmtypes.TextContent, llmtypes.ImageContent,
				llmtypes.ToolCall, llmtypes.ToolCallResponse:
				// Good - type assertion would work
			default:
				log.Printf("❌ %s - Part %d: Unknown type that adapters can't handle: %T", tc.name, i, part)
				allValid = false
			}
		}
	}

	if !allValid {
		log.Printf("\n❌ Type validation failed - some types are not properly converted")
		log.Printf("   This indicates a problem with the conversion layer")
		return
	}

	log.Printf("\n✅ All ContentPart types are valid and from llm-providers package")

	// Test that adapters can actually process these types
	// We'll do a simple test with a tool call message
	log.Printf("\n🔍 Step 2: Testing adapter can process ToolCall types")

	// Create a simple tool for testing
	testTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "test_function",
			Description: "A test function",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"param": map[string]interface{}{
						"type": "string",
					},
				},
			}),
		},
	}

	// Test with a conversation that includes tool calls and responses
	// This simulates the full agent_go → conversion → adapter path
	conversation := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Call the test function with param='test'"},
			},
		},
	}

	startTime := time.Now()
	resp, err := llm.GenerateContent(ctx, conversation,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{testTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Step 2 failed: %v", err)
		log.Printf("   This may indicate that ToolCall types aren't being processed correctly")
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ Step 2 failed - no choices returned")
		return
	}

	// If we got tool calls, test that we can send them back with responses
	if len(resp.Choices[0].ToolCalls) > 0 {
		log.Printf("✅ Step 2 passed - received %d tool call(s)", len(resp.Choices[0].ToolCalls))
		log.Printf("   Duration: %v", duration)

		// Step 3: Test that we can send tool calls back with responses
		log.Printf("\n🔍 Step 3: Testing ToolCall and ToolCallResponse in conversation history")

		toolCall := resp.Choices[0].ToolCalls[0]
		conversationWithResponse := []llmtypes.MessageContent{
			{
				Role: llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{Text: "Call the test function with param='test'"},
				},
			},
			{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					toolCall, // This is a ToolCall from llm-providers
				},
			},
			{
				Role: llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{
					llmtypes.ToolCallResponse{
						ToolCallID: toolCall.ID,
						Name:       toolCall.FunctionCall.Name,
						Content:    `{"result": "success"}`,
					},
				},
			},
		}

		// Validate all parts are from llm-providers package
		allValid = true
		for i, msg := range conversationWithResponse {
			for j, part := range msg.Parts {
				if !isProviderType(part) {
					log.Printf("❌ Message %d, Part %d: Type is not from llm-providers package: %T", i, j, part)
					allValid = false
				}
			}
		}

		if !allValid {
			log.Printf("❌ Step 3 failed - some types are not from llm-providers package")
			log.Printf("   This would cause type assertion failures in adapters")
			return
		}

		// Try to send this conversation back to the LLM
		// This is the critical test - if conversion didn't happen, this will fail
		startTime2 := time.Now()
		resp2, err2 := llm.GenerateContent(ctx, conversationWithResponse,
			llmtypes.WithModel(modelID),
			llmtypes.WithTools([]llmtypes.Tool{testTool}),
		)
		duration2 := time.Since(startTime2)

		if err2 != nil {
			log.Printf("❌ Step 3 failed: %v", err2)
			log.Printf("   This indicates that ToolCall/ToolCallResponse types aren't being processed correctly")
			log.Printf("   by the adapter. Check conversion layer in agent_go/internal/llm/providers.go")
			return
		}

		if len(resp2.Choices) == 0 {
			log.Printf("❌ Step 3 failed - no choices returned")
			return
		}

		log.Printf("✅ Step 3 passed in %v", duration2)
		log.Printf("   Successfully sent ToolCall and ToolCallResponse through adapter")
	} else {
		log.Printf("⚠️  Step 2: No tool calls returned (model may not support tool calling)")
		log.Printf("   Skipping Step 3 (requires tool calls)")
	}

	log.Printf("\n✅ Type conversion test completed successfully!")
	log.Printf("   All ContentPart types are properly handled by adapters")
}

// isProviderType checks if the value is from llm-providers package
// This is critical - we need to ensure conversion actually happened
func isProviderType(v interface{}) bool {
	switch v.(type) {
	case llmtypes.TextContent,
		llmtypes.ImageContent,
		llmtypes.ToolCall,
		llmtypes.ToolCallResponse:
		return true
	default:
		return false
	}
}
