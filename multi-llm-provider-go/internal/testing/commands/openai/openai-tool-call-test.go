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

var OpenAIToolCallTestCmd = &cobra.Command{
	Use:   "openai-tool-call",
	Short: "Test OpenAI tool calling",
	Run:   runOpenAIToolCallTest,
}

type openaiToolCallTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var openaiToolCallFlags openaiToolCallTestFlags

func init() {
	OpenAIToolCallTestCmd.Flags().StringVar(&openaiToolCallFlags.model, "model", "", "OpenAI model to test (default: gpt-4o-mini)")
	OpenAIToolCallTestCmd.Flags().BoolVar(&openaiToolCallFlags.record, "record", false, "Record LLM responses to testdata/")
	OpenAIToolCallTestCmd.Flags().BoolVar(&openaiToolCallFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	OpenAIToolCallTestCmd.Flags().StringVar(&openaiToolCallFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runOpenAIToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := openaiToolCallFlags.model
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	log.Printf("🚀 Testing OpenAI Tool Calling with %s", modelID)

	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Printf("❌ OPENAI_API_KEY environment variable is required")
		return
	}

	ctx := context.Background()

	// Setup recorder if recording or replaying
	var rec *recorder.Recorder
	if openaiToolCallFlags.record || openaiToolCallFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  openaiToolCallFlags.record,
			TestName: "tool_call",
			Provider: "openai",
			ModelID:  modelID,
			BaseDir:  openaiToolCallFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if openaiToolCallFlags.replay {
			rec.SetReplayMode(true)
		}

		if openaiToolCallFlags.record {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", openaiToolCallFlags.testDir)
		}
		if openaiToolCallFlags.replay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", openaiToolCallFlags.testDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create OpenAI LLM using our adapter
	logger := testing.GetTestLogger()
	openaiLLM, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create OpenAI LLM: %v", err)
		return
	}

	// Run shared tool call test with context (for recorder support)
	shared.RunToolCallTestWithContext(ctx, openaiLLM, modelID)
}
