package bedrock

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var BedrockStructuredOutputTestCmd = &cobra.Command{
	Use:   "bedrock-structured-output",
	Short: "Test Bedrock structured JSON output with JSON mode",
	Run:   runBedrockStructuredOutputTest,
}

type bedrockStructuredOutputTestFlags struct {
	model string
}

var bedrockStructuredOutputFlags bedrockStructuredOutputTestFlags

func init() {
	BedrockStructuredOutputTestCmd.Flags().StringVar(&bedrockStructuredOutputFlags.model, "model", "", "Bedrock model to test (default: global.anthropic.claude-sonnet-4-5-20250929-v1:0)")
}

func runBedrockStructuredOutputTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := bedrockStructuredOutputFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing Bedrock Structured Output with %s", modelID)

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

	// Run shared structured output test with JSON mode
	// useJSONMode=true, useJSONSchema=false, useToolBased=false
	shared.RunStructuredOutputTest(llm, modelID, true, false, false)
}
