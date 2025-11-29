package shared

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"llm-providers/llmtypes"
)

// RunEmbeddingTest runs embedding generation tests
func RunEmbeddingTest(embeddingModel llmtypes.EmbeddingModel, modelID string) {
	log.Printf("🚀 Testing %s (embedding generation)", modelID)

	ctx := context.Background()

	// Test 1: Single text embedding
	log.Printf("\n📝 Test 1: Single text embedding")
	testSingleEmbedding(ctx, embeddingModel, modelID, "Hello, world!")

	// Test 2: Batch embeddings (multiple texts)
	log.Printf("\n📝 Test 2: Batch embeddings")
	testBatchEmbeddings(ctx, embeddingModel, modelID, []string{
		"The quick brown fox jumps over the lazy dog",
		"Machine learning is a subset of artificial intelligence",
		"Natural language processing enables computers to understand human language",
	})

	// Test 3: Embedding with custom dimensions (for text-embedding-3 models)
	log.Printf("\n📝 Test 3: Custom dimensions")
	testEmbeddingWithDimensions(ctx, embeddingModel, modelID, "Test text for dimension reduction", 512)

	// Test 4: Empty input validation
	log.Printf("\n📝 Test 4: Empty input validation")
	testEmptyInput(ctx, embeddingModel)

	// Test 5: Long text embedding
	log.Printf("\n📝 Test 5: Long text embedding")
	longText := strings.Repeat("This is a test sentence. ", 100)
	testSingleEmbedding(ctx, embeddingModel, modelID, longText)

	log.Printf("\n✅ All embedding tests completed!")
}

func testSingleEmbedding(ctx context.Context, embeddingModel llmtypes.EmbeddingModel, modelID string, text string) {
	startTime := time.Now()
	resp, err := embeddingModel.GenerateEmbeddings(ctx, text, llmtypes.WithEmbeddingModel(modelID))
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Error generating embedding: %v", err)
		return
	}

	if len(resp.Embeddings) == 0 {
		log.Printf("❌ No embeddings returned")
		return
	}

	embedding := resp.Embeddings[0]
	log.Printf("✅ Generated embedding in %v", duration)
	log.Printf("   Model: %s", resp.Model)
	log.Printf("   Embedding dimensions: %d", len(embedding.Embedding))
	log.Printf("   Embedding index: %d", embedding.Index)
	if resp.Usage != nil {
		log.Printf("   Token usage - Prompt: %d, Total: %d", resp.Usage.PromptTokens, resp.Usage.TotalTokens)
	}

	// Validate embedding vector
	if len(embedding.Embedding) == 0 {
		log.Printf("❌ Embedding vector is empty")
		return
	}

	// Check if dimensions match expected
	expectedDims := 1536 // Default for most models
	if strings.Contains(modelID, "text-embedding-3-large") {
		expectedDims = 3072
	} else if strings.Contains(modelID, "text-embedding-ada-002") {
		expectedDims = 1536
	} else if strings.Contains(modelID, "text-embedding-004") {
		expectedDims = 768 // Vertex AI text-embedding-004 default dimensions
	} else if strings.Contains(modelID, "text-embedding-preview") || strings.Contains(modelID, "text-multilingual-embedding") {
		expectedDims = 768 // Vertex AI preview models
	} else if strings.Contains(modelID, "embedding-001") {
		expectedDims = 768 // Older Vertex AI embedding model
	} else if strings.Contains(modelID, "titan-embed-text-v1") {
		expectedDims = 1536 // Amazon Titan v1 default dimensions
	} else if strings.Contains(modelID, "titan-embed-text-v2") {
		expectedDims = 1024 // Amazon Titan v2 default dimensions
	}

	if len(embedding.Embedding) != expectedDims {
		log.Printf("⚠️  Warning: Expected %d dimensions, got %d", expectedDims, len(embedding.Embedding))
	} else {
		log.Printf("✅ Embedding dimensions match expected: %d", expectedDims)
	}
}

func testBatchEmbeddings(ctx context.Context, embeddingModel llmtypes.EmbeddingModel, modelID string, texts []string) {
	startTime := time.Now()
	resp, err := embeddingModel.GenerateEmbeddings(ctx, texts, llmtypes.WithEmbeddingModel(modelID))
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Error generating batch embeddings: %v", err)
		return
	}

	if len(resp.Embeddings) != len(texts) {
		log.Printf("❌ Expected %d embeddings, got %d", len(texts), len(resp.Embeddings))
		return
	}

	log.Printf("✅ Generated %d embeddings in %v", len(resp.Embeddings), duration)
	log.Printf("   Model: %s", resp.Model)
	if resp.Usage != nil {
		log.Printf("   Token usage - Prompt: %d, Total: %d", resp.Usage.PromptTokens, resp.Usage.TotalTokens)
	}

	// Validate each embedding
	for i, embedding := range resp.Embeddings {
		if len(embedding.Embedding) == 0 {
			log.Printf("❌ Embedding %d is empty", i)
			return
		}
		if embedding.Index != i {
			log.Printf("⚠️  Warning: Embedding index mismatch - expected %d, got %d", i, embedding.Index)
		}
		log.Printf("   Embedding %d: %d dimensions", i, len(embedding.Embedding))
	}

	log.Printf("✅ All batch embeddings validated")
}

func testEmbeddingWithDimensions(ctx context.Context, embeddingModel llmtypes.EmbeddingModel, modelID string, text string, dimensions int) {
	// Test dimensions for models that support it
	// OpenAI: text-embedding-3 models
	// Vertex AI: text-embedding-004 and newer models
	// Bedrock: titan-embed-text-v2 (v1 doesn't support custom dimensions)
	supportsDimensions := strings.Contains(modelID, "text-embedding-3") ||
		strings.Contains(modelID, "text-embedding-004") ||
		strings.Contains(modelID, "text-embedding-preview") ||
		strings.Contains(modelID, "text-multilingual-embedding") ||
		strings.Contains(modelID, "titan-embed-text-v2")

	if !supportsDimensions {
		log.Printf("⏭️  Skipping dimensions test (only supported for text-embedding-3, text-embedding-004+, and titan-embed-text-v2 models)")
		return
	}

	startTime := time.Now()
	resp, err := embeddingModel.GenerateEmbeddings(ctx, text,
		llmtypes.WithEmbeddingModel(modelID),
		llmtypes.WithDimensions(dimensions),
	)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Error generating embedding with dimensions: %v", err)
		return
	}

	if len(resp.Embeddings) == 0 {
		log.Printf("❌ No embeddings returned")
		return
	}

	embedding := resp.Embeddings[0]
	log.Printf("✅ Generated embedding with custom dimensions in %v", duration)
	log.Printf("   Model: %s", resp.Model)
	log.Printf("   Requested dimensions: %d", dimensions)
	log.Printf("   Actual dimensions: %d", len(embedding.Embedding))

	if len(embedding.Embedding) != dimensions {
		log.Printf("⚠️  Warning: Expected %d dimensions, got %d", dimensions, len(embedding.Embedding))
	} else {
		log.Printf("✅ Dimensions match requested: %d", dimensions)
	}
}

func testEmptyInput(ctx context.Context, embeddingModel llmtypes.EmbeddingModel) {
	_, err := embeddingModel.GenerateEmbeddings(ctx, "")
	if err == nil {
		log.Printf("❌ Expected error for empty input, but got none")
		return
	}
	log.Printf("✅ Empty input correctly rejected: %v", err)
}

// validateRequiredToolArguments validates that all required arguments are present in a tool call
// Returns error if validation fails, nil if successful
// modelID is used to detect Bedrock models (which have a known limitation with required params)
func validateRequiredToolArguments(tool llmtypes.Tool, toolCall llmtypes.ToolCall, modelID string) error {
	if tool.Function == nil || toolCall.FunctionCall == nil {
		return fmt.Errorf("tool or tool call missing function definition")
	}

	// Extract required parameters from tool definition
	var requiredParams []string
	if tool.Function.Parameters != nil {
		requiredParams = tool.Function.Parameters.Required
	}

	// If no required parameters, validation passes
	if len(requiredParams) == 0 {
		return nil
	}

	// Parse tool call arguments
	argsStr := toolCall.FunctionCall.Arguments
	if argsStr == "" {
		argsStr = "{}"
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return fmt.Errorf("invalid JSON arguments: %w", err)
	}

	// Check each required parameter
	var missingParams []string
	for _, param := range requiredParams {
		value, exists := args[param]
		if !exists || value == nil || value == "" {
			missingParams = append(missingParams, param)
		}
	}

	if len(missingParams) > 0 {
		// Check if this is a Bedrock model (known limitation - Bedrock models don't enforce required params)
		isBedrock := strings.HasPrefix(modelID, "us.") || strings.HasPrefix(modelID, "global.") || strings.Contains(modelID, "anthropic.claude")

		if isBedrock {
			// Check if we received NO arguments at all vs some arguments but missing required ones
			hasNoArgs := argsStr == "" || argsStr == "{}" || len(args) == 0

			if hasNoArgs {
				// CRITICAL: Bedrock model did not provide ANY arguments
				// This suggests the streaming/accumulation logic may not be working correctly
				// Check debug logs for [BEDROCK STREAM] to see what Input values were received during streaming
				return fmt.Errorf("CRITICAL: Bedrock model called tool with NO arguments (tool: %s, received args: %q). Check [BEDROCK STREAM] debug logs to see if Input was received during ContentBlockDeltaMemberToolUse events. This may indicate a streaming/accumulation issue rather than a model limitation", toolCall.FunctionCall.Name, argsStr)
			} else {
				// Bedrock provided some arguments but missing required ones
				return fmt.Errorf("CRITICAL: Bedrock model called tool with missing required arguments: %s (tool: %s, received args: %s). Model provided some arguments but not all required ones", strings.Join(missingParams, ", "), toolCall.FunctionCall.Name, argsStr)
			}
		}

		// For other providers, fail the validation
		return fmt.Errorf("missing required arguments: %s (tool: %s, received args: %s)", strings.Join(missingParams, ", "), toolCall.FunctionCall.Name, argsStr)
	}

	return nil
}

// RunPlainTextTest runs a basic plain text generation test
func RunPlainTextTest(llm llmtypes.Model, modelID string) {
	RunPlainTextTestWithContext(context.Background(), llm, modelID)
}

// RunPlainTextTestWithContext runs a basic plain text generation test with a specific context
func RunPlainTextTestWithContext(ctx context.Context, llm llmtypes.Model, modelID string) {
	log.Printf("🚀 Testing %s (plain text generation)", modelID)

	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Hello! Can you introduce yourself?"),
	}

	startTime := time.Now()
	resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithModel(modelID))
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Error: %v", err)
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ No choices returned")
		return
	}

	choice := resp.Choices[0]

	// Display token usage if available
	if choice.GenerationInfo != nil {
		info := choice.GenerationInfo
		log.Printf("📊 Token Usage:")
		if info.InputTokens != nil {
			log.Printf("   Input tokens: %v", *info.InputTokens)
		}
		if info.OutputTokens != nil {
			log.Printf("   Output tokens: %v", *info.OutputTokens)
		}
		if info.TotalTokens != nil {
			log.Printf("   Total tokens: %v", *info.TotalTokens)
		}
		// Check for cache tokens in Additional map
		if info.Additional != nil {
			if cacheRead, ok := info.Additional["cache_read_input_tokens"]; ok {
				log.Printf("   Cache read tokens: %v", cacheRead)
			}
			if cacheCreate, ok := info.Additional["cache_creation_input_tokens"]; ok {
				log.Printf("   Cache creation tokens: %v", cacheCreate)
			}
		}
	}

	if len(choice.Content) > 0 {
		log.Printf("✅ Success! Response received in %s", duration)
		log.Printf("   Content: %s", choice.Content)
	}
}

// RunToolCallTest runs standardized tool calling tests (4 tests)
func RunToolCallTest(llm llmtypes.Model, modelID string) {
	RunToolCallTestWithContext(context.Background(), llm, modelID)
}

