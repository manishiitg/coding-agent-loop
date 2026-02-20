package anthropic

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

var AnthropicToolCallEventsTestCmd = &cobra.Command{
	Use:   "anthropic-tool-call-events",
	Short: "Test Anthropic (Claude) tool call events",
	Run:   runAnthropicToolCallEventsTest,
}

type anthropicToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var anthropicToolCallEventsFlags anthropicToolCallEventsTestFlags

func init() {
	AnthropicToolCallEventsTestCmd.Flags().StringVar(&anthropicToolCallEventsFlags.model, "model", "", "Anthropic model to test (default: claude-3-5-sonnet-20241022)")
	AnthropicToolCallEventsTestCmd.Flags().BoolVar(&anthropicToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	AnthropicToolCallEventsTestCmd.Flags().BoolVar(&anthropicToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	AnthropicToolCallEventsTestCmd.Flags().StringVar(&anthropicToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runAnthropicToolCallEventsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := anthropicToolCallEventsFlags.model
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}

	log.Printf("🚀 Testing Anthropic Tool Call Events with %s", modelID)

	// Check for API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Printf("❌ ANTHROPIC_API_KEY environment variable is required")
		return
	}

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if anthropicToolCallEventsFlags.record || anthropicToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  anthropicToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "anthropic",
			ModelID:  modelID,
			BaseDir:  anthropicToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if anthropicToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}

		if anthropicToolCallEventsFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", anthropicToolCallEventsFlags.testDir)
		}
		if anthropicToolCallEventsFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", anthropicToolCallEventsFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create Anthropic LLM using our adapter with event emitter
	logger := testing.GetTestLogger()
	anthropicLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderAnthropic,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create Anthropic LLM: %v", err)
		return
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, anthropicLLM, modelID, testEmitter)
}
