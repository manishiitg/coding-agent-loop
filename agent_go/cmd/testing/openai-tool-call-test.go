package testing

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
)

var openaiToolCallTestCmd = &cobra.Command{
	Use:   "openai-tool-call",
	Short: "Test OpenAI tool calling with new adapter",
	Run:   runOpenAIToolCallTest,
}

type openaiToolCallTestFlags struct {
	model string
}

var openaiToolCallFlags openaiToolCallTestFlags

func init() {
	openaiToolCallTestCmd.Flags().StringVar(&openaiToolCallFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
}

func runOpenAIToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiToolCallFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Tool Calling with %s using new adapter", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	// Create OpenAI LLM using our adapter
	logger := GetTestLogger()
	openaiLLM, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI LLM: %w", err)
		return
	}

	// Define test tool
	tool := llmtypes.Tool{
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

	// Test tool calling with auto tool choice
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Read the contents of go.mod"),
	}

	log.Printf("🔧 Testing tool calling with auto tool choice...")
	startTime := time.Now()
	resp, err := openaiLLM.GenerateContent(ctx, messages,
		llmtypes.WithTools([]llmtypes.Tool{tool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("❌ Tool call failed: %w", err)
		return
	}

	// Validate response
	if len(resp.Choices) == 0 {
		log.Printf("❌ No response choices")
		return
	}

	choice := resp.Choices[0]
	if len(choice.ToolCalls) == 0 {
		log.Printf("❌ No tool calls detected")
		return
	}

	toolCall := choice.ToolCalls[0]
	log.Printf("✅ Tool call successful in %s", duration)
	log.Printf("   Tool: %s", toolCall.FunctionCall.Name)
	log.Printf("   Args: %s", toolCall.FunctionCall.Arguments)

	// Check token usage
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
	}

	// Test second tool call with required tool choice
	secondMessages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Read the contents of README.md"),
	}

	log.Printf("\n🔧 Testing second tool call with required tool choice...")
	secondStartTime := time.Now()
	secondResp, err := openaiLLM.GenerateContent(ctx, secondMessages,
		llmtypes.WithTools([]llmtypes.Tool{tool}),
		llmtypes.WithToolChoiceString("required"),
	)
	secondDuration := time.Since(secondStartTime)

	if err != nil {
		log.Printf("❌ Second tool call failed: %w", err)
		return
	}

	if len(secondResp.Choices) == 0 || len(secondResp.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Second tool call failed - no tool calls detected")
		return
	}

	secondToolCall := secondResp.Choices[0].ToolCalls[0]
	log.Printf("✅ Second tool call successful in %s", secondDuration)
	log.Printf("   Tool: %s", secondToolCall.FunctionCall.Name)
	log.Printf("   Args: %s", secondToolCall.FunctionCall.Arguments)

	// Test with multiple tools
	log.Printf("\n🔧 Testing with multiple tools...")
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

	multiToolMessages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather in San Francisco?"),
	}

	multiToolStartTime := time.Now()
	multiToolResp, err := openaiLLM.GenerateContent(ctx, multiToolMessages,
		llmtypes.WithTools([]llmtypes.Tool{tool, weatherTool}),
		llmtypes.WithToolChoiceString("auto"),
	)
	multiToolDuration := time.Since(multiToolStartTime)

	if err != nil {
		log.Printf("❌ Multiple tool call failed: %w", err)
		return
	}

	if len(multiToolResp.Choices) == 0 || len(multiToolResp.Choices[0].ToolCalls) == 0 {
		log.Printf("❌ Multiple tool call failed - no tool calls detected")
		return
	}

	multiToolCall := multiToolResp.Choices[0].ToolCalls[0]
	log.Printf("✅ Multiple tool call successful in %s", multiToolDuration)
	log.Printf("   Tool: %s", multiToolCall.FunctionCall.Name)
	log.Printf("   Args: %s", multiToolCall.FunctionCall.Arguments)

	log.Printf("\n🎯 All OpenAI tool calling tests completed successfully!")
}