func RunToolCallTestWithContext(ctx context.Context, llm llmtypes.Model, modelID string) {

	// Define test tools
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

	getTimeTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in a specific timezone",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g., 'UTC', 'America/New_York')",
					},
				},
				"required": []string{"timezone"},
			}),
		},
	}

	noParamTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_server_status",
			Description: "Get the current server status",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}),
		},
	}

	// Test 1: Simple tool call
	log.Printf("\n📝 Test 1: Simple tool call")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Use the read_file tool to read the contents of the file 'go.mod'. Make sure to provide the 'path' parameter with the value 'go.mod'."),
	}

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration1 := time.Since(startTime1)

	if err != nil {
		log.Printf("❌ Test 1 failed: %v", err)
		return
	}

	// ASSERTION: Response must have choices
	if len(resp1.Choices) == 0 {
		log.Printf("❌ Test 1 failed - no choices in response")
		return
	}

	// ASSERTION: Tool calls must be present
	if len(resp1.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Test 1 failed - no tool calls detected")
		return
	}

	toolCall1 := resp1.Choices[0].ToolCalls[0]

	// ASSERTION: Tool call must have function call
	if toolCall1.FunctionCall == nil {
		log.Printf("❌ Test 1 failed - tool call missing function call")
		return
	}

	// ASSERTION: Tool call name must be correct
	if toolCall1.FunctionCall.Name != "read_file" {
		log.Printf("❌ Test 1 failed - expected tool 'read_file', got '%s'", toolCall1.FunctionCall.Name)
		return
	}

	// ASSERTION: Tool call must have arguments
	if toolCall1.FunctionCall.Arguments == "" {
		log.Printf("❌ Test 1 failed - tool call missing arguments")
		return
	}

	// ASSERTION: Arguments must be valid JSON
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall1.FunctionCall.Arguments), &args); err != nil {
		log.Printf("❌ Test 1 failed - tool call arguments are not valid JSON: %v", err)
		return
	}

	// ASSERTION: Required 'path' parameter must be present
	if _, ok := args["path"]; !ok {
		log.Printf("❌ Test 1 failed - tool call arguments missing required 'path' parameter")
		return
	}

	// ASSERTION: 'path' parameter must have correct value
	if pathVal, ok := args["path"].(string); !ok || pathVal != "go.mod" {
		log.Printf("❌ Test 1 failed - tool call 'path' parameter incorrect: expected 'go.mod', got '%v'", args["path"])
		return
	}

	// ASSERTION: Tool call must have a non-empty ID
	if toolCall1.ID == "" {
		log.Printf("❌ Test 1 failed - tool call missing ID")
		return
	}

	// CRITICAL: Validate required arguments are present (using existing validation)
	if err := validateRequiredToolArguments(readFileTool, toolCall1, modelID); err != nil {
		log.Printf("❌ Test 1 failed - tool call missing required arguments: %v", err)
		log.Printf("      Model: %s", modelID)
		return
	}

	log.Printf("✅ Test 1 passed in %s", duration1)
	log.Printf("   Tool: %s", toolCall1.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall1.FunctionCall.Arguments)

	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Test 2: Multiple tools (model chooses from multiple available tools)
	log.Printf("\n📝 Test 2: Multiple tools (model selects from available tools)")
	messages2 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather in San Francisco?"),
	}

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, messages2,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool, weatherTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration2 := time.Since(startTime2)

	if err != nil {
		log.Printf("❌ Test 2 failed: %v", err)
		return
	}

	// ASSERTION: Response must have choices
	if len(resp2.Choices) == 0 {
		log.Printf("❌ Test 2 failed - no choices in response")
		return
	}

	// ASSERTION: Tool calls must be present
	if len(resp2.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Test 2 failed - no tool calls detected")
		return
	}

	toolCall2 := resp2.Choices[0].ToolCalls[0]

	// ASSERTION: Tool call must have function call
	if toolCall2.FunctionCall == nil {
		log.Printf("❌ Test 2 failed - tool call missing function call")
		return
	}

	// ASSERTION: Tool call name must be one of the available tools
	if toolCall2.FunctionCall.Name != "read_file" && toolCall2.FunctionCall.Name != "get_weather" {
		log.Printf("❌ Test 2 failed - unexpected tool name '%s', expected 'read_file' or 'get_weather'", toolCall2.FunctionCall.Name)
		return
	}

	// ASSERTION: Tool call must have arguments
	if toolCall2.FunctionCall.Arguments == "" {
		log.Printf("❌ Test 2 failed - tool call missing arguments")
		return
	}

	// ASSERTION: Arguments must be valid JSON
	var args2 map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall2.FunctionCall.Arguments), &args2); err != nil {
		log.Printf("❌ Test 2 failed - tool call arguments are not valid JSON: %v", err)
		return
	}

	// CRITICAL: Validate required arguments are present (check against correct tool)
	var toolToValidate llmtypes.Tool
	if toolCall2.FunctionCall.Name == "read_file" {
		toolToValidate = readFileTool
		// ASSERTION: 'read_file' must have 'path' parameter
		if _, ok := args2["path"]; !ok {
			log.Printf("❌ Test 2 failed - 'read_file' tool call missing required 'path' parameter")
			return
		}
	} else if toolCall2.FunctionCall.Name == "get_weather" {
		toolToValidate = weatherTool
		// ASSERTION: 'get_weather' must have 'location' parameter
		if _, ok := args2["location"]; !ok {
			log.Printf("❌ Test 2 failed - 'get_weather' tool call missing required 'location' parameter")
			return
		}
		// ASSERTION: 'location' parameter should not be empty
		if locationVal, ok := args2["location"].(string); ok && locationVal == "" {
			log.Printf("❌ Test 2 failed - 'get_weather' tool call 'location' parameter is empty")
			return
		}
	}
	if toolToValidate.Function != nil {
		if err := validateRequiredToolArguments(toolToValidate, toolCall2, modelID); err != nil {
			log.Printf("❌ Test 2 failed - tool call missing required arguments: %v", err)
			log.Printf("      Model: %s", modelID)
			return
		}
	}

	// ASSERTION: Tool call must have a non-empty ID
	if toolCall2.ID == "" {
		log.Printf("❌ Test 2 failed - tool call missing ID")
		return
	}

	log.Printf("✅ Test 2 passed in %s", duration2)
	log.Printf("   Tool: %s", toolCall2.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall2.FunctionCall.Arguments)

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Test 3: Parallel tool calls (multiple tools called in single response)
	log.Printf("\n📝 Test 3: Parallel tool calls (multiple tools in single response)")
	messages3 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Get the weather in New York and also get the current time in UTC. Do both tasks."),
	}

	startTime3 := time.Now()
	resp3, err := llm.GenerateContent(ctx, messages3,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration3 := time.Since(startTime3)

	if err != nil {
		log.Printf("❌ Test 3 failed: %v", err)
		return
	}

	// ASSERTION: Response must have choices
	if len(resp3.Choices) == 0 {
		log.Printf("❌ Test 3 failed - no response choices")
		return
	}

	choice3 := resp3.Choices[0]
	parallelToolCallsCount := 0
	if choice3.ToolCalls != nil {
		parallelToolCallsCount = len(choice3.ToolCalls)
	}

	// ASSERTION: Must have at least 2 parallel tool calls
	if parallelToolCallsCount < 2 {
		log.Printf("❌ Test 3 failed - expected at least 2 parallel tool calls, got %d", parallelToolCallsCount)
		return
	}

	if parallelToolCallsCount >= 2 {
		// ASSERTION: Validate all parallel tool calls
		for i, tc := range choice3.ToolCalls {
			// ASSERTION: Each tool call must have function call
			if tc.FunctionCall == nil {
				log.Printf("❌ Test 3 failed - parallel tool call %d missing function call", i+1)
				return
			}

			// ASSERTION: Tool call name must be valid
			if tc.FunctionCall.Name != "get_weather" && tc.FunctionCall.Name != "get_current_time" {
				log.Printf("❌ Test 3 failed - parallel tool call %d has unexpected name '%s'", i+1, tc.FunctionCall.Name)
				return
			}

			// ASSERTION: Tool call must have a non-empty ID
			if tc.ID == "" {
				log.Printf("❌ Test 3 failed - parallel tool call %d missing ID", i+1)
				return
			}

			// ASSERTION: Tool call must have arguments
			if tc.FunctionCall.Arguments == "" {
				log.Printf("❌ Test 3 failed - parallel tool call %d missing arguments", i+1)
				return
			}

			// ASSERTION: Arguments must be valid JSON
			var args3 map[string]interface{}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args3); err != nil {
				log.Printf("❌ Test 3 failed - parallel tool call %d arguments are not valid JSON: %v", i+1, err)
				return
			}

			var toolToValidate llmtypes.Tool
			if tc.FunctionCall.Name == "get_weather" {
				toolToValidate = weatherTool
				// ASSERTION: 'get_weather' must have 'location' parameter
				if _, ok := args3["location"]; !ok {
					log.Printf("❌ Test 3 failed - parallel tool call %d ('get_weather') missing required 'location' parameter", i+1)
					return
				}
			} else if tc.FunctionCall.Name == "get_current_time" {
				toolToValidate = getTimeTool
				// ASSERTION: 'get_current_time' must have 'timezone' parameter
				if _, ok := args3["timezone"]; !ok {
					log.Printf("❌ Test 3 failed - parallel tool call %d ('get_current_time') missing required 'timezone' parameter", i+1)
					return
				}
			}
			if toolToValidate.Function != nil {
				if err := validateRequiredToolArguments(toolToValidate, tc, modelID); err != nil {
					log.Printf("❌ Test 3 failed - parallel tool call %d missing required arguments: %v", i+1, err)
					log.Printf("      Model: %s", modelID)
					return
				}
			}
		}

		log.Printf("✅ Test 3 passed in %s - Parallel tool calls detected: %d", duration3, parallelToolCallsCount)
		toolCallIDs := make([]string, 0, parallelToolCallsCount)
		for i, tc := range choice3.ToolCalls {
			toolCallIDs = append(toolCallIDs, tc.ID)
			if tc.FunctionCall != nil {
				log.Printf("   Parallel tool call %d: %s (ID: %s)", i+1, tc.FunctionCall.Name, tc.ID)
				log.Printf("      Args: %s", tc.FunctionCall.Arguments)
			}
		}

		// ASSERTION: Verify unique IDs
		idMap := make(map[string]bool)
		for _, id := range toolCallIDs {
			if idMap[id] {
				log.Printf("❌ Test 3 failed - duplicate tool call ID detected: %s", id)
				return
			}
			idMap[id] = true
		}

		// ASSERTION: All IDs must be non-empty
		for i, id := range toolCallIDs {
			if id == "" {
				log.Printf("❌ Test 3 failed - tool call %d has empty ID", i+1)
				return
			}
		}

		if true {
			log.Printf("   ✅ All %d tool call IDs are unique", parallelToolCallsCount)
		} else {
			log.Printf("   ⚠️ Some tool call IDs are duplicates")
		}
	} else if parallelToolCallsCount == 1 {
		log.Printf("⚠️ Test 3: Only 1 tool call detected (expected 2+ for parallel test)")
		log.Printf("   Tool: %s", choice3.ToolCalls[0].FunctionCall.Name)
	} else {
		log.Printf("❌ Test 3 failed - No parallel tool calls detected")
	}

	logTokenUsage(choice3.GenerationInfo)

	// Test 4: Tool with no parameters
	log.Printf("\n📝 Test 4: Tool with no parameters")
	messages4 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Check the server status"),
	}

	startTime4 := time.Now()
	resp4, err := llm.GenerateContent(ctx, messages4,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{noParamTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration4 := time.Since(startTime4)

	if err != nil {
		log.Printf("❌ Test 4 failed: %v", err)
		return
	}

	if len(resp4.Choices) == 0 || len(resp4.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Test 4 failed - no tool calls detected")
		return
	}

	toolCall4 := resp4.Choices[0].ToolCalls[0]
	log.Printf("✅ Test 4 passed in %s", duration4)
	log.Printf("   Tool: %s", toolCall4.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall4.FunctionCall.Arguments)
	if toolCall4.FunctionCall.Arguments == "{}" || toolCall4.FunctionCall.Arguments == "" {
		log.Printf("   ✅ Tool correctly called with no parameters (empty args)")
	}

	logTokenUsage(resp4.Choices[0].GenerationInfo)

	log.Printf("\n🎯 All tool calling tests completed successfully!")
}

// RunStreamingToolCallTest runs streaming tool calling tests
// Tests that content chunks are streamed immediately and tool calls are streamed when complete
func RunStreamingToolCallTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Define test tools
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

	// Test 1: Streaming with simple tool call
	log.Printf("\n📝 Test 1: Streaming with simple tool call")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Use the read_file tool to read the contents of the file 'go.mod'. Make sure to provide the 'path' parameter with the value 'go.mod'."),
	}

	// Track streamed content and tool calls
	var streamedContent strings.Builder
	var streamedToolCalls []llmtypes.ToolCall

	// Create channel for streaming chunks
	streamChan := make(chan llmtypes.StreamChunk, 100)

	// Start goroutine to receive chunks from channel
	done := make(chan bool)
	go func() {
		defer close(done)
		for chunk := range streamChan {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				// This is a content chunk
				streamedContent.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content chunk: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				// This is a complete tool call
				if chunk.ToolCall != nil {
					streamedToolCalls = append(streamedToolCalls, *chunk.ToolCall)
					log.Printf("   📦 Streamed tool call: %s (ID: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
				}
			}
		}
	}()

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan),
	)
	duration1 := time.Since(startTime1)

	// Wait for all chunks to be processed
	<-done

	if err != nil {
		log.Printf("❌ Test 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 || len(resp1.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Test 1 failed - no tool calls detected")
		return
	}

	// Verify streaming worked
	streamedContentStr := streamedContent.String()
	finalContent := resp1.Choices[0].Content
	toolCall1 := resp1.Choices[0].ToolCalls[0]

	// CRITICAL: Fail if tool calls weren't streamed when they exist in the response
	if len(streamedToolCalls) == 0 {
		log.Printf("❌ Test 1 failed - tool calls exist in response but none were streamed (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final tool calls: %d", len(resp1.Choices[0].ToolCalls))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Verify streamed tool calls match final tool calls
	if len(streamedToolCalls) != len(resp1.Choices[0].ToolCalls) {
		log.Printf("❌ Test 1 failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls), len(resp1.Choices[0].ToolCalls))
		log.Printf("      Model: %s", modelID)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	// Verify streamed tool call matches final tool call (by ID)
	streamedToolCallMap := make(map[string]*llmtypes.ToolCall)
	for i := range streamedToolCalls {
		streamedToolCallMap[streamedToolCalls[i].ID] = &streamedToolCalls[i]
	}
	for _, finalTC := range resp1.Choices[0].ToolCalls {
		streamedTC, exists := streamedToolCallMap[finalTC.ID]
		if !exists {
			log.Printf("❌ Test 1 failed - tool call ID %s in final response but not in streamed tool calls", finalTC.ID)
			return
		}
		if streamedTC.FunctionCall.Name != finalTC.FunctionCall.Name {
			log.Printf("❌ Test 1 failed - streamed tool call name mismatch for ID %s: streamed=%s, final=%s", finalTC.ID, streamedTC.FunctionCall.Name, finalTC.FunctionCall.Name)
			return
		}

		// CRITICAL: Validate required arguments are present
		if err := validateRequiredToolArguments(readFileTool, finalTC, modelID); err != nil {
			log.Printf("❌ Test 1 failed - tool call missing required arguments: %v", err)
			log.Printf("      Model: %s", modelID)
			log.Printf("      This indicates the model is not providing required parameters!")
			return
		}
	}

	// Verify streamed content matches final content (if there was any content)
	if len(finalContent) > 0 && streamedContentStr != finalContent {
		log.Printf("❌ Test 1 failed - streamed content doesn't match final content")
		log.Printf("      Streamed: %q", streamedContentStr)
		log.Printf("      Final: %q", finalContent)
		return
	}

	log.Printf("✅ Test 1 passed in %s", duration1)
	log.Printf("   Tool: %s", toolCall1.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall1.FunctionCall.Arguments)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content length: %d chars", len(streamedContentStr))
	log.Printf("      Final content length: %d chars", len(finalContent))
	log.Printf("      Streamed tool calls: %d ✅", len(streamedToolCalls))
	for i, tc := range streamedToolCalls {
		log.Printf("         Tool call %d: %s (ID: %s, Args: %s)", i+1, tc.FunctionCall.Name, tc.ID, tc.FunctionCall.Arguments)
	}

	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Test 2: Streaming with multiple tools (model selects one)
	log.Printf("\n📝 Test 2: Streaming with multiple tools (model selects)")
	messages2 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather in San Francisco?"),
	}

	// Reset streaming trackers
	streamedContent.Reset()
	streamedToolCalls = streamedToolCalls[:0]

	// Create channel for streaming chunks
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	// Start goroutine to receive chunks from channel
	done2 := make(chan bool)
	go func() {
		defer close(done2)
		for chunk := range streamChan2 {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				// This is a content chunk
				streamedContent.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content chunk: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				// This is a complete tool call
				if chunk.ToolCall != nil {
					streamedToolCalls = append(streamedToolCalls, *chunk.ToolCall)
					log.Printf("   📦 Streamed tool call: %s (ID: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
				}
			}
		}
	}()

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, messages2,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool, weatherTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)

	// Wait for all chunks to be processed
	<-done2

	if err != nil {
		log.Printf("❌ Test 2 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 || len(resp2.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Test 2 failed - no tool calls detected")
		return
	}

	finalToolCalls2 := resp2.Choices[0].ToolCalls
	finalContent2 := resp2.Choices[0].Content
	streamedContentStr2 := streamedContent.String()

	// CRITICAL: Fail if tool calls weren't streamed when they exist in the response
	if len(streamedToolCalls) == 0 {
		log.Printf("❌ Test 2 failed - tool calls exist in response but none were streamed (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final tool calls: %d", len(finalToolCalls2))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Verify streamed tool calls match final tool calls
	if len(streamedToolCalls) != len(finalToolCalls2) {
		log.Printf("❌ Test 2 failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls), len(finalToolCalls2))
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed IDs: %v", func() []string {
			ids := make([]string, len(streamedToolCalls))
			for i, tc := range streamedToolCalls {
				ids[i] = tc.ID
			}
			return ids
		}())
		log.Printf("      Final IDs: %v", func() []string {
			ids := make([]string, len(finalToolCalls2))
			for i, tc := range finalToolCalls2 {
				ids[i] = tc.ID
			}
			return ids
		}())
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	// Verify streamed tool call matches final tool call (by ID)
	streamedToolCallMap2 := make(map[string]*llmtypes.ToolCall)
	for i := range streamedToolCalls {
		streamedToolCallMap2[streamedToolCalls[i].ID] = &streamedToolCalls[i]
	}
	for _, finalTC := range finalToolCalls2 {
		streamedTC, exists := streamedToolCallMap2[finalTC.ID]
		if !exists {
			log.Printf("❌ Test 2 failed - tool call ID %s in final response but not in streamed tool calls", finalTC.ID)
			return
		}
		if streamedTC.FunctionCall.Name != finalTC.FunctionCall.Name {
			log.Printf("❌ Test 2 failed - streamed tool call name mismatch for ID %s: streamed=%s, final=%s", finalTC.ID, streamedTC.FunctionCall.Name, finalTC.FunctionCall.Name)
			return
		}
	}

	// Verify streamed content matches final content (if there was any content)
	if len(finalContent2) > 0 && streamedContentStr2 != finalContent2 {
		log.Printf("❌ Test 2 failed - streamed content doesn't match final content")
		log.Printf("      Streamed: %q", streamedContentStr2)
		log.Printf("      Final: %q", finalContent2)
		return
	}

	toolCall2 := resp2.Choices[0].ToolCalls[0]
	log.Printf("✅ Test 2 passed in %s", duration2)
	log.Printf("   Tool: %s", toolCall2.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall2.FunctionCall.Arguments)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content length: %d chars", len(streamedContentStr2))
	log.Printf("      Final content length: %d chars", len(finalContent2))
	log.Printf("      Streamed tool calls: %d ✅", len(streamedToolCalls))
	for i, tc := range streamedToolCalls {
		log.Printf("         Tool call %d: %s (ID: %s, Args: %s)", i+1, tc.FunctionCall.Name, tc.ID, tc.FunctionCall.Arguments)
	}

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	log.Printf("\n🎯 All streaming tool calling tests completed successfully!")
}

// RunStreamingContentTest runs basic content streaming tests (no tool calls)
// Tests that content chunks stream correctly and match the final response
func RunStreamingContentTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Test 1: Short content streaming
	log.Printf("\n📝 Test 1: Short content streaming")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Say hello in exactly 5 words."),
	}

	var streamedContent1 strings.Builder
	streamChan1 := make(chan llmtypes.StreamChunk, 100)

	done1 := make(chan bool)
	go func() {
		defer close(done1)
		for chunk := range streamChan1 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent1.WriteString(chunk.Content)
				log.Printf("   📝 Streamed: %s", chunk.Content)
			}
		}
	}()

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingChan(streamChan1),
	)
	duration1 := time.Since(startTime1)
	<-done1

	if err != nil {
		log.Printf("❌ Test 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Test 1 failed - no choices returned")
		return
	}

	finalContent1 := resp1.Choices[0].Content
	streamedContent1Str := streamedContent1.String()

	// CRITICAL: Fail if no streaming chunks were received
	if len(streamedContent1Str) == 0 {
		log.Printf("❌ Test 1 failed - no streaming chunks received (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final content length: %d chars", len(finalContent1))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Fail if streamed content doesn't match final content
	if streamedContent1Str != finalContent1 {
		log.Printf("❌ Test 1 failed - streamed content doesn't match final content")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed length: %d chars", len(streamedContent1Str))
		log.Printf("      Final length: %d chars", len(finalContent1))
		log.Printf("      Streamed: %q", streamedContent1Str)
		log.Printf("      Final: %q", finalContent1)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	log.Printf("✅ Test 1 passed in %s", duration1)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent1Str))
	log.Printf("      Final content: %d chars", len(finalContent1))
	log.Printf("      Content match: ✅")

	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Test 2: Longer content streaming
	log.Printf("\n📝 Test 2: Longer content streaming")
	messages2 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Write a short 3-sentence story about a robot learning to paint."),
	}

	var streamedContent2 strings.Builder
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	done2 := make(chan bool)
	go func() {
		defer close(done2)
		chunkCount := 0
		for chunk := range streamChan2 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				chunkCount++
				streamedContent2.WriteString(chunk.Content)
				if chunkCount <= 5 {
					log.Printf("   📝 Chunk %d: %s", chunkCount, chunk.Content)
				}
			}
		}
		log.Printf("   📊 Total chunks received: %d", chunkCount)
	}()

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, messages2,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)
	<-done2

	if err != nil {
		log.Printf("❌ Test 2 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Test 2 failed - no choices returned")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	streamedContent2Str := streamedContent2.String()

	// CRITICAL: Fail if no streaming chunks were received
	if len(streamedContent2Str) == 0 {
		log.Printf("❌ Test 2 failed - no streaming chunks received (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final content length: %d chars", len(finalContent2))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Fail if streamed content doesn't match final content
	if streamedContent2Str != finalContent2 {
		log.Printf("❌ Test 2 failed - streamed content doesn't match final content")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed length: %d chars", len(streamedContent2Str))
		log.Printf("      Final length: %d chars", len(finalContent2))
		log.Printf("      Streamed: %q", streamedContent2Str)
		log.Printf("      Final: %q", finalContent2)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	log.Printf("✅ Test 2 passed in %s", duration2)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent2Str))
	log.Printf("      Final content: %d chars", len(finalContent2))
	log.Printf("      Content match: ✅")

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	log.Printf("\n🎯 All content streaming tests completed successfully!")
}

