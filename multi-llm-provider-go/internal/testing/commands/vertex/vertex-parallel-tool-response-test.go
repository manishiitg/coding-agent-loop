package vertex

import (
	"context"
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var VertexParallelToolResponseTestCmd = &cobra.Command{
	Use:   "vertex-parallel-tool-response",
	Short: "Test Vertex AI (Gemini) parallel tool calls with responses and continued conversation",
	Run:   runVertexParallelToolResponseTest,
}

type vertexParallelToolResponseTestFlags struct {
	model string
}

var vertexParallelToolResponseFlags vertexParallelToolResponseTestFlags

func init() {
	VertexParallelToolResponseTestCmd.Flags().StringVar(&vertexParallelToolResponseFlags.model, "model", "", "Vertex AI model to test (default: gemini-3-pro-preview)")
}

func runVertexParallelToolResponseTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := vertexParallelToolResponseFlags.model
	if modelID == "" {
		modelID = "gemini-3-pro-preview"
	}

	log.Printf("🚀 Testing Vertex AI Parallel Tool Calls with Responses using %s", modelID)

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

	// Run parallel tool call with response test
	shared.RunParallelToolCallWithResponseTest(vertexLLM, modelID)
}
