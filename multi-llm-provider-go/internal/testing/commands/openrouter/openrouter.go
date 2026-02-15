package openrouter

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var OpenRouterCmd = &cobra.Command{
	Use:   "openrouter",
	Short: "Test OpenRouter plain text generation",
	Run:   runOpenRouter,
}

type openrouterTestFlags struct {
	model  string
	apiKey string
}

var openrouterFlags openrouterTestFlags

func init() {
	OpenRouterCmd.Flags().StringVar(&openrouterFlags.model, "model", "moonshotai/kimi-k2", "OpenRouter model to test")
	OpenRouterCmd.Flags().StringVar(&openrouterFlags.apiKey, "api-key", "", "OpenRouter API key (or set OPEN_ROUTER_API_KEY env var)")
}

func runOpenRouter(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key from environment or flag
	apiKey := openrouterFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPEN_ROUTER_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or OPEN_ROUTER_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("OPEN_ROUTER_API_KEY", apiKey)

	// Set default model if not specified
	modelID := openrouterFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	// Initialize OpenRouter LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenRouter,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize OpenRouter LLM: %v", err)
	}

	// Run shared plain text test
	shared.RunPlainTextTest(llmInstance, modelID)
}