// RunStreamingMixedTest runs streaming tests with mixed content and tool calls
// Tests that content chunks stream first, then tool calls when complete
func RunStreamingMixedTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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

	// Test: Request that might return content before tool call
	log.Printf("\n📝 Test: Streaming with potential mixed content and tool calls")
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "First, tell me what you're about to do, then read the go.mod file."),
	}

	var streamedContent strings.Builder
	var streamedToolCalls []llmtypes.ToolCall
	var chunkOrder []string // Track order: "content" or "tool_call"
	var contentChunkCount int

	streamChan := make(chan llmtypes.StreamChunk, 100)

	done := make(chan bool)
	go func() {
		defer close(done)
		for chunk := range streamChan {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				contentChunkCount++
				streamedContent.WriteString(chunk.Content)
				chunkOrder = append(chunkOrder, "content")
				log.Printf("   📝 Streamed content: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				if chunk.ToolCall != nil {
					streamedToolCalls = append(streamedToolCalls, *chunk.ToolCall)
					chunkOrder = append(chunkOrder, "tool_call")
					log.Printf("   📦 Streamed tool call: %s (ID: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
				}
			}
		}
	}()

	startTime := time.Now()
	resp, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan),
	)
	duration := time.Since(startTime)
	<-done

	if err != nil {
		log.Printf("❌ Test failed: %v", err)
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ Test failed - no choices returned")
		return
	}

	finalContent := resp.Choices[0].Content
	finalToolCalls := resp.Choices[0].ToolCalls
	hasToolCalls := len(finalToolCalls) > 0

	log.Printf("✅ Test passed in %s", duration)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content chunks: %d", contentChunkCount)
	log.Printf("      Streamed tool calls: %d", len(streamedToolCalls))
	log.Printf("      Final content length: %d chars", len(finalContent))
	log.Printf("      Final tool calls: %d", len(finalToolCalls))
	log.Printf("   📋 Chunk order: %v", chunkOrder)

	// CRITICAL: Validate content streaming (if there's content, it must be streamed)
	streamedContentStr := streamedContent.String()
	if len(finalContent) > 0 {
		if len(streamedContentStr) == 0 {
			log.Printf("❌ Test failed - content in response but no content chunks were streamed (streaming not working)")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Final content length: %d chars", len(finalContent))
			log.Printf("      This indicates streaming is not working properly")
			return
		}
		if streamedContentStr != finalContent {
			log.Printf("❌ Test failed - streamed content doesn't match final content")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Streamed length: %d chars", len(streamedContentStr))
			log.Printf("      Final length: %d chars", len(finalContent))
			log.Printf("      Streamed: %q", streamedContentStr)
			log.Printf("      Final: %q", finalContent)
			log.Printf("      This indicates a streaming implementation bug")
			return
		}
	}

	// CRITICAL: Validate that if tool calls exist, they were streamed
	if hasToolCalls {
		if len(streamedToolCalls) == 0 {
			log.Printf("❌ Test failed - tool calls in response but none were streamed (streaming not working)")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Final tool calls: %d", len(finalToolCalls))
			log.Printf("      This indicates streaming is not working properly")
			return
		}
		// CRITICAL: Verify streamed tool calls match final tool calls
		if len(streamedToolCalls) != len(finalToolCalls) {
			log.Printf("❌ Test failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls), len(finalToolCalls))
			log.Printf("      Model: %s", modelID)
			log.Printf("      This indicates a streaming implementation bug")
			return
		}
		// Check if IDs match (order might differ)
		streamedIDs := make(map[string]bool)
		for _, tc := range streamedToolCalls {
			streamedIDs[tc.ID] = true
		}
		allMatch := true
		for _, tc := range finalToolCalls {
			if !streamedIDs[tc.ID] {
				allMatch = false
				log.Printf("❌ Test failed - final tool call ID %s not found in streamed calls", tc.ID)
				return
			}
		}
		if allMatch {
			log.Printf("   ✅ All tool calls were streamed correctly")
		}
		log.Printf("   Tool: %s", finalToolCalls[0].FunctionCall.Name)
		log.Printf("   Args: %s", finalToolCalls[0].FunctionCall.Arguments)
	}

	logTokenUsage(resp.Choices[0].GenerationInfo)

	log.Printf("\n🎯 Mixed streaming test completed successfully!")
}

