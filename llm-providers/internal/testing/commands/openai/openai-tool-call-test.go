package openai

import (
	"log"
	"os"

	llmproviders "llm-providers"

	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var OpenAIToolCallTestCmd = &cobra.Command{
	Use:   "openai-tool-call",
	Short: "Test OpenAI tool calling",
	Run:   runOpenAIToolCallTest,
}

type openaiToolCallTestFlags struct {
	model string
}

var openaiToolCallFlags openaiToolCallTestFlags

func init() {
	OpenAIToolCallTestCmd.Flags().StringVar(&openaiToolCallFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
}

func runOpenAIToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiToolCallFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Tool Calling with %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	// Create OpenAI LLM using our adapter
	logger := testing.GetTestLogger()
	openaiLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI LLM: %v", err)
		return
	}

	// Run shared tool call test
	shared.RunToolCallTest(openaiLLM, modelID)
}
