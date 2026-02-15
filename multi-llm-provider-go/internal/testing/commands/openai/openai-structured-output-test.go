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

var OpenAIStructuredOutputTestCmd = &cobra.Command{
	Use:   "openai-structured-output",
	Short: "Test OpenAI structured JSON output with JSON Schema",
	Run:   runOpenAIStructuredOutputTest,
}

type openaiStructuredOutputTestFlags struct {
	model string
}

var openaiStructuredOutputFlags openaiStructuredOutputTestFlags

func init() {
	OpenAIStructuredOutputTestCmd.Flags().StringVar(&openaiStructuredOutputFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
}

func runOpenAIStructuredOutputTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiStructuredOutputFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Structured Output with %s", modelID)

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

	// Run shared structured output test with JSON Schema approach
	// useJSONMode=false, useJSONSchema=true, useToolBased=false
	shared.RunStructuredOutputTest(llm, modelID, false, true, false)
}
