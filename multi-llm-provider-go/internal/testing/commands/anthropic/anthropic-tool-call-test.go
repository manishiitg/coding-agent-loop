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

var AnthropicToolCallTestCmd = &cobra.Command{
	Use:   "anthropic-tool-call",
	Short: "Test Anthropic (Claude) tool calling",
	Run:   runAnthropicToolCallTest,
}

type anthropicToolCallTestFlags struct {
	model string
}

var anthropicToolCallFlags anthropicToolCallTestFlags

func init() {
	AnthropicToolCallTestCmd.Flags().StringVar(&anthropicToolCallFlags.model, "model", "", "Anthropic model to test (default: claude-3-5-sonnet-20241022)")
}

func runAnthropicToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := anthropicToolCallFlags.model
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}

	log.Printf("🚀 Testing Anthropic Tool Calling with %s", modelID)

	// Check for API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Printf("❌ ANTHROPIC_API_KEY environment variable is required")
		return
	}

	// Create Anthropic LLM using our adapter
	logger := testing.GetTestLogger()
	anthropicLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAnthropic,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Anthropic LLM: %v", err)
		return
	}

	// Run shared tool call test
	shared.RunToolCallTest(anthropicLLM, modelID)
}
