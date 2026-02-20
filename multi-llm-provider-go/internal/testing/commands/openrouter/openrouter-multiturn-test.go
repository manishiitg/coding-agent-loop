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

var OpenRouterMultiTurnTestCmd = &cobra.Command{
	Use:   "openrouter-multiturn",
	Short: "Test OpenRouter multi-turn conversations",
	Run:   runOpenRouterMultiTurnTest,
}

type openrouterMultiTurnTestFlags struct {
	model string
}

var openrouterMultiTurnFlags openrouterMultiTurnTestFlags

func init() {
	OpenRouterMultiTurnTestCmd.Flags().StringVar(&openrouterMultiTurnFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
}

func runOpenRouterMultiTurnTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterMultiTurnFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Multi-Turn Conversations using %s", modelID)

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

	// Run shared multi-turn conversation test
	shared.RunMultiTurnConversationTest(llm, modelID)
}
