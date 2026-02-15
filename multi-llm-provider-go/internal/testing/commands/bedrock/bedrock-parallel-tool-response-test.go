package bedrock

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var BedrockParallelToolResponseTestCmd = &cobra.Command{
	Use:   "bedrock-parallel-tool-response",
	Short: "Test Bedrock parallel tool calls with responses and continued conversation",
	Run:   runBedrockParallelToolResponseTest,
}

type bedrockParallelToolResponseTestFlags struct {
	model string
}

var bedrockParallelToolResponseFlags bedrockParallelToolResponseTestFlags

func init() {
	BedrockParallelToolResponseTestCmd.Flags().StringVar(&bedrockParallelToolResponseFlags.model, "model", "", "Bedrock model to test (default: global.anthropic.claude-sonnet-4-5-20250929-v1:0)")
}

func runBedrockParallelToolResponseTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get model ID
	modelID := bedrockParallelToolResponseFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing Bedrock Parallel Tool Calls with Responses using %s", modelID)

	// Initialize Bedrock LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Bedrock LLM: %v", err)
	}

	// Run parallel tool call with response test
	shared.RunParallelToolCallWithResponseTest(llmInstance, modelID)
}