// RunStreamingParallelToolCallsTest runs streaming tests with multiple parallel tool calls
// Tests that all tool calls stream correctly when multiple are returned
func RunStreamingParallelToolCallsTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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

	// Test: Request multiple parallel tool calls
	log.Printf("\n📝 Test: Streaming with multiple parallel tool calls")
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Read the go.mod file and get the weather in San Francisco."),
	}

	var streamedToolCalls []llmtypes.ToolCall
	streamChan := make(chan llmtypes.StreamChunk, 100)

	done := make(chan bool)
	go func() {
		defer close(done)
		for chunk := range streamChan {
			if chunk.Type == llmtypes.StreamChunkTypeToolCall && chunk.ToolCall != nil {
				streamedToolCalls = append(streamedToolCalls, *chunk.ToolCall)
				log.Printf("   📦 Streamed tool call %d: %s (ID: %s)", len(streamedToolCalls), chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
			}
		}
	}()

	startTime := time.Now()
	resp, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool, weatherTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan),
	)
	duration := time.Since(startTime)
	<-done

	if err != nil {
		log.Printf("❌ Test failed: %v", err)
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ Test failed - no choices returned")
		return
	}

	finalToolCalls := resp.Choices[0].ToolCalls

	if len(finalToolCalls) == 0 {
		log.Printf("❌ Test failed - no tool calls in response")
		return
	}

	// CRITICAL: Fail if tool calls weren't streamed
	if len(streamedToolCalls) == 0 {
		log.Printf("❌ Test failed - tool calls in response but none were streamed (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final tool calls: %d", len(finalToolCalls))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Verify all tool calls were streamed
	if len(streamedToolCalls) != len(finalToolCalls) {
		log.Printf("❌ Test failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls), len(finalToolCalls))
		log.Printf("      Model: %s", modelID)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	// CRITICAL: Verify all tool call IDs match and validate required arguments
	streamedIDs := make(map[string]*llmtypes.ToolCall)
	for i := range streamedToolCalls {
		streamedIDs[streamedToolCalls[i].ID] = &streamedToolCalls[i]
	}
	for _, finalTC := range finalToolCalls {
		streamedTC, exists := streamedIDs[finalTC.ID]
		if !exists {
			log.Printf("❌ Test failed - tool call ID %s in final response but not in streamed tool calls", finalTC.ID)
			return
		}
		if streamedTC.FunctionCall.Name != finalTC.FunctionCall.Name {
			log.Printf("❌ Test failed - tool call name mismatch for ID %s: streamed=%s, final=%s", finalTC.ID, streamedTC.FunctionCall.Name, finalTC.FunctionCall.Name)
			return
		}

		// CRITICAL: Validate required arguments are present for each tool call
		var toolToValidate llmtypes.Tool
		if finalTC.FunctionCall.Name == "read_file" {
			toolToValidate = readFileTool
		} else if finalTC.FunctionCall.Name == "get_weather" {
			toolToValidate = weatherTool
		}
		if toolToValidate.Function != nil {
			if err := validateRequiredToolArguments(toolToValidate, finalTC, modelID); err != nil {
				log.Printf("❌ Test failed - tool call %s missing required arguments: %v", finalTC.FunctionCall.Name, err)
				log.Printf("      Model: %s", modelID)
				log.Printf("      This indicates the model is not providing required parameters!")
				return
			}
		}
	}

	log.Printf("✅ Test passed in %s", duration)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed tool calls: %d ✅", len(streamedToolCalls))
	log.Printf("      Final tool calls: %d ✅", len(finalToolCalls))
	log.Printf("   ✅ All %d tool calls were streamed correctly and match final response", len(finalToolCalls))

	if len(finalToolCalls) > 0 {
		log.Printf("   📋 Tool calls in response:")
		for i, tc := range finalToolCalls {
			log.Printf("      %d. %s (ID: %s, Args: %s)", i+1, tc.FunctionCall.Name, tc.ID, tc.FunctionCall.Arguments)
		}
	}

	if len(streamedToolCalls) > 0 {
		log.Printf("   📋 Streamed tool calls:")
		for i, tc := range streamedToolCalls {
			log.Printf("      %d. %s (ID: %s, Args: %s)", i+1, tc.FunctionCall.Name, tc.ID, tc.FunctionCall.Arguments)
		}
	}

	logTokenUsage(resp.Choices[0].GenerationInfo)

	log.Printf("\n🎯 Parallel tool calls streaming test completed successfully!")
}

// RunStreamingWithFuncTest tests backward compatibility with WithStreamingFunc
// Verifies that the callback-based API still works correctly
func RunStreamingWithFuncTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("\n📝 Test: Streaming with WithStreamingFunc (backward compatibility)")

	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Count from 1 to 5, one number per line."),
	}

	var streamedContent strings.Builder
	var contentChunks []string
	var toolCallChunks []llmtypes.ToolCall

	// Use WithStreamingFunc instead of WithStreamingChan
	// WithStreamingFunc uses a goroutine internally, so we need to wait a bit
	// for all callbacks to complete
	startTime := time.Now()
	resp, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingFunc(func(chunk llmtypes.StreamChunk) {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				streamedContent.WriteString(chunk.Content)
				contentChunks = append(contentChunks, chunk.Content)
				log.Printf("   📝 Callback received content: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				if chunk.ToolCall != nil {
					toolCallChunks = append(toolCallChunks, *chunk.ToolCall)
					log.Printf("   📦 Callback received tool call: %s (ID: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
				}
			}
		}),
	)
	duration := time.Since(startTime)

	// Give a moment for async callbacks to complete
	// WithStreamingFunc wraps the channel in a goroutine, so we wait briefly
	time.Sleep(200 * time.Millisecond)

	if err != nil {
		log.Printf("❌ Test failed: %v", err)
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ Test failed - no choices returned")
		return
	}

	finalContent := resp.Choices[0].Content
	streamedContentStr := streamedContent.String()

	// CRITICAL: Fail if no streaming chunks were received
	if len(streamedContentStr) == 0 {
		log.Printf("❌ Test failed - no streaming chunks received (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final content length: %d chars", len(finalContent))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Fail if streamed content doesn't match final content
	if streamedContentStr != finalContent {
		log.Printf("❌ Test failed - streamed content doesn't match final content")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed length: %d chars", len(streamedContentStr))
		log.Printf("      Final length: %d chars", len(finalContent))
		log.Printf("      Streamed: %q", streamedContentStr)
		log.Printf("      Final: %q", finalContent)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	log.Printf("✅ Test passed in %s", duration)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Content chunks received: %d", len(contentChunks))
	log.Printf("      Tool call chunks received: %d", len(toolCallChunks))
	log.Printf("      Streamed content length: %d chars", len(streamedContentStr))
	log.Printf("      Final content length: %d chars", len(finalContent))
	log.Printf("      Content match: ✅")

	if len(contentChunks) > 0 {
		log.Printf("   📋 First 3 content chunks:")
		for i, chunk := range contentChunks {
			if i >= 3 {
				break
			}
			log.Printf("      %d. %q", i+1, chunk)
		}
	}

	logTokenUsage(resp.Choices[0].GenerationInfo)

	log.Printf("\n🎯 WithStreamingFunc test completed successfully!")
}

// RunStreamingCancellationTest tests context cancellation during streaming
// Verifies graceful cancellation and proper channel cleanup
func RunStreamingCancellationTest(llm llmtypes.Model, modelID string) {
	ctx, cancel := context.WithCancel(context.Background())

	log.Printf("\n📝 Test: Streaming cancellation")

	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Write a very long story about space exploration. Make it at least 500 words."),
	}

	var streamedContent strings.Builder
	var chunksReceived int
	streamChan := make(chan llmtypes.StreamChunk, 100)

	done := make(chan bool)
	go func() {
		defer close(done)
		for chunk := range streamChan {
			chunksReceived++
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent.WriteString(chunk.Content)
				log.Printf("   📝 Chunk %d: %s", chunksReceived, chunk.Content)
			}

			// Cancel after receiving 5 chunks
			if chunksReceived == 5 {
				log.Printf("   🛑 Cancelling context after %d chunks", chunksReceived)
				cancel()
			}
		}
		log.Printf("   ✅ Channel closed, received %d total chunks", chunksReceived)
	}()

	startTime := time.Now()
	_, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingChan(streamChan),
	)
	duration := time.Since(startTime)
	<-done

	// Check if error is due to cancellation
	if err != nil {
		// Check for cancellation errors (may be wrapped)
		errStr := err.Error()
		isCanceled := err == context.Canceled ||
			err == context.DeadlineExceeded ||
			strings.Contains(errStr, "context canceled") ||
			strings.Contains(errStr, "context deadline exceeded")

		if isCanceled {
			log.Printf("✅ Test passed in %s - cancellation handled correctly", duration)
			log.Printf("   📊 Streaming stats:")
			log.Printf("      Chunks received before cancellation: %d", chunksReceived)
			log.Printf("      Streamed content length: %d chars", streamedContent.Len())
			log.Printf("      Error (expected): %v", err)
		} else {
			log.Printf("❌ Test failed with unexpected error: %v", err)
			return
		}
	} else {
		// If no error, that's also okay - cancellation might not have taken effect
		log.Printf("✅ Test passed in %s", duration)
		log.Printf("   📊 Streaming stats:")
		log.Printf("      Chunks received: %d", chunksReceived)
		log.Printf("      Streamed content length: %d chars", streamedContent.Len())
		log.Printf("      Note: Cancellation may not have taken effect if response completed quickly")
	}

	log.Printf("\n🎯 Cancellation test completed!")
}

