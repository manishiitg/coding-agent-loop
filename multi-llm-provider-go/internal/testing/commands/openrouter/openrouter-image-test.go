package openrouter

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var OpenRouterImageTestCmd = &cobra.Command{
	Use:   "openrouter-image",
	Short: "Test OpenRouter image understanding (vision)",
	Run:   runOpenRouterImageTest,
}

type openrouterImageTestFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var openrouterImageFlags openrouterImageTestFlags

func init() {
	OpenRouterImageTestCmd.Flags().StringVar(&openrouterImageFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
	OpenRouterImageTestCmd.Flags().StringVar(&openrouterImageFlags.imagePath, "image-path", "", "Path to image file (JPEG, PNG, GIF, WebP)")
	OpenRouterImageTestCmd.Flags().StringVar(&openrouterImageFlags.imageURL, "image-url", "", "URL of image to test")
}

func runOpenRouterImageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterImageFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Image Understanding with %s", modelID)

	// Check for API key
	if os.Getenv("OPEN_ROUTER_API_KEY") == "" {
		log.Printf("❌ OPEN_ROUTER_API_KEY environment variable is required")
		return
	}

	// Create OpenRouter LLM using our adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenRouter,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenRouter LLM: %v", err)
		return
	}

	// Run shared image test
	shared.RunImageTest(llm, modelID, openrouterImageFlags.imagePath, openrouterImageFlags.imageURL)
}
