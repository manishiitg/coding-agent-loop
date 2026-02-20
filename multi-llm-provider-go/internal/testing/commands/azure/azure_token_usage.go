package azure

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	sharedutils "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var AzureTokenUsageTestCmd = &cobra.Command{
	Use:   "azure-token-usage",
	Short: "Test Azure AI token usage extraction",
	Long: `Test token usage extraction from Azure AI LLM calls.

This command tests if Azure AI returns token usage information in their GenerationInfo.`,
	Run: runAzureTokenUsageTest,
}

var (
	azureTokenTestPrompt string
	azureTokenTestRecord bool
	azureTokenTestReplay bool
	azureTokenTestDir    string
	azureTokenTestModel  string
)

func init() {
	AzureTokenUsageTestCmd.Flags().StringVar(&azureTokenTestPrompt, "prompt", "Hello world", "Test prompt")
	AzureTokenUsageTestCmd.Flags().StringVar(&azureTokenTestModel, "model", "gpt-4o", "Azure AI model to test")
	AzureTokenUsageTestCmd.Flags().StringVar(&azureFlags.endpoint, "endpoint", "", "Azure AI endpoint (or set AZURE_AI_ENDPOINT env var)")
	AzureTokenUsageTestCmd.Flags().StringVar(&azureFlags.apiKey, "api-key", "", "Azure AI API key (or set AZURE_AI_API_KEY env var)")
	AzureTokenUsageTestCmd.Flags().BoolVar(&azureTokenTestRecord, "record", false, "Record LLM responses to testdata/")
	AzureTokenUsageTestCmd.Flags().BoolVar(&azureTokenTestReplay, "replay", false, "Replay recorded responses from testdata/")
	AzureTokenUsageTestCmd.Flags().StringVar(&azureTokenTestDir, "test-dir", "testdata", "Directory for test recordings")
}

func runAzureTokenUsageTest(cmd *cobra.Command, args []string) {
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

	fmt.Printf("🧪 Testing Azure AI Token Usage Extraction\n")
	fmt.Printf("==========================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: azureTokenTestPrompt}},
		},
	}

	// Set environment for Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	// Initialize tracer
	tracer := testing.InitializeTracer(logger)

	// Start trace
	mainTraceID := tracer.StartTrace("Azure AI Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "azure",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Setup recorder if recording or replaying
	ctx := context.Background()
	var rec *recorder.Recorder
	if azureTokenTestRecord || azureTokenTestReplay {
		recConfig := recorder.RecordingConfig{
			Enabled:  azureTokenTestRecord,
			TestName: "token_usage",
			Provider: "azure",
			ModelID:  azureTokenTestModel,
			BaseDir:  azureTokenTestDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if azureTokenTestReplay {
			rec.SetReplayMode(true)
		}

		if azureTokenTestRecord {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", azureTokenTestDir)
		}
		if azureTokenTestReplay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", azureTokenTestDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Test Azure AI
	testAzureTokenUsage(ctx, messages, mainTraceID, logger, rec)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Azure AI Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testAzureTokenUsage runs Azure AI token usage tests
func testAzureTokenUsage(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	// Get model ID
	modelID := azureTokenTestModel
	if modelID == "" {
		modelID = "gpt-4o"
	}

	// Test 1: Simple query test
	fmt.Printf("\n🧪 TEST: Azure AI %s (Simple Query)\n", modelID)
	fmt.Printf("==========================================\n")

	// Setup recorder for this model if needed
	modelCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = modelID
		rec = recorder.NewRecorder(recConfig)
		if azureTokenTestReplay {
			rec.SetReplayMode(true)
		}
		modelCtx = recorder.WithRecorder(ctx, rec)
	}

	config := llmproviders.Config{
		Provider:     llmproviders.ProviderAzure,
		ModelID:      modelID,
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      modelCtx,
	}

	llm, err := llmproviders.InitializeLLM(config)
	if err != nil {
		fmt.Printf("❌ Error creating Azure AI %s LLM: %v\n", modelID, err)
		fmt.Printf("⏭️  Skipping Azure AI %s test\n", modelID)
		return
	}

	fmt.Printf("🔧 Created Azure AI %s LLM using providers.go\n", modelID)
	sharedutils.TestLLMTokenUsage(modelCtx, llm, messages, azureTokenTestPrompt)

	// Test 2: Cache test with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Azure AI %s (Multi-Turn Conversation with Cache)\n", modelID)
	fmt.Printf("===================================================\n")

	// Setup recorder for cache test if needed
	cacheCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = modelID
		recConfig.TestName = "token_usage_cache"
		rec = recorder.NewRecorder(recConfig)
		if azureTokenTestReplay {
			rec.SetReplayMode(true)
		}
		cacheCtx = recorder.WithRecorder(ctx, rec)
	}
	sharedutils.TestLLMTokenUsageWithCache(cacheCtx, llm)
}
