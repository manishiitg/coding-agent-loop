package testing

import (
	"context"
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
)

var bedrockCmd = &cobra.Command{
	Use:   "bedrock",
	Short: "Test Bedrock tool calling",
	Long:  "Test AWS Bedrock LLM with tool calling capabilities",
	Run:   runBedrock,
}

// bedrockTestFlags holds the bedrock test specific flags
type bedrockTestFlags struct {
	model        string
	region       string
	verbose      bool
	showResponse bool
}

var bedrockFlags bedrockTestFlags

func init() {
	// Bedrock test specific flags
	bedrockCmd.Flags().StringVar(&bedrockFlags.model, "model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
	bedrockCmd.Flags().StringVar(&bedrockFlags.region, "region", "us-east-1", "AWS region for Bedrock")
	bedrockCmd.Flags().BoolVar(&bedrockFlags.verbose, "verbose", false, "enable verbose output")
	bedrockCmd.Flags().BoolVar(&bedrockFlags.showResponse, "show-response", true, "show full response")
}

func runBedrock(cmd *cobra.Command, args []string) {
	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	logger.Info("🚀 Testing Bedrock Tool Calling...")

	// Use model ID from flags (default is already set to the new model)
	modelID := bedrockFlags.model
	if modelID == "" {
		// Fallback to the new model if not specified
		modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
	}

	// Create Bedrock LLM using new adapter
	llm, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to create Bedrock LLM: %v", err)
	}

	// Define a simple tool
	weatherTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
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

	// Test messages
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather like in Tokyo?"),
	}

	ctx := context.Background()

	// Call with tool
	logger.Info("📞 Calling Bedrock with tool...")
	resp, err := llm.GenerateContent(ctx, messages,
		llmtypes.WithTools([]llmtypes.Tool{weatherTool}),
		llmtypes.WithToolChoiceString("required"),
	)
	if err != nil {
		logger.Fatal("❌ Tool call failed", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ Response received", map[string]interface{}{
		"response": resp.Choices[0].Content,
	})
}
