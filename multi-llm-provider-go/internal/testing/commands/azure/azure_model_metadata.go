package azure

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
)

var AzureModelMetadataTestCmd = &cobra.Command{
	Use:   "azure-model-metadata",
	Short: "Display Azure AI model metadata from API (pricing, context window, capabilities)",
	Long:  "Fetches and displays metadata for Azure AI models from the API. Use --model to get info for a specific model, or --all to list all available models.",
	Run:   runAzureModelMetadataTest,
}

type azureModelMetadataTestFlags struct {
	model    string
	listAll  bool
	chatOnly bool
}

var azureModelMetadataFlags azureModelMetadataTestFlags

func init() {
	AzureModelMetadataTestCmd.Flags().StringVar(&azureModelMetadataFlags.model, "model", "", "Azure AI model/deployment name to show metadata for")
	AzureModelMetadataTestCmd.Flags().BoolVar(&azureModelMetadataFlags.listAll, "all", false, "List all available Azure AI models from API")
	AzureModelMetadataTestCmd.Flags().BoolVar(&azureModelMetadataFlags.chatOnly, "chat-only", false, "Only show chat/completion models (filter out embeddings, whisper, etc)")
	AzureModelMetadataTestCmd.Flags().StringVar(&azureFlags.endpoint, "endpoint", "", "Azure AI endpoint (or set AZURE_AI_ENDPOINT env var)")
	AzureModelMetadataTestCmd.Flags().StringVar(&azureFlags.apiKey, "api-key", "", "Azure AI API key (or set AZURE_AI_API_KEY env var)")
}

func runAzureModelMetadataTest(cmd *cobra.Command, args []string) {
	// Load .env file if present
	_ = godotenv.Load(".env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	// Get endpoint from environment or flag
	endpoint := azureFlags.endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	if endpoint == "" {
		log.Fatal("Endpoint required: set --endpoint flag or AZURE_AI_ENDPOINT environment variable")
	}

	// Get API key from environment or flag
	apiKey := azureFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("AZURE_AI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or AZURE_AI_API_KEY environment variable")
	}

	if azureModelMetadataFlags.listAll || azureModelMetadataFlags.model == "" {
		// List all models from API
		logger.Infof("🧪 Fetching Azure AI models from API...")
		logger.Infof("📡 Endpoint: %s", endpoint)
		listAllAzureModelsFromAPI(endpoint, apiKey, azureModelMetadataFlags.chatOnly)
		return
	}

	// Show specific model metadata
	logger.Infof("🧪 Getting Azure AI model metadata for: %s", azureModelMetadataFlags.model)
	logger.Infof("📡 Endpoint: %s", endpoint)

	metadata, err := azure.GetAzureModelMetadataFromAPI(endpoint, apiKey, azureModelMetadataFlags.model)
	if err != nil {
		log.Fatalf("❌ Failed to get model metadata: %v", err)
	}

	displayModelMetadata(metadata)
	logger.Infof("✅ Model metadata retrieved successfully!")
}

