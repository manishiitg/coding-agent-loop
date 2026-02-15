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

var OpenAIParallelToolResponseTestCmd = &cobra.Command{
	Use:   "openai-parallel-tool-response",
	Short: "Test OpenAI parallel tool calls with responses and continued conversation",
	Run:   runOpenAIParallelToolResponseTest,
}

type openaiParallelToolResponseTestFlags struct {
	model  string
	apiKey string
}

var openaiParallelToolResponseFlags openaiParallelToolResponseTestFlags

func init() {
	OpenAIParallelToolResponseTestCmd.Flags().StringVar(&openaiParallelToolResponseFlags.model, "model", "gpt-4o-mini", "OpenAI model to test")
	OpenAIParallelToolResponseTestCmd.Flags().StringVar(&openaiParallelToolResponseFlags.apiKey, "api-key", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
}

func runOpenAIParallelToolResponseTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key from environment or flag
	apiKey := openaiParallelToolResponseFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or OPENAI_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("OPENAI_API_KEY", apiKey)

	// Set default model if not specified
	modelID := openaiParallelToolResponseFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	// Initialize OpenAI LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize OpenAI LLM: %v", err)
	}

	// Run parallel tool call with response test
	shared.RunParallelToolCallWithResponseTest(llmInstance, modelID)
}
