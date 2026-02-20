package vertex

import (
	"context"
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Test Vertex AI (Gemini) plain text generation",
	Run:   runVertex,
}

type vertexTestFlags struct {
	model   string
	apiKey  string
	record  bool
	replay  bool
	testDir string
}

var vertexFlags vertexTestFlags

func init() {
	VertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	VertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
	VertexCmd.Flags().BoolVar(&vertexFlags.record, "record", false, "Record LLM responses to testdata/")
	VertexCmd.Flags().BoolVar(&vertexFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	VertexCmd.Flags().StringVar(&vertexFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runVertex(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get API key
	apiKey := vertexFlags.apiKey
	if apiKey == "" {
		if key := os.Getenv("VERTEX_API_KEY"); key != "" {
			apiKey = key
		} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKey = key
		}
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or VERTEX_API_KEY/GOOGLE_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	_ = os.Setenv("VERTEX_API_KEY", apiKey) //nolint:errcheck // Test code, safe to ignore

	ctx := context.Background()

	// Set default model if not specified
	modelID := vertexFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if vertexFlags.record || vertexFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexFlags.record,
			TestName: "plain_text",
			Provider: "vertex",
			ModelID:  modelID,
			BaseDir:  vertexFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		// For replay mode, disable recording
		if vertexFlags.replay {
			rec.SetReplayMode(true)
		}

		if vertexFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", vertexFlags.testDir)
		}
		if vertexFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", vertexFlags.testDir)
		}

		// Add recorder to context
		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Initialize Vertex AI LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	// Run shared plain text test (pass context with recorder if available)
	shared.RunPlainTextTestWithContext(ctx, llmInstance, modelID)
}