func listAllAzureModelsFromAPI(endpoint, apiKey string, chatOnly bool) {
	startTime := time.Now()
	models, err := azure.GetAllAzureModelsFromAPI(endpoint, apiKey)
	fetchDuration := time.Since(startTime)

	if err != nil {
		log.Fatalf("❌ Failed to fetch models from API: %v", err)
	}

	// Sort by model ID for consistent output
	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelID < models[j].ModelID
	})

	// Group models by type
	chatModels := []*llmtypes.ModelMetadata{}
	reasoningModels := []*llmtypes.ModelMetadata{}
	embeddingModels := []*llmtypes.ModelMetadata{}
	otherModels := []*llmtypes.ModelMetadata{}

	for _, m := range models {
		modelLower := strings.ToLower(m.ModelID)

		// Categorize by model type
		if strings.Contains(modelLower, "embedding") || strings.Contains(modelLower, "embed") {
			embeddingModels = append(embeddingModels, m)
		} else if strings.HasPrefix(modelLower, "o1") || strings.HasPrefix(modelLower, "o3") || strings.HasPrefix(modelLower, "o4") {
			reasoningModels = append(reasoningModels, m)
		} else if strings.Contains(modelLower, "dall-e") ||
			strings.Contains(modelLower, "whisper") ||
			strings.Contains(modelLower, "tts") ||
			strings.Contains(modelLower, "babbage") ||
			strings.Contains(modelLower, "davinci") ||
			strings.Contains(modelLower, "curie") ||
			strings.Contains(modelLower, "ada") ||
			strings.Contains(modelLower, "flux") ||
			strings.Contains(modelLower, "stable-diffusion") ||
			strings.Contains(modelLower, "stable-image") ||
			strings.Contains(modelLower, "sora") ||
			strings.Contains(modelLower, "gpt-image") ||
			strings.Contains(modelLower, "gpt-realtime") ||
			strings.Contains(modelLower, "gpt-audio") ||
			strings.Contains(modelLower, "transcribe") ||
			strings.Contains(modelLower, "rerank") ||
			strings.Contains(modelLower, "codex") {
			otherModels = append(otherModels, m)
		} else if strings.Contains(modelLower, "gpt") ||
			strings.Contains(modelLower, "llama") ||
			strings.Contains(modelLower, "mistral") ||
			strings.Contains(modelLower, "phi-") ||
			strings.Contains(modelLower, "claude") ||
			strings.Contains(modelLower, "cohere-command") ||
			strings.Contains(modelLower, "deepseek") ||
			strings.Contains(modelLower, "jamba") ||
			strings.Contains(modelLower, "grok") ||
			strings.Contains(modelLower, "qwen") ||
			strings.Contains(modelLower, "jais") ||
			strings.Contains(modelLower, "kimi") ||
			strings.Contains(modelLower, "model-router") ||
			strings.Contains(modelLower, "computer-use") {
			chatModels = append(chatModels, m)
		} else {
			otherModels = append(otherModels, m)
		}
	}

	fmt.Println(repeat("=", 110))
	fmt.Printf("📋 Azure AI Models from API (%d models fetched in %v)\n", len(models), fetchDuration)
	fmt.Println(repeat("=", 110))

	// Display chat models
	if len(chatModels) > 0 {
		fmt.Println("\n🤖 Chat/Completion Models (GPT):")
		fmt.Println(repeat("-", 110))
		fmt.Printf("%-35s | %12s | %12s | %12s | %s\n",
			"Model ID", "Context", "Input $/1M", "Output $/1M", "Capabilities")
		fmt.Println(repeat("-", 110))
		for _, m := range chatModels {
			caps := getCapabilitiesString(m)
			costInfo := ""
			if m.InputCostPer1MTokens > 0 || m.OutputCostPer1MTokens > 0 {
				costInfo = fmt.Sprintf("%12.2f | %12.2f", m.InputCostPer1MTokens, m.OutputCostPer1MTokens)
			} else {
				costInfo = fmt.Sprintf("%12s | %12s", "N/A", "N/A")
			}
			fmt.Printf("%-35s | %12d | %s | %s\n",
				truncateString(m.ModelID, 35),
				m.ContextWindow,
				costInfo,
				caps)
		}
	}

	// Display reasoning models
	if len(reasoningModels) > 0 {
		fmt.Println("\n🧠 Reasoning Models (o1/o3):")
		fmt.Println(repeat("-", 110))
		fmt.Printf("%-35s | %12s | %12s | %12s | %12s\n",
			"Model ID", "Context", "Input $/1M", "Output $/1M", "Reasoning $/1M")
		fmt.Println(repeat("-", 110))
		for _, m := range reasoningModels {
			costInfo := ""
			if m.InputCostPer1MTokens > 0 || m.OutputCostPer1MTokens > 0 {
				costInfo = fmt.Sprintf("%12.2f | %12.2f | %12.2f",
					m.InputCostPer1MTokens, m.OutputCostPer1MTokens, m.ReasoningCostPer1MTokens)
			} else {
				costInfo = fmt.Sprintf("%12s | %12s | %12s", "N/A", "N/A", "N/A")
			}
			fmt.Printf("%-35s | %12d | %s\n",
				truncateString(m.ModelID, 35),
				m.ContextWindow,
				costInfo)
		}
	}

	// Display embedding models (unless chat-only)
	if !chatOnly && len(embeddingModels) > 0 {
		fmt.Println("\n📊 Embedding Models:")
		fmt.Println(repeat("-", 110))
		fmt.Printf("%-35s | %12s | %12s\n",
			"Model ID", "Context", "Cost $/1M")
		fmt.Println(repeat("-", 110))
		for _, m := range embeddingModels {
			costInfo := ""
			if m.InputCostPer1MTokens > 0 {
				costInfo = fmt.Sprintf("%12.2f", m.InputCostPer1MTokens)
			} else {
				costInfo = fmt.Sprintf("%12s", "N/A")
			}
			fmt.Printf("%-35s | %12d | %s\n",
				truncateString(m.ModelID, 35),
				m.ContextWindow,
				costInfo)
		}
	}

	// Display other models (unless chat-only)
	if !chatOnly && len(otherModels) > 0 {
		fmt.Println("\n🎨 Other Models (DALL-E, Whisper, TTS, Legacy):")
		fmt.Println(repeat("-", 110))
		fmt.Printf("%-35s | %s\n", "Model ID", "Type")
		fmt.Println(repeat("-", 110))
		for _, m := range otherModels {
			modelType := "other"
			modelLower := strings.ToLower(m.ModelID)
			if strings.Contains(modelLower, "dall-e") {
				modelType = "image-generation"
			} else if strings.Contains(modelLower, "whisper") {
				modelType = "speech-to-text"
			} else if strings.Contains(modelLower, "tts") {
				modelType = "text-to-speech"
			} else if strings.Contains(modelLower, "babbage") || strings.Contains(modelLower, "davinci") {
				modelType = "legacy-completion"
			}
			fmt.Printf("%-35s | %s\n",
				truncateString(m.ModelID, 35),
				modelType)
		}
	}

	fmt.Println(repeat("=", 110))
	fmt.Println("\n📝 Notes:")
	fmt.Println("   - Models fetched dynamically from Azure AI API")
	fmt.Println("   - Pricing data merged from static configuration (N/A = pricing not configured)")
	fmt.Println("   - Context window defaults to 128K for unknown models")
	fmt.Println("   - Use --chat-only flag to filter out non-chat models")
}

