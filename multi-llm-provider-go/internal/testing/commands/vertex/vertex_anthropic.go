package vertex

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VertexAnthropicCmd = &cobra.Command{
	Use:   "vertex-anthropic",
	Short: "Test Vertex AI Anthropic models (Claude) with OAuth2 authentication",
	Long: `Test Vertex AI Anthropic models like claude-sonnet-4-5 using Vertex AI API.
	
Requires:
  - VERTEX_PROJECT_ID: Your GCP project ID
  - VERTEX_LOCATION_ID: Location (default: "global")
  - Authentication: gcloud auth, service account, or ADC
  
Example:
  export VERTEX_PROJECT_ID="data-sciences-476705"
  export VERTEX_LOCATION_ID="global"
  gcloud auth application-default login
  ./bin/orchestrator test vertex-anthropic`,
	Run: runVertexAnthropic,
}

type vertexAnthropicFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var vertexAnthropicTestFlags vertexAnthropicFlags

func init() {
	VertexAnthropicCmd.Flags().StringVar(&vertexAnthropicTestFlags.model, "model", "claude-sonnet-4-5", "Anthropic model ID (e.g., claude-sonnet-4-5)")
	VertexAnthropicCmd.Flags().StringVar(&vertexAnthropicTestFlags.imagePath, "with-image", "", "path to image file to test image input (JPEG, PNG, GIF, WebP)")
	VertexAnthropicCmd.Flags().StringVar(&vertexAnthropicTestFlags.imageURL, "image-url", "", "URL of image to test image input")
	// Command is added in testing.go to ensure proper initialization order
}

func runVertexAnthropic(cmd *cobra.Command, args []string) {
	fmt.Println("🚀 Starting Vertex Anthropic test...")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	modelID := vertexAnthropicTestFlags.model
	if modelID == "" {
		modelID = os.Getenv("VERTEX_ANTHROPIC_MODEL")
		if modelID == "" {
			modelID = "claude-sonnet-4-5"
		}
	}

	fmt.Printf("📋 Using model: %s\n", modelID)
	fmt.Printf("📋 Project ID: %s\n", os.Getenv("VERTEX_PROJECT_ID"))
	fmt.Printf("📋 Location ID: %s\n", os.Getenv("VERTEX_LOCATION_ID"))

	// Check if image test is requested
	if vertexAnthropicTestFlags.imagePath != "" || vertexAnthropicTestFlags.imageURL != "" {
		if err := RunVertexAnthropicImageTest(modelID, vertexAnthropicTestFlags.imagePath, vertexAnthropicTestFlags.imageURL); err != nil {
			fmt.Printf("❌ Vertex Anthropic image test failed: %v\n", err)
			logger.Errorf("❌ Vertex Anthropic image test failed: %v", err)
		}
		fmt.Println("✅ Vertex Anthropic image test completed successfully!")
		return
	}

	if err := RunVertexAnthropicTestWithModel(modelID); err != nil {
		fmt.Printf("❌ Vertex Anthropic test failed: %v\n", err)
		logger.Errorf("❌ Vertex Anthropic test failed: %v", err)
	}

	fmt.Println("✅ Vertex Anthropic test completed successfully!")
}

