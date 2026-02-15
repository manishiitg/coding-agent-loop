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

var BedrockStreamingFuncTestCmd = &cobra.Command{
	Use:   "bedrock-streaming-func",
	Short: "Test Bedrock streaming with WithStreamingFunc (backward compatibility)",
	Run:   runBedrockStreamingFuncTest,
}

func init() {
	BedrockStreamingFuncTestCmd.Flags().String("model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
}

func runBedrockStreamingFuncTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	modelID, _ := cmd.Flags().GetString("model")
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Bedrock LLM: %v", err)
	}

	shared.RunStreamingWithFuncTest(llmInstance, modelID)
}