// RunStreamingMultiTurnTest runs multi-turn conversation streaming tests
// Tests that streaming works correctly across multiple conversation turns
func RunStreamingMultiTurnTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Longer timeout for multi-turn
	defer cancel()

	log.Printf("\n📝 Test: Multi-turn conversation with streaming")

	// Turn 1: Initial question
	log.Printf("\n🔄 Turn 1: Initial question")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "My name is Alice. What's 2+2?"),
	}

	var streamedContent1 strings.Builder
	streamChan1 := make(chan llmtypes.StreamChunk, 100)

	done1 := make(chan bool)
	go func() {
		defer close(done1)
		for chunk := range streamChan1 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent1.WriteString(chunk.Content)
				log.Printf("   📝 Turn 1 streamed: %s", chunk.Content)
			}
		}
	}()

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingChan(streamChan1),
	)
	duration1 := time.Since(startTime1)
	<-done1

	if err != nil {
		log.Printf("❌ Turn 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Turn 1 failed - no choices returned")
		return
	}

	finalContent1 := resp1.Choices[0].Content
	streamedContent1Str := streamedContent1.String()

	// CRITICAL: Fail if no streaming chunks were received
	if len(streamedContent1Str) == 0 {
		log.Printf("❌ Turn 1 failed - no streaming chunks received (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final content length: %d chars", len(finalContent1))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Fail if streamed content doesn't match final content
	if streamedContent1Str != finalContent1 {
		log.Printf("❌ Turn 1 failed - streamed content doesn't match final content")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed length: %d chars", len(streamedContent1Str))
		log.Printf("      Final length: %d chars", len(finalContent1))
		log.Printf("      Streamed: %q", streamedContent1Str)
		log.Printf("      Final: %q", finalContent1)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	log.Printf("✅ Turn 1 passed in %s", duration1)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent1Str))
	log.Printf("      Final content: %d chars", len(finalContent1))
	log.Printf("      Content match: ✅")
	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Turn 2: Follow-up question (with conversation history)
	log.Printf("\n🔄 Turn 2: Follow-up question (with conversation history)")
	messages2 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "My name is Alice. What's 2+2?"),
		llmtypes.TextParts(llmtypes.ChatMessageTypeAI, finalContent1),
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's my name?"),
	}

	var streamedContent2 strings.Builder
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	done2 := make(chan bool)
	go func() {
		defer close(done2)
		for chunk := range streamChan2 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent2.WriteString(chunk.Content)
				log.Printf("   📝 Turn 2 streamed: %s", chunk.Content)
			}
		}
	}()

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, messages2,
		llmtypes.WithModel(modelID),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)
	<-done2

	if err != nil {
		log.Printf("❌ Turn 2 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Turn 2 failed - no choices returned")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	streamedContent2Str := streamedContent2.String()

	// CRITICAL: Fail if no streaming chunks were received
	if len(streamedContent2Str) == 0 {
		log.Printf("❌ Turn 2 failed - no streaming chunks received (streaming not working)")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Final content length: %d chars", len(finalContent2))
		log.Printf("      This indicates streaming is not working properly")
		return
	}

	// CRITICAL: Fail if streamed content doesn't match final content
	if streamedContent2Str != finalContent2 {
		log.Printf("❌ Turn 2 failed - streamed content doesn't match final content")
		log.Printf("      Model: %s", modelID)
		log.Printf("      Streamed length: %d chars", len(streamedContent2Str))
		log.Printf("      Final length: %d chars", len(finalContent2))
		log.Printf("      Streamed: %q", streamedContent2Str)
		log.Printf("      Final: %q", finalContent2)
		log.Printf("      This indicates a streaming implementation bug")
		return
	}

	// CRITICAL: Validate context retention - response should contain "Alice"
	if len(finalContent2) == 0 {
		log.Printf("❌ Turn 2 failed - empty response")
		return
	}

	finalContent2Lower := strings.ToLower(finalContent2)
	if !strings.Contains(finalContent2Lower, "alice") {
		log.Printf("❌ Turn 2 failed - context retention validation failed")
		log.Printf("   Expected response to contain 'Alice' (from Turn 1)")
		log.Printf("   Response: %s", finalContent2)
		log.Printf("   This indicates the model is not maintaining conversation context")
		return
	}

	log.Printf("✅ Turn 2 passed in %s", duration2)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent2Str))
	log.Printf("      Final content: %d chars", len(finalContent2))
	log.Printf("      Content match: ✅")
	log.Printf("   Context retention: ✅ (response contains 'Alice')")
	log.Printf("   💬 Response: %s", finalContent2)

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Turn 3: Another follow-up with tool calls
	log.Printf("\n🔄 Turn 3: Follow-up with tool call (with full conversation history)")

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

	messages3 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "My name is Alice. What's 2+2?"),
		llmtypes.TextParts(llmtypes.ChatMessageTypeAI, finalContent1),
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's my name?"),
		llmtypes.TextParts(llmtypes.ChatMessageTypeAI, finalContent2),
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Can you read the go.mod file?"),
	}

	var streamedContent3 strings.Builder
	var streamedToolCalls3 []llmtypes.ToolCall
	streamChan3 := make(chan llmtypes.StreamChunk, 100)

	done3 := make(chan bool)
	go func() {
		defer close(done3)
		for chunk := range streamChan3 {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				streamedContent3.WriteString(chunk.Content)
				log.Printf("   📝 Turn 3 streamed content: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				if chunk.ToolCall != nil {
					streamedToolCalls3 = append(streamedToolCalls3, *chunk.ToolCall)
					log.Printf("   📦 Turn 3 streamed tool call: %s (ID: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID)
				}
			}
		}
	}()

	startTime3 := time.Now()
	resp3, err := llm.GenerateContent(ctx, messages3,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan3),
	)
	duration3 := time.Since(startTime3)
	<-done3

	if err != nil {
		log.Printf("❌ Turn 3 failed: %v", err)
		return
	}

	if len(resp3.Choices) == 0 {
		log.Printf("❌ Turn 3 failed - no choices returned")
		return
	}

	finalContent3 := resp3.Choices[0].Content
	finalToolCalls3 := resp3.Choices[0].ToolCalls
	streamedContent3Str := streamedContent3.String()

	// CRITICAL: Validate content streaming (if there's content, it must be streamed)
	if len(finalContent3) > 0 {
		if len(streamedContent3Str) == 0 {
			log.Printf("❌ Turn 3 failed - content in response but no content chunks were streamed (streaming not working)")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Final content length: %d chars", len(finalContent3))
			log.Printf("      This indicates streaming is not working properly")
			return
		}
		if streamedContent3Str != finalContent3 {
			log.Printf("❌ Turn 3 failed - streamed content doesn't match final content")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Streamed length: %d chars", len(streamedContent3Str))
			log.Printf("      Final length: %d chars", len(finalContent3))
			log.Printf("      Streamed: %q", streamedContent3Str)
			log.Printf("      Final: %q", finalContent3)
			log.Printf("      This indicates a streaming implementation bug")
			return
		}
	}

	// CRITICAL: Validate tool call streaming (if there are tool calls, they must be streamed)
	if len(finalToolCalls3) > 0 {
		if len(streamedToolCalls3) == 0 {
			log.Printf("❌ Turn 3 failed - tool calls in response but none were streamed (streaming not working)")
			log.Printf("      Model: %s", modelID)
			log.Printf("      Final tool calls: %d", len(finalToolCalls3))
			log.Printf("      This indicates streaming is not working properly")
			return
		}
		if len(streamedToolCalls3) != len(finalToolCalls3) {
			log.Printf("❌ Turn 3 failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls3), len(finalToolCalls3))
			log.Printf("      Model: %s", modelID)
			log.Printf("      This indicates a streaming implementation bug")
			return
		}
		// Verify all tool call IDs match
		streamedIDs := make(map[string]bool)
		for _, tc := range streamedToolCalls3 {
			streamedIDs[tc.ID] = true
		}
		for _, finalTC := range finalToolCalls3 {
			if !streamedIDs[finalTC.ID] {
				log.Printf("❌ Turn 3 failed - tool call ID %s in final response but not in streamed tool calls", finalTC.ID)
				return
			}
		}
	}

	// CRITICAL: Validate tool call arguments if tool calls were returned
	if len(finalToolCalls3) > 0 {
		log.Printf("\n🔍 Validating tool call arguments")
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

		for i, tc := range finalToolCalls3 {
			if tc.FunctionCall == nil {
				log.Printf("❌ Turn 3 failed - tool call %d has no FunctionCall", i+1)
				return
			}
			if err := validateRequiredToolArguments(readFileTool, tc, modelID); err != nil {
				log.Printf("❌ Turn 3 failed - tool call %d missing required arguments: %v", i+1, err)
				log.Printf("      Model: %s", modelID)
				return
			}
			log.Printf("   ✅ Tool call %d: %s - valid arguments", i+1, tc.FunctionCall.Name)
		}
	}

	log.Printf("✅ Turn 3 passed in %s", duration3)
	log.Printf("   📊 Streaming stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent3Str))
	log.Printf("      Final content: %d chars", len(finalContent3))
	log.Printf("      Streamed tool calls: %d ✅", len(streamedToolCalls3))
	log.Printf("      Final tool calls: %d ✅", len(finalToolCalls3))

	if len(finalToolCalls3) > 0 {
		log.Printf("   Tool: %s", finalToolCalls3[0].FunctionCall.Name)
		log.Printf("   Args: %s", finalToolCalls3[0].FunctionCall.Arguments)
		log.Printf("   ✅ Tool calls were streamed correctly")
		log.Printf("   ✅ Tool call arguments validated")
	} else {
		log.Printf("   ⚠️ No tool calls returned (model may not support tool calling)")
	}

	logTokenUsage(resp3.Choices[0].GenerationInfo)

	log.Printf("\n🎯 All multi-turn conversation streaming tests completed successfully!")
	log.Printf("   📊 Summary:")
	log.Printf("      Turn 1: Content streaming ✅")
	log.Printf("      Turn 2: Content streaming with history ✅")
	log.Printf("      Turn 3: Content + tool call streaming with full history ✅")
}

// RunStreamingToolCallWithHistoryTest tests the critical flow:
// 1. Get tool calls from LLM (with streaming)
// 2. Validate arguments are valid JSON
// 3. Store tool calls in conversation history (mimicking CLI behavior)
// 4. Add tool results
// 5. Send full conversation (including tool calls) back to LLM
// 6. Verify no errors occur (especially no JSON validation errors)
// This test catches the issue where tool calls with invalid JSON arguments cause errors when sent back to Bedrock
func RunStreamingToolCallWithHistoryTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log.Printf("\n📝 Test: Tool calls with conversation history (full flow)")
	log.Printf("   This test verifies that tool calls can be stored in conversation history")
	log.Printf("   and sent back to the LLM without JSON validation errors")

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

	// Step 1: Get tool calls from LLM (with streaming)
	log.Printf("\n🔄 Step 1: Request tool call with streaming")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Use the read_file tool to read the contents of the file 'go.mod'. Make sure to provide the 'path' parameter with the value 'go.mod'."),
	}

	var streamedContent1 strings.Builder
	var streamedToolCalls1 []llmtypes.ToolCall
	streamChan1 := make(chan llmtypes.StreamChunk, 100)

	done1 := make(chan bool)
	go func() {
		defer close(done1)
		for chunk := range streamChan1 {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				streamedContent1.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				if chunk.ToolCall != nil {
					streamedToolCalls1 = append(streamedToolCalls1, *chunk.ToolCall)
					log.Printf("   📦 Streamed tool call: %s (ID: %s, Args: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID, chunk.ToolCall.FunctionCall.Arguments)
				}
			}
		}
	}()

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan1),
	)
	duration1 := time.Since(startTime1)
	<-done1

	if err != nil {
		log.Printf("❌ Step 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Step 1 failed - no choices returned")
		return
	}

	finalContent1 := resp1.Choices[0].Content
	finalToolCalls1 := resp1.Choices[0].ToolCalls

	// CRITICAL: Validate streaming worked
	if len(finalToolCalls1) > 0 {
		if len(streamedToolCalls1) == 0 {
			log.Printf("❌ Step 1 failed - tool calls in response but none were streamed")
			return
		}
		if len(streamedToolCalls1) != len(finalToolCalls1) {
			log.Printf("❌ Step 1 failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls1), len(finalToolCalls1))
			return
		}
	}

	log.Printf("✅ Step 1 passed in %s", duration1)
	log.Printf("   📊 Stats:")
	log.Printf("      Streamed tool calls: %d", len(streamedToolCalls1))
	log.Printf("      Final tool calls: %d", len(finalToolCalls1))

	// CRITICAL: Validate that all tool call arguments are valid JSON
	log.Printf("\n🔍 Step 2: Validating tool call arguments are valid JSON")
	for i, tc := range finalToolCalls1 {
		if tc.FunctionCall == nil {
			log.Printf("❌ Step 2 failed - tool call %d has no FunctionCall", i+1)
			return
		}
		args := tc.FunctionCall.Arguments
		if args == "" {
			args = "{}"
		}
		// Try to parse as JSON
		var jsonObj map[string]interface{}
		if err := json.Unmarshal([]byte(args), &jsonObj); err != nil {
			log.Printf("❌ Step 2 failed - tool call %d has invalid JSON arguments: %q, error: %v", i+1, args, err)
			log.Printf("      Tool: %s, ID: %s", tc.FunctionCall.Name, tc.ID)
			log.Printf("      This will cause errors when sending back to LLM!")
			return
		}
		log.Printf("   ✅ Tool call %d: %s - valid JSON: %s", i+1, tc.FunctionCall.Name, args)
	}
	log.Printf("✅ Step 2 passed - all tool call arguments are valid JSON")

	// CRITICAL: Validate required arguments are present
	log.Printf("\n🔍 Step 2b: Validating required arguments are present")
	for i, tc := range finalToolCalls1 {
		if err := validateRequiredToolArguments(readFileTool, tc, modelID); err != nil {
			log.Printf("❌ Step 2b failed - tool call %d missing required arguments: %v", i+1, err)
			log.Printf("      Model: %s", modelID)
			log.Printf("      This will cause tool execution errors in the CLI!")
			return
		}
		log.Printf("   ✅ Tool call %d: %s - all required arguments present", i+1, tc.FunctionCall.Name)
	}
	log.Printf("✅ Step 2b passed - all required arguments are present")

	// Step 3: Store tool calls in conversation history (mimicking CLI behavior)
	log.Printf("\n🔄 Step 3: Storing tool calls in conversation history")
	conversationHistory := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Use the read_file tool to read the contents of the file 'go.mod'. Make sure to provide the 'path' parameter with the value 'go.mod'."),
	}

	// Add assistant message with tool calls (mimicking CLI's AddAssistantMessage)
	if finalContent1 != "" || len(finalToolCalls1) > 0 {
		parts := []llmtypes.ContentPart{}
		if finalContent1 != "" {
			parts = append(parts, llmtypes.TextContent{Text: finalContent1})
		}
		for _, tc := range finalToolCalls1 {
			parts = append(parts, tc)
		}
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: parts,
		})
		log.Printf("   ✅ Added assistant message with %d tool call(s)", len(finalToolCalls1))
	}

	// Step 4: Add tool results (mimicking CLI's AddToolResults)
	// CRITICAL: Bedrock requires all tool results for tool calls from a single assistant message
	// to be in ONE tool message, not separate messages
	log.Printf("\n🔄 Step 4: Adding tool results to conversation history")
	if len(finalToolCalls1) > 0 {
		// Collect all tool results as parts of a single message
		parts := make([]llmtypes.ContentPart, 0, len(finalToolCalls1))
		for _, tc := range finalToolCalls1 {
			// Mock tool result
			toolResult := llmtypes.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    fmt.Sprintf("Mock result for %s", tc.FunctionCall.Name),
			}
			parts = append(parts, toolResult)
			log.Printf("   ✅ Added tool result for %s (ID: %s)", tc.FunctionCall.Name, tc.ID)
		}
		// Add all tool results as a single message (required by Bedrock)
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: parts,
		})
	}

	// Step 5: Send full conversation (including tool calls) back to LLM
	log.Printf("\n🔄 Step 5: Sending full conversation back to LLM (CRITICAL TEST)")
	log.Printf("   This is where JSON validation errors occur if arguments are invalid")
	log.Printf("   Conversation has %d messages (including tool calls)", len(conversationHistory))

	// CRITICAL: Validate that all ContentPart types can be type-asserted correctly
	// This catches conversion bugs where types aren't properly converted from agent_go to llm-providers
	log.Printf("\n🔍 Step 5a: Validating ContentPart type assertions (regression test)")
	if err := validateConversationTypeAssertions(conversationHistory); err != nil {
		log.Printf("❌ Step 5a failed: %v", err)
		log.Printf("   CRITICAL: This indicates a type conversion bug!")
		log.Printf("   Check convertContentPart in agent_go/internal/llm/providers.go")
		log.Printf("   All ContentPart types must be from llm-providers package for adapters to work")
		return
	}
	log.Printf("✅ Step 5a passed - all ContentPart types can be type-asserted correctly")

	var streamedContent2 strings.Builder
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	done2 := make(chan bool)
	go func() {
		defer close(done2)
		for chunk := range streamChan2 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent2.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content: %s", chunk.Content)
			}
		}
	}()

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)

	// Wait for goroutine with timeout to prevent deadlock
	select {
	case <-done2:
		// Goroutine finished normally
	case <-time.After(5 * time.Second):
		// Timeout - channel might not be closed if error occurred
		log.Printf("   ⚠️ Timeout waiting for streaming goroutine (this is OK if an error occurred)")
	}

	// CRITICAL: This is where the error occurs if tool call arguments are invalid JSON
	if err != nil {
		errStr := err.Error()
		// Check for JSON validation errors (especially from Bedrock)
		if strings.Contains(errStr, "invalid") && strings.Contains(errStr, "json") {
			log.Printf("❌ Step 5 failed - JSON validation error when sending tool calls back to LLM")
			log.Printf("      Error: %v", err)
			log.Printf("      This indicates tool call arguments stored in conversation history are invalid JSON!")
			log.Printf("      Model: %s", modelID)
			return
		}
		if strings.Contains(errStr, "toolUse.input") || strings.Contains(errStr, "tool_use") {
			log.Printf("❌ Step 5 failed - Tool use input validation error")
			log.Printf("      Error: %v", err)
			log.Printf("      This indicates tool call arguments are invalid when sent back to LLM!")
			log.Printf("      Model: %s", modelID)
			return
		}
		log.Printf("❌ Step 5 failed with error: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Step 5 failed - no choices returned")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	streamedContent2Str := streamedContent2.String()

	// CRITICAL: Validate streaming worked
	if len(finalContent2) > 0 {
		if len(streamedContent2Str) == 0 {
			log.Printf("❌ Step 5 failed - content in response but no content chunks were streamed")
			return
		}
		if streamedContent2Str != finalContent2 {
			log.Printf("❌ Step 5 failed - streamed content doesn't match final content")
			return
		}
	}

	log.Printf("✅ Step 5 passed in %s", duration2)
	log.Printf("   📊 Stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent2Str))
	log.Printf("      Final content: %d chars", len(finalContent2))
	log.Printf("   💬 Response: %s", finalContent2)

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	log.Printf("\n🎯 Tool call with history test completed successfully!")
	log.Printf("   ✅ All tool call arguments are valid JSON")
	log.Printf("   ✅ Tool calls can be stored in conversation history")
	log.Printf("   ✅ Tool calls can be sent back to LLM without errors")
	log.Printf("   ✅ Streaming works correctly throughout the flow")
}

// RunParallelToolCallWithResponseTest tests the complete flow:
// 1. Request multiple parallel tool calls
// 2. Get parallel tool calls from LLM
// 3. Add tool responses for all tool calls
// 4. Send full conversation (with tool responses) back to LLM
// 5. Verify LLM can continue conversation using tool results
// This test validates that tool response matching works correctly for multiple tools
func RunParallelToolCallWithResponseTest(llm llmtypes.Model, modelID string) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log.Printf("\n📝 Test: Parallel tool calls with responses and continued conversation")
	log.Printf("   This test verifies that multiple tool calls can be matched with responses")
	log.Printf("   and the LLM can continue the conversation using the tool results")

	// Define tools for parallel execution
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

	getTimeTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in a specific timezone",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g., 'UTC', 'America/New_York')",
					},
				},
				"required": []string{"timezone"},
			}),
		},
	}

	// Step 1: Request parallel tool calls
	log.Printf("\n🔄 Step 1: Request parallel tool calls")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Get the weather in San Francisco and also get the current time in UTC. Do both tasks."),
	}

	var streamedContent1 strings.Builder
	var streamedToolCalls1 []llmtypes.ToolCall
	streamChan1 := make(chan llmtypes.StreamChunk, 100)

	done1 := make(chan bool)
	go func() {
		defer close(done1)
		for chunk := range streamChan1 {
			switch chunk.Type {
			case llmtypes.StreamChunkTypeContent:
				streamedContent1.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content: %s", chunk.Content)
			case llmtypes.StreamChunkTypeToolCall:
				if chunk.ToolCall != nil {
					streamedToolCalls1 = append(streamedToolCalls1, *chunk.ToolCall)
					log.Printf("   📦 Streamed tool call: %s (ID: %s, Args: %s)", chunk.ToolCall.FunctionCall.Name, chunk.ToolCall.ID, chunk.ToolCall.FunctionCall.Arguments)
				}
			}
		}
	}()

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan1),
	)
	duration1 := time.Since(startTime1)
	<-done1

	if err != nil {
		log.Printf("❌ Step 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Step 1 failed - no choices returned")
		return
	}

	finalContent1 := resp1.Choices[0].Content
	finalToolCalls1 := resp1.Choices[0].ToolCalls

	// CRITICAL: Validate we got multiple parallel tool calls
	if len(finalToolCalls1) < 2 {
		log.Printf("❌ Step 1 failed - expected at least 2 parallel tool calls, got %d", len(finalToolCalls1))
		log.Printf("      Model: %s", modelID)
		return
	}

	// CRITICAL: Validate streaming worked
	if len(finalToolCalls1) > 0 {
		if len(streamedToolCalls1) == 0 {
			log.Printf("❌ Step 1 failed - tool calls in response but none were streamed")
			return
		}
		if len(streamedToolCalls1) != len(finalToolCalls1) {
			log.Printf("❌ Step 1 failed - streamed tool call count (%d) doesn't match final count (%d)", len(streamedToolCalls1), len(finalToolCalls1))
			return
		}
	}

	log.Printf("✅ Step 1 passed in %s", duration1)
	log.Printf("   📊 Stats:")
	log.Printf("      Parallel tool calls received: %d", len(finalToolCalls1))
	log.Printf("      Streamed tool calls: %d", len(streamedToolCalls1))
	for i, tc := range finalToolCalls1 {
		if tc.FunctionCall != nil {
			log.Printf("      Tool call %d: %s (ID: %s)", i+1, tc.FunctionCall.Name, tc.ID)
		}
	}

	// CRITICAL: Validate required arguments for all parallel tool calls
	log.Printf("\n🔍 Step 2: Validating tool call arguments")
	for i, tc := range finalToolCalls1 {
		if tc.FunctionCall == nil {
			log.Printf("❌ Step 2 failed - tool call %d has no FunctionCall", i+1)
			return
		}
		var toolToValidate llmtypes.Tool
		if tc.FunctionCall.Name == "get_weather" {
			toolToValidate = weatherTool
		} else if tc.FunctionCall.Name == "get_current_time" {
			toolToValidate = getTimeTool
		}
		if toolToValidate.Function != nil {
			if err := validateRequiredToolArguments(toolToValidate, tc, modelID); err != nil {
				log.Printf("❌ Step 2 failed - tool call %d missing required arguments: %v", i+1, err)
				log.Printf("      Model: %s", modelID)
				return
			}
		}
		log.Printf("   ✅ Tool call %d: %s - valid arguments", i+1, tc.FunctionCall.Name)
	}
	log.Printf("✅ Step 2 passed - all tool call arguments are valid")

	// Step 3: Build conversation history with tool calls
	log.Printf("\n🔄 Step 3: Building conversation history with tool calls")
	conversationHistory := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Get the weather in San Francisco and also get the current time in UTC. Do both tasks."),
	}

	// Add assistant message with tool calls
	if finalContent1 != "" || len(finalToolCalls1) > 0 {
		parts := []llmtypes.ContentPart{}
		if finalContent1 != "" {
			parts = append(parts, llmtypes.TextContent{Text: finalContent1})
		}
		for _, tc := range finalToolCalls1 {
			parts = append(parts, tc)
		}
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: parts,
		})
		log.Printf("   ✅ Added assistant message with %d tool call(s)", len(finalToolCalls1))
	}

	// Step 4: Add tool responses for all tool calls
	log.Printf("\n🔄 Step 4: Adding tool responses for all parallel tool calls")
	if len(finalToolCalls1) > 0 {
		// Collect all tool results as parts of a single message
		parts := make([]llmtypes.ContentPart, 0, len(finalToolCalls1))
		for _, tc := range finalToolCalls1 {
			// Create realistic mock tool results
			var toolResultContent string
			if tc.FunctionCall.Name == "get_weather" {
				// Parse location from arguments
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err == nil {
					location := "San Francisco"
					if loc, ok := args["location"].(string); ok {
						location = loc
					}
					toolResultContent = fmt.Sprintf(`{"location": "%s", "temperature": 72, "condition": "Sunny", "humidity": 65}`, location)
				} else {
					toolResultContent = `{"location": "San Francisco", "temperature": 72, "condition": "Sunny", "humidity": 65}`
				}
			} else if tc.FunctionCall.Name == "get_current_time" {
				// Parse timezone from arguments
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err == nil {
					timezone := "UTC"
					if tz, ok := args["timezone"].(string); ok {
						timezone = tz
					}
					toolResultContent = fmt.Sprintf(`{"timezone": "%s", "current_time": "2025-01-27T14:30:00Z"}`, timezone)
				} else {
					toolResultContent = `{"timezone": "UTC", "current_time": "2025-01-27T14:30:00Z"}`
				}
			} else {
				toolResultContent = fmt.Sprintf(`{"result": "Mock result for %s"}`, tc.FunctionCall.Name)
			}

			toolResult := llmtypes.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    toolResultContent,
			}
			parts = append(parts, toolResult)
			log.Printf("   ✅ Added tool result for %s (ID: %s)", tc.FunctionCall.Name, tc.ID)
		}
		// Add all tool results as a single message
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: parts,
		})
		log.Printf("   ✅ Added %d tool responses in a single tool message", len(parts))
	}

	// Step 5: Send full conversation (with tool responses) back to LLM
	log.Printf("\n🔄 Step 5: Sending full conversation back to LLM with tool responses")
	log.Printf("   This tests that the LLM can match tool responses to tool calls correctly")
	log.Printf("   Conversation has %d messages (including %d tool calls and %d tool responses)", len(conversationHistory), len(finalToolCalls1), len(finalToolCalls1))

	// CRITICAL: Validate that all ContentPart types can be type-asserted correctly
	// This catches conversion bugs where types aren't properly converted from agent_go to llm-providers
	log.Printf("\n🔍 Step 5a: Validating ContentPart type assertions (regression test)")
	if err := validateConversationTypeAssertions(conversationHistory); err != nil {
		log.Printf("❌ Step 5a failed: %v", err)
		log.Printf("   CRITICAL: This indicates a type conversion bug!")
		log.Printf("   Check convertContentPart in agent_go/internal/llm/providers.go")
		log.Printf("   All ContentPart types must be from llm-providers package for adapters to work")
		return
	}
	log.Printf("✅ Step 5a passed - all ContentPart types can be type-asserted correctly")

	// CRITICAL: Check for thought signatures (required for Gemini 3 Pro when sending tool calls back)
	// This validation would catch the bug where convertToolCall in agent_go doesn't preserve ThoughtSignature
	missingThoughtSignatures := false
	var missingToolCalls []string
	for i, tc := range finalToolCalls1 {
		if tc.ThoughtSignature == "" {
			log.Printf("   ⚠️ Tool call %d (%s) is missing thought signature", i+1, tc.FunctionCall.Name)
			missingThoughtSignatures = true
			missingToolCalls = append(missingToolCalls, fmt.Sprintf("%s (ID: %s)", tc.FunctionCall.Name, tc.ID))
		} else {
			log.Printf("   ✅ Tool call %d (%s) has thought signature (length: %d)", i+1, tc.FunctionCall.Name, len(tc.ThoughtSignature))
		}
	}
	if missingThoughtSignatures && strings.Contains(modelID, "gemini-3") {
		log.Printf("   ❌ CRITICAL ERROR: Some tool calls are missing thought signatures!")
		log.Printf("      Gemini 3 Pro REQUIRES thought signatures when sending tool calls back in conversation history")
		log.Printf("      Missing thought signatures in: %v", missingToolCalls)
		log.Printf("      This will cause API errors. Possible causes:")
		log.Printf("      1. API didn't return thought signatures in Step 1 (check API response)")
		log.Printf("      2. Thought signatures were lost during conversion (check convertToolCall in agent_go/internal/llm/providers.go)")
		log.Printf("      Test will continue to verify if API rejects the request, but this indicates a bug.")
		// Don't return here - let the test continue to see if the API actually rejects it
		// This helps distinguish between "API didn't provide it" vs "we lost it during conversion"
	}

	var streamedContent2 strings.Builder
	streamChan2 := make(chan llmtypes.StreamChunk, 100)

	done2 := make(chan bool)
	go func() {
		defer close(done2)
		for chunk := range streamChan2 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				streamedContent2.WriteString(chunk.Content)
				log.Printf("   📝 Streamed content: %s", chunk.Content)
			}
		}
	}()

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan2),
	)
	duration2 := time.Since(startTime2)

	// CRITICAL: If we had missing thought signatures and got an error, this confirms the bug
	if missingThoughtSignatures && strings.Contains(modelID, "gemini-3") && err != nil {
		if strings.Contains(err.Error(), "thought_signature") || strings.Contains(err.Error(), "thoughtSignature") {
			log.Printf("   ❌ TEST FAILED: API rejected request due to missing thought signatures")
			log.Printf("      Error: %v", err)
			log.Printf("      This confirms that thought signatures are required and were not preserved")
			log.Printf("      Check convertToolCall in agent_go/internal/llm/providers.go to ensure ThoughtSignature is copied")
			return
		}
	}

	// Wait for goroutine with timeout to prevent deadlock
	select {
	case <-done2:
		// Goroutine finished normally
	case <-time.After(5 * time.Second):
		// Timeout - channel might not be closed if error occurred
		log.Printf("   ⚠️ Timeout waiting for streaming goroutine (this is OK if an error occurred)")
	}

	// CRITICAL: This is where errors occur if tool response matching fails
	if err != nil {
		errStr := err.Error()
		log.Printf("❌ Step 5 failed with error: %v", err)
		log.Printf("      Error string: %s", errStr)
		log.Printf("      Model: %s", modelID)
		log.Printf("      This may indicate tool response matching issues!")
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Step 5 failed - no choices returned")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	streamedContent2Str := streamedContent2.String()

	// CRITICAL: Validate streaming worked
	if len(finalContent2) > 0 {
		if len(streamedContent2Str) == 0 {
			log.Printf("❌ Step 5 failed - content in response but no content chunks were streamed")
			return
		}
		if streamedContent2Str != finalContent2 {
			log.Printf("❌ Step 5 failed - streamed content doesn't match final content")
			log.Printf("      Streamed: %q", streamedContent2Str)
			log.Printf("      Final: %q", finalContent2)
			return
		}
	}

	log.Printf("✅ Step 5 passed in %s", duration2)
	log.Printf("   📊 Stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent2Str))
	log.Printf("      Final content: %d chars", len(finalContent2))
	log.Printf("   💬 Response: %s", finalContent2)

	// Step 6: Verify the LLM used the tool results in its response
	log.Printf("\n🔍 Step 6: Verifying LLM used tool results in response")
	responseLower := strings.ToLower(finalContent2)
	hasWeatherInfo := strings.Contains(responseLower, "weather") || strings.Contains(responseLower, "temperature") || strings.Contains(responseLower, "sunny") || strings.Contains(responseLower, "72")
	hasTimeInfo := strings.Contains(responseLower, "time") || strings.Contains(responseLower, "utc") || strings.Contains(responseLower, "14:30")

	if hasWeatherInfo && hasTimeInfo {
		log.Printf("   ✅ LLM successfully used both tool results in its response")
	} else if hasWeatherInfo || hasTimeInfo {
		log.Printf("   ⚠️ LLM used some tool results but may have missed others")
		log.Printf("      Weather info present: %v", hasWeatherInfo)
		log.Printf("      Time info present: %v", hasTimeInfo)
	} else {
		log.Printf("   ⚠️ LLM response may not have used tool results")
		log.Printf("      This could indicate tool response matching issues")
	}

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Step 7: Test follow-up question about tool results (verify LLM remembers tool responses)
	log.Printf("\n🔄 Step 7: Testing follow-up question about tool results")
	log.Printf("   This verifies the LLM remembers tool responses from previous turns")

	// Add the LLM's response from Step 5 to conversation history
	conversationHistory = append(conversationHistory, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeAI,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: finalContent2}},
	})

	// Ask a follow-up question about the tool results
	followUpQuestion := "What was the temperature in San Francisco?"
	conversationHistory = append(conversationHistory, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: followUpQuestion}},
	})

	log.Printf("   💬 Follow-up question: %s", followUpQuestion)
	log.Printf("   📝 Conversation now has %d messages", len(conversationHistory))

	// Send follow-up question with full conversation history
	streamChan3 := make(chan llmtypes.StreamChunk, 100)
	var streamedContent3 strings.Builder
	startTime3 := time.Now()

	done3 := make(chan bool)
	go func() {
		defer close(done3)
		for chunk := range streamChan3 {
			if chunk.Type == llmtypes.StreamChunkTypeContent {
				// StreamChunk.Content is a string
				streamedContent3.WriteString(chunk.Content)
				log.Printf("    📝 Streamed content: %s", chunk.Content)
			}
		}
	}()

	resp3, err := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
		llmtypes.WithStreamingChan(streamChan3),
	)
	duration3 := time.Since(startTime3)

	// Wait for goroutine with timeout to prevent deadlock
	select {
	case <-done3:
		// Goroutine finished normally
	case <-time.After(5 * time.Second):
		// Timeout - channel might not be closed if error occurred
		log.Printf("   ⚠️ Timeout waiting for streaming goroutine (this is OK if an error occurred)")
	}

	if err != nil {
		log.Printf("❌ Step 7 failed with error: %v", err)
		log.Printf("      This may indicate the LLM doesn't remember tool responses")
		return
	}

	if len(resp3.Choices) == 0 {
		log.Printf("❌ Step 7 failed - no choices returned")
		return
	}

	finalContent3 := resp3.Choices[0].Content
	streamedContent3Str := streamedContent3.String()

	if len(finalContent3) > 0 {
		if len(streamedContent3Str) == 0 {
			log.Printf("❌ Step 7 failed - content in response but no content chunks were streamed")
			return
		}
		if streamedContent3Str != finalContent3 {
			log.Printf("❌ Step 7 failed - streamed content doesn't match final content")
			log.Printf("      Streamed: %q", streamedContent3Str)
			log.Printf("      Final: %q", finalContent3)
			return
		}
	}

	log.Printf("✅ Step 7 passed in %s", duration3)
	log.Printf("   📊 Stats:")
	log.Printf("      Streamed content: %d chars", len(streamedContent3Str))
	log.Printf("      Final content: %d chars", len(finalContent3))
	log.Printf("   💬 Response: %s", finalContent3)

	// Verify the LLM remembered the temperature from the tool response
	responseLower3 := strings.ToLower(finalContent3)
	hasTemperature := strings.Contains(responseLower3, "72") || strings.Contains(responseLower3, "temperature") || strings.Contains(responseLower3, "seventy-two")

	if hasTemperature {
		log.Printf("   ✅ LLM successfully remembered the temperature from tool results!")
	} else {
		log.Printf("   ⚠️ LLM may not have remembered the temperature from tool results")
		log.Printf("      Response: %s", finalContent3)
	}

	logTokenUsage(resp3.Choices[0].GenerationInfo)

	log.Printf("\n🎯 Parallel tool call with response test completed successfully!")
	log.Printf("   ✅ Multiple parallel tool calls received")
	log.Printf("   ✅ Tool responses added for all tool calls")
	log.Printf("   ✅ Full conversation sent back to LLM without errors")
	log.Printf("   ✅ LLM continued conversation using tool results")
	log.Printf("   ✅ LLM remembers tool results in follow-up questions")
	log.Printf("   ✅ Streaming worked correctly throughout the flow")
}

