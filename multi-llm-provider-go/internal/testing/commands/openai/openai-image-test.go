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

var OpenAIImageTestCmd = &cobra.Command{
	Use:   "openai-image",
	Short: "Test OpenAI image understanding (vision)",
	Run:   runOpenAIImageTest,
}

type openaiImageTestFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var openaiImageFlags openaiImageTestFlags

func init() {
	OpenAIImageTestCmd.Flags().StringVar(&openaiImageFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
	OpenAIImageTestCmd.Flags().StringVar(&openaiImageFlags.imagePath, "image-path", "", "Path to image file (JPEG, PNG, GIF, WebP)")
	OpenAIImageTestCmd.Flags().StringVar(&openaiImageFlags.imageURL, "image-url", "", "URL of image to test")
}

func runOpenAIImageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiImageFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Image Understanding with %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	// Create OpenAI LLM using our adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI LLM: %v", err)
		return
	}

	// Run shared image test
	shared.RunImageTest(llm, modelID, openaiImageFlags.imagePath, openaiImageFlags.imageURL)
}