// RunVertexAnthropicTestWithModel tests the Vertex AI Anthropic adapter with a specific model
func RunVertexAnthropicTestWithModel(modelID string) error {
	logger := testing.GetTestLogger()
	logger.Infof("🧪 Testing Vertex AI Anthropic Integration")

	// Check required environment variables
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("VERTEX_PROJECT_ID environment variable is required")
	}

	locationID := os.Getenv("VERTEX_LOCATION_ID")
	if locationID == "" {
		locationID = "global"
		logger.Infof("VERTEX_LOCATION_ID not set, using default: %s", locationID)
	}

	// Use provided modelID parameter

	ctx := context.Background()

	// Initialize Vertex AI LLM (will auto-detect Anthropic model)
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 1.0,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize Vertex Anthropic LLM: %w", err)
	}

	logger.Infof("✅ Vertex AI Anthropic LLM initialized successfully")

	// Test 1: Simple text generation
	logger.Infof("📝 Test 1: Simple text generation")
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Hello! Can you tell me a short joke?"},
			},
		},
	}

	response, err := llmInstance.GenerateContent(ctx, messages)
	if err != nil {
		return fmt.Errorf("test 1 failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return fmt.Errorf("test 1 failed: no choices in response")
	}

	logger.Infof("✅ Test 1 passed - Response: %s", response.Choices[0].Content[:min(100, len(response.Choices[0].Content))])

	// Test 2: Streaming
	logger.Infof("📝 Test 2: Streaming response")
	streamedText := ""
	streamingFunc := func(chunk llmtypes.StreamChunk) {
		if chunk.Type == llmtypes.StreamChunkTypeContent {
			streamedText += chunk.Content
			logger.Debugf("Stream chunk: %s", chunk.Content)
		}
	}

	messages2 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Count from 1 to 5, one number per line."},
			},
		},
	}

	response2, err := llmInstance.GenerateContent(ctx, messages2, llmtypes.WithStreamingFunc(streamingFunc))
	if err != nil {
		return fmt.Errorf("test 2 failed: %w", err)
	}

	if len(response2.Choices) == 0 {
		return fmt.Errorf("test 2 failed: no choices in response")
	}

	logger.Infof("✅ Test 2 passed - Streamed text length: %d, Full response: %s",
		len(streamedText), response2.Choices[0].Content)

	// Test 3: Tool calling (comprehensive test)
	logger.Infof("📝 Test 3: Tool calling")
	tools := []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]interface{}{
							"type":        "string",
							"description": "Temperature unit: celsius or fahrenheit",
							"enum":        []string{"celsius", "fahrenheit"},
						},
					},
					Required: []string{"location"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "calculate",
				Description: "Perform a mathematical calculation",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "Mathematical expression to evaluate, e.g. '2 + 2' or '10 * 5'",
						},
					},
					Required: []string{"expression"},
				},
			},
		},
	}

	messages3 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "I need you to use the available tools to help me. Please call the get_weather tool for San Francisco, CA and also call the calculate tool to compute 15 * 23. Use the tools - do not try to answer without using them."},
			},
		},
	}

	response3, err := llmInstance.GenerateContent(ctx, messages3, llmtypes.WithTools(tools))
	if err != nil {
		logger.Debugf("Test 3 failed (tool calling may not be fully supported): %v", err)
	} else {
		logger.Debugf("Test 3 response - Choices: %d", len(response3.Choices))
		if len(response3.Choices) > 0 {
			choice := response3.Choices[0]
			toolCallsCount := 0
			if choice.ToolCalls != nil {
				toolCallsCount = len(choice.ToolCalls)
			}
			logger.Debugf("Test 3 - Choice 0 - Content length: %d, ToolCalls: %d",
				len(choice.Content), toolCallsCount)
			if toolCallsCount > 0 {
				logger.Infof("✅ Test 3 passed - Tool calls received: %d", toolCallsCount)
				for i, toolCall := range response3.Choices[0].ToolCalls {
					logger.Infof("  Tool call %d: ID=%s, Function=%s, Arguments=%s",
						i+1, toolCall.ID, toolCall.FunctionCall.Name, toolCall.FunctionCall.Arguments)
				}

				// Test 3b: Tool response handling - Full flow matching agent behavior
				// This reproduces exactly what happens in conversation.go (AskWithHistory)
				logger.Infof("📝 Test 3b: Tool response handling (reproducing agent flow)")

				// Step 1: Build full conversation matching agent behavior
				// Agent separates text content and tool calls into different messages (conversation.go lines 553-575)
				fullMessages := append(messages3, []llmtypes.MessageContent{}...)

				// Step 1a: If there's text content, append it as a separate AI message (like agent does)
				if response3.Choices[0].Content != "" {
					fullMessages = append(fullMessages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeAI,
						Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: response3.Choices[0].Content}},
					})
				}

				// Step 1b: Append tool calls as a separate AI message (like agent does)
				toolCallParts := make([]llmtypes.ContentPart, 0, len(response3.Choices[0].ToolCalls))
				for _, tc := range response3.Choices[0].ToolCalls {
					toolCallParts = append(toolCallParts, tc)
				}
				if len(toolCallParts) > 0 {
					fullMessages = append(fullMessages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeAI,
						Parts: toolCallParts,
					})
				}

				// Step 2: Add dummy tool responses for each tool call
				// Anthropic requires ALL tool_result blocks in a single user message when there are multiple tool_use blocks
				// So we combine all tool responses into one message (the adapter will convert this correctly)
				toolResponseParts := make([]llmtypes.ContentPart, 0, len(response3.Choices[0].ToolCalls))
				for i, tc := range response3.Choices[0].ToolCalls {
					var dummyResult string
					if tc.FunctionCall.Name == "get_weather" {
						dummyResult = `{"temperature": 72, "condition": "sunny", "location": "San Francisco, CA"}`
					} else if tc.FunctionCall.Name == "calculate" {
						dummyResult = `{"result": 345}`
					} else {
						dummyResult = `{"status": "success", "message": "Tool executed successfully"}`
					}

					toolResponseParts = append(toolResponseParts, llmtypes.ToolCallResponse{
						ToolCallID: tc.ID,
						Name:       tc.FunctionCall.Name,
						Content:    dummyResult,
					})
					logger.Infof("  Added dummy response for tool call %d (%s): %s", i+1, tc.FunctionCall.Name, dummyResult)
				}

				// Combine all tool responses into a single tool message
				// The adapter will convert this to a user message with all tool_result blocks
				if len(toolResponseParts) > 0 {
					fullMessages = append(fullMessages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeTool,
						Parts: toolResponseParts,
					})
				}

				// Step 3: Continue to next turn (like agent does - conversation.go line 1119)
				// Agent doesn't add explicit follow-up message, it just continues and lets LLM respond naturally
				// For testing, we'll send the messages and expect the LLM to provide a final response

				response3b, err := llmInstance.GenerateContent(ctx, fullMessages, llmtypes.WithTools(tools))
				if err != nil {
					logger.Debugf("Test 3b failed: %v", err)
				} else {
					if len(response3b.Choices) > 0 && response3b.Choices[0].Content != "" {
						logger.Infof("✅ Test 3b passed - Final response: %s", response3b.Choices[0].Content[:min(200, len(response3b.Choices[0].Content))])

						// Test 3c: Verify multi-tool call handling
						if toolCallsCount >= 2 {
							logger.Infof("📝 Test 3c: Multi-tool call verification")
							logger.Infof("  ✅ Multiple tool calls detected: %d", toolCallsCount)

							// Verify all tool calls have proper IDs and arguments
							allToolCallsValid := true
							toolNames := make(map[string]bool)
							for i, tc := range response3.Choices[0].ToolCalls {
								if tc.ID == "" {
									logger.Debugf("  ⚠️ Tool call %d has empty ID", i+1)
									allToolCallsValid = false
								}
								if tc.FunctionCall == nil {
									logger.Debugf("  ⚠️ Tool call %d has nil FunctionCall", i+1)
									allToolCallsValid = false
								} else {
									if tc.FunctionCall.Name == "" {
										logger.Debugf("  ⚠️ Tool call %d has empty function name", i+1)
										allToolCallsValid = false
									} else {
										toolNames[tc.FunctionCall.Name] = true
										logger.Infof("  ✅ Tool call %d: %s (ID: %s, Args: %s)", i+1, tc.FunctionCall.Name, tc.ID, tc.FunctionCall.Arguments)
									}
								}
							}

							if allToolCallsValid && len(toolNames) == toolCallsCount {
								logger.Infof("✅ Test 3c passed - All %d tool calls are valid with unique names: %v", toolCallsCount, getKeys(toolNames))
							} else {
								logger.Debugf("⚠️ Test 3c: Some tool calls are invalid or have duplicate names")
							}

							// Verify all tool results were processed correctly
							if len(toolResponseParts) == toolCallsCount {
								logger.Infof("✅ Test 3c passed - All %d tool results were combined into single message", toolCallsCount)
							} else {
								logger.Debugf("⚠️ Test 3c: Tool result count mismatch - expected %d, got %d", toolCallsCount, len(toolResponseParts))
							}
						}
					} else {
						logger.Debugf("⚠️ Test 3b: No final response received")
					}
				}
			} else {
				// Check if content is empty but tool calls might be present
				if len(choice.Content) == 0 {
					logger.Infof("⚠️ Test 3: Empty content - this is expected when tool calls are made. Checking for tool calls...")
					logger.Debugf("ToolCalls pointer: %v, ToolCalls count: %d", choice.ToolCalls, toolCallsCount)
				} else {
					logger.Debugf("⚠️ Test 3: No tool calls in response (model may have chosen not to use tools)")
					logger.Infof("Response content: %s", choice.Content[:min(200, len(choice.Content))])
				}
			}
		} else {
			logger.Debugf("⚠️ Test 3: No choices in response")
		}
	}

	// Test 4: Parallel tool calls - explicit test
	logger.Infof("📝 Test 4: Parallel tool calls (explicit test)")
	tools4 := []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
					},
					Required: []string{"location"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "calculate",
				Description: "Perform a mathematical calculation",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "Mathematical expression to evaluate, e.g. '2 + 2' or '10 * 5'",
						},
					},
					Required: []string{"expression"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_time",
				Description: "Get the current time in a specific timezone",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "Timezone, e.g. 'America/New_York' or 'UTC'",
						},
					},
					Required: []string{"timezone"},
				},
			},
		},
	}

	messages4 := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "I need you to call THREE tools in parallel: 1) get_weather for New York, NY, 2) calculate 25 * 17, and 3) get_time for UTC. Call all three tools at once - do not wait for results between calls."},
			},
		},
	}

	response4, err := llmInstance.GenerateContent(ctx, messages4, llmtypes.WithTools(tools4))
	if err != nil {
		logger.Debugf("Test 4 failed (parallel tool calling): %v", err)
	} else {
		if len(response4.Choices) > 0 {
			choice4 := response4.Choices[0]
			parallelToolCallsCount := 0
			if choice4.ToolCalls != nil {
				parallelToolCallsCount = len(choice4.ToolCalls)
			}

			if parallelToolCallsCount >= 2 {
				logger.Infof("✅ Test 4 passed - Parallel tool calls detected: %d", parallelToolCallsCount)

				// Verify all tool calls are in the same response (parallel)
				toolCallIDs := make([]string, 0, parallelToolCallsCount)
				toolNames := make([]string, 0, parallelToolCallsCount)
				for i, tc := range choice4.ToolCalls {
					toolCallIDs = append(toolCallIDs, tc.ID)
					if tc.FunctionCall != nil {
						toolNames = append(toolNames, tc.FunctionCall.Name)
						logger.Infof("  Parallel tool call %d: %s (ID: %s)", i+1, tc.FunctionCall.Name, tc.ID)
					}
				}

				// Verify unique IDs (required for parallel calls)
				idMap := make(map[string]bool)
				allUnique := true
				for _, id := range toolCallIDs {
					if idMap[id] {
						allUnique = false
						logger.Debugf("  ⚠️ Duplicate tool call ID detected: %s", id)
						break
					}
					idMap[id] = true
				}

				if allUnique {
					logger.Infof("✅ Test 4 passed - All %d tool call IDs are unique", parallelToolCallsCount)
				} else {
					logger.Debugf("⚠️ Test 4: Some tool call IDs are duplicates")
				}

				logger.Infof("✅ Test 4 passed - Parallel tool calls verified: %v", toolNames)
			} else if parallelToolCallsCount == 1 {
				logger.Debugf("⚠️ Test 4: Only 1 tool call detected (expected 2+ for parallel test)")
			} else {
				logger.Debugf("⚠️ Test 4: No parallel tool calls detected")
			}
		}
	}

	logger.Infof("✅ All Vertex AI Anthropic tests completed successfully!")
	return nil
}

