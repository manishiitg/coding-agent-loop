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

var OpenRouterStructuredOutputTestCmd = &cobra.Command{
	Use:   "openrouter-structured-output",
	Short: "Test OpenRouter structured JSON output with JSON mode",
	Run:   runOpenRouterStructuredOutputTest,
}

type openrouterStructuredOutputTestFlags struct {
	model string
}

var openrouterStructuredOutputFlags openrouterStructuredOutputTestFlags

func init() {
	OpenRouterStructuredOutputTestCmd.Flags().StringVar(&openrouterStructuredOutputFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
}

func runOpenRouterStructuredOutputTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterStructuredOutputFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Structured Output with %s", modelID)

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

	// Run shared structured output test with JSON mode
	// useJSONMode=true, useJSONSchema=false, useToolBased=false
	shared.RunStructuredOutputTest(llm, modelID, true, false, false)
}

