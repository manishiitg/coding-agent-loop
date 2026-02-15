package vertex

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var VertexEmbeddingTestCmd = &cobra.Command{
	Use:   "vertex-embedding",
	Short: "Test Vertex AI embedding generation",
	Run:   runVertexEmbeddingTest,
}

type vertexEmbeddingTestFlags struct {
	model string
}

var vertexEmbeddingFlags vertexEmbeddingTestFlags

func init() {
	VertexEmbeddingTestCmd.Flags().StringVar(&vertexEmbeddingFlags.model, "model", "", "Vertex AI embedding model to test (default: text-embedding-004)")
}

func runVertexEmbeddingTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := vertexEmbeddingFlags.model
	if modelID == "" {
		modelID = "text-embedding-004"
	}

	log.Printf("🚀 Testing Vertex AI Embedding Generation with %s", modelID)

	// Check for API key
	if os.Getenv("VERTEX_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		log.Printf("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
		return
	}

	// Create Vertex AI embedding model
	logger := testing.GetTestLogger()
	embeddingModel, err := llmproviders.InitializeEmbeddingModel(llmproviders.Config{
		Provider: llmproviders.ProviderVertex,
		ModelID:  modelID,
		Logger:   logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Vertex AI embedding model: %v", err)
		return
	}

	// Run shared embedding test
	shared.RunEmbeddingTest(embeddingModel, modelID)
}
