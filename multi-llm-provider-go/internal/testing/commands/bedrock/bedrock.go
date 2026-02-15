package bedrock

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var BedrockCmd = &cobra.Command{
	Use:   "bedrock",
	Short: "Test Bedrock plain text generation",
	Long:  "Test AWS Bedrock LLM with plain text generation",
	Run:   runBedrock,
}

type bedrockTestFlags struct {
	model   string
	record  bool
	replay  bool
	testDir string
}

var bedrockFlags bedrockTestFlags

func init() {
	BedrockCmd.Flags().StringVar(&bedrockFlags.model, "model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
	BedrockCmd.Flags().BoolVar(&bedrockFlags.record, "record", false, "Record LLM responses to testdata/")
	BedrockCmd.Flags().BoolVar(&bedrockFlags.replay, "replay", false, "Replay recorded responses from testdata/")
	BedrockCmd.Flags().StringVar(&bedrockFlags.testDir, "test-dir", "testdata", "Directory for test recordings")
}

func runBedrock(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Use model ID from flags (default is already set to the new model)
	modelID := bedrockFlags.model
	if modelID == "" {
		modelID = os.Getenv("BEDROCK_PRIMARY_MODEL")
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	}

	ctx := context.Background()
	var rec *recorder.Recorder

	if bedrockFlags.record || bedrockFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  bedrockFlags.record,
			TestName: "plain_text",
			Provider: "bedrock",
			ModelID:  modelID,
			BaseDir:  bedrockFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if bedrockFlags.replay {
			rec.SetReplayMode(true)
		}
		if bedrockFlags.record {
			logger.Infof("📹 [RECORDER] Recording enabled - responses will be saved to %s/bedrock/", bedrockFlags.testDir)
		}
		if bedrockFlags.replay {
			logger.Infof("▶️  [RECORDER] Replay enabled - using recorded responses from %s/bedrock/", bedrockFlags.testDir)
		}
		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Create Bedrock LLM using new adapter
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to create Bedrock LLM: %v", err)
	}

	// Run shared plain text test with context
	shared.RunPlainTextTestWithContext(ctx, llm, modelID)
}
