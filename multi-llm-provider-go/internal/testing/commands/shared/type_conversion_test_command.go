package shared

import (
	"context"
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TypeConversionTestCmd is a command to test type conversion
// This can be called from any provider's test command
var TypeConversionTestCmd = &cobra.Command{
	Use:   "type-conversion",
	Short: "Test type conversion from agent_go to llm-providers",
	Long: `This test validates that all ContentPart types (TextContent, ImageContent, 
ToolCall, ToolCallResponse) work correctly when passed through the conversion layer.

This test would have caught the bug where ToolCall and ToolCallResponse weren't
being converted from agent_go/internal/llmtypes to llm-providers/llmtypes.`,
	Run: runTypeConversionTest,
}

func init() {
	TypeConversionTestCmd.Flags().String("provider", "vertex", "LLM provider to test (vertex, openai, anthropic, bedrock)")
	TypeConversionTestCmd.Flags().String("model", "", "Model ID (uses provider default if not specified)")
	TypeConversionTestCmd.Flags().String("api-key", "", "API key (or set provider-specific env var)")
}

func runTypeConversionTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	providerStr, _ := cmd.Flags().GetString("provider")
	modelID, _ := cmd.Flags().GetString("model")
	apiKey, _ := cmd.Flags().GetString("api-key")

	// Set API key if provided
	if apiKey != "" {
		switch providerStr {
		case "vertex":
			os.Setenv("VERTEX_API_KEY", apiKey)
		case "openai":
			os.Setenv("OPENAI_API_KEY", apiKey)
		case "anthropic":
			os.Setenv("ANTHROPIC_API_KEY", apiKey)
		}
	}

	// Determine provider
	var provider llmproviders.Provider
	switch providerStr {
	case "vertex":
		provider = llmproviders.ProviderVertex
		if modelID == "" {
			modelID = "gemini-2.5-flash"
		}
	case "openai":
		provider = llmproviders.ProviderOpenAI
		if modelID == "" {
			modelID = "gpt-4o-mini"
		}
	case "anthropic":
		provider = llmproviders.ProviderAnthropic
		if modelID == "" {
			modelID = "claude-haiku-4-5-20251001"
		}
	case "bedrock":
		provider = llmproviders.ProviderBedrock
		if modelID == "" {
			modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		}
	default:
		log.Fatalf("Unknown provider: %s", providerStr)
	}

	// Initialize LLM
	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    provider,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     context.Background(),
	})
	if err != nil {
		log.Fatalf("Failed to initialize LLM: %v", err)
	}

	// Run the type conversion test
	RunTypeConversionTest(llmInstance, modelID)
}

