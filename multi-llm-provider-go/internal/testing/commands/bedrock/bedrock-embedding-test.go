package bedrock

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var BedrockEmbeddingTestCmd = &cobra.Command{
	Use:   "bedrock-embedding",
	Short: "Test Bedrock embedding generation",
	Run:   runBedrockEmbeddingTest,
}

type bedrockEmbeddingTestFlags struct {
	model string
}

var bedrockEmbeddingFlags bedrockEmbeddingTestFlags

func init() {
	BedrockEmbeddingTestCmd.Flags().StringVar(&bedrockEmbeddingFlags.model, "model", "", "Bedrock embedding model to test (default: amazon.titan-embed-text-v1)")
}

func runBedrockEmbeddingTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := bedrockEmbeddingFlags.model
	if modelID == "" {
		modelID = "amazon.titan-embed-text-v1"
	}

	log.Printf("🚀 Testing Bedrock Embedding Generation with %s", modelID)

	// Check for AWS credentials (they should be in environment or AWS config)
	if os.Getenv("AWS_REGION") == "" {
		log.Printf("⚠️  AWS_REGION not set, using default region from AWS config")
	}

	// Create Bedrock embedding model
	logger := testing.GetTestLogger()
	embeddingModel, err := llmproviders.InitializeEmbeddingModel(llmproviders.Config{
		Provider: llmproviders.ProviderBedrock,
		ModelID:  modelID,
		Logger:   logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Bedrock embedding model: %v", err)
		return
	}

	// Run shared embedding test
	shared.RunEmbeddingTest(embeddingModel, modelID)
}


