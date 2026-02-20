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

var AnthropicCmd = &cobra.Command{
	Use:   "anthropic",
	Short: "Test Anthropic (Claude) plain text generation",
	Run:   runAnthropic,
}

type anthropicTestFlags struct {
	model  string
	apiKey string
}

var anthropicFlags anthropicTestFlags

func init() {
	AnthropicCmd.Flags().StringVar(&anthropicFlags.model, "model", "claude-haiku-4-5-20251001", "Claude model to test")
	AnthropicCmd.Flags().StringVar(&anthropicFlags.apiKey, "api-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env var)")
}

func runAnthropic(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key from environment or flag
	apiKey := anthropicFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or ANTHROPIC_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("ANTHROPIC_API_KEY", apiKey)

	// Set default model if not specified
	modelID := anthropicFlags.model
	if modelID == "" {
		modelID = "claude-haiku-4-5-20251001"
	}

	// Initialize Anthropic LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAnthropic,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Anthropic LLM: %v", err)
	}

	// Run shared plain text test
	shared.RunPlainTextTest(llmInstance, modelID)
}
