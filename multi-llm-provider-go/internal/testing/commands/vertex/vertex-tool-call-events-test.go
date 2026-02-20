package vertex

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

var VertexToolCallEventsTestCmd = &cobra.Command{
	Use:   "vertex-tool-call-events",
	Short: "Test Vertex AI (Gemini) tool call events",
	Run:   runVertexToolCallEventsTest,
}

type vertexToolCallEventsTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var vertexToolCallEventsFlags vertexToolCallEventsTestFlags

func init() {
	VertexToolCallEventsTestCmd.Flags().StringVar(&vertexToolCallEventsFlags.model, "model", "", "Vertex AI model to test (default: gemini-2.5-flash)")
	VertexToolCallEventsTestCmd.Flags().BoolVar(&vertexToolCallEventsFlags.record, "record", false, "Record LLM responses to testdata/")
	VertexToolCallEventsTestCmd.Flags().BoolVar(&vertexToolCallEventsFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	VertexToolCallEventsTestCmd.Flags().StringVar(&vertexToolCallEventsFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runVertexToolCallEventsTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get model ID
	modelID := vertexToolCallEventsFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// Check for API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
	}

	// Set API key as environment variable
	_ = os.Setenv("VERTEX_API_KEY", apiKey) //nolint:errcheck // Test code, safe to ignore

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if vertexToolCallEventsFlags.record || vertexToolCallEventsFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexToolCallEventsFlags.record,
			TestName: "tool_call_events",
			Provider: "vertex",
			ModelID:  modelID,
			BaseDir:  vertexToolCallEventsFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if vertexToolCallEventsFlags.replay {
			rec.SetReplayMode(true)
		}

		if vertexToolCallEventsFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", vertexToolCallEventsFlags.testDir)
		}
		if vertexToolCallEventsFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", vertexToolCallEventsFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create test event emitter
	testEmitter := shared.NewTestEventEmitter()

	// Create Vertex AI LLM using our adapter with event emitter
	vertexLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      modelID,
		Temperature:  0.7,
		Logger:       logger,
		EventEmitter: testEmitter,
		Context:      ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create Vertex AI LLM: %v", err)
		return
	}

	// Run shared tool call event test with context
	shared.RunToolCallEventTestWithContext(ctx, vertexLLM, modelID, testEmitter)
}
