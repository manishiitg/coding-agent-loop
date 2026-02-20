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

var OpenRouterParallelToolResponseTestCmd = &cobra.Command{
	Use:   "openrouter-parallel-tool-response",
	Short: "Test OpenRouter parallel tool calls with responses and continued conversation",
	Run:   runOpenRouterParallelToolResponseTest,
}

type openrouterParallelToolResponseTestFlags struct {
	model string
}

var openrouterParallelToolResponseFlags openrouterParallelToolResponseTestFlags

func init() {
	OpenRouterParallelToolResponseTestCmd.Flags().StringVar(&openrouterParallelToolResponseFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
}

func runOpenRouterParallelToolResponseTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterParallelToolResponseFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Parallel Tool Calls with Responses using %s", modelID)

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

	// Run shared parallel tool call with response test (non-streaming)
	shared.RunParallelToolCallWithResponseTestNonStreaming(llm, modelID)
}