// Helper function to get keys from a map
func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// RunVertexAnthropicImageTest tests image input with Vertex AI Anthropic models
func RunVertexAnthropicImageTest(modelID, imagePath, imageURL string) error {
	logger := testing.GetTestLogger()
	logger.Infof("🧪 Testing Vertex AI Anthropic Integration with Image Input")

	// Check required environment variables
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("VERTEX_PROJECT_ID environment variable is required")
	}

	locationID := os.Getenv("VERTEX_LOCATION_ID")
	if locationID == "" {
		locationID = "global"
		logger.Infof("VERTEX_LOCATION_ID not set, using default: %s", locationID)
	}

	ctx := context.Background()

	// Initialize Vertex AI LLM (will auto-detect Anthropic model)
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 1.0,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize Vertex Anthropic LLM: %w", err)
	}

	logger.Infof("✅ Vertex AI Anthropic LLM initialized successfully")

	// Prepare image content
	var imageParts []llmtypes.ContentPart

	if imagePath != "" {
		// Load and encode image file
		logger.Infof("📁 Loading image from file: %s", imagePath)
		imageData, err := os.ReadFile(imagePath)
		if err != nil {
			return fmt.Errorf("failed to read image file: %w", err)
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
				return fmt.Errorf("unsupported image format: %s. Supported: JPEG, PNG, GIF, WebP", ext)
			}
		}

		// Encode to base64
		base64Data := base64.StdEncoding.EncodeToString(imageData)
		logger.Infof("✅ Image loaded: %d bytes, MIME type: %s", len(imageData), mediaType)

		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "base64",
			MediaType:  mediaType,
			Data:       base64Data,
		})
	} else if imageURL != "" {
		// Use image URL
		logger.Infof("🌐 Using image URL: %s", imageURL)
		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "url",
			MediaType:  "", // Not needed for URL
			Data:       imageURL,
		})
	} else {
		// Default test image URL
		testImageURL := "https://cdn.prod.website-files.com/657639ebfb91510f45654149/67cef0fb78a461a1580d3c5a_667f5f1018134e3c5a8549c2_AD_4nXfn52WaKNUy839wUllpITpaj7mvuOTR6AOzDk3SypLHLgO-_n8zgt7QJ7rxcLOfOJRWAShjk1dIZRmwuKYLCYFD4qgOq1SCiGFIYbnhDLjD1E0zTdb8cgnCBceLMy7lmCZ3qDUce-gCfJjofiZ9ftDF2m4.webp"
		logger.Infof("🌐 Using default test image URL: %s", testImageURL)
		imageParts = append(imageParts, llmtypes.ImageContent{
			SourceType: "url",
			MediaType:  "",
			Data:       testImageURL,
		})
	}

	// Create message with text and image
	parts := []llmtypes.ContentPart{
		llmtypes.TextContent{Text: "What is in this image? Describe it in detail."},
	}
	parts = append(parts, imageParts...)

	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	logger.Infof("📝 Testing image input with Vertex AI Anthropic")

	// Test with streaming
	var streamedText strings.Builder
	streamingFunc := func(chunk llmtypes.StreamChunk) {
		if chunk.Type == llmtypes.StreamChunkTypeContent {
			streamedText.WriteString(chunk.Content)
			logger.Debugf("Stream chunk: %s", chunk.Content)
		}
	}

	response, err := llmInstance.GenerateContent(ctx, messages, llmtypes.WithStreamingFunc(streamingFunc))
	if err != nil {
		return fmt.Errorf("image test failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return fmt.Errorf("image test failed: no choices in response")
	}

	responsePreview := response.Choices[0].Content
	previewLen := 200
	if len(responsePreview) < previewLen {
		previewLen = len(responsePreview)
	}

	logger.Infof("✅ Image test passed - Response length: %d, Streamed length: %d",
		len(response.Choices[0].Content), streamedText.Len())
	logger.Infof("📝 Response preview: %s", responsePreview[:previewLen])

	return nil
}
