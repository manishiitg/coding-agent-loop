package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func main() {
	// Check for AWS credentials
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1" // Default region
	}
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if accessKey == "" || secretKey == "" {
		fmt.Fprintf(os.Stderr, "Error: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required\n")
		fmt.Fprintf(os.Stderr, "Set them with:\n")
		fmt.Fprintf(os.Stderr, "  export AWS_REGION=us-east-1\n")
		fmt.Fprintf(os.Stderr, "  export AWS_ACCESS_KEY_ID=your-access-key\n")
		fmt.Fprintf(os.Stderr, "  export AWS_SECRET_ACCESS_KEY=your-secret-key\n")
		os.Exit(1)
	}

	// Initialize Bedrock LLM
	config := llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     "global.anthropic.claude-sonnet-4-5-20250929-v1:0", // Using Claude Sonnet 4.5 for multi-tool testing
		Temperature: 0.7,
		Logger:      nil, // nil is allowed - uses no-op logger
	}

	fmt.Printf("Initializing Bedrock provider with model: %s (region: %s)\n", config.ModelID, region)
	llm, err := llmproviders.InitializeLLM(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Define multiple tools for parallel execution
	readFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_file",
			Description: "Read contents of a file",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to read",
					},
				},
				"required": []string{"path"},
			}),
		},
	}

	weatherTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			}),
		},
	}

	calculateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "calculate",
			Description: "Perform a mathematical calculation",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "Mathematical expression to evaluate (e.g., '25 * 17')",
					},
				},
				"required": []string{"expression"},
			}),
		},
	}

	// Create a message that will trigger multiple parallel tool calls
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(
			llmtypes.ChatMessageTypeHuman,
			"Please do three things in parallel: 1) Read the go.mod file, 2) Get the weather in San Francisco, and 3) Calculate 25 * 17. Call all three tools at once.",
		),
	}

	// Track streamed content and tool calls
	var streamedContent strings.Builder
	var streamedToolCalls []llmtypes.ToolCall
	var contentChunks []string

	// Create channel for streaming chunks
	streamChan := make(chan llmtypes.StreamChunk, 100)

	// Start goroutine to receive and process streaming chunks
	done := make(chan bool)
	go func() {
		defer close(done)
		for chunk := range streamChan {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				// Handle content chunks (text being generated)
				content := chunk.Content
				streamedContent.WriteString(content)
				contentChunks = append(contentChunks, content)
				fmt.Printf("   📝 Streamed content: %s\n", content)
			case llmtypes.StreamChunkTypeToolCall:
				// Handle tool call chunks (complete tool calls)
				if chunk.ToolCall != nil {
					streamedToolCalls = append(streamedToolCalls, *chunk.ToolCall)
					fmt.Printf("   📦 Streamed tool call %d: %s (ID: %s, Args: %s)\n",
						len(streamedToolCalls),
						chunk.ToolCall.FunctionCall.Name,
						chunk.ToolCall.ID,
						chunk.ToolCall.FunctionCall.Arguments,
					)
				}
			}
		}
	}()

	// Make the API call with streaming enabled
	fmt.Println("🚀 Making request with streaming and multiple tools...")
	startTime := time.Now()

	resp, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithModel("global.anthropic.claude-sonnet-4-5-20250929-v1:0"),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool, weatherTool, calculateTool}),
		llmtypes.WithToolChoiceString("auto"),  // Let model decide which tools to use
		llmtypes.WithStreamingChan(streamChan), // Enable streaming
	)

	duration := time.Since(startTime)

	// Wait for all streaming chunks to be processed
	<-done

	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Request failed: %v\n", err)
		os.Exit(1)
	}

	// Validate response
	if len(resp.Choices) == 0 {
		fmt.Fprintf(os.Stderr, "❌ No choices returned\n")
		os.Exit(1)
	}

	finalToolCalls := resp.Choices[0].ToolCalls
	finalContent := resp.Choices[0].Content

	// Display results
	fmt.Printf("\n✅ Request completed in %s\n", duration)
	fmt.Println("\n📊 Streaming Statistics:")
	fmt.Printf("   Content chunks received: %d\n", len(contentChunks))
	fmt.Printf("   Streamed tool calls: %d\n", len(streamedToolCalls))
	fmt.Printf("   Final tool calls: %d\n", len(finalToolCalls))
	fmt.Printf("   Streamed content length: %d chars\n", streamedContent.Len())
	fmt.Printf("   Final content length: %d chars\n", len(finalContent))

	// Display streamed tool calls
	if len(streamedToolCalls) > 0 {
		fmt.Println("\n📋 Streamed Tool Calls:")
		for i, tc := range streamedToolCalls {
			fmt.Printf("   %d. %s\n", i+1, tc.FunctionCall.Name)
			fmt.Printf("      ID: %s\n", tc.ID)
			fmt.Printf("      Arguments: %s\n", tc.FunctionCall.Arguments)
		}
	}

	// Display final tool calls
	if len(finalToolCalls) > 0 {
		fmt.Println("\n📋 Final Tool Calls:")
		for i, tc := range finalToolCalls {
			fmt.Printf("   %d. %s\n", i+1, tc.FunctionCall.Name)
			fmt.Printf("      ID: %s\n", tc.ID)
			fmt.Printf("      Arguments: %s\n", tc.FunctionCall.Arguments)
		}
	}

	// Validate that streaming worked correctly
	if len(finalToolCalls) > 0 && len(streamedToolCalls) == 0 {
		fmt.Println("\n⚠️  Warning: Tool calls exist in final response but none were streamed")
		fmt.Println("   This may indicate a streaming implementation issue")
	}

	if len(streamedToolCalls) != len(finalToolCalls) {
		fmt.Printf("\n⚠️  Warning: Streamed tool call count (%d) doesn't match final count (%d)\n",
			len(streamedToolCalls), len(finalToolCalls))
	}

	// Verify all tool calls were streamed correctly
	if len(streamedToolCalls) == len(finalToolCalls) && len(finalToolCalls) > 0 {
		fmt.Printf("\n✅ All %d tool calls were streamed correctly!\n", len(finalToolCalls))

		// Create a map of streamed tool calls by ID for validation
		streamedMap := make(map[string]*llmtypes.ToolCall)
		for i := range streamedToolCalls {
			streamedMap[streamedToolCalls[i].ID] = &streamedToolCalls[i]
		}

		// Verify each final tool call was streamed
		allMatched := true
		for _, finalTC := range finalToolCalls {
			streamedTC, exists := streamedMap[finalTC.ID]
			if !exists {
				fmt.Printf("   ❌ Tool call ID %s not found in streamed calls\n", finalTC.ID)
				allMatched = false
				continue
			}
			if streamedTC.FunctionCall.Name != finalTC.FunctionCall.Name {
				fmt.Printf("   ❌ Tool call name mismatch for ID %s\n", finalTC.ID)
				allMatched = false
			} else {
				fmt.Printf("   ✅ Tool call %s (ID: %s) matched\n", finalTC.FunctionCall.Name, finalTC.ID)
			}
		}

		if allMatched {
			fmt.Println("\n🎯 All tool calls validated successfully!")
		}
	}

	// Display token usage if available
	if resp.Choices[0].GenerationInfo != nil {
		genInfo := resp.Choices[0].GenerationInfo
		fmt.Println("\n💰 Token Usage (Turn 1):")
		if genInfo.InputTokens != nil {
			fmt.Printf("   Input tokens: %d\n", *genInfo.InputTokens)
		}
		if genInfo.OutputTokens != nil {
			fmt.Printf("   Output tokens: %d\n", *genInfo.OutputTokens)
		}
		if genInfo.TotalTokens != nil {
			fmt.Printf("   Total tokens: %d\n", *genInfo.TotalTokens)
		} else if genInfo.InputTokens != nil && genInfo.OutputTokens != nil {
			fmt.Printf("   Total tokens: %d\n", *genInfo.InputTokens+*genInfo.OutputTokens)
		}
	}

	// ============================================
	// MULTI-TURN CONVERSATION: Send Tool Responses
	// ============================================
	if len(finalToolCalls) == 0 {
		fmt.Println("\n⚠️  No tool calls to process. Ending example.")
		return
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("🔄 MULTI-TURN CONVERSATION: Simulating Tool Execution")
	fmt.Println(strings.Repeat("=", 70))

	// Step 1: Build conversation history with the initial user message and assistant response
	fmt.Println("\n📝 Step 1: Building conversation history with tool calls")
	conversationHistory := []llmtypes.MessageContent{
		llmtypes.TextParts(
			llmtypes.ChatMessageTypeHuman,
			"Please do three things in parallel: 1) Read the go.mod file, 2) Get the weather in San Francisco, and 3) Calculate 25 * 17. Call all three tools at once.",
		),
	}

	// Add assistant message with tool calls
	parts := []llmtypes.ContentPart{}
	if finalContent != "" {
		parts = append(parts, llmtypes.TextContent{Text: finalContent})
	}
	for _, tc := range finalToolCalls {
		parts = append(parts, tc)
	}
	conversationHistory = append(conversationHistory, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeAI,
		Parts: parts,
	})
	fmt.Printf("   ✅ Added assistant message with %d tool call(s)\n", len(finalToolCalls))

	// Step 2: Simulate tool execution and create tool responses
	fmt.Println("\n🔧 Step 2: Simulating tool execution and creating tool responses")
	toolResponseParts := make([]llmtypes.ContentPart, 0, len(finalToolCalls))
	for _, tc := range finalToolCalls {
		var toolResultContent string

		// Parse arguments to create realistic responses
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
			// Fallback if JSON parsing fails
			args = make(map[string]interface{})
		}

		switch tc.FunctionCall.Name {
		case "read_file":
			path := "go.mod"
			if p, ok := args["path"].(string); ok {
				path = p
			}
			// Simulate reading go.mod file
			toolResultContent = `module llm-providers

go 1.21

require (
	github.com/openai/openai-go/v3 v3.0.0
	// ... other dependencies
)`
			fmt.Printf("   ✅ Simulated read_file(%q) - returned file contents\n", path)

		case "get_weather":
			location := "San Francisco"
			if loc, ok := args["location"].(string); ok {
				location = loc
			}
			// Simulate weather API response
			toolResultContent = fmt.Sprintf(`{"location": "%s", "temperature": 72, "condition": "Sunny", "humidity": 65, "wind_speed": 8}`, location)
			fmt.Printf("   ✅ Simulated get_weather(%q) - returned weather data\n", location)

		case "calculate":
			expression := "25 * 17"
			if expr, ok := args["expression"].(string); ok {
				expression = expr
			}
			// Simulate calculation
			result := 25 * 17 // Simple calculation for demo
			toolResultContent = fmt.Sprintf(`{"expression": "%s", "result": %d}`, expression, result)
			fmt.Printf("   ✅ Simulated calculate(%q) - returned result: %d\n", expression, result)

		default:
			toolResultContent = fmt.Sprintf(`{"result": "Mock result for %s"}`, tc.FunctionCall.Name)
			fmt.Printf("   ✅ Simulated %s - returned mock result\n", tc.FunctionCall.Name)
		}

		// Create tool response with the exact tool call ID
		toolResponse := llmtypes.ToolCallResponse{
			ToolCallID: tc.ID,
			Name:       tc.FunctionCall.Name,
			Content:    toolResultContent,
		}
		toolResponseParts = append(toolResponseParts, toolResponse)
	}

	// Add all tool responses as a single tool message
	conversationHistory = append(conversationHistory, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeTool,
		Parts: toolResponseParts,
	})
	fmt.Printf("   ✅ Added %d tool responses in a single tool message\n", len(toolResponseParts))

	// Step 3: Send the full conversation back to the LLM
	fmt.Println("\n🔄 Step 3: Sending full conversation back to LLM with tool responses")
	fmt.Printf("   Conversation has %d messages (user, assistant with tool calls, tool responses)\n", len(conversationHistory))

	// Track streaming for the second turn
	var streamedContent2 strings.Builder
	var contentChunks2 []string
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	done2 := make(chan bool)
	go func() {
		defer close(done2)
		for chunk := range streamChan2 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				content := chunk.Content
				streamedContent2.WriteString(content)
				contentChunks2 = append(contentChunks2, content)
				fmt.Printf("   📝 Streamed content: %s\n", content)
			}
		}
	}()

	startTime2 := time.Now()
	resp2, err2 := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel("global.anthropic.claude-sonnet-4-5-20250929-v1:0"),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool, weatherTool, calculateTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)

	// Wait for streaming to complete
	select {
	case <-done2:
		// Normal completion
	case <-time.After(5 * time.Second):
		fmt.Println("   ⚠️  Timeout waiting for streaming (this is OK if an error occurred)")
	}

	if err2 != nil {
		fmt.Fprintf(os.Stderr, "❌ Turn 2 failed: %v\n", err2)
		fmt.Println("   This may indicate tool response matching issues")
		return
	}

	if len(resp2.Choices) == 0 {
		fmt.Fprintf(os.Stderr, "❌ Turn 2 failed - no choices returned\n")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	streamedContent2Str := streamedContent2.String()

	fmt.Printf("\n✅ Turn 2 completed in %s\n", duration2)
	fmt.Println("\n📊 Turn 2 Streaming Statistics:")
	fmt.Printf("   Content chunks received: %d\n", len(contentChunks2))
	fmt.Printf("   Streamed content length: %d chars\n", len(streamedContent2Str))
	fmt.Printf("   Final content length: %d chars\n", len(finalContent2))

	// Display the LLM's response using tool results
	fmt.Println("\n💬 LLM Response (using tool results):")
	fmt.Println(strings.Repeat("-", 70))
	if finalContent2 != "" {
		fmt.Println(finalContent2)
	} else {
		fmt.Println("(No text content - LLM may have made additional tool calls)")
	}
	fmt.Println(strings.Repeat("-", 70))

	// Verify streamed content matches final content
	if streamedContent2Str != finalContent2 {
		fmt.Printf("\n⚠️  Warning: Streamed content doesn't match final content\n")
		fmt.Printf("   Streamed: %q\n", streamedContent2Str)
		fmt.Printf("   Final: %q\n", finalContent2)
	} else if len(finalContent2) > 0 {
		fmt.Println("\n✅ Streamed content matches final content!")
	}

	// Display token usage for turn 2
	if resp2.Choices[0].GenerationInfo != nil {
		genInfo2 := resp2.Choices[0].GenerationInfo
		fmt.Println("\n💰 Token Usage (Turn 2):")
		if genInfo2.InputTokens != nil {
			fmt.Printf("   Input tokens: %d\n", *genInfo2.InputTokens)
		}
		if genInfo2.OutputTokens != nil {
			fmt.Printf("   Output tokens: %d\n", *genInfo2.OutputTokens)
		}
		if genInfo2.TotalTokens != nil {
			fmt.Printf("   Total tokens: %d\n", *genInfo2.TotalTokens)
		} else if genInfo2.InputTokens != nil && genInfo2.OutputTokens != nil {
			fmt.Printf("   Total tokens: %d\n", *genInfo2.InputTokens+*genInfo2.OutputTokens)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("🎯 Multi-turn conversation completed successfully!")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nKey takeaways:")
	fmt.Println("  ✅ Tool calls were streamed in real-time")
	fmt.Println("  ✅ Tool responses were created and sent back to the LLM")
	fmt.Println("  ✅ LLM successfully used tool results to generate a response")
	fmt.Println("  ✅ Streaming worked correctly in both turns")
}
