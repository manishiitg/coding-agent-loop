package bedrock

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var BedrockImageTestCmd = &cobra.Command{
	Use:   "bedrock-image",
	Short: "Test Bedrock image understanding (vision)",
	Run:   runBedrockImageTest,
}

type bedrockImageTestFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var bedrockImageFlags bedrockImageTestFlags

func init() {
	BedrockImageTestCmd.Flags().StringVar(&bedrockImageFlags.model, "model", "", "Bedrock model to test")
	BedrockImageTestCmd.Flags().StringVar(&bedrockImageFlags.imagePath, "image-path", "", "Path to image file (JPEG, PNG, GIF, WebP)")
	BedrockImageTestCmd.Flags().StringVar(&bedrockImageFlags.imageURL, "image-url", "", "URL of image to test")
}

func runBedrockImageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := bedrockImageFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing Bedrock Image Understanding with %s", modelID)

	// Create Bedrock LLM using our adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Bedrock LLM: %v", err)
		return
	}

	// Run shared image test
	shared.RunImageTest(llm, modelID, bedrockImageFlags.imagePath, bedrockImageFlags.imageURL)
}
