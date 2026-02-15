package openrouter

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

var OpenRouterModelMetadataTestCmd = &cobra.Command{
	Use:   "openrouter-model-metadata",
	Short: "Test OpenRouter model metadata fetching",
	Long:  "Fetches and displays model metadata (pricing, context window) from OpenRouter API for a given model",
	Run:   runOpenRouterModelMetadataTest,
}

type openrouterModelMetadataTestFlags struct {
	model string
}

var openrouterModelMetadataFlags openrouterModelMetadataTestFlags

func init() {
	OpenRouterModelMetadataTestCmd.Flags().StringVar(&openrouterModelMetadataFlags.model, "model", "mistralai/devstral-2512", "OpenRouter model ID to test (format: provider/model-name)")
}

func runOpenRouterModelMetadataTest(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	modelID := openrouterModelMetadataFlags.model
	if modelID == "" {
		log.Fatal("Model ID is required: use --model flag")
	}

	logger.Infof("🧪 Testing OpenRouter model metadata for: %s", modelID)
	logger.Infof("📡 Fetching metadata from OpenRouter API...")

	// Fetch model metadata
	metadata, err := openai.GetOpenRouterModelMetadata(modelID)
	if err != nil {
		log.Fatalf("❌ Failed to fetch model metadata: %v", err)
	}

	// Display metadata
	fmt.Println("\n" + repeat("=", 80))
	fmt.Printf("✅ Model Metadata Retrieved Successfully\n")
	fmt.Println(repeat("=", 80))
	fmt.Printf("Model ID:        %s\n", metadata.ModelID)
	fmt.Printf("Model Name:      %s\n", metadata.ModelName)
	fmt.Printf("Provider:        %s\n", metadata.Provider)
	fmt.Printf("Context Window:  %d tokens\n", metadata.ContextWindow)
	fmt.Println(repeat("-", 80))
	fmt.Println("Pricing (per 1 million tokens):")
	fmt.Printf("  Input:         $%.2f\n", metadata.InputCostPer1MTokens)
	fmt.Printf("  Output:        $%.2f\n", metadata.OutputCostPer1MTokens)
	if metadata.ReasoningCostPer1MTokens > 0 {
		fmt.Printf("  Reasoning:     $%.2f\n", metadata.ReasoningCostPer1MTokens)
	}
	if metadata.CachedInputCostPer1MTokens > 0 {
		fmt.Printf("  Cached Read:   $%.2f\n", metadata.CachedInputCostPer1MTokens)
	}
	if metadata.CachedInputCostWritePer1MTokens > 0 {
		fmt.Printf("  Cached Write:  $%.2f\n", metadata.CachedInputCostWritePer1MTokens)
	}
	fmt.Println(repeat("=", 80))

	// Validate metadata
	if metadata.ModelID == "" {
		log.Fatal("❌ Model ID is empty")
	}
	if metadata.ModelName == "" {
		log.Fatal("❌ Model name is empty")
	}
	if metadata.ContextWindow <= 0 {
		log.Fatalf("❌ Invalid context window: %d", metadata.ContextWindow)
	}
	if metadata.InputCostPer1MTokens == 0 && metadata.OutputCostPer1MTokens == 0 {
		log.Fatal("❌ Both input and output costs are zero - pricing information may be missing")
	}

	logger.Infof("✅ Model metadata test passed!")
	logger.Infof("   Model: %s", metadata.ModelName)
	logger.Infof("   Context Window: %d tokens", metadata.ContextWindow)
	logger.Infof("   Input Cost: $%.2f per 1M tokens", metadata.InputCostPer1MTokens)
	logger.Infof("   Output Cost: $%.2f per 1M tokens", metadata.OutputCostPer1MTokens)
}

// Helper function to repeat a string
func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