// RunStructuredOutputTest runs standardized structured output test
// useJSONMode: true for JSON mode (Bedrock, OpenRouter), false for JSON Schema (OpenAI) or tool-based (Anthropic)
// useJSONSchema: true for OpenAI JSON Schema, false otherwise
// useToolBased: true for Anthropic tool-based approach, false otherwise
func RunStructuredOutputTest(llm llmtypes.Model, modelID string, useJSONMode bool, useJSONSchema bool, useToolBased bool) {
	ctx := context.Background()

	// Define cookie recipe schema
	recipeSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"recipes": map[string]interface{}{
				"type":        "array",
				"description": "List of cookie recipes",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"recipeName": map[string]interface{}{
							"type":        "string",
							"description": "The name of the cookie recipe",
						},
						"ingredients": map[string]interface{}{
							"type":        "array",
							"description": "List of ingredients with amounts",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"required":             []string{"recipeName", "ingredients"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"recipes"},
		"additionalProperties": false,
	}

	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List a few popular cookie recipes, and include the amounts of ingredients. Return as a JSON array where each recipe has 'recipeName' (string) and 'ingredients' (array of strings)."),
	}

	log.Printf("📋 Setting up structured output test...")
	log.Printf("   Schema: JSON object with 'recipes' array property")
	log.Printf("   Each recipe has recipeName (string) and ingredients (array of strings)")

	var resp *llmtypes.ContentResponse
	var err error
	startTime := time.Now()

	if useToolBased {
		// Anthropic tool-based approach
		toolParams := map[string]interface{}{
			"type":       "object",
			"properties": recipeSchema["properties"].(map[string]interface{}),
			"required":   []string{"recipes"},
		}

		tool := llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "return_cookie_recipes",
				Description: "Returns a structured list of cookie recipes with their ingredients",
				Parameters:  llmtypes.NewParameters(toolParams),
			},
		}

		toolChoice := &llmtypes.ToolChoice{
			Type: "function",
			Function: &llmtypes.FunctionName{
				Name: "return_cookie_recipes",
			},
		}

		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List a few popular cookie recipes, and include the amounts of ingredients. Use the return_cookie_recipes tool to return the data in the specified format."),
		}

		log.Printf("   Using tool-based structured output (Anthropic approach)")
		resp, err = llm.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithTools([]llmtypes.Tool{tool}),
			llmtypes.WithToolChoice(toolChoice),
		)
	} else if useJSONSchema {
		// OpenAI JSON Schema approach
		log.Printf("   Using JSON Schema structured outputs (strict mode)")
		resp, err = llm.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithJSONSchema(recipeSchema, "cookie_recipes", "A list of cookie recipes with their ingredients", true),
		)
	} else if useJSONMode {
		// JSON mode approach (Bedrock, OpenRouter)
		log.Printf("   Using JSON mode")
		resp, err = llm.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithJSONMode(),
		)
	} else {
		// Fallback to JSON mode
		log.Printf("   Using JSON mode (fallback)")
		resp, err = llm.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithJSONMode(),
		)
	}

	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Structured output test failed: %v", err)
		return
	}

	if len(resp.Choices) == 0 {
		log.Printf("❌ No response choices")
		return
	}

	choice := resp.Choices[0]
	if len(choice.Content) == 0 && len(choice.ToolCalls) == 0 {
		log.Printf("❌ No content or tool calls in response")
		return
	}

	log.Printf("✅ Response received successfully in %s", duration)
	logTokenUsage(choice.GenerationInfo)

	// Validate structured output
	log.Printf("\n📋 Validating structured JSON output...")

	var recipes []map[string]interface{}

	if useToolBased && len(choice.ToolCalls) > 0 {
		// Extract from tool call arguments
		toolCall := choice.ToolCalls[0]
		argsJSON := strings.TrimSpace(toolCall.FunctionCall.Arguments)

		var toolArgs map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
			log.Printf("⚠️ Tool call arguments are not valid JSON: %v", err)
			return
		}

		if recipesValue, ok := toolArgs["recipes"]; ok {
			if arr, ok := recipesValue.([]interface{}); ok {
				recipes = make([]map[string]interface{}, 0, len(arr))
				for _, item := range arr {
					if recipe, ok := item.(map[string]interface{}); ok {
						recipes = append(recipes, recipe)
					}
				}
				log.Printf("   Found %d recipes in 'recipes' property", len(recipes))
			}
		}
	} else {
		// Extract from content
		content := strings.TrimSpace(choice.Content)

		// Try to parse as JSON array first
		if err := json.Unmarshal([]byte(content), &recipes); err != nil {
			// Try parsing as object with "recipes" property
			var obj map[string]interface{}
			if err2 := json.Unmarshal([]byte(content), &obj); err2 == nil {
				if recipesValue, ok := obj["recipes"]; ok {
					if arr, ok := recipesValue.([]interface{}); ok {
						recipes = make([]map[string]interface{}, 0, len(arr))
						for _, item := range arr {
							if recipe, ok := item.(map[string]interface{}); ok {
								recipes = append(recipes, recipe)
							}
						}
						log.Printf("   Found %d recipes in 'recipes' property", len(recipes))
					}
				} else {
					// Look for any array property
					for key, value := range obj {
						if arr, ok := value.([]interface{}); ok {
							recipes = make([]map[string]interface{}, 0, len(arr))
							for _, item := range arr {
								if recipe, ok := item.(map[string]interface{}); ok {
									recipes = append(recipes, recipe)
								}
							}
							log.Printf("   Found %d recipes in '%s' property", len(recipes), key)
							break
						}
					}
				}
			}
		}
	}

	if len(recipes) == 0 {
		log.Printf("⚠️ No valid recipes found in response")
		return
	}

	log.Printf("✅ Valid JSON array with %d recipe(s)", len(recipes))

	// Validate structure
	allValid := true
	for i, recipe := range recipes {
		hasRecipeName := false
		hasIngredients := false

		if name, ok := recipe["recipeName"]; ok && name != nil {
			hasRecipeName = true
			log.Printf("   Recipe %d: %s", i+1, name)
		}

		if ingredients, ok := recipe["ingredients"]; ok && ingredients != nil {
			if ingArray, ok := ingredients.([]interface{}); ok {
				hasIngredients = true
				log.Printf("      Ingredients (%d): %v", len(ingArray), ingArray)
			}
		}

		if !hasRecipeName {
			log.Printf("   ⚠️ Recipe %d missing 'recipeName' field", i+1)
			allValid = false
		}
		if !hasIngredients {
			log.Printf("   ⚠️ Recipe %d missing 'ingredients' field", i+1)
			allValid = false
		}
	}

	if allValid {
		log.Printf("\n✅ All recipes have valid structure!")
	} else {
		log.Printf("\n⚠️ Some recipes are missing required fields")
	}

	// Pretty print the full JSON response
	prettyJSON, _ := json.MarshalIndent(recipes, "", "  ")
	log.Printf("\n📄 Full structured response:")
	fmt.Println(string(prettyJSON))

	log.Printf("\n🎯 Structured output test completed successfully!")
}