func displayModelMetadata(m *llmtypes.ModelMetadata) {
	fmt.Println()
	fmt.Println(repeat("=", 80))
	fmt.Printf("✅ Model Metadata: %s\n", m.ModelID)
	fmt.Println(repeat("=", 80))
	fmt.Printf("Model ID:        %s\n", m.ModelID)
	fmt.Printf("Model Name:      %s\n", m.ModelName)
	fmt.Printf("Provider:        %s\n", m.Provider)
	fmt.Printf("Context Window:  %d tokens\n", m.ContextWindow)
	fmt.Println(repeat("-", 80))
	fmt.Println("Pricing (per 1 million tokens):")
	if m.InputCostPer1MTokens > 0 || m.OutputCostPer1MTokens > 0 {
		fmt.Printf("  Input:         $%.2f\n", m.InputCostPer1MTokens)
		fmt.Printf("  Output:        $%.2f\n", m.OutputCostPer1MTokens)
		if m.ReasoningCostPer1MTokens > 0 {
			fmt.Printf("  Reasoning:     $%.2f\n", m.ReasoningCostPer1MTokens)
		}
	} else {
		fmt.Println("  (Pricing not configured for this model)")
	}
	if m.CachedInputCostPer1MTokens > 0 {
		fmt.Printf("  Cached Read:   $%.2f\n", m.CachedInputCostPer1MTokens)
	}
	if m.CachedInputCostWritePer1MTokens > 0 {
		fmt.Printf("  Cached Write:  $%.2f\n", m.CachedInputCostWritePer1MTokens)
	}
	fmt.Println(repeat("-", 80))
	fmt.Println("Capabilities:")
	fmt.Printf("  Tool Calls:       %v\n", m.SupportsToolCalls)
	fmt.Printf("  JSON Mode:        %v\n", m.SupportsJSONMode)
	fmt.Printf("  Reasoning Effort: %v\n", m.SupportsReasoningEffort)
	if m.SupportsReasoningEffort && len(m.ReasoningEffortLevels) > 0 {
		fmt.Printf("    Levels:         %v\n", m.ReasoningEffortLevels)
	}
	fmt.Printf("  Thinking Level:   %v\n", m.SupportsThinkingLevel)
	fmt.Printf("  Thinking Budget:  %v\n", m.SupportsThinkingBudget)
	fmt.Println(repeat("=", 80))
}

func getCapabilitiesString(m *llmtypes.ModelMetadata) string {
	caps := []string{}
	if m.SupportsToolCalls {
		caps = append(caps, "tools")
	}
	if m.SupportsJSONMode {
		caps = append(caps, "json")
	}
	if m.SupportsReasoningEffort {
		caps = append(caps, "reasoning")
	}
	if len(caps) == 0 {
		return "text"
	}
	return strings.Join(caps, ", ")
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Helper function to repeat a string
func repeat(s string, count int) string {
	var sb strings.Builder
	for i := 0; i < count; i++ {
		sb.WriteString(s)
	}
	return sb.String()
}
