package openai

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

var OpenAICmd = &cobra.Command{
	Use:   "openai",
	Short: "Test OpenAI (GPT) plain text generation",
	Run:   runOpenAI,
}

type openaiTestFlags struct {
	model   string
	apiKey  string
	record  bool
	replay  bool
	testDir string
}

var openaiFlags openaiTestFlags

func init() {
	OpenAICmd.Flags().StringVar(&openaiFlags.model, "model", "gpt-4o", "OpenAI model to test")
	OpenAICmd.Flags().StringVar(&openaiFlags.apiKey, "api-key", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
	OpenAICmd.Flags().BoolVar(&openaiFlags.record, "record", false, "Record LLM responses to testdata/")
	OpenAICmd.Flags().BoolVar(&openaiFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	OpenAICmd.Flags().StringVar(&openaiFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runOpenAI(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key from environment or flag
	apiKey := openaiFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or OPENAI_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("OPENAI_API_KEY", apiKey)

	// Set default model if not specified
	modelID := openaiFlags.model
	if modelID == "" {
		modelID = "gpt-4o"
	}

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if openaiFlags.record || openaiFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  openaiFlags.record,
			TestName: "plain_text",
			Provider: "openai",
			ModelID:  modelID,
			BaseDir:  openaiFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if openaiFlags.replay {
			rec.SetReplayMode(true)
		}

		if openaiFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", openaiFlags.testDir)
		}
		if openaiFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", openaiFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Initialize OpenAI LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize OpenAI LLM: %v", err)
	}

	// Run shared plain text test with context (for recorder support)
	shared.RunPlainTextTestWithContext(ctx, llmInstance, modelID)
}