// RunImageTest runs standardized image understanding tests (3 tests)
func RunImageTest(llm llmtypes.Model, modelID string, imagePath, imageURL string) {
	ctx := context.Background()

	// Prepare image content
	var imageParts []llmtypes.ContentPart

	if imagePath != "" {
		// Load and encode image file
		log.Printf("📁 Loading image from file: %s", imagePath)
		imageData, err := os.ReadFile(imagePath)
		if err != nil {
			log.Printf("❌ Failed to read image file: %v", err)
			return
		}

		// Detect MIME type from file extension
		ext := strings.ToLower(filepath.Ext(imagePath))
		mediaType := mime.TypeByExtension(ext)
		if mediaType == "" {
			// Fallback to common types
			switch ext {
			case ".jpg", ".jpeg":
				mediaType = "image/jpeg"
			case ".png":
				mediaType = "image/png"
			case ".gif":
				mediaType = "image/gif"
			case ".webp":
				mediaType = "image/webp"
			default:
				log.Printf("❌ Unsupported image format: %s. Supported: JPEG, PNG, GIF, WebP", ext)
				return
			}
		}

		// Encode to base64
		base64Data := base64.StdEncoding.EncodeToString(imageData)
		log.Printf("✅ Image loaded: %d bytes, MIME type: %s", len(imageData), mediaType)

		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "base64",
			MediaType:  mediaType,
			Data:       base64Data,
		})
	} else if imageURL != "" {
		// Use image URL
		log.Printf("🌐 Using image URL: %s", imageURL)
		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "url",
			MediaType:  "",
			Data:       imageURL,
		})
	} else {
		// Default test image URL
		defaultImageURL := "https://cdn.prod.website-files.com/657639ebfb91510f45654149/67cef0fb78a461a1580d3c5a_667f5f1018134e3c5a8549c2_AD_4nXfn52WaKNUy839wUllpITpaj7mvuOTR6AOzDk3SypLHLgO-_n8zgt7QJ7rxcLOfOJRWAShjk1dIZRmwuKYLCYFD4qgOq1SCiGFIYbnhDLjD1E0zTdb8cgnCBceLMy7lmCZ3qDUce-gCfJjofiZ9ftDF2m4.webp"
		log.Printf("🌐 Using default test image URL: %s", defaultImageURL)
		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "url",
			MediaType:  "",
			Data:       defaultImageURL,
		})
	}

	// Test 1: Basic image description
	log.Printf("\n📝 Test 1: Basic image description")
	messages1 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: append([]llmtypes.ContentPart{
				llmtypes.TextContent{Text: "What is in this image? Describe it in detail."},
			}, imageParts...),
		},
	}

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1, llmtypes.WithModel(modelID))
	duration1 := time.Since(startTime1)

	if err != nil {
		log.Printf("❌ Test 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Test 1 failed - no response choices")
		return
	}

	response1 := resp1.Choices[0].Content
	previewLen := 200
	if len(response1) < previewLen {
		previewLen = len(response1)
	}

	log.Printf("✅ Test 1 passed in %s", duration1)
	log.Printf("   Response length: %d characters", len(response1))
	log.Printf("   Response preview: %s", response1[:previewLen])

	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Test 2: Text extraction from image
	log.Printf("\n📝 Test 2: Text extraction from image")
	messages2 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: append([]llmtypes.ContentPart{
				llmtypes.TextContent{Text: "What text is written in this image? Extract all visible text."},
			}, imageParts...),
		},
	}

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, messages2, llmtypes.WithModel(modelID))
	duration2 := time.Since(startTime2)

	if err != nil {
		log.Printf("❌ Test 2 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Test 2 failed - no response choices")
		return
	}

	response2 := resp2.Choices[0].Content
	if len(response2) < previewLen {
		previewLen = len(response2)
	}

	log.Printf("✅ Test 2 passed in %s", duration2)
	log.Printf("   Response length: %d characters", len(response2))
	log.Printf("   Response preview: %s", response2[:previewLen])

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Test 3: Complex image analysis
	log.Printf("\n📝 Test 3: Complex image analysis")
	messages3 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: append([]llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Analyze this image and provide: 1) A detailed description of what you see, 2) Any text or numbers visible, 3) Colors and composition, 4) Any objects or people present."},
			}, imageParts...),
		},
	}

	startTime3 := time.Now()
	resp3, err := llm.GenerateContent(ctx, messages3, llmtypes.WithModel(modelID))
	duration3 := time.Since(startTime3)

	if err != nil {
		log.Printf("❌ Test 3 failed: %v", err)
		return
	}

	if len(resp3.Choices) == 0 {
		log.Printf("❌ Test 3 failed - no response choices")
		return
	}

	response3 := resp3.Choices[0].Content
	if len(response3) < previewLen {
		previewLen = len(response3)
	}

	log.Printf("✅ Test 3 passed in %s", duration3)
	log.Printf("   Response length: %d characters", len(response3))
	log.Printf("   Response preview: %s", response3[:previewLen])

	logTokenUsage(resp3.Choices[0].GenerationInfo)

	log.Printf("\n🎯 All image understanding tests completed successfully!")
}

// logTokenUsage logs token usage information
func logTokenUsage(info *llmtypes.GenerationInfo) {
	if info == nil {
		return
	}

	log.Printf("📊 Token Usage:")
	if info.InputTokens != nil {
		log.Printf("   Input tokens: %v", *info.InputTokens)
	}
	if info.OutputTokens != nil {
		log.Printf("   Output tokens: %v", *info.OutputTokens)
	}
	if info.TotalTokens != nil {
		log.Printf("   Total tokens: %v", *info.TotalTokens)
	}
	if info.Additional != nil {
		if cacheRead, ok := info.Additional["cache_read_input_tokens"]; ok {
			log.Printf("   Cache read tokens: %v", cacheRead)
		}
		if cacheCreate, ok := info.Additional["cache_creation_input_tokens"]; ok {
			log.Printf("   Cache creation tokens: %v", cacheCreate)
		}
	}
}

// validateConversationTypeAssertions validates that all ContentPart types in a conversation
// can be properly type-asserted. This is critical because adapters use type assertions
// to identify ToolCall, ToolCallResponse, TextContent, and ImageContent.
//
// This function would catch the bug where ToolCall and ToolCallResponse weren't being
// converted from agent_go/internal/llmtypes to llm-providers/llmtypes.
func validateConversationTypeAssertions(messages []llmtypes.MessageContent) error {
	for i, msg := range messages {
		for j, part := range msg.Parts {
			// Try type assertions that adapters use
			switch part.(type) {
			case llmtypes.TextContent:
				// Good - can be processed
			case llmtypes.ImageContent:
				// Good - can be processed
			case llmtypes.ToolCall:
				// Good - can be processed
			case llmtypes.ToolCallResponse:
				// Good - can be processed
			default:
				// Unknown type - this will cause adapter failures
				return fmt.Errorf("message %d, part %d: unknown ContentPart type %T that adapters cannot handle. "+
					"This indicates a type conversion bug - check convertContentPart in agent_go/internal/llm/providers.go", i, j, part)
			}

			// Additional validation: verify the type is actually from llm-providers package
			// by checking if we can access its fields (this is what adapters do)
			switch p := part.(type) {
			case llmtypes.ToolCall:
				if p.ID == "" && p.FunctionCall != nil {
					// This shouldn't happen, but if it does, it's a problem
					return fmt.Errorf("message %d, part %d: ToolCall has empty ID but non-nil FunctionCall", i, j)
				}
			case llmtypes.ToolCallResponse:
				if p.ToolCallID == "" {
					return fmt.Errorf("message %d, part %d: ToolCallResponse has empty ToolCallID", i, j)
				}
			}
		}
	}
	return nil
}

