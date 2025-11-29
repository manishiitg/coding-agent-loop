package bedrock

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	llmproviders "llm-providers"
	"llm-providers/internal/recorder"
	"llm-providers/internal/testing"
	"llm-providers/internal/testing/commands/shared"
)

var LLMToolCallTestCmd = &cobra.Command{
	Use:   "llm-tool-call",
	Short: "Test LLM tool calling with Bedrock",
	Run:   runLLMToolCallTest,
}

type llmToolCallTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var llmToolCallFlags llmToolCallTestFlags

func init() {
	LLMToolCallTestCmd.Flags().StringVar(&llmToolCallFlags.model, "model", "", "Bedrock model to test")
	LLMToolCallTestCmd.Flags().BoolVar(&llmToolCallFlags.record, "record", false, "Record LLM responses to testdata/")
	LLMToolCallTestCmd.Flags().BoolVar(&llmToolCallFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	LLMToolCallTestCmd.Flags().StringVar(&llmToolCallFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runLLMToolCallTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	// Get model ID
	modelID := llmToolCallFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	log.Printf("🚀 Testing LLM Tool Calling with %s", modelID)

	ctx := context.Background()
	var rec *recorder.Recorder

	if llmToolCallFlags.record || llmToolCallFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  llmToolCallFlags.record,
			TestName: "tool_call",
			Provider: "bedrock",
			ModelID:  modelID,
			BaseDir:  llmToolCallFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if llmToolCallFlags.replay {
			rec.SetReplayMode(true)
		}
		if llmToolCallFlags.record {
			log.Printf("📹 [RECORDER] Recording enabled - responses will be saved to %s/bedrock/", llmToolCallFlags.testDir)
		}
		if llmToolCallFlags.replay {
			log.Printf("▶️  [RECORDER] Replay enabled - using recorded responses from %s/bedrock/", llmToolCallFlags.testDir)
		}
		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create Bedrock LLM using internal adapter
	logger := testing.GetTestLogger()
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Printf("❌ Failed to create Bedrock LLM: %v", err)
		return
	}

	// Run shared tool call test with context
	shared.RunToolCallTestWithContext(ctx, llm, modelID)
}
