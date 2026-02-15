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

var OpenAIStreamingToolCallTestCmd = &cobra.Command{
	Use:   "openai-streaming-tool-call",
	Short: "Test OpenAI streaming with tool calling",
	Run:   runOpenAIStreamingToolCallTest,
}

type openaiStreamingToolCallTestFlags struct {
	model string
}

var openaiStreamingToolCallFlags openaiStreamingToolCallTestFlags

func init() {
	OpenAIStreamingToolCallTestCmd.Flags().StringVar(&openaiStreamingToolCallFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
}

func runOpenAIStreamingToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiStreamingToolCallFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Streaming Tool Calling with %s", modelID)

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

	// Run shared streaming tool call test
	shared.RunStreamingToolCallTest(openaiLLM, modelID)
}
