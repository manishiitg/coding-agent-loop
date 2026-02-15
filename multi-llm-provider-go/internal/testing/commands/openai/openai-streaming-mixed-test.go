package openai

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

var OpenAIStreamingMixedTestCmd = &cobra.Command{
	Use:   "openai-streaming-mixed",
	Short: "Test OpenAI streaming with mixed content and tool calls",
	Run:   runOpenAIStreamingMixedTest,
}

func init() {
	OpenAIStreamingMixedTestCmd.Flags().String("model", "gpt-4o-mini", "OpenAI model to test")
	OpenAIStreamingMixedTestCmd.Flags().String("api-key", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
}

func runOpenAIStreamingMixedTest(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key from environment or flag
	apiKey, _ := cmd.Flags().GetString("api-key")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or OPENAI_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("OPENAI_API_KEY", apiKey)

	// Get model
	modelID, _ := cmd.Flags().GetString("model")
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	// Initialize OpenAI LLM
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize OpenAI LLM: %v", err)
	}

	// Run streaming mixed test
	shared.RunStreamingMixedTest(llmInstance, modelID)
}
