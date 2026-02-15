package openai

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var OpenAIEmbeddingTestCmd = &cobra.Command{
	Use:   "openai-embedding",
	Short: "Test OpenAI embedding generation",
	Run:   runOpenAIEmbeddingTest,
}

type openaiEmbeddingTestFlags struct {
	model string
}

var openaiEmbeddingFlags openaiEmbeddingTestFlags

func init() {
	OpenAIEmbeddingTestCmd.Flags().StringVar(&openaiEmbeddingFlags.model, "model", "", "OpenAI embedding model to test (default: text-embedding-3-small)")
}

func runOpenAIEmbeddingTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiEmbeddingFlags.model
	if modelID == "" {
		modelID = "text-embedding-3-small"
	}

	log.Printf("🚀 Testing OpenAI Embedding Generation with %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	// Create OpenAI embedding model
	logger := testing.GetTestLogger()
	embeddingModel, err := llmproviders.InitializeEmbeddingModel(llmproviders.Config{
		Provider: llmproviders.ProviderOpenAI,
		ModelID:  modelID,
		Logger:   logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI embedding model: %v", err)
		return
	}

	// Run shared embedding test
	shared.RunEmbeddingTest(embeddingModel, modelID)
}
