package bedrock

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	llmproviders "llm-providers"
	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"
)

var BedrockCmd = &cobra.Command{
	Use:   "bedrock",
	Short: "Test Bedrock plain text generation",
	Long:  "Test AWS Bedrock LLM with plain text generation",
	Run:   runBedrock,
}

type bedrockTestFlags struct {
	model string
}

var bedrockFlags bedrockTestFlags

func init() {
	BedrockCmd.Flags().StringVar(&bedrockFlags.model, "model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
}

func runBedrock(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Use model ID from flags (default is already set to the new model)
	modelID := bedrockFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	// Create Bedrock LLM using new adapter
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to create Bedrock LLM: %v", err)
	}

	// Run shared plain text test
	shared.RunPlainTextTest(llm, modelID)
}
