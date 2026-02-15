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

var VertexStructuredOutputTestCmd = &cobra.Command{
	Use:   "vertex-structured-output",
	Short: "Test Vertex AI structured JSON output with JSON mode",
	Run:   runVertexStructuredOutputTest,
}

type vertexStructuredOutputTestFlags struct {
	model string
}

var vertexStructuredOutputFlags vertexStructuredOutputTestFlags

func init() {
	VertexStructuredOutputTestCmd.Flags().StringVar(&vertexStructuredOutputFlags.model, "model", "", "Vertex AI model to test (default: gemini-2.5-flash)")
}

func runVertexStructuredOutputTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := vertexStructuredOutputFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	log.Printf("🚀 Testing Vertex AI Structured Output with %s", modelID)

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

	// Run shared structured output test with JSON mode
	// useJSONMode=true, useJSONSchema=false, useToolBased=false
	shared.RunStructuredOutputTest(llm, modelID, true, false, false)
}