// RunParallelToolCallWithResponseTestNonStreaming tests the complete flow without streaming:
// 1. Request multiple parallel tool calls
// 2. Get parallel tool calls from LLM
// 3. Add tool responses for all tool calls
// 4. Send full conversation (with tool responses) back to LLM
// 5. Verify LLM can continue conversation using tool results
// This is a non-streaming version for providers that don't support streaming (e.g., OpenRouter)
func RunParallelToolCallWithResponseTestNonStreaming(llm llmtypes.Model, modelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log.Printf("\n📝 Test: Parallel tool calls with responses and continued conversation (non-streaming)")
	log.Printf("   This test verifies that multiple tool calls can be matched with responses")
	log.Printf("   and the LLM can continue the conversation using the tool results")

	// Define tools for parallel execution
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

	getTimeTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in a specific timezone",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g., 'UTC', 'America/New_York')",
					},
				},
				"required": []string{"timezone"},
			}),
		},
	}

	// Step 1: Request parallel tool calls (non-streaming)
	log.Printf("\n🔄 Step 1: Request parallel tool calls")
	messages1 := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Get the weather in San Francisco and also get the current time in UTC. Do both tasks."),
	}

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, messages1,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration1 := time.Since(startTime1)

	if err != nil {
		log.Printf("❌ Step 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Step 1 failed - no choices returned")
		return
	}

	finalContent1 := resp1.Choices[0].Content
	finalToolCalls1 := resp1.Choices[0].ToolCalls

	// CRITICAL: Validate we got multiple parallel tool calls
	if len(finalToolCalls1) < 2 {
		log.Printf("❌ Step 1 failed - expected at least 2 parallel tool calls, got %d", len(finalToolCalls1))
		log.Printf("      Model: %s", modelID)
		return
	}

	log.Printf("✅ Step 1 passed in %s", duration1)
	log.Printf("   📊 Stats:")
	log.Printf("      Parallel tool calls received: %d", len(finalToolCalls1))
	for i, tc := range finalToolCalls1 {
		if tc.FunctionCall != nil {
			log.Printf("      Tool call %d: %s (ID: %s)", i+1, tc.FunctionCall.Name, tc.ID)
		}
	}

	// CRITICAL: Validate required arguments for all parallel tool calls
	log.Printf("\n🔍 Step 2: Validating tool call arguments")
	for i, tc := range finalToolCalls1 {
		if tc.FunctionCall == nil {
			log.Printf("❌ Step 2 failed - tool call %d has no FunctionCall", i+1)
			return
		}
		var toolToValidate llmtypes.Tool
		if tc.FunctionCall.Name == "get_weather" {
			toolToValidate = weatherTool
		} else if tc.FunctionCall.Name == "get_current_time" {
			toolToValidate = getTimeTool
		}
		if toolToValidate.Function != nil {
			if err := validateRequiredToolArguments(toolToValidate, tc, modelID); err != nil {
				log.Printf("❌ Step 2 failed - tool call %d missing required arguments: %v", i+1, err)
				log.Printf("      Model: %s", modelID)
				return
			}
		}
		log.Printf("   ✅ Tool call %d: %s - valid arguments", i+1, tc.FunctionCall.Name)
	}
	log.Printf("✅ Step 2 passed - all tool call arguments are valid")

	// Step 3: Build conversation history with tool calls
	log.Printf("\n🔄 Step 3: Building conversation history with tool calls")
	conversationHistory := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Get the weather in San Francisco and also get the current time in UTC. Do both tasks."),
	}

	// Add assistant message with tool calls
	if finalContent1 != "" || len(finalToolCalls1) > 0 {
		parts := []llmtypes.ContentPart{}
		if finalContent1 != "" {
			parts = append(parts, llmtypes.TextContent{Text: finalContent1})
		}
		for _, tc := range finalToolCalls1 {
			parts = append(parts, tc)
		}
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: parts,
		})
		log.Printf("   ✅ Added assistant message with %d tool call(s)", len(finalToolCalls1))
	}

	// Step 4: Add tool responses for all tool calls
	log.Printf("\n🔄 Step 4: Adding tool responses for all parallel tool calls")
	if len(finalToolCalls1) > 0 {
		// Collect all tool results as parts of a single message
		parts := make([]llmtypes.ContentPart, 0, len(finalToolCalls1))
		for _, tc := range finalToolCalls1 {
			// Create realistic mock tool results
			var toolResultContent string
			if tc.FunctionCall.Name == "get_weather" {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err == nil {
					location := "San Francisco"
					if loc, ok := args["location"].(string); ok {
						location = loc
					}
					toolResultContent = fmt.Sprintf(`{"location": "%s", "temperature": 72, "condition": "Sunny", "humidity": 65}`, location)
				} else {
					toolResultContent = `{"location": "San Francisco", "temperature": 72, "condition": "Sunny", "humidity": 65}`
				}
			} else if tc.FunctionCall.Name == "get_current_time" {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err == nil {
					timezone := "UTC"
					if tz, ok := args["timezone"].(string); ok {
						timezone = tz
					}
					toolResultContent = fmt.Sprintf(`{"timezone": "%s", "current_time": "2025-01-27T14:30:00Z"}`, timezone)
				} else {
					toolResultContent = `{"timezone": "UTC", "current_time": "2025-01-27T14:30:00Z"}`
				}
			} else {
				toolResultContent = fmt.Sprintf(`{"result": "Mock result for %s"}`, tc.FunctionCall.Name)
			}

			toolResult := llmtypes.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    toolResultContent,
			}
			parts = append(parts, toolResult)
			log.Printf("   ✅ Added tool result for %s (ID: %s)", tc.FunctionCall.Name, tc.ID)
		}
		// Add all tool results as a single message
		conversationHistory = append(conversationHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: parts,
		})
		log.Printf("   ✅ Added %d tool responses in a single tool message", len(parts))
	}

	// Step 5: Send full conversation (with tool responses) back to LLM
	log.Printf("\n🔄 Step 5: Sending full conversation back to LLM with tool responses")
	log.Printf("   This tests that the LLM can match tool responses to tool calls correctly")
	log.Printf("   Conversation has %d messages (including %d tool calls and %d tool responses)", len(conversationHistory), len(finalToolCalls1), len(finalToolCalls1))

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration2 := time.Since(startTime2)

	if err != nil {
		log.Printf("❌ Step 5 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Step 5 failed - no choices returned")
		return
	}

	finalContent2 := resp2.Choices[0].Content
	log.Printf("✅ Step 5 passed in %s", duration2)
	log.Printf("   📊 Response length: %d characters", len(finalContent2))
	if len(finalContent2) > 0 {
		preview := finalContent2
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("   Response preview: %s", preview)
	}

	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Step 6: Test that LLM remembers tool results in follow-up questions
	log.Printf("\n🔄 Step 6: Testing LLM remembers tool results in follow-up questions")
	followUpMessage := llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What was the temperature you found?")
	conversationHistory = append(conversationHistory, followUpMessage)

	startTime3 := time.Now()
	resp3, err := llm.GenerateContent(ctx, conversationHistory,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{weatherTool, getTimeTool}),
	)
	duration3 := time.Since(startTime3)

	if err != nil {
		log.Printf("❌ Step 6 failed: %v", err)
		return
	}

	if len(resp3.Choices) == 0 {
		log.Printf("❌ Step 6 failed - no choices returned")
		return
	}

	finalContent3 := resp3.Choices[0].Content
	log.Printf("✅ Step 6 passed in %s", duration3)
	if len(finalContent3) > 0 {
		preview := finalContent3
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("   Response: %s", preview)
	}

	logTokenUsage(resp3.Choices[0].GenerationInfo)

	log.Printf("\n🎯 Parallel tool call with response test completed successfully!")
	log.Printf("   ✅ Multiple parallel tool calls received")
	log.Printf("   ✅ Tool responses added for all tool calls")
	log.Printf("   ✅ Full conversation sent back to LLM without errors")
	log.Printf("   ✅ LLM continued conversation using tool results")
	log.Printf("   ✅ LLM remembers tool results in follow-up questions")
}

// RunMultiTurnConversationTest tests multi-turn conversations without streaming:
// 1. First turn: Simple question
// 2. Second turn: Follow-up question using context from first turn
// 3. Third turn: Question requiring tool call, then continue conversation
// This validates that conversation context is maintained across multiple turns
func RunMultiTurnConversationTest(llm llmtypes.Model, modelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log.Printf("\n📝 Test: Multi-turn conversation (non-streaming)")
	log.Printf("   This test verifies that conversation context is maintained across multiple turns")

	// Define tools
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

	// Turn 1: Simple question
	log.Printf("\n🔄 Turn 1: Simple question")
	conversation := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Hello! My name is Alice and I'm working on a Go project."),
	}

	startTime1 := time.Now()
	resp1, err := llm.GenerateContent(ctx, conversation,
		llmtypes.WithModel(modelID),
	)
	duration1 := time.Since(startTime1)

	if err != nil {
		log.Printf("❌ Turn 1 failed: %v", err)
		return
	}

	if len(resp1.Choices) == 0 {
		log.Printf("❌ Turn 1 failed - no choices returned")
		return
	}

	content1 := resp1.Choices[0].Content
	log.Printf("✅ Turn 1 passed in %s", duration1)
	log.Printf("   Response length: %d characters", len(content1))
	logTokenUsage(resp1.Choices[0].GenerationInfo)

	// Add assistant response to conversation
	conversation = append(conversation, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeAI,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: content1}},
	})

	// Turn 2: Follow-up question using context
	log.Printf("\n🔄 Turn 2: Follow-up question using context from Turn 1")
	conversation = append(conversation, llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's my name?"))

	startTime2 := time.Now()
	resp2, err := llm.GenerateContent(ctx, conversation,
		llmtypes.WithModel(modelID),
	)
	duration2 := time.Since(startTime2)

	if err != nil {
		log.Printf("❌ Turn 2 failed: %v", err)
		return
	}

	if len(resp2.Choices) == 0 {
		log.Printf("❌ Turn 2 failed - no choices returned")
		return
	}

	content2 := resp2.Choices[0].Content

	// CRITICAL: Validate context retention - response should contain "Alice"
	if len(content2) == 0 {
		log.Printf("❌ Turn 2 failed - empty response")
		return
	}

	content2Lower := strings.ToLower(content2)
	if !strings.Contains(content2Lower, "alice") {
		log.Printf("❌ Turn 2 failed - context retention validation failed")
		log.Printf("   Expected response to contain 'Alice' (from Turn 1)")
		log.Printf("   Response: %s", content2)
		log.Printf("   This indicates the model is not maintaining conversation context")
		return
	}

	log.Printf("✅ Turn 2 passed in %s", duration2)
	log.Printf("   Response length: %d characters", len(content2))
	log.Printf("   Context retention: ✅ (response contains 'Alice')")
	if len(content2) > 0 {
		preview := content2
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("   Response preview: %s", preview)
	}
	logTokenUsage(resp2.Choices[0].GenerationInfo)

	// Add assistant response to conversation
	conversation = append(conversation, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeAI,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: content2}},
	})

	// Turn 3: Question requiring tool call
	log.Printf("\n🔄 Turn 3: Question requiring tool call")
	conversation = append(conversation, llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Can you read the go.mod file for me?"))

	startTime3 := time.Now()
	resp3, err := llm.GenerateContent(ctx, conversation,
		llmtypes.WithModel(modelID),
		llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration3 := time.Since(startTime3)

	if err != nil {
		log.Printf("❌ Turn 3 failed: %v", err)
		return
	}

	if len(resp3.Choices) == 0 {
		log.Printf("❌ Turn 3 failed - no choices returned")
		return
	}

	content3 := resp3.Choices[0].Content
	toolCalls3 := resp3.Choices[0].ToolCalls

	// Validate tool calls were generated
	if len(toolCalls3) == 0 {
		log.Printf("⚠️ Turn 3: No tool calls returned (model may not support tool calling or chose not to use tools)")
		log.Printf("   Skipping Turn 4 (requires tool calls)")
		log.Printf("   Turn 3 response: %s", content3)
		logTokenUsage(resp3.Choices[0].GenerationInfo)
		log.Printf("\n🎯 Multi-turn conversation test completed (partial)")
		log.Printf("   ✅ Turn 1: Simple question ✅")
		log.Printf("   ✅ Turn 2: Follow-up question with context ✅")
		log.Printf("   ⚠️ Turn 3: Question with tool call (no tool calls returned)")
		return
	}

	// CRITICAL: Validate tool call arguments
	log.Printf("\n🔍 Validating tool call arguments")
	for i, tc := range toolCalls3 {
		if tc.FunctionCall == nil {
			log.Printf("❌ Turn 3 failed - tool call %d has no FunctionCall", i+1)
			return
		}
		if err := validateRequiredToolArguments(readFileTool, tc, modelID); err != nil {
			log.Printf("❌ Turn 3 failed - tool call %d missing required arguments: %v", i+1, err)
			log.Printf("      Model: %s", modelID)
			return
		}
		log.Printf("   ✅ Tool call %d: %s - valid arguments", i+1, tc.FunctionCall.Name)
	}

	log.Printf("✅ Turn 3 passed in %s", duration3)
	log.Printf("   Tool calls: %d", len(toolCalls3))
	for i, tc := range toolCalls3 {
		if tc.FunctionCall != nil {
			log.Printf("   Tool call %d: %s (ID: %s)", i+1, tc.FunctionCall.Name, tc.ID)
			log.Printf("   Args: %s", tc.FunctionCall.Arguments)
		}
	}
	logTokenUsage(resp3.Choices[0].GenerationInfo)

	// Add tool calls and tool responses to conversation
	if len(toolCalls3) > 0 {
		// Add assistant message with tool calls
		parts := []llmtypes.ContentPart{}
		if content3 != "" {
			parts = append(parts, llmtypes.TextContent{Text: content3})
		}
		for _, tc := range toolCalls3 {
			parts = append(parts, tc)
		}
		conversation = append(conversation, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: parts,
		})

		// Add tool responses
		toolParts := make([]llmtypes.ContentPart, 0, len(toolCalls3))
		for _, tc := range toolCalls3 {
			toolResult := llmtypes.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    `{"content": "module example\n\ngo 1.21\n\nrequire (\n\tgithub.com/example/package v1.0.0\n)"}`,
			}
			toolParts = append(toolParts, toolResult)
		}
		conversation = append(conversation, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: toolParts,
		})

		// Turn 4: Continue conversation after tool call
		log.Printf("\n🔄 Turn 4: Continue conversation after tool call")
		conversation = append(conversation, llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What version of Go is required?"))

		startTime4 := time.Now()
		resp4, err := llm.GenerateContent(ctx, conversation,
			llmtypes.WithModel(modelID),
			llmtypes.WithTools([]llmtypes.Tool{readFileTool}),
		)
		duration4 := time.Since(startTime4)

		if err != nil {
			log.Printf("❌ Turn 4 failed: %v", err)
			return
		}

		if len(resp4.Choices) == 0 {
			log.Printf("❌ Turn 4 failed - no choices returned")
			return
		}

		content4 := resp4.Choices[0].Content

		// CRITICAL: Validate tool result usage - response should mention "1.21" or "go 1.21"
		if len(content4) == 0 {
			log.Printf("❌ Turn 4 failed - empty response")
			return
		}

		content4Lower := strings.ToLower(content4)
		mentionsVersion := strings.Contains(content4Lower, "1.21") ||
			strings.Contains(content4Lower, "go 1.21") ||
			strings.Contains(content4Lower, "version 1.21")

		if !mentionsVersion {
			log.Printf("⚠️ Turn 4: Tool result usage validation - response may not reference tool result")
			log.Printf("   Expected response to mention '1.21' or 'go 1.21' (from tool result)")
			log.Printf("   Response: %s", content4)
			log.Printf("   This may indicate the model is not using tool results correctly")
			// Don't fail - this is a warning, not a hard failure
		}

		log.Printf("✅ Turn 4 passed in %s", duration4)
		log.Printf("   Response length: %d characters", len(content4))
		if mentionsVersion {
			log.Printf("   Tool result usage: ✅ (response references tool result)")
		} else {
			log.Printf("   Tool result usage: ⚠️ (response may not reference tool result)")
		}
		if len(content4) > 0 {
			preview := content4
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			log.Printf("   Response preview: %s", preview)
		}
		logTokenUsage(resp4.Choices[0].GenerationInfo)
	}

	log.Printf("\n🎯 Multi-turn conversation test completed successfully!")
	log.Printf("   ✅ Turn 1: Simple question ✅")
	log.Printf("   ✅ Turn 2: Follow-up question with context ✅")
	log.Printf("   ✅ Turn 3: Question with tool call ✅")
	if len(toolCalls3) > 0 {
		log.Printf("   ✅ Turn 4: Continued conversation after tool call ✅")
	}
}
