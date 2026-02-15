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

var VertexImageTestCmd = &cobra.Command{
	Use:   "vertex-image",
	Short: "Test Vertex AI image understanding (vision)",
	Run:   runVertexImageTest,
}

type vertexImageTestFlags struct {
	model     string
	imagePath string
	imageURL  string
}

var vertexImageFlags vertexImageTestFlags

func init() {
	VertexImageTestCmd.Flags().StringVar(&vertexImageFlags.model, "model", "", "Vertex AI model to test (default: gemini-2.5-flash)")
	VertexImageTestCmd.Flags().StringVar(&vertexImageFlags.imagePath, "image-path", "", "Path to image file (JPEG, PNG, GIF, WebP)")
	VertexImageTestCmd.Flags().StringVar(&vertexImageFlags.imageURL, "image-url", "", "URL of image to test")
}

func runVertexImageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := vertexImageFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	log.Printf("🚀 Testing Vertex AI Image Understanding with %s", modelID)

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
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
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

	// Run shared image test
	shared.RunImageTest(llm, modelID, vertexImageFlags.imagePath, vertexImageFlags.imageURL)
}
