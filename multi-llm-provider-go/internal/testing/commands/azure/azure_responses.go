package azure

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

var AzureResponsesTestCmd = &cobra.Command{
	Use:   "azure-responses",
	Short: "Test Azure AI Responses API (agentic models like gpt-5.2-codex)",
	Run:   runAzureResponsesTest,
}

type azureResponsesFlags struct {
	model    string
	endpoint string
	apiKey   string
	record   bool
	replay   bool
	testDir  string
}

var azRespFlags azureResponsesFlags

func init() {
	AzureResponsesTestCmd.Flags().StringVar(&azRespFlags.model, "model", "gpt-5.2-codex", "Azure AI model to test")
	AzureResponsesTestCmd.Flags().StringVar(&azRespFlags.endpoint, "endpoint", "", "Azure AI endpoint")
	AzureResponsesTestCmd.Flags().StringVar(&azRespFlags.apiKey, "api-key", "", "Azure AI API key")
	AzureResponsesTestCmd.Flags().BoolVar(&azRespFlags.record, "record", false, "Record LLM responses")
	AzureResponsesTestCmd.Flags().BoolVar(&azRespFlags.replay, "replay", false, "Replay recorded responses")
	AzureResponsesTestCmd.Flags().StringVar(&azRespFlags.testDir, "test-dir", "testdata", "Test directory")
}

func runAzureResponsesTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	endpoint := azRespFlags.endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	apiKey := azRespFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("AZURE_AI_API_KEY")
	}

	modelID := azRespFlags.model

	ctx := context.Background()
	var rec *recorder.Recorder

	if azRespFlags.record || azRespFlags.replay {
		recConfig := recorder.RecordingConfig{
			Enabled:  azRespFlags.record,
			TestName: "codex_agentic",
			Provider: "azure",
			ModelID:  modelID,
			BaseDir:  azRespFlags.testDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if azRespFlags.replay {
			rec.SetReplayMode(true)
		}
		ctx = recorder.WithRecorder(ctx, rec)
	}

	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider: llmproviders.ProviderAzure,
		ModelID:  modelID,
		Logger:   logger,
		Context:  ctx,
		APIKeys: &llmproviders.ProviderAPIKeys{
			Azure: &llmproviders.AzureAPIConfig{
				Endpoint: endpoint,
				APIKey:   apiKey,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize Azure AI LLM: %v", err)
	}

	shared.RunCodexAgenticTest(ctx, llmInstance, modelID)
}
