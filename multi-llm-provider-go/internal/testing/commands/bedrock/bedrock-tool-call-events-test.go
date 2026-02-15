package bedrock

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var BedrockToolCallEventsTestCmd = &cobra.Command{
	Use:   "bedrock-tool-call-events",
	Short: "Test Bedrock tool call events",
	Run:   runBedrockToolCallEventsTest,
}

type bedrockToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var bedrockToolCallEventsFlags bedrockToolCallEventsTestFlags

func init() {
	BedrockToolCallEventsTestCmd.Flags().StringVar(&bedrockToolCallEventsFlags.model, "model", "", "Bedrock model to test")
	BedrockToolCallEventsTestCmd.Flags().BoolVar(&bedrockToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	BedrockToolCallEventsTestCmd.Flags().BoolVar(&bedrockToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	BedrockToolCallEventsTestCmd.Flags().StringVar(&bedrockToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runBedrockToolCallEventsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := bedrockToolCallEventsFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing Bedrock Tool Call Events with %s", modelID)

	ctx := context.Background()
	var rec *recorder.Recorder

	if bedrockToolCallEventsFlags.record || bedrockToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  bedrockToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "bedrock",
			ModelID:  modelID,
			BaseDir:  bedrockToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if bedrockToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}
		if bedrockToolCallEventsFlags.record {
			log.Printf("📹 [RECORDER] Recording enabled - responses will be saved to %s/bedrock/", bedrockToolCallEventsFlags.testDir)
		}
		if bedrockToolCallEventsFlags.replay {
			log.Printf("▶️  [RECORDER] Replay enabled - using recorded responses from %s/bedrock/", bedrockToolCallEventsFlags.testDir)
		}
		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create Bedrock LLM using internal adapter with event emitter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderBedrock,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create Bedrock LLM: %v", err)
		return
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, llm, modelID, testEmitter)
}
