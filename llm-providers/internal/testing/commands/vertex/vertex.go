package vertex

import (
	"context"
	"log"
	"os"

	llmproviders "llm-providers"

	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Test Vertex AI (Gemini) plain text generation",
	Run:   runVertex,
}

type vertexTestFlags struct {
	model  string
	apiKey string
}

var vertexFlags vertexTestFlags

func init() {
	VertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	VertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
}

func runVertex(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

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

	// Set default model if not specified
	modelID := vertexFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// Initialize Vertex AI LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	// Run shared plain text test
	shared.RunPlainTextTest(llmInstance, modelID)
}
