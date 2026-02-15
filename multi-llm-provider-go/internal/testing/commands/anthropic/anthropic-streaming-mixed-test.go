package anthropic

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

var AnthropicStreamingMixedTestCmd = &cobra.Command{
	Use:   "anthropic-streaming-mixed",
	Short: "Test Anthropic streaming with mixed content and tool calls",
	Run:   runAnthropicStreamingMixedTest,
}

func init() {
	AnthropicStreamingMixedTestCmd.Flags().String("model", "claude-3-5-sonnet-20241022", "Anthropic model to test")
	AnthropicStreamingMixedTestCmd.Flags().String("api-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env var)")
}

func runAnthropicStreamingMixedTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	apiKey, _ := cmd.Flags().GetString("api-key")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or ANTHROPIC_API_KEY environment variable")
	}

	os.Setenv("ANTHROPIC_API_KEY", apiKey)

	modelID, _ := cmd.Flags().GetString("model")
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}

	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAnthropic,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Anthropic LLM: %v", err)
	}

	shared.RunStreamingMixedTest(llmInstance, modelID)
}
