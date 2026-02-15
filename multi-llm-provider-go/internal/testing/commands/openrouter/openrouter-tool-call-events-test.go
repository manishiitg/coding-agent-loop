package openrouter

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

var OpenRouterToolCallEventsTestCmd = &cobra.Command{
	Use:   "openrouter-tool-call-events",
	Short: "Test OpenRouter tool call events",
	Run:   runOpenRouterToolCallEventsTest,
}

type openrouterToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var openrouterToolCallEventsFlags openrouterToolCallEventsTestFlags

func init() {
	OpenRouterToolCallEventsTestCmd.Flags().StringVar(&openrouterToolCallEventsFlags.model, "model", "", "OpenRouter model to test (default: moonshotai/kimi-k2)")
	OpenRouterToolCallEventsTestCmd.Flags().BoolVar(&openrouterToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	OpenRouterToolCallEventsTestCmd.Flags().BoolVar(&openrouterToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	OpenRouterToolCallEventsTestCmd.Flags().StringVar(&openrouterToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runOpenRouterToolCallEventsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openrouterToolCallEventsFlags.model
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	log.Printf("🚀 Testing OpenRouter Tool Call Events with %s", modelID)

	// Check for API key
	if os.Getenv("OPEN_ROUTER_API_KEY") == "" {
		log.Printf("❌ OPEN_ROUTER_API_KEY environment variable is required")
		return
	}

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if openrouterToolCallEventsFlags.record || openrouterToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  openrouterToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "openrouter",
			ModelID:  modelID,
			BaseDir:  openrouterToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if openrouterToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}

		if openrouterToolCallEventsFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", openrouterToolCallEventsFlags.testDir)
		}
		if openrouterToolCallEventsFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", openrouterToolCallEventsFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create OpenRouter LLM using our adapter with event emitter
	logger := testing.GetTestLogger()
	openrouterLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderOpenRouter,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenRouter LLM: %v", err)
		return
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, openrouterLLM, modelID, testEmitter)
}

