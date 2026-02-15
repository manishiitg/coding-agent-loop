package anthropic

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var AnthropicImageTestCmd = &cobra.Command{
	Use:   "anthropic-image",
	Short: "Test Anthropic image understanding (vision)",
	Run:   runAnthropicImageTest,
}

type anthropicImageTestFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var anthropicImageFlags anthropicImageTestFlags

func init() {
	AnthropicImageTestCmd.Flags().StringVar(&anthropicImageFlags.model, "model", "", "Anthropic model to test (default: claude-sonnet-4-5-20250929)")
	AnthropicImageTestCmd.Flags().StringVar(&anthropicImageFlags.imagePath, "image-path", "", "Path to image file (JPEG, PNG, GIF, WebP)")
	AnthropicImageTestCmd.Flags().StringVar(&anthropicImageFlags.imageURL, "image-url", "", "URL of image to test")
}

func runAnthropicImageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := anthropicImageFlags.model
	if modelID == "" {
		modelID = "claude-sonnet-4-5-20250929"
	}

	log.Printf("🚀 Testing Anthropic Image Understanding with %s", modelID)

	// Check for API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Printf("❌ ANTHROPIC_API_KEY environment variable is required")
		return
	}

	// Create Anthropic LLM using our adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAnthropic,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Anthropic LLM: %v", err)
		return
	}

	// Run shared image test
	shared.RunImageTest(llm, modelID, anthropicImageFlags.imagePath, anthropicImageFlags.imageURL)
}
