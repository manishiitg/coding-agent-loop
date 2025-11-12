package vertex

import (
	"context"
	"log"
	"os"

	llmproviders "llm-providers"

	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var VertexToolCallTestCmd = &cobra.Command{
	Use:   "vertex-tool-call",
	Short: "Test Vertex AI (Gemini) tool calling",
	Run:   runVertexToolCallTest,
}

type vertexToolCallTestFlags struct {
	model string
}

var vertexToolCallFlags vertexToolCallTestFlags

func init() {
	VertexToolCallTestCmd.Flags().StringVar(&vertexToolCallFlags.model, "model", "", "Vertex AI model to test (default: gemini-2.5-flash)")
}

func runVertexToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := vertexToolCallFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	log.Printf("🚀 Testing Vertex AI Tool Calling with %s", modelID)

	// Check for API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Printf("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
		return
	}

	// Create Vertex AI LLM using our adapter
	logger := testing.GetTestLogger()
	vertexLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     context.Background(),
	})
	if err != nil {
		log.Printf("❌ Failed to create Vertex AI LLM: %v", err)
		return
	}

	// Run shared tool call test
	shared.RunToolCallTest(vertexLLM, modelID)
}
