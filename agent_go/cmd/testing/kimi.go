package testing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var kimiTestCmd = &cobra.Command{
	Use:   "kimi",
	Short: "Smoke test the Kimi API provider",
	Long: `Smoke test Kimi provider integration from the builder repo.

This test:
1. Loads KIMI_API_KEY from agent_go/.env or the current environment
2. Runs a direct text generation check through the Kimi provider

Examples:
  orchestrator test kimi
  orchestrator test kimi --model kimi-k2.6`,
	RunE: runKimiTest,
}

func init() {
	kimiTestCmd.Flags().String("model", "", "Kimi model to use (default: kimi-k2.6)")
	_ = viper.BindPFlag("kimi.model", kimiTestCmd.Flags().Lookup("model"))
}

func runKimiTest(cmd *cobra.Command, args []string) error {
	loadKimiDotEnv()

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	modelID := strings.TrimSpace(viper.GetString("kimi.model"))
	if modelID == "" {
		modelID = "kimi-k2.6"
	}

	apiKey := strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("KIMI_API_KEY environment variable is not set")
	}

	logger.Info("=== Kimi Smoke Test ===")
	logger.Info(fmt.Sprintf("Model: %s", modelID))

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderKimi,
		ModelID:     modelID,
		Temperature: 0.1,
		APIKeys: &llm.ProviderAPIKeys{
			Kimi: &apiKey,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize Kimi model %q: %w", modelID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := llmModel.GenerateContent(ctx, []llmtypes.MessageContent{
		llmtypes.TextPart(llmtypes.ChatMessageTypeHuman, "Reply with exactly: KIMI_TEXT_OK"),
	})
	if err != nil {
		return fmt.Errorf("Kimi smoke test failed for model %q: %w", modelID, err)
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("Kimi smoke test returned no choices for model %q", modelID)
	}

	content := strings.TrimSpace(resp.Choices[0].Content)
	if content == "" {
		return fmt.Errorf("Kimi smoke test returned an empty response for model %q", modelID)
	}

	fmt.Println("\nKimi smoke test completed successfully.")
	fmt.Printf("\nText response (%s):\n%s\n", modelID, content)

	return nil
}

func loadKimiDotEnv() {
	if err := godotenv.Load("agent_go/.env"); err == nil {
		return
	}
	if err := godotenv.Load(".env"); err == nil {
		return
	}
	_ = godotenv.Load("../.env")
}
