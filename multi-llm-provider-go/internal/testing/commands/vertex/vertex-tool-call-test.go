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

var VertexToolCallTestCmd = &cobra.Command{
	Use:   "vertex-tool-call",
	Short: "Test Vertex AI (Gemini) tool calling",
	Run:   runVertexToolCallTest,
}

type vertexToolCallTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var vertexToolCallFlags vertexToolCallTestFlags

func init() {
	VertexToolCallTestCmd.Flags().StringVar(&vertexToolCallFlags.model, "model", "", "Vertex AI model to test (default: gemini-2.5-flash)")
	VertexToolCallTestCmd.Flags().BoolVar(&vertexToolCallFlags.record, "record", false, "Record LLM responses to testdata/")
	VertexToolCallTestCmd.Flags().BoolVar(&vertexToolCallFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	VertexToolCallTestCmd.Flags().StringVar(&vertexToolCallFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runVertexToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get model ID
	modelID := vertexToolCallFlags.model
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
	if vertexToolCallFlags.record || vertexToolCallFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexToolCallFlags.record,
			TestName: "tool_call",
			Provider: "vertex",
			ModelID:  modelID,
			BaseDir:  vertexToolCallFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if vertexToolCallFlags.replay {
			rec.SetReplayMode(true)
		}

		if vertexToolCallFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", vertexToolCallFlags.testDir)
		}
		if vertexToolCallFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", vertexToolCallFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create Vertex AI LLM using our adapter
	vertexLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("❌ Failed to create Vertex AI LLM: %v", err)
	}

	// Run shared tool call test with context (for recorder support)
	shared.RunToolCallTestWithContext(ctx, vertexLLM, modelID)
}
