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

var AzureStructuredOutputTestCmd = &cobra.Command{
	Use:   "azure-structured-output",
	Short: "Test Azure AI structured JSON output with JSON Schema",
	Run:   runAzureStructuredOutputTest,
}

type azureStructuredOutputTestFlags struct {
	model string
}

var azureStructuredOutputFlags azureStructuredOutputTestFlags

func init() {
	AzureStructuredOutputTestCmd.Flags().StringVar(&azureStructuredOutputFlags.model, "model", "gpt-4o", "Azure AI model to test")
	AzureStructuredOutputTestCmd.Flags().StringVar(&azureFlags.endpoint, "endpoint", "", "Azure AI endpoint (or set AZURE_AI_ENDPOINT env var)")
	AzureStructuredOutputTestCmd.Flags().StringVar(&azureFlags.apiKey, "api-key", "", "Azure AI API key (or set AZURE_AI_API_KEY env var)")
}

func runAzureStructuredOutputTest(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get endpoint from environment or flag
	endpoint := azureFlags.endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	if endpoint == "" {
		log.Fatal("Endpoint required: set --endpoint flag or AZURE_AI_ENDPOINT environment variable")
	}

	// Get API key from environment or flag
	apiKey := azureFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("AZURE_AI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or AZURE_AI_API_KEY environment variable")
	}

	// Set environment variables for internal LLM provider to pick up
	os.Setenv("AZURE_AI_ENDPOINT", endpoint)
	os.Setenv("AZURE_AI_API_KEY", apiKey)

	// Get model ID
	modelID := azureStructuredOutputFlags.model
	if modelID == "" {
		modelID = "gpt-4o"
	}

	log.Printf("🚀 Testing Azure AI Structured Output with %s", modelID)

	// Create Azure LLM using our adapter
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderAzure,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Azure AI LLM: %v", err)
	}

	// Run shared structured output test with JSON Schema approach
	// useJSONMode=false, useJSONSchema=true, useToolBased=false
	shared.RunStructuredOutputTest(llmInstance, modelID, false, true, false)
}
