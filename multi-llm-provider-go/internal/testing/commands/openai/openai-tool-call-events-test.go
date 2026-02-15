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
)

var OpenAIToolCallEventsTestCmd = &cobra.Command{
	Use:   "openai-tool-call-events",
	Short: "Test OpenAI tool call events",
	Run:   runOpenAIToolCallEventsTest,
}

type openaiToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var openaiToolCallEventsFlags openaiToolCallEventsTestFlags

func init() {
	OpenAIToolCallEventsTestCmd.Flags().StringVar(&openaiToolCallEventsFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
	OpenAIToolCallEventsTestCmd.Flags().BoolVar(&openaiToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	OpenAIToolCallEventsTestCmd.Flags().BoolVar(&openaiToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	OpenAIToolCallEventsTestCmd.Flags().StringVar(&openaiToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runOpenAIToolCallEventsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiToolCallEventsFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Tool Call Events with %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if openaiToolCallEventsFlags.record || openaiToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  openaiToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "openai",
			ModelID:  modelID,
			BaseDir:  openaiToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if openaiToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}

		if openaiToolCallEventsFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", openaiToolCallEventsFlags.testDir)
		}
		if openaiToolCallEventsFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", openaiToolCallEventsFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create OpenAI LLM using our adapter with event emitter
	logger := testing.GetTestLogger()
	openaiLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI LLM: %v", err)
		return
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, openaiLLM, modelID, testEmitter)
}
