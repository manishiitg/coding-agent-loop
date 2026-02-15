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

var AnthropicStructuredOutputTestCmd = &cobra.Command{
	Use:   "anthropic-structured-output",
	Short: "Test Anthropic structured JSON output using tool-based approach",
	Run:   runAnthropicStructuredOutputTest,
}

type anthropicStructuredOutputTestFlags struct {
	model string
}

var anthropicStructuredOutputFlags anthropicStructuredOutputTestFlags

func init() {
	AnthropicStructuredOutputTestCmd.Flags().StringVar(&anthropicStructuredOutputFlags.model, "model", "", "Anthropic model to test (default: claude-haiku-4-5-20251001)")
}

func runAnthropicStructuredOutputTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := anthropicStructuredOutputFlags.model
	if modelID == "" {
		modelID = "claude-haiku-4-5-20251001"
	}

	log.Printf("🚀 Testing Anthropic Structured Output with %s", modelID)

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

	// Run shared structured output test with tool-based approach
	// useJSONMode=false, useJSONSchema=false, useToolBased=true
	shared.RunStructuredOutputTest(llm, modelID, false, false, true)
}
