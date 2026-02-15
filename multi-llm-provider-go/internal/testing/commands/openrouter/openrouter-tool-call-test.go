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

var OpenRouterToolCallTestCmd = &cobra.Command{
	Use:   "openrouter-tool-call",
	Short: "Test OpenRouter tool calling",
	Run:   runOpenRouterToolCallTest,
}

type openrouterToolCallTestFlags struct {
	model string
}

var openrouterToolCallFlags openrouterToolCallTestFlags

func init() {
	OpenRouterToolCallTestCmd.Flags().StringVar(&openrouterToolCallFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
}

func runOpenRouterToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterToolCallFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Tool Calling with %s", modelID)

	// Check for API key
	if os.Getenv("OPEN_ROUTER_API_KEY") == "" {
		log.Printf("❌ OPEN_ROUTER_API_KEY environment variable is required")
		return
	}

	// Create OpenRouter LLM using our adapter
	logger := testing.GetTestLogger()
	openrouterLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenRouter,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenRouter LLM: %v", err)
		return
	}

	// Run shared tool call test
	shared.RunToolCallTest(openrouterLLM, modelID)
}
