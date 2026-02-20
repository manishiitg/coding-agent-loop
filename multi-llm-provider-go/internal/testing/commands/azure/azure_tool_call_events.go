package azure

import (
	"context"
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var AzureToolCallEventsTestCmd = &cobra.Command{
	Use:   "azure-tool-call-events",
	Short: "Test Azure AI tool call events",
	Run:   runAzureToolCallEventsTest,
}

type azureToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var azureToolCallEventsFlags azureToolCallEventsTestFlags

func init() {
	AzureToolCallEventsTestCmd.Flags().StringVar(&azureToolCallEventsFlags.model, "model", "gpt-4o", "Azure AI model to test")
	AzureToolCallEventsTestCmd.Flags().StringVar(&azureFlags.endpoint, "endpoint", "", "Azure AI endpoint (or set AZURE_AI_ENDPOINT env var)")
	AzureToolCallEventsTestCmd.Flags().StringVar(&azureFlags.apiKey, "api-key", "", "Azure AI API key (or set AZURE_AI_API_KEY env var)")
	AzureToolCallEventsTestCmd.Flags().BoolVar(&azureToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	AzureToolCallEventsTestCmd.Flags().BoolVar(&azureToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	AzureToolCallEventsTestCmd.Flags().StringVar(&azureToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runAzureToolCallEventsTest(cmd *cobra.Command, args []string) {
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
	modelID := azureToolCallEventsFlags.model
	if modelID == "" {
		modelID = "gpt-4o"
	}

	log.Printf("🚀 Testing Azure AI Tool Call Events with %s", modelID)

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if azureToolCallEventsFlags.record || azureToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  azureToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "azure",
			ModelID:  modelID,
			BaseDir:  azureToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if azureToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}

		if azureToolCallEventsFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", azureToolCallEventsFlags.testDir)
		}
		if azureToolCallEventsFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", azureToolCallEventsFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create Azure LLM using our adapter with event emitter
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderAzure,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Azure AI LLM: %v", err)
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, llmInstance, modelID, testEmitter)
}
