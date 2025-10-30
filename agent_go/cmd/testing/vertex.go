package testing

import (
	"context"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
)

var vertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Test Vertex AI (Gemini) tool calling",
	Long:  "Test Google Vertex AI LLM with tool calling capabilities",
	Run:   runVertex,
}

// vertexTestFlags holds the vertex test specific flags
type vertexTestFlags struct {
	model        string
	apiKey       string
	verbose      bool
	showResponse bool
}

var vertexFlags vertexTestFlags

func init() {
	// Vertex test specific flags
	vertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	vertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
	vertexCmd.Flags().BoolVar(&vertexFlags.verbose, "verbose", false, "enable verbose output")
	vertexCmd.Flags().BoolVar(&vertexFlags.showResponse, "show-response", true, "show full response")
}

func runVertex(cmd *cobra.Command, args []string) {
	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	logger.Info("🚀 Testing Vertex AI (Gemini) Tool Calling...")

	// Get API key
	apiKey := vertexFlags.apiKey
	if apiKey == "" {
		// Try environment variables
		if key := os.Getenv("VERTEX_API_KEY"); key != "" {
			apiKey = key
		} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKey = key
		}
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or VERTEX_API_KEY/GOOGLE_API_KEY environment variable")
	}

	// Create Google AI LLM with API key
	llm, err := googleai.New(context.Background(),
		googleai.WithAPIKey(apiKey),
		googleai.WithDefaultModel(vertexFlags.model),
	)
	if err != nil {
		log.Fatalf("Failed to create Vertex LLM: %v", err)
	}

	// Define a simple tool
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

	// Test messages
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What's the weather like in Tokyo?"),
	}

	ctx := context.Background()

	// Call with tool
	logger.Info("📞 Calling Vertex AI with tool...")
	resp, err := llm.GenerateContent(ctx, messages,
		llms.WithModel("gemini-2.5-flash"),
		llms.WithTools([]llms.Tool{weatherTool}),
	)
	if err != nil {
		logger.Fatal("❌ Tool call failed", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ Response received", map[string]interface{}{
		"response": resp.Choices[0].Content,
	})
}
