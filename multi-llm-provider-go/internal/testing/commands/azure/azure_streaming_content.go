package azure

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

var AzureStreamingContentTestCmd = &cobra.Command{
	Use:   "azure-streaming-content",
	Short: "Test Azure AI streaming content (no tool calls)",
	Run:   runAzureStreamingContentTest,
}

func init() {
	AzureStreamingContentTestCmd.Flags().String("model", "gpt-4o", "Azure AI model to test")
	AzureStreamingContentTestCmd.Flags().String("endpoint", "", "Azure AI endpoint (or set AZURE_AI_ENDPOINT env var)")
	AzureStreamingContentTestCmd.Flags().String("api-key", "", "Azure AI API key (or set AZURE_AI_API_KEY env var)")
}

func runAzureStreamingContentTest(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get endpoint from environment or flag
	endpoint, _ := cmd.Flags().GetString("endpoint")
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	if endpoint == "" {
		log.Fatal("Endpoint required: set --endpoint flag or AZURE_AI_ENDPOINT environment variable")
	}

	// Get API key from environment or flag
	apiKey, _ := cmd.Flags().GetString("api-key")
	if apiKey == "" {
		apiKey = os.Getenv("AZURE_AI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or AZURE_AI_API_KEY environment variable")
	}

	// Set environment variables for internal LLM provider to pick up
	os.Setenv("AZURE_AI_ENDPOINT", endpoint)
	os.Setenv("AZURE_AI_API_KEY", apiKey)

	// Get model
	modelID, _ := cmd.Flags().GetString("model")
	if modelID == "" {
		modelID = "gpt-4o"
	}

	// Initialize Azure LLM
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAzure,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Azure AI LLM: %v", err)
	}

	// Run streaming content test
	shared.RunStreamingContentTest(llmInstance, modelID)
}
