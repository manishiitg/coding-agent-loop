package bedrock

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "llm-providers"
	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"
)

var LLMToolCallTestCmd = &cobra.Command{
	Use:   "llm-tool-call",
	Short: "Test LLM tool calling with Bedrock",
	Run:   runLLMToolCallTest,
}

type llmToolCallTestFlags struct {
	model string
}

var llmToolCallFlags llmToolCallTestFlags

func init() {
	LLMToolCallTestCmd.Flags().StringVar(&llmToolCallFlags.model, "model", "", "Bedrock model to test")
}

func runLLMToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := llmToolCallFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing LLM Tool Calling with %s", modelID)

	// Create Bedrock LLM using internal adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Bedrock LLM: %v", err)
		return
	}

	// Run shared tool call test
	shared.RunToolCallTest(llm, modelID)
}
