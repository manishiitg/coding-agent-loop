package testing

import (
	"context"
	"fmt"
	"log"
	"os"

	"mcp-agent/agent_go/internal/llm"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tmc/langchaingo/llms"
)

var vertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Test Vertex AI (Gemini) with API key and tool calling",
	Run:   runVertex,
}

type vertexTestFlags struct {
	model     string
	apiKey    string
	withTools bool
}

var vertexFlags vertexTestFlags

func init() {
	vertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	vertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
	vertexCmd.Flags().BoolVar(&vertexFlags.withTools, "with-tools", false, "enable tool calling")
}

func runVertex(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// Get API key
	apiKey := vertexFlags.apiKey
	if apiKey == "" {
		if key := os.Getenv("VERTEX_API_KEY"); key != "" {
			apiKey = key
		} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKey = key
		}
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or VERTEX_API_KEY/GOOGLE_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("VERTEX_API_KEY", apiKey)

	ctx := context.Background()

	testType := "plain generation"
	if vertexFlags.withTools {
		testType = "tool calling"
	}
	logger.Info(fmt.Sprintf("🚀 Testing Vertex AI (%s)", testType))

	// Set default model if not specified
	modelID := vertexFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// Initialize Vertex AI LLM using internal provider
	// The internal provider automatically uses vertex.New() which switches to BackendGeminiAPI with API key
	llmInstance, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	// Define a simple weather tool
	weatherTool := llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
	}

	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What's the weather in Tokyo?"),
	}

	// Call with or without tools
	var resp *llms.ContentResponse
	if vertexFlags.withTools {
		resp, err = llmInstance.GenerateContent(ctx, messages,
			llms.WithModel(modelID),
			llms.WithTools([]llms.Tool{weatherTool}))
	} else {
		resp, err = llmInstance.GenerateContent(ctx, messages, llms.WithModel(modelID))
	}

	if err != nil {
		log.Fatalf("❌ Error: %v", err)
	}

	if len(resp.Choices) == 0 {
		log.Fatal("❌ No choices returned")
	}

	choice := resp.Choices[0]

	// Check for tool calls
	if len(choice.ToolCalls) > 0 {
		logger.Info(fmt.Sprintf("✅ Success! Detected %d tool call(s)", len(choice.ToolCalls)))
		for i, toolCall := range choice.ToolCalls {
			logger.Info(fmt.Sprintf("🔧 Tool #%d", i+1), map[string]interface{}{
				"name":      toolCall.FunctionCall.Name,
				"arguments": toolCall.FunctionCall.Arguments,
			})
		}
	} else if len(choice.Content) > 0 {
		logger.Info("✅ Success! Response received", map[string]interface{}{
			"content": choice.Content,
		})
	}
}
