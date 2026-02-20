package vertex

import (
	"context"
	"log"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/spf13/cobra"
)

// VertexGeminiThinkingLevelTestCmd tests Gemini 3 Pro thinking_level behavior (low/high) on Vertex.
var VertexGeminiThinkingLevelTestCmd = &cobra.Command{
	Use:   "vertex-gemini-thinking-level",
	Short: "Test Gemini 3 Pro thinking_level (e.g., low/high) on Vertex",
	Run:   runVertexGeminiThinkingLevelTest,
}

type vertexGeminiThinkingLevelFlags struct {
	model         string
	thinkingLevel string
	record        bool
	replay        bool
	testDir       string
}

var vertexGeminiThinkingFlags vertexGeminiThinkingLevelFlags

func init() {
	VertexGeminiThinkingLevelTestCmd.Flags().StringVar(&vertexGeminiThinkingFlags.model, "model", "gemini-3-pro-preview", "Gemini model to test (default: gemini-3-pro-preview)")
	VertexGeminiThinkingLevelTestCmd.Flags().StringVar(&vertexGeminiThinkingFlags.thinkingLevel, "thinking-level", "low", "Thinking level to use (low or high)")
	VertexGeminiThinkingLevelTestCmd.Flags().BoolVar(&vertexGeminiThinkingFlags.record, "record", false, "Record LLM responses")
	VertexGeminiThinkingLevelTestCmd.Flags().BoolVar(&vertexGeminiThinkingFlags.replay, "replay", false, "Replay recorded responses")
	VertexGeminiThinkingLevelTestCmd.Flags().StringVar(&vertexGeminiThinkingFlags.testDir, "test-dir", "testdata", "Test directory for recordings")
}

func runVertexGeminiThinkingLevelTest(cmd *cobra.Command, args []string) {
	logFile := ""  // Use default stdout logging for this test
	logLevel := "" // Default log level
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	ctx := context.Background()

	// Setup recorder if recording or replaying
	if vertexGeminiThinkingFlags.record || vertexGeminiThinkingFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexGeminiThinkingFlags.record,
			TestName: "gemini_thinking_level",
			Provider: "vertex",
			ModelID:  vertexGeminiThinkingFlags.model,
			BaseDir:  vertexGeminiThinkingFlags.testDir,
		}
		rec := recorder.NewRecorder(recConfig)
		if vertexGeminiThinkingFlags.replay {
			rec.SetReplayMode(true)
		}
		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Initialize Vertex AI LLM using internal provider
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     vertexGeminiThinkingFlags.model,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	// Run shared thinking-level test with the requested level (default: low)
	shared.RunGeminiThinkingLevelTestWithContext(ctx, llmInstance, vertexGeminiThinkingFlags.model, vertexGeminiThinkingFlags.thinkingLevel)
}
