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

var BedrockStreamingToolCallHistoryTestCmd = &cobra.Command{
	Use:   "bedrock-streaming-toolcall-history",
	Short: "Test Bedrock streaming with tool calls stored in conversation history",
	Long: `This test verifies the critical flow:
1. Get tool calls from LLM (with streaming)
2. Validate arguments are valid JSON
3. Store tool calls in conversation history
4. Add tool results
5. Send full conversation (including tool calls) back to LLM
6. Verify no errors occur (especially no JSON validation errors)

This test catches the issue where tool calls with invalid JSON arguments 
cause errors when sent back to Bedrock.`,
	Run: runBedrockStreamingToolCallHistoryTest,
}

func init() {
	BedrockStreamingToolCallHistoryTestCmd.Flags().String("model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
}

func runBedrockStreamingToolCallHistoryTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	// Bind environment variables for viper
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()
	_ = viper.BindEnv("log-file", "LOG_FILE")   // Ignore error - env var binding is best effort
	_ = viper.BindEnv("log-level", "LOG_LEVEL") // Ignore error - env var binding is best effort

	logFile := viper.GetString("log-file")
	if logFile == "" {
		logFile = os.Getenv("LOG_FILE")
	}
	logLevel := viper.GetString("log-level")
	if logLevel == "" {
		logLevel = os.Getenv("LOG_LEVEL")
	}
	if logLevel == "" {
		logLevel = "debug" // Default to debug for this test
	}
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

	shared.RunStreamingToolCallWithHistoryTest(llmInstance, modelID)
}
