package llmproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"llm-providers/interfaces"
	"llm-providers/llmtypes"
	anthropicadapter "llm-providers/pkg/adapters/anthropic"
	bedrockadapter "llm-providers/pkg/adapters/bedrock"
	openaiadapter "llm-providers/pkg/adapters/openai"
	vertexadapter "llm-providers/pkg/adapters/vertex"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openaisdk "github.com/openai/openai-go/v3"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/openai/openai-go/v3/option"

	"google.golang.org/genai"
)

// Provider represents the available LLM providers
type Provider string

const (
	ProviderBedrock    Provider = "bedrock"
	ProviderOpenAI     Provider = "openai"
	ProviderAnthropic  Provider = "anthropic"
	ProviderOpenRouter Provider = "openrouter"
	ProviderVertex     Provider = "vertex"
)

// Config holds configuration for LLM initialization
type Config struct {
	Provider    Provider
	ModelID     string
	Temperature float64
	// EventEmitter for emitting LLM events (replaces Tracers)
	EventEmitter interfaces.EventEmitter
	TraceID      interfaces.TraceID
	// Fallback configuration for rate limiting
	FallbackModels []string
	MaxRetries     int
	// Logger for structured logging
	Logger interfaces.Logger
	// Context for LLM initialization (optional, uses background with timeout if not provided)
	Context context.Context
}

// InitializeLLM creates and initializes an LLM based on the provider configuration
func InitializeLLM(config Config) (llmtypes.Model, error) {
	var llm llmtypes.Model
	var err error

	switch config.Provider {
	case ProviderBedrock:
		llm, err = initializeBedrockWithFallback(config)
	case ProviderOpenAI:
		llm, err = initializeOpenAIWithFallback(config)
	case ProviderAnthropic:
		llm, err = initializeAnthropic(config)
	case ProviderOpenRouter:
		llm, err = initializeOpenRouterWithFallback(config)
	case ProviderVertex:
		llm, err = initializeVertexWithFallback(config)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}

	if err != nil {
		return nil, err
	}

	// Wrap the LLM with provider information and tracing
	return NewProviderAwareLLM(llm, config.Provider, config.ModelID, config.EventEmitter, config.TraceID, config.Logger), nil
}

// InitializeEmbeddingModel creates and initializes an embedding model based on the provider configuration
// Supported providers: OpenAI, OpenRouter, Vertex AI, Bedrock
func InitializeEmbeddingModel(config Config) (llmtypes.EmbeddingModel, error) {
	var embeddingModel llmtypes.EmbeddingModel
	var err error

	switch config.Provider {
	case ProviderOpenAI:
		embeddingModel, err = initializeOpenAIEmbedding(config)
	case ProviderOpenRouter:
		// OpenRouter uses OpenAI-compatible API, so we can use OpenAI adapter
		embeddingModel, err = initializeOpenAIEmbedding(config)
	case ProviderVertex:
		embeddingModel, err = initializeVertexEmbedding(config)
	case ProviderBedrock:
		embeddingModel, err = initializeBedrockEmbedding(config)
	default:
		return nil, fmt.Errorf("embedding generation not supported for provider: %s. Supported providers: openai, openrouter, vertex, bedrock", config.Provider)
	}

	if err != nil {
		return nil, err
	}

	return embeddingModel, nil
}

// initializeOpenAIEmbedding creates and configures an OpenAI embedding model instance
func initializeOpenAIEmbedding(config Config) (llmtypes.EmbeddingModel, error) {
	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required for OpenAI embedding provider")
	}

	// Set default embedding model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "text-embedding-3-small"
	}

	// Create OpenAI client using official SDK
	client := openaisdk.NewClient(
		option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
	)

	// Create OpenAI adapter (it implements both Model and EmbeddingModel interfaces)
	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	embeddingModel := openaiadapter.NewOpenAIAdapter(&client, modelID, logger)

	logger.Infof("Initialized OpenAI Embedding Model - model_id: %s", modelID)
	return embeddingModel, nil
}

// initializeVertexEmbedding creates and configures a Vertex AI embedding model instance
func initializeVertexEmbedding(config Config) (llmtypes.EmbeddingModel, error) {
	// Set default embedding model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "text-embedding-004" // Latest Vertex AI embedding model
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	logger.Infof("Initializing Vertex AI Embedding Model - model_id: %s", modelID)

	// Check for API key from environment
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		// Try alternative environment variable names
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required for Vertex AI embedding models")
	}

	// Use provided context or use background context
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Create Google GenAI client with API key authentication
	// Using BackendGeminiAPI for Gemini Developer API
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GenAI client: %w", err)
	}

	// Create Vertex adapter (it implements both Model and EmbeddingModel interfaces)
	embeddingModel := vertexadapter.NewGoogleGenAIAdapter(client, modelID, logger)

	logger.Infof("Initialized Vertex AI Embedding Model - model_id: %s", modelID)
	return embeddingModel, nil
}

// initializeBedrockEmbedding creates and configures a Bedrock embedding model instance
func initializeBedrockEmbedding(config Config) (llmtypes.EmbeddingModel, error) {
	// Set default embedding model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "amazon.titan-embed-text-v1" // Default Bedrock embedding model
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	logger.Infof("Initializing Bedrock Embedding Model - model_id: %s", modelID)

	// Create AWS config
	cfg, err := awsconfig.LoadDefaultConfig(config.Context)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Bedrock runtime client
	client := bedrockruntime.NewFromConfig(cfg)

	// Create Bedrock adapter (it implements both Model and EmbeddingModel interfaces)
	embeddingModel := bedrockadapter.NewBedrockAdapter(client, modelID, logger)

	logger.Infof("Initialized Bedrock Embedding Model - model_id: %s", modelID)
	return embeddingModel, nil
}

// initializeBedrockWithFallback creates a Bedrock LLM with fallback models for rate limiting
func initializeBedrockWithFallback(config Config) (llmtypes.Model, error) {
	// Try primary model first
	llm, err := initializeBedrock(config)
	if err == nil {
		return llm, nil
	}

	// If primary fails and we have fallback models, try them
	if len(config.FallbackModels) > 0 {
		logger := config.Logger
		logger.Infof("Primary Bedrock model failed, trying fallback models - primary_model: %s, fallback_models: %v, error: %s", config.ModelID, config.FallbackModels, err.Error())

		for _, fallbackModel := range config.FallbackModels {
			fallbackConfig := config
			fallbackConfig.ModelID = fallbackModel

			llm, err := initializeBedrock(fallbackConfig)
			if err == nil {
				logger.Infof("Successfully initialized fallback Bedrock model - fallback_model: %s", fallbackModel)
				return llm, nil
			}

			logger.Infof("Fallback Bedrock model failed - fallback_model: %s, error: %s", fallbackModel, err.Error())
		}
	}

	// If all models fail, return the original error
	return nil, fmt.Errorf("all Bedrock models failed: %w", err)
}

// initializeOpenAIWithFallback creates an OpenAI LLM with fallback models for rate limiting
func initializeOpenAIWithFallback(config Config) (llmtypes.Model, error) {
	// Try primary model first
	llm, err := initializeOpenAI(config)
	if err == nil {
		return llm, nil
	}

	// If primary fails and we have fallback models, try them
	if len(config.FallbackModels) > 0 {
		logger := config.Logger
		logger.Infof("Primary OpenAI model failed, trying fallback models - primary_model: %s, fallback_models: %v, error: %s", config.ModelID, config.FallbackModels, err.Error())

		for _, fallbackModel := range config.FallbackModels {
			fallbackConfig := config
			fallbackConfig.ModelID = fallbackModel

			llm, err := initializeOpenAI(fallbackConfig)
			if err == nil {
				logger.Infof("Successfully initialized fallback OpenAI model - fallback_model: %s", fallbackModel)
				return llm, nil
			}

			logger.Infof("Fallback OpenAI model failed - fallback_model: %s, error: %s", fallbackModel, err.Error())
		}
	}

	// If all models fail, return the original error
	return nil, fmt.Errorf("all OpenAI models failed: %w", err)
}

// initializeOpenRouterWithFallback creates an OpenRouter LLM with fallback models for rate limiting
func initializeOpenRouterWithFallback(config Config) (llmtypes.Model, error) {
	// Try primary model first
	llm, err := initializeOpenRouter(config)
	if err == nil {
		return llm, nil
	}

	// If primary fails and we have fallback models, try them
	if len(config.FallbackModels) > 0 {
		logger := config.Logger
		logger.Infof("Primary OpenRouter model failed, trying fallback models - primary_model: %s, fallback_models: %v, error: %s", config.ModelID, config.FallbackModels, err.Error())

		for _, fallbackModel := range config.FallbackModels {
			fallbackConfig := config
			fallbackConfig.ModelID = fallbackModel

			llm, err := initializeOpenRouter(fallbackConfig)
			if err == nil {
				logger.Infof("Successfully initialized fallback OpenRouter model - fallback_model: %s", fallbackModel)
				return llm, nil
			}

			logger.Infof("Fallback OpenRouter model failed - fallback_model: %s, error: %s", fallbackModel, err.Error())
		}
	}

	// If all models fail, return the original error
	return nil, fmt.Errorf("all OpenRouter models failed: %w", err)
}

// initializeVertexWithFallback creates a Vertex AI LLM with fallback models for rate limiting
func initializeVertexWithFallback(config Config) (llmtypes.Model, error) {
	// Try primary model first
	llm, err := initializeVertex(config)
	if err == nil {
		return llm, nil
	}

	// If primary fails and we have fallback models, try them
	if len(config.FallbackModels) > 0 {
		logger := config.Logger
		logger.Infof("Primary Vertex model failed, trying fallback models - primary_model: %s, fallback_models: %v, error: %s", config.ModelID, config.FallbackModels, err.Error())

		for _, fallbackModel := range config.FallbackModels {
			fallbackConfig := config
			fallbackConfig.ModelID = fallbackModel

			llm, err := initializeVertex(fallbackConfig)
			if err == nil {
				logger.Infof("Successfully initialized fallback Vertex model - fallback_model: %s", fallbackModel)
				return llm, nil
			}

			logger.Infof("Fallback Vertex model failed - fallback_model: %s, error: %s", fallbackModel, err.Error())
		}
	}

	// If all models fail, return the original error
	return nil, fmt.Errorf("all Vertex models failed: %w", err)
}

// initializeBedrock creates and configures a Bedrock LLM instance
func initializeBedrock(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data - use typed structure directly
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    40000, // Will be set at call time
		TopP:         config.Temperature,
		User:         "bedrock_user",
		CustomFields: map[string]string{
			"provider":  "bedrock",
			"operation": "llm_initialization",
		},
	}

	var logger = config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Debug: Log AWS environment variables
	logger.Infof("Initializing Bedrock LLM with model: %s", config.ModelID)
	logger.Infof("AWS_REGION: %s", os.Getenv("AWS_REGION"))
	logger.Infof("AWS_ACCESS_KEY_ID: %s", os.Getenv("AWS_ACCESS_KEY_ID"))
	logger.Infof("AWS_SECRET_ACCESS_KEY: %s", os.Getenv("AWS_SECRET_ACCESS_KEY"))

	// Get region from environment (default to us-east-1)
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
		logger.Infof("AWS_REGION not set, using default: %s", region)
	}

	// Load AWS SDK configuration
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(region))
	if err != nil {
		logger.Errorf("Failed to load AWS config: %w", err)

		// Emit LLM initialization error event - use typed structure directly
		errorMetadata := LLMMetadata{
			ModelVersion: config.ModelID,
			User:         "bedrock_user",
			CustomFields: map[string]string{
				"provider":  "bedrock",
				"operation": OperationLLMInitialization,
				"error":     err.Error(),
				"status":    StatusLLMFailed,
			},
		}
		emitLLMInitializationError(config.EventEmitter, string(config.Provider), config.ModelID, OperationLLMInitialization, err, config.TraceID, errorMetadata)

		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// Create Bedrock runtime client
	client := bedrockruntime.NewFromConfig(cfg)

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "us.anthropic.claude-3-sonnet-20240229-v1:0"
	}

	// Create Bedrock adapter
	llm := bedrockadapter.NewBedrockAdapter(client, modelID, logger)

	// Emit LLM initialization success event - use typed structure directly
	successMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		User:         "bedrock_user",
		CustomFields: map[string]string{
			"provider":     "bedrock",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), config.ModelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Bedrock LLM - model_id: %s", config.ModelID)
	return llm, nil
}

// IsO3O4Model detects o3/o4 models (OpenAI) for conditional logic in agent
func IsO3O4Model(modelID string) bool {
	// Covers gpt-4o, gpt-4.0, gpt-4.1, gpt-4, gpt-3.5, etc
	return strings.HasPrefix(modelID, "o3") ||
		strings.HasPrefix(modelID, "o4")
}

// extractTokenUsageFromGenerationInfo extracts token usage from GenerationInfo
func extractTokenUsageFromGenerationInfo(generationInfo *llmtypes.GenerationInfo) TokenUsage {
	usage := TokenUsage{Unit: "TOKENS"}

	if generationInfo == nil {
		return usage
	}

	// Extract input tokens (check multiple naming conventions in priority order)
	if generationInfo.InputTokens != nil {
		usage.InputTokens = *generationInfo.InputTokens
	} else if generationInfo.InputTokensCap != nil {
		usage.InputTokens = *generationInfo.InputTokensCap
	} else if generationInfo.PromptTokens != nil {
		usage.InputTokens = *generationInfo.PromptTokens
	} else if generationInfo.PromptTokensCap != nil {
		usage.InputTokens = *generationInfo.PromptTokensCap
	}

	// Extract output tokens (check multiple naming conventions in priority order)
	if generationInfo.OutputTokens != nil {
		usage.OutputTokens = *generationInfo.OutputTokens
	} else if generationInfo.OutputTokensCap != nil {
		usage.OutputTokens = *generationInfo.OutputTokensCap
	} else if generationInfo.CompletionTokens != nil {
		usage.OutputTokens = *generationInfo.CompletionTokens
	} else if generationInfo.CompletionTokensCap != nil {
		usage.OutputTokens = *generationInfo.CompletionTokensCap
	}

	// Extract total tokens (check multiple naming conventions in priority order)
	if generationInfo.TotalTokens != nil {
		usage.TotalTokens = *generationInfo.TotalTokens
	} else if generationInfo.TotalTokensCap != nil {
		usage.TotalTokens = *generationInfo.TotalTokensCap
	}

	// Calculate total tokens if not provided by the provider
	if usage.TotalTokens == 0 && usage.InputTokens > 0 && usage.OutputTokens > 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}

	return usage
}

// Helper functions for event emission
func emitLLMInitializationStart(emitter interfaces.EventEmitter, provider string, modelID string, temperature float64, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitLLMInitializationStart(provider, modelID, temperature, traceID, metadata)
	}
}

func emitLLMInitializationSuccess(emitter interfaces.EventEmitter, provider string, modelID string, capabilities string, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitLLMInitializationSuccess(provider, modelID, capabilities, traceID, metadata)
	}
}

func emitLLMInitializationError(emitter interfaces.EventEmitter, provider string, modelID string, operation string, err error, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitLLMInitializationError(provider, modelID, operation, err, traceID, metadata)
	}
}

func emitLLMGenerationSuccess(emitter interfaces.EventEmitter, provider string, modelID string, operation string, messages int, temperature float64, messageContent string, responseLength int, choicesCount int, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitLLMGenerationSuccess(provider, modelID, operation, messages, temperature, messageContent, responseLength, choicesCount, traceID, metadata)
	}
}

func emitLLMGenerationError(emitter interfaces.EventEmitter, provider string, modelID string, operation string, messages int, temperature float64, messageContent string, err error, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitLLMGenerationError(provider, modelID, operation, messages, temperature, messageContent, err, traceID, metadata)
	}
}

// initializeOpenAI creates and configures an OpenAI LLM instance
func initializeOpenAI(config Config) (llmtypes.Model, error) {
	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required for OpenAI provider")
	}

	// LLM Initialization event data - use typed structure directly
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time
		TopP:         config.Temperature,
		User:         "openai_user",
		CustomFields: map[string]string{
			"provider":  "openai",
			"operation": "llm_initialization",
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "gpt-4.1"
	}

	// Create OpenAI client using official SDK
	client := openaisdk.NewClient(
		option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
	)

	// Create OpenAI adapter
	logger := config.Logger
	llm := openaiadapter.NewOpenAIAdapter(&client, modelID, logger)

	// Emit LLM initialization success event - use typed structure directly
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "openai_user",
		CustomFields: map[string]string{
			"provider":     "openai",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized OpenAI LLM - model_id: %s", modelID)
	return llm, nil
}

// initializeAnthropic creates and configures an Anthropic LLM instance
func initializeAnthropic(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data - use typed structure directly
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time
		TopP:         config.Temperature,
		User:         "anthropic_user",
		CustomFields: map[string]string{
			"provider":  "anthropic",
			"operation": "llm_initialization",
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
	}

	// Use provided model or default
	modelID := config.ModelID
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}

	logger := config.Logger
	logger.Infof("Initializing Anthropic LLM with model: %s", modelID)

	// Create Anthropic SDK client
	// NewClient reads from environment by default, but we can explicitly set API key
	// Note: Beta header for prompt caching must be added per-request, not at client level
	client := anthropic.NewClient(
		anthropicoption.WithAPIKey(apiKey),
	)

	// Create Anthropic adapter
	llm := anthropicadapter.NewAnthropicAdapter(client, modelID, logger)

	// Emit LLM initialization success event - use typed structure directly
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "anthropic_user",
		CustomFields: map[string]string{
			"provider":     "anthropic",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Anthropic LLM - model_id: %s", modelID)
	return llm, nil
}

// initializeOpenRouter creates and configures an OpenRouter LLM instance
func initializeOpenRouter(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data - use typed structure directly
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time
		TopP:         config.Temperature,
		User:         "openrouter_user",
		CustomFields: map[string]string{
			"provider":  "openrouter",
			"operation": OperationLLMInitialization,
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Check for API key
	if os.Getenv("OPEN_ROUTER_API_KEY") == "" {
		return nil, fmt.Errorf("OPEN_ROUTER_API_KEY environment variable is required for OpenRouter provider")
	}

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	logger := config.Logger
	logger.Infof("🔧 Initializing OpenRouter LLM - model_id: %s, base_url: https://openrouter.ai/api/v1", modelID)

	// 🆕 DETAILED OPENROUTER INITIALIZATION LOGGING
	logger.Infof("🔧 [DEBUG] Creating OpenRouter LLM with OpenAI client...")
	logger.Infof("🔧 [DEBUG] Model: %s", modelID)
	logger.Infof("🔧 [DEBUG] Base URL: https://openrouter.ai/api/v1")
	logger.Infof("🔧 [DEBUG] API Key present: %v", os.Getenv("OPEN_ROUTER_API_KEY") != "")

	// Create OpenAI SDK client with OpenRouter base URL
	clientOptions := []option.RequestOption{
		option.WithAPIKey(os.Getenv("OPEN_ROUTER_API_KEY")),
		option.WithBaseURL("https://openrouter.ai/api/v1"),
	}

	// Add optional OpenRouter headers if provided
	if httpReferer := os.Getenv("OPENROUTER_HTTP_REFERER"); httpReferer != "" {
		clientOptions = append(clientOptions, option.WithHeader("HTTP-Referer", httpReferer))
		logger.Infof("🔧 [DEBUG] Added HTTP-Referer header: %s", httpReferer)
	}
	if xTitle := os.Getenv("OPENROUTER_X_TITLE"); xTitle != "" {
		clientOptions = append(clientOptions, option.WithHeader("X-Title", xTitle))
		logger.Infof("🔧 [DEBUG] Added X-Title header: %s", xTitle)
	}

	client := openaisdk.NewClient(clientOptions...)

	// Create OpenAI adapter with OpenRouter configuration
	llm := openaiadapter.NewOpenAIAdapter(&client, modelID, logger)

	// 🆕 POST-INITIALIZATION LOGGING
	logger.Infof("🔧 [DEBUG] OpenRouter LLM creation completed - LLM: %v", llm != nil)

	// Emit LLM initialization success event - use typed structure directly
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "openrouter_user",
		CustomFields: map[string]string{
			"provider":     "openrouter",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("✅ Successfully initialized OpenRouter LLM - model_id: %s", modelID)
	return llm, nil
}

// initializeVertex creates and configures a Vertex AI LLM instance
// Supports both Gemini (via API key) and Anthropic (via OAuth2) models
func initializeVertex(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data - use typed structure directly
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time
		TopP:         config.Temperature,
		User:         "vertex_user",
		CustomFields: map[string]string{
			"provider":  "vertex",
			"operation": "llm_initialization",
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	logger := config.Logger

	// Detect if this is an Anthropic model (starts with "claude-\n")
	isAnthropicModel := strings.HasPrefix(modelID, "claude-")

	if isAnthropicModel {
		// Initialize Vertex AI Anthropic adapter
		return initializeVertexAnthropic(config, modelID, logger)
	}

	// Initialize Gemini adapter (existing implementation)
	return initializeVertexGemini(config, modelID, logger)
}

// initializeVertexAnthropic creates and configures a Vertex AI Anthropic LLM instance
func initializeVertexAnthropic(config Config, modelID string, logger interfaces.Logger) (llmtypes.Model, error) {
	logger.Infof("Initializing Vertex AI Anthropic LLM - model_id: %s", modelID)

	// Get required configuration
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("VERTEX_PROJECT_ID environment variable is required for Anthropic models")
	}

	locationID := os.Getenv("VERTEX_LOCATION_ID")
	if locationID == "" {
		locationID = "global" // Default location
		logger.Infof("VERTEX_LOCATION_ID not set, using default: %s", locationID)
	}

	// Create Vertex Anthropic adapter
	llm := vertexadapter.NewVertexAnthropicAdapter(projectID, locationID, modelID, logger)

	// Emit LLM initialization success event
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "vertex_user",
		CustomFields: map[string]string{
			"provider":     "vertex",
			"model_type":   "anthropic",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Vertex AI Anthropic LLM - model_id: %s, project: %s, location: %s", modelID, projectID, locationID)
	return llm, nil
}

// initializeVertexGemini creates and configures a Vertex AI Gemini LLM instance
func initializeVertexGemini(config Config, modelID string, logger interfaces.Logger) (llmtypes.Model, error) {
	logger.Infof("Initializing Vertex AI (Gemini) LLM with API key - model_id: %s", modelID)

	// Check for API key from environment
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		// Try alternative environment variable names
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required for Gemini models")
	}

	// Use provided context or use background context
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Create Google GenAI client with API key authentication
	// Using BackendGeminiAPI for Gemini Developer API
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		logger.Errorf("Failed to create GenAI client: %w", err)

		// Emit LLM initialization error event
		errorMetadata := LLMMetadata{
			ModelVersion: modelID,
			User:         "vertex_user",
			CustomFields: map[string]string{
				"provider":   "vertex",
				"model_type": "gemini",
				"operation":  OperationLLMInitialization,
				"error":      err.Error(),
				"status":     StatusLLMFailed,
			},
		}
		emitLLMInitializationError(config.EventEmitter, string(config.Provider), modelID, OperationLLMInitialization, err, config.TraceID, errorMetadata)

		return nil, fmt.Errorf("create genai client: %w", err)
	}

	// Create adapter wrapper that implements llmtypes.Model interface
	llm := vertexadapter.NewGoogleGenAIAdapter(client, modelID, logger)

	// Emit LLM initialization success event - use typed structure directly
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "vertex_user",
		CustomFields: map[string]string{
			"provider":     "vertex",
			"model_type":   "gemini",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Vertex AI Gemini LLM - model_id: %s", modelID)
	return llm, nil
}

// GetDefaultModel returns the default model for each provider from environment variables
func GetDefaultModel(provider Provider) string {
	switch provider {
	case ProviderBedrock:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("BEDROCK_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "us.anthropic.claude-sonnet-4-20250514-v1:0"
	case ProviderOpenAI:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("OPENAI_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "gpt-4.1-mini"
	case ProviderAnthropic:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("ANTHROPIC_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "claude-3-5-sonnet-20241022"
	case ProviderOpenRouter:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("OPENROUTER_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "moonshotai/kimi-k2"
	case ProviderVertex:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("VERTEX_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "gemini-2.5-flash"
	default:
		return ""
	}
}

// GetDefaultFallbackModels returns fallback models for each provider from environment variables
func GetDefaultFallbackModels(provider Provider) []string {
	switch provider {
	case ProviderBedrock:
		// Get Bedrock fallback models from environment variable
		fallbackModelsEnv := os.Getenv("BEDROCK_FALLBACK_MODELS")
		if fallbackModelsEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(fallbackModelsEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No fallback models if environment variable is not set
		return []string{}
	case ProviderOpenAI:
		// Get fallback models from environment variable
		fallbackModelsEnv := os.Getenv("OPENAI_FALLBACK_MODELS")
		if fallbackModelsEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(fallbackModelsEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No fallback models if environment variable is not set
		return []string{}
	case ProviderOpenRouter:
		// Get fallback models from environment variable
		fallbackModelsEnv := os.Getenv("OPENROUTER_FALLBACK_MODELS")
		if fallbackModelsEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(fallbackModelsEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No fallback models if environment variable is not set
		return []string{}
	case ProviderVertex:
		// Get fallback models from environment variable
		fallbackModelsEnv := os.Getenv("VERTEX_FALLBACK_MODELS")
		if fallbackModelsEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(fallbackModelsEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No fallback models if environment variable is not set
		return []string{}
	default:
		return []string{}
	}
}

// GetCrossProviderFallbackModels returns cross-provider fallback models (e.g., OpenAI for Bedrock)
func GetCrossProviderFallbackModels(provider Provider) []string {
	switch provider {
	case ProviderBedrock:
		// Get OpenAI cross-provider fallback models
		openaiFallbackEnv := os.Getenv("BEDROCK_OPENAI_FALLBACK_MODELS")
		if openaiFallbackEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(openaiFallbackEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No cross-provider fallbacks if environment variable is not set
		return []string{}
	case ProviderOpenAI:
		// For OpenAI provider, no cross-provider fallbacks by default
		return []string{}
	case ProviderOpenRouter:
		// Get cross-provider fallback models for OpenRouter
		crossFallbackEnv := os.Getenv("OPENROUTER_CROSS_FALLBACK_MODELS")
		if crossFallbackEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(crossFallbackEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No cross-provider fallbacks if environment variable is not set
		return []string{}
	case ProviderVertex:
		// Get Anthropic cross-provider fallback models for Vertex
		anthropicFallbackEnv := os.Getenv("VERTEX_ANTHROPIC_FALLBACK_MODELS")
		if anthropicFallbackEnv != "" {
			// Split by comma and trim whitespace
			models := strings.Split(anthropicFallbackEnv, ",")
			for i, model := range models {
				models[i] = strings.TrimSpace(model)
			}
			return models
		}
		// No cross-provider fallbacks if environment variable is not set
		return []string{}
	default:
		return []string{}
	}
}

// ValidateProvider checks if the provider is supported
func ValidateProvider(provider string) (Provider, error) {
	switch Provider(provider) {
	case ProviderBedrock, ProviderOpenAI, ProviderAnthropic, ProviderOpenRouter, ProviderVertex:
		return Provider(provider), nil
	default:
		return "", fmt.Errorf("unsupported provider: %s. Supported providers: bedrock, openai, anthropic, openrouter, vertex", provider)
	}
}

// ProviderAwareLLM is a wrapper around LLM that preserves provider information
// and automatically captures token usage in LLM events
type ProviderAwareLLM struct {
	llmtypes.Model
	provider     Provider
	modelID      string
	eventEmitter interfaces.EventEmitter
	traceID      interfaces.TraceID
	logger       interfaces.Logger
}

// NewProviderAwareLLM creates a new provider-aware LLM wrapper
func NewProviderAwareLLM(llm llmtypes.Model, provider Provider, modelID string, eventEmitter interfaces.EventEmitter, traceID interfaces.TraceID, logger interfaces.Logger) *ProviderAwareLLM {
	return &ProviderAwareLLM{
		Model:        llm,
		provider:     provider,
		modelID:      modelID,
		eventEmitter: eventEmitter,
		traceID:      traceID,
		logger:       logger,
	}
}

// GetProvider returns the provider of this LLM
func (p *ProviderAwareLLM) GetProvider() Provider {
	return p.provider
}

// GetModelID returns the model ID of this LLM
func (p *ProviderAwareLLM) GetModelID() string {
	return p.modelID
}

// GenerateContent wraps the underlying LLM's GenerateContent method to automatically capture token usage
func (p *ProviderAwareLLM) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Note: LLM generation start event is now emitted at the agent level to avoid duplication

	// 🆕 DETAILED DEBUG LOGGING - Track execution flow
	startTime := time.Now()
	p.logger.Infof("🚀 [DEBUG] GenerateContent START - Provider: %s, Model: %s, Messages: %d",
		string(p.provider), p.modelID, len(messages))

	// 🆕 CONTEXT DEBUGGING
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		p.logger.Infof("⏰ [DEBUG] Context deadline: %v, Time until deadline: %v", deadline, timeUntilDeadline)
	} else {
		p.logger.Infof("⏰ [DEBUG] Context has no deadline")
	}

	// 🆕 GOROUTINE DEBUGGING
	p.logger.Infof("🧵 [DEBUG] Goroutine count before LLM call: %d", runtime.NumGoroutine())

	// Automatically add usage parameter for OpenRouter requests to get cache token information
	if p.provider == ProviderOpenRouter {
		p.logger.Infof("🔧 Adding OpenRouter usage parameter for cache token information")
		options = append(options, WithOpenRouterUsage())
		p.logger.Infof("🔧 OpenRouter options count after adding usage parameter: %d", len(options))

		// 🆕 DETAILED OPENROUTER DEBUGGING
		p.logger.Infof("🔧 [DEBUG] About to call OpenRouter API - Time: %v", time.Now())
		p.logger.Infof("🔧 [DEBUG] OpenRouter request details - Messages: %d, Options: %d", len(messages), len(options))

		// Log message content lengths for debugging
		for i, msg := range messages {
			contentLength := 0
			for _, part := range msg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					contentLength += len(textPart.Text)
				}
			}
			p.logger.Infof("🔧 [DEBUG] Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
		}
	}

	// 🆕 TIMING DEBUGGING - Track the actual LLM call
	llmCallStart := time.Now()
	p.logger.Infof("📞 [DEBUG] About to call p.Model.GenerateContent - Time: %v", llmCallStart)

	// 🆕 DETAILED EXECUTION TRACKING
	p.logger.Infof("🔍 [DEBUG] Context details - Err: %v, Done: %v", ctx.Err(), ctx.Done())
	p.logger.Infof("🔍 [DEBUG] Options count: %d", len(options))
	for i, opt := range options {
		p.logger.Infof("🔍 [DEBUG] Option %d: %T", i+1, opt)
	}
	p.logger.Infof("🔍 [DEBUG] Messages count: %d", len(messages))
	p.logger.Infof("🔍 [DEBUG] About to call underlying LLM.GenerateContent...")

	// Call the underlying LLM
	resp, err := p.Model.GenerateContent(ctx, messages, options...)

	// 🆕 IMMEDIATE POST-CALL LOGGING
	p.logger.Infof("🔍 [DEBUG] Underlying LLM.GenerateContent returned - Time: %v", time.Now())
	p.logger.Infof("🔍 [DEBUG] Return values - Error: %v, Response: %w", err != nil, resp != nil)

	// 🆕 TIMING DEBUGGING - Track LLM call completion
	llmCallDuration := time.Since(llmCallStart)
	totalDuration := time.Since(startTime)
	p.logger.Infof("📞 [DEBUG] p.Model.GenerateContent completed - Duration: %v, Total duration: %v", llmCallDuration, totalDuration)

	// 🆕 POST-CALL DEBUGGING
	p.logger.Infof("🧵 [DEBUG] Goroutine count after LLM call: %d", runtime.NumGoroutine())
	if err != nil {
		p.logger.Infof("❌ [DEBUG] LLM call failed - Error: %v, Error type: %T", err, err)
	} else {
		p.logger.Infof("✅ [DEBUG] LLM call succeeded - Response: %v", resp != nil)
	}

	// 🆕 ENHANCED BEDROCK RESPONSE DEBUGGING
	p.logger.Infof("🔍 Raw Bedrock response received - err: %v, resp: %w", err, resp != nil)

	// 🆕 DETAILED BEDROCK RESPONSE ANALYSIS
	if resp != nil {
		p.logger.Infof("🔍 Response type: %T", resp)
		p.logger.Infof("🔍 Response pointer: %p", resp)
		p.logger.Infof("🔍 Response.Choices pointer: %p", resp.Choices)
		if resp.Choices != nil {
			p.logger.Infof("🔍 Response.Choices length: %d", len(resp.Choices))
			for i, choice := range resp.Choices {
				p.logger.Infof("🔍 Choice %d - Type: %T, Content: %v, Content length: %d",
					i, choice, choice.Content != "", len(choice.Content))
				if choice.Content != "" {
					p.logger.Infof("🔍 Choice %d - First 100 chars: %s", i, truncateString(choice.Content, 100))
				}

				// 🆕 OPENROUTER CACHE DEBUGGING
				if p.provider == ProviderOpenRouter && choice.GenerationInfo != nil {
					info := choice.GenerationInfo
					p.logger.Infof("🔍 OpenRouter GenerationInfo: CacheDiscount=%v, CachedContentTokens=%v",
						info.CacheDiscount, info.CachedContentTokens)
					// Check additional fields for cache-related info
					if info.Additional != nil {
						for key, value := range info.Additional {
							if strings.Contains(strings.ToLower(key), "cache") {
								p.logger.Infof("🔍 OpenRouter Cache Field - %s: %v (type: %T)", key, value, value)
							}
						}
					}
				} else if choice.GenerationInfo != nil {
					info := choice.GenerationInfo
					p.logger.Infof("🔍 GenerationInfo: InputTokens=%v, OutputTokens=%v, TotalTokens=%v",
						info.InputTokens, info.OutputTokens, info.TotalTokens)
				}
			}
		}
	}

	// 🆕 AWS BEDROCK SPECIFIC ERROR DETAILS
	if err != nil && p.provider == ProviderBedrock {
		p.logger.Infof("🔍 AWS Bedrock Error Details:")
		p.logger.Infof("🔍 Error type: %T", err)
		p.logger.Infof("🔍 Error message: %s", err.Error())

		// Check for AWS-specific error types
		if awsErr, ok := err.(interface{ Code() string }); ok {
			p.logger.Infof("🔍 AWS Error Code: %s", awsErr.Code())
		}
		if awsErr, ok := err.(interface{ Message() string }); ok {
			p.logger.Infof("🔍 AWS Error Message: %s", awsErr.Message())
		}
		if awsErr, ok := err.(interface{ RequestID() string }); ok {
			p.logger.Infof("🔍 AWS Request ID: %s", awsErr.RequestID())
		}

		// Log the full error for debugging
		p.logger.Infof("🔍 Full error details: %+v", err)
	}

	if resp != nil {
		p.logger.Infof("🔍 Response structure - Choices: %v, Choices count: %d", resp.Choices != nil, len(resp.Choices))
		if len(resp.Choices) > 0 {
			choice := resp.Choices[0]
			p.logger.Infof("🔍 First choice - Content: %v, Content length: %d, GenerationInfo: %v",
				choice.Content != "", len(choice.Content), choice.GenerationInfo != nil)
			if choice.GenerationInfo != nil {
				info := choice.GenerationInfo
				p.logger.Infof("🔍 GenerationInfo: InputTokens=%v, OutputTokens=%v, TotalTokens=%v",
					info.InputTokens, info.OutputTokens, info.TotalTokens)
			}
		}
	}

	// Check if we have a valid response
	if err != nil {
		// 🆕 ENHANCED ERROR LOGGING FOR TURN 2 DEBUGGING
		p.logger.Infof("❌ LLM generation failed - provider: %s, model: %s, error: %v", string(p.provider), p.modelID, err)
		p.logger.Infof("❌ Error details - type: %T, message: %s", err, err.Error())

		// 🆕 SERVER ERROR DETECTION AND LOGGING
		if strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "Provider returned error") {
			p.logger.Warnf("🔄 502 Bad Gateway error detected, will trigger fallback mechanism")
			p.logger.Warnf("🔄 Server error details - provider: %s, model: %s, error: %s", string(p.provider), p.modelID, err.Error())
		} else if strings.Contains(err.Error(), "503") {
			p.logger.Warnf("🔄 503 Service Unavailable error detected, will trigger fallback mechanism")
		} else if strings.Contains(err.Error(), "504") {
			p.logger.Warnf("🔄 504 Gateway Timeout error detected, will trigger fallback mechanism")
		} else if strings.Contains(err.Error(), "500") {
			p.logger.Warnf("🔄 500 Internal Server Error detected, will trigger fallback mechanism")
		}

		// Log the messages that were sent to help debug
		p.logger.Infof("📤 Messages sent to LLM - count: %d", len(messages))
		for i, msg := range messages {
			// Calculate actual content length from message parts
			contentLength := 0
			for _, part := range msg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					contentLength += len(textPart.Text)
				}
			}
			p.logger.Infof("📤 Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
		}

		// Emit LLM generation error event with rich debugging information
		errorMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"provider":        string(p.provider),
				"model_id":        p.modelID,
				"messages":        fmt.Sprintf("%d", len(messages)),
				"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
				"message_content": extractMessageContentAsString(messages),
				"error":           err.Error(),
				"error_type":      fmt.Sprintf("%T", err),
				"debug_note":      "Enhanced error logging for turn 2 debugging",
			},
		}
		emitLLMGenerationError(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), err, p.traceID, errorMetadata)

		return nil, err
	}

	// 🆕 ENHANCED RESPONSE VALIDATION LOGGING
	p.logger.Infof("✅ LLM generation succeeded - provider: %s, model: %s", string(p.provider), p.modelID)

	// Validate response structure
	if resp == nil {
		p.logger.Infof("❌ Response is nil - this will cause 'no results' error")

		// Emit LLM generation error event for nil response
		errorMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"debug_note": "Response validation failed - nil response",
			},
		}
		emitLLMGenerationError(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), fmt.Errorf("response validation failed - nil response"), p.traceID, errorMetadata)

		return nil, fmt.Errorf("response is nil")
	}

	if resp.Choices == nil {
		p.logger.Infof("❌ Response.Choices is nil - this will cause 'no results' error")

		// Enhanced logging for ALL providers when choices is nil
		p.logger.Errorf("🔍 Nil Choices Debug Information for %s:", string(p.provider))
		p.logger.Errorf("   Model ID: %s", p.modelID)
		p.logger.Errorf("   Provider: %s", string(p.provider))
		p.logger.Errorf("   Response Type: %T", resp)
		p.logger.Errorf("   Response Pointer: %p", resp)
		p.logger.Errorf("   Response Nil: %v", resp == nil)

		// Log the ENTIRE response structure for comprehensive debugging
		p.logger.Errorf("🔍 COMPLETE LLM RESPONSE STRUCTURE:")
		p.logger.Errorf("   Full Response: %+v", resp)

		// Log the options that were passed to the LLM
		p.logger.Errorf("🔍 LLM CALL OPTIONS:")
		for i, opt := range options {
			p.logger.Errorf("   Option %d: %T = %+v", i+1, opt, opt)
		}

		// Log the messages that were sent to the LLM
		p.logger.Errorf("🔍 MESSAGES SENT TO LLM:")
		for i, msg := range messages {
			p.logger.Errorf("   Message %d - Role: %s, Parts: %d", i+1, msg.Role, len(msg.Parts))
			for j, part := range msg.Parts {
				p.logger.Errorf("     Part %d - Type: %T, Content: %+v", j+1, part, part)
			}
		}

		// Emit LLM generation error event for nil choices
		errorMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"provider":        string(p.provider),
				"model_id":        p.modelID,
				"messages":        fmt.Sprintf("%d", len(messages)),
				"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
				"message_content": extractMessageContentAsString(messages),
				"error":           "Response.Choices is nil",
				"debug_note":      "Response validation failed - nil choices",
			},
		}
		emitLLMGenerationError(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), fmt.Errorf("response.Choices is nil"), p.traceID, errorMetadata)

		return nil, fmt.Errorf("response.Choices is nil")
	}

	if len(resp.Choices) == 0 {
		p.logger.Infof("❌ Response.Choices is empty array - this will cause 'no results' error")

		// Enhanced logging for ALL providers when choices array is empty
		p.logger.Errorf("🔍 Empty Choices Array Debug Information for %s:", string(p.provider))
		p.logger.Errorf("   Model ID: %s", p.modelID)
		p.logger.Errorf("   Provider: %s", string(p.provider))
		p.logger.Errorf("   Response Type: %T", resp)
		p.logger.Errorf("   Response Pointer: %p", resp)
		p.logger.Errorf("   Choices Array Length: %d", len(resp.Choices))
		p.logger.Errorf("   Choices Array Nil: %v", resp.Choices == nil)
		p.logger.Errorf("   Choices Array Cap: %d", cap(resp.Choices))

		// Log the ENTIRE response structure for comprehensive debugging
		p.logger.Errorf("🔍 COMPLETE LLM RESPONSE STRUCTURE:")
		p.logger.Errorf("   Full Response: %+v", resp)

		// Log the options that were passed to the LLM
		p.logger.Errorf("🔍 LLM CALL OPTIONS:")
		for i, opt := range options {
			p.logger.Errorf("   Option %d: %T = %+v", i+1, opt, opt)
		}

		// Log the messages that were sent to the LLM
		p.logger.Errorf("🔍 MESSAGES SENT TO LLM:")
		for i, msg := range messages {
			p.logger.Errorf("   Message %d - Role: %s, Parts: %d", i+1, msg.Role, len(msg.Parts))
			for j, part := range msg.Parts {
				p.logger.Errorf("     Part %d - Type: %T, Content: %+v", j+1, part, part)
			}
		}

		// Emit LLM generation error event for empty choices
		errorMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"provider":        string(p.provider),
				"model_id":        p.modelID,
				"messages":        fmt.Sprintf("%d", len(messages)),
				"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
				"message_content": extractMessageContentAsString(messages),
				"error":           "Response.Choices is empty",
				"debug_note":      "Response validation failed - empty choices array",
			},
		}
		emitLLMGenerationError(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), fmt.Errorf("response.Choices is empty"), p.traceID, errorMetadata)

		return nil, fmt.Errorf("response.Choices is empty")
	}

	// Validate first choice has content
	firstChoice := resp.Choices[0]
	if firstChoice.Content == "" {
		// Check if this is a valid tool call response
		if len(firstChoice.ToolCalls) > 0 {
			p.logger.Infof("✅ Valid tool call response detected - Content is empty but ToolCalls present")
			p.logger.Infof("   Tool Calls: %d", len(firstChoice.ToolCalls))
			for i, toolCall := range firstChoice.ToolCalls {
				p.logger.Infof("   Tool Call %d: ID=%s, Type=%s", i+1, toolCall.ID, toolCall.Type)
			}
			// This is a valid response, continue processing
		} else if firstChoice.FuncCall != nil { // Legacy function call handling
			p.logger.Infof("✅ Valid function call response detected - Content is empty but FuncCall present")
			p.logger.Infof("   Function Call: Name=%s", firstChoice.FuncCall.Name)
			// This is a valid response, continue processing
		} else {
			// This is actually an empty content error
			p.logger.Infof("❌ Choice.Content is empty - this will cause 'no results' error")

			// Enhanced logging for ALL providers when choice content is empty
			p.logger.Errorf("🔍 Empty Choice Content Debug Information for %s:", string(p.provider))
			p.logger.Errorf("   Model ID: %s", p.modelID)
			p.logger.Errorf("   Provider: %s", string(p.provider))
			p.logger.Errorf("   Response Type: %T", resp)
			p.logger.Errorf("   Response Pointer: %p", resp)
			p.logger.Errorf("   Choices Count: %d", len(resp.Choices))
			p.logger.Errorf("   First Choice Type: %T", firstChoice)
			p.logger.Errorf("   First Choice Content Empty: %v", firstChoice.Content == "")

			p.logger.Errorf("   First Choice Content Length: %d", len(firstChoice.Content))

			// Detailed choice structure logging
			p.logger.Errorf("🔍 DETAILED CHOICE STRUCTURE:")
			p.logger.Errorf("   Choice.StopReason: %v", firstChoice.StopReason)
			toolCallsCount := 0
			if firstChoice.ToolCalls != nil {
				toolCallsCount = len(firstChoice.ToolCalls)
			}
			p.logger.Errorf("   Choice.ToolCalls: %v (nil: %v, count: %d)", firstChoice.ToolCalls != nil, firstChoice.ToolCalls == nil, toolCallsCount)
			if len(firstChoice.ToolCalls) > 0 {
				for i, tc := range firstChoice.ToolCalls {
					p.logger.Errorf("     ToolCall %d: ID=%s, Type=%s, FunctionName=%s, Arguments=%s",
						i+1, tc.ID, tc.Type, tc.FunctionCall.Name, truncateString(tc.FunctionCall.Arguments, 200))
				}
			}
			p.logger.Errorf("   Choice.FuncCall: %v", firstChoice.FuncCall != nil)
			if firstChoice.FuncCall != nil {
				p.logger.Errorf("     FuncCall Name: %s, Arguments: %s",
					firstChoice.FuncCall.Name, truncateString(firstChoice.FuncCall.Arguments, 200))
			}
			p.logger.Errorf("   Choice.GenerationInfo: %v (nil: %v)", firstChoice.GenerationInfo != nil, firstChoice.GenerationInfo == nil)
			if firstChoice.GenerationInfo != nil {
				info := firstChoice.GenerationInfo
				p.logger.Errorf("     GenerationInfo: InputTokens=%v, OutputTokens=%v, TotalTokens=%v",
					info.InputTokens, info.OutputTokens, info.TotalTokens)
				// Log additional fields if present
				if info.Additional != nil {
					for key, value := range info.Additional {
						valueStr := fmt.Sprintf("%v", value)
						if len(valueStr) > 200 {
							valueStr = truncateString(valueStr, 200)
						}
						p.logger.Errorf("       %s: %s (type: %T)", key, valueStr, value)
					}
				}
			}

			// Log the ENTIRE response structure for comprehensive debugging
			p.logger.Errorf("🔍 COMPLETE LLM RESPONSE STRUCTURE:")
			p.logger.Errorf("   Full Response: %+v", resp)

			// Serialize response to JSON for raw-like representation
			// Note: This is the processed response from langchaingo, not the raw HTTP response
			// but it gives us a JSON representation of what we received
			if respJSON, err := json.MarshalIndent(resp, "   ", "  "); err == nil {
				jsonStr := string(respJSON)
				// Truncate if too long to avoid massive log files
				if len(jsonStr) > 5000 {
					jsonStr = jsonStr[:5000] + "\n   ... (truncated, total length: " + fmt.Sprintf("%d", len(jsonStr)) + " bytes)"
				}
				p.logger.Errorf("🔍 RAW RESPONSE AS JSON (processed by langchaingo):")
				p.logger.Errorf("%s", jsonStr)
			} else {
				p.logger.Errorf("   ⚠️ Failed to serialize response to JSON: %w", err)
			}

			// Log the options that were passed to the LLM
			p.logger.Errorf("🔍 LLM CALL OPTIONS:")
			for i, opt := range options {
				p.logger.Errorf("   Option %d: %T = %+v", i+1, opt, opt)
			}

			// Log the messages that were sent to the LLM
			p.logger.Errorf("🔍 MESSAGES SENT TO LLM:")
			for i, msg := range messages {
				p.logger.Errorf("   Message %d - Role: %s, Parts: %d", i+1, msg.Role, len(msg.Parts))
				for j, part := range msg.Parts {
					p.logger.Errorf("     Part %d - Type: %T, Content: %+v", j+1, part, part)
				}
			}

			// Emit LLM generation error event for empty choice content
			errorMetadata := LLMMetadata{
				User: "llm_generation_user",
				CustomFields: map[string]string{
					"provider":        string(p.provider),
					"model_id":        p.modelID,
					"messages":        fmt.Sprintf("%d", len(messages)),
					"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
					"message_content": extractMessageContentAsString(messages),
					"error":           "Choice.Content is empty",
					"debug_note":      "Response validation failed - empty content",
				},
			}
			emitLLMGenerationError(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), fmt.Errorf("choice.Content is empty"), p.traceID, errorMetadata)

			return nil, fmt.Errorf("choice.Content is empty")
		}
	}

	// 🆕 ENHANCED SUCCESS LOGGING
	p.logger.Infof("✅ LLM generation validation passed - provider: %s, model: %s", string(p.provider), p.modelID)
	p.logger.Infof("✅ Response structure - Choices: %v, Choices count: %d", resp.Choices != nil, len(resp.Choices))
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		p.logger.Infof("✅ First choice - Content: %v, Content length: %d, GenerationInfo: %v",
			choice.Content != "", len(choice.Content), choice.GenerationInfo != nil)
		if choice.GenerationInfo != nil {
			p.logger.Infof("✅ GenerationInfo available: InputTokens=%v, OutputTokens=%v, TotalTokens=%v",
				choice.GenerationInfo.InputTokens, choice.GenerationInfo.OutputTokens, choice.GenerationInfo.TotalTokens)
		}
	}

	// Extract token usage from GenerationInfo if available
	if len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil {
		// Extract token usage and create success event with comprehensive data
		usage := extractTokenUsageFromGenerationInfo(resp.Choices[0].GenerationInfo)

		// Calculate total tokens if not provided by the provider
		if usage.TotalTokens == 0 && usage.InputTokens > 0 && usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}

		p.logger.Infof("Token usage extracted: Input=%d, Output=%d, Total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)

		// Emit LLM generation success event with token usage
		successMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"provider":        string(p.provider),
				"model_id":        p.modelID,
				"messages":        fmt.Sprintf("%d", len(messages)),
				"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
				"message_content": extractMessageContentAsString(messages),
				"response_length": fmt.Sprintf("%d", len(resp.Choices[0].Content)),
				"choices_count":   fmt.Sprintf("%d", len(resp.Choices)),
				"input_tokens":    fmt.Sprintf("%d", usage.InputTokens),
				"output_tokens":   fmt.Sprintf("%d", usage.OutputTokens),
				"total_tokens":    fmt.Sprintf("%d", usage.TotalTokens),
				"note":            "Token usage extracted from GenerationInfo",
			},
		}
		emitLLMGenerationSuccess(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), len(resp.Choices[0].Content), len(resp.Choices), p.traceID, successMetadata)
	} else {
		// No token usage available, emit success event without usage
		p.logger.Infof("No GenerationInfo available")

		// Emit LLM generation success event without token usage
		successMetadata := LLMMetadata{
			User: "llm_generation_user",
			CustomFields: map[string]string{
				"provider":        string(p.provider),
				"model_id":        p.modelID,
				"messages":        fmt.Sprintf("%d", len(messages)),
				"temperature":     fmt.Sprintf("%f", getTemperatureFromOptions(options)),
				"message_content": extractMessageContentAsString(messages),
				"response_length": fmt.Sprintf("%d", len(resp.Choices[0].Content)),
				"choices_count":   fmt.Sprintf("%d", len(resp.Choices)),
				"note":            "No GenerationInfo available for token usage",
			},
		}
		emitLLMGenerationSuccess(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), len(resp.Choices[0].Content), len(resp.Choices), p.traceID, successMetadata)
	}

	return resp, nil
}

// extractMessageContentAsString converts message content to a readable string
func extractMessageContentAsString(messages []llmtypes.MessageContent) string {
	if len(messages) == 0 {
		return "no messages"
	}

	var result strings.Builder
	for i, msg := range messages {
		if i > 0 {
			result.WriteString(" | ")
		}
		result.WriteString(fmt.Sprintf("Role:%s", msg.Role))

		for j, part := range msg.Parts {
			if j > 0 {
				result.WriteString(",")
			}
			if textPart, ok := part.(llmtypes.TextContent); ok {
				content := textPart.Text
				if len(content) > 100 {
					content = content[:100] + "..."
				}
				result.WriteString(fmt.Sprintf("Text:%s", content))
			} else {
				result.WriteString(fmt.Sprintf("Part:%T", part))
			}
		}
	}
	return result.String()
}

// getTemperatureFromOptions extracts temperature from call options
func getTemperatureFromOptions(options []llmtypes.CallOption) float64 {
	// For now, return default temperature since CallOption is a function type
	// and we can't easily extract the temperature value
	return 0.7 // default temperature
}

// truncateString truncates a string to a specified length
func truncateString(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length] + "..."
}

// WithOpenRouterUsage enables usage parameter for OpenRouter requests to get cache token information
func WithOpenRouterUsage() CallOption {
	return func(opts *CallOptions) {
		// Set the usage parameter in the request metadata (not CallOptions metadata)
		// This will be passed to the actual HTTP request body
		if opts.Metadata == nil {
			opts.Metadata = &llmtypes.Metadata{
				Usage: &llmtypes.UsageMetadata{Include: true},
			}
		} else {
			if opts.Metadata.Usage == nil {
				opts.Metadata.Usage = &llmtypes.UsageMetadata{Include: true}
			} else {
				opts.Metadata.Usage.Include = true
			}
		}
	}
}

// LLM Configuration Management Functions

// LLMDefaultsResponse represents the response structure for LLM defaults
type LLMDefaultsResponse struct {
	PrimaryConfig    map[string]interface{} `json:"primary_config"`
	OpenrouterConfig map[string]interface{} `json:"openrouter_config"`
	BedrockConfig    map[string]interface{} `json:"bedrock_config"`
	OpenaiConfig     map[string]interface{} `json:"openai_config"`
	AvailableModels  map[string][]string    `json:"available_models"`
}

// APIKeyValidationRequest represents a request to validate an API key
type APIKeyValidationRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	ModelID  string `json:"model_id,omitempty"` // Optional model ID for Bedrock validation
}

// APIKeyValidationResponse represents the response for API key validation
type APIKeyValidationResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GetLLMDefaults returns default LLM configurations from environment variables
func GetLLMDefaults() LLMDefaultsResponse {
	// Get primary configuration from environment
	defaultProvider := os.Getenv("AGENT_PROVIDER")
	if defaultProvider == "" {
		defaultProvider = "openrouter" // fallback default
	}

	defaultModel := os.Getenv("AGENT_MODEL")
	if defaultModel == "" {
		defaultModel = "x-ai/grok-code-fast-1" // fallback default
	}

	// Parse fallback models
	fallbackStr := os.Getenv("OPENROUTER_FALLBACK_MODELS")
	var fallbackModels []string
	if fallbackStr != "" {
		fallbackModels = strings.Split(fallbackStr, ",")
		for i, model := range fallbackModels {
			fallbackModels[i] = strings.TrimSpace(model)
		}
	} else {
		fallbackModels = []string{} // No fallback defaults
	}

	// Parse cross-provider fallback
	crossProvider := os.Getenv("OPENROUTER_CROSS_FALLBACK_PROVIDER")
	if crossProvider == "" {
		crossProvider = "openai" // Default fallback provider
	}
	crossModelsStr := os.Getenv("OPENROUTER_CROSS_FALLBACK_MODELS")
	if crossModelsStr == "" {
		crossModelsStr = os.Getenv("OPEN_ROUTER_CROSS_FALLBACK_MODELS") // Fallback to old naming
	}
	var crossModels []string
	if crossModelsStr != "" {
		crossModels = strings.Split(crossModelsStr, ",")
		for i, model := range crossModels {
			crossModels[i] = strings.TrimSpace(model)
		}
	} else {
		crossModels = []string{} // No cross-provider fallback defaults
	}

	var crossProviderFallback *map[string]interface{}
	if crossProvider != "" && len(crossModels) > 0 {
		crossProviderFallback = &map[string]interface{}{
			"provider": crossProvider,
			"models":   crossModels,
		}
	}

	// Get API keys from environment for prefilling
	openrouterAPIKey := os.Getenv("OPENROUTER_API_KEY")
	if openrouterAPIKey == "" {
		openrouterAPIKey = os.Getenv("OPEN_ROUTER_API_KEY") // Fallback to old naming
	}
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")

	// Bedrock configuration
	bedrockModel := os.Getenv("BEDROCK_MODEL")
	if bedrockModel == "" {
		bedrockModel = os.Getenv("BEDROCK_PRIMARY_MODEL") // Fallback to old naming
	}
	if bedrockModel == "" {
		bedrockModel = "us.anthropic.claude-sonnet-4-20250514-v1:0" // fallback default
	}

	bedrockFallbackStr := os.Getenv("BEDROCK_FALLBACK_MODELS")
	var bedrockFallbacks []string
	if bedrockFallbackStr != "" {
		bedrockFallbacks = strings.Split(bedrockFallbackStr, ",")
		for i, model := range bedrockFallbacks {
			bedrockFallbacks[i] = strings.TrimSpace(model)
		}
	} else {
		bedrockFallbacks = []string{} // No fallback defaults
	}

	bedrockRegion := os.Getenv("BEDROCK_REGION")
	if bedrockRegion == "" {
		bedrockRegion = "us-east-1" // fallback default
	}

	bedrockCrossProvider := os.Getenv("BEDROCK_CROSS_FALLBACK_PROVIDER")
	if bedrockCrossProvider == "" {
		bedrockCrossProvider = "openai" // Default fallback provider
	}
	bedrockCrossModelsStr := os.Getenv("BEDROCK_CROSS_FALLBACK_MODELS")
	if bedrockCrossModelsStr == "" {
		bedrockCrossModelsStr = os.Getenv("BEDROCK_OPENAI_FALLBACK_MODELS") // Fallback to old naming
	}
	var bedrockCrossModels []string
	if bedrockCrossModelsStr != "" {
		bedrockCrossModels = strings.Split(bedrockCrossModelsStr, ",")
		for i, model := range bedrockCrossModels {
			bedrockCrossModels[i] = strings.TrimSpace(model)
		}
	} else {
		bedrockCrossModels = []string{} // No cross-provider fallback defaults
	}

	var bedrockCrossProviderFallback *map[string]interface{}
	if bedrockCrossProvider != "" && len(bedrockCrossModels) > 0 {
		bedrockCrossProviderFallback = &map[string]interface{}{
			"provider": bedrockCrossProvider,
			"models":   bedrockCrossModels,
		}
	}

	// OpenAI configuration
	openaiModel := os.Getenv("OPENAI_MODEL")
	if openaiModel == "" {
		openaiModel = os.Getenv("OPENAI_PRIMARY_MODEL") // Fallback to old naming
	}
	if openaiModel == "" {
		openaiModel = "gpt-4o" // fallback default
	}

	openaiFallbackStr := os.Getenv("OPENAI_FALLBACK_MODELS")
	var openaiFallbacks []string
	if openaiFallbackStr != "" {
		openaiFallbacks = strings.Split(openaiFallbackStr, ",")
		for i, model := range openaiFallbacks {
			openaiFallbacks[i] = strings.TrimSpace(model)
		}
	} else {
		openaiFallbacks = []string{} // No fallback defaults
	}

	openaiCrossProvider := os.Getenv("OPENAI_CROSS_FALLBACK_PROVIDER")
	if openaiCrossProvider == "" {
		openaiCrossProvider = "bedrock" // Default fallback provider
	}
	openaiCrossModelsStr := os.Getenv("OPENAI_CROSS_FALLBACK_MODELS")
	if openaiCrossModelsStr == "" {
		openaiCrossModelsStr = os.Getenv("OPENAI_BEDROCK_FALLBACK_MODELS") // Fallback to old naming
	}
	var openaiCrossModels []string
	if openaiCrossModelsStr != "" {
		openaiCrossModels = strings.Split(openaiCrossModelsStr, ",")
		for i, model := range openaiCrossModels {
			openaiCrossModels[i] = strings.TrimSpace(model)
		}
	} else {
		openaiCrossModels = []string{} // No cross-provider fallback defaults
	}

	var openaiCrossProviderFallback *map[string]interface{}
	if openaiCrossProvider != "" && len(openaiCrossModels) > 0 {
		openaiCrossProviderFallback = &map[string]interface{}{
			"provider": openaiCrossProvider,
			"models":   openaiCrossModels,
		}
	}

	// Build response
	return LLMDefaultsResponse{
		PrimaryConfig: map[string]interface{}{
			"provider":                defaultProvider,
			"model_id":                defaultModel,
			"fallback_models":         fallbackModels,
			"cross_provider_fallback": crossProviderFallback,
		},
		OpenrouterConfig: map[string]interface{}{
			"provider":                "openrouter",
			"model_id":                defaultModel,
			"fallback_models":         fallbackModels,
			"cross_provider_fallback": crossProviderFallback,
			"api_key":                 openrouterAPIKey, // Prefill from environment if available
		},
		BedrockConfig: map[string]interface{}{
			"provider":                "bedrock",
			"model_id":                bedrockModel,
			"fallback_models":         bedrockFallbacks,
			"cross_provider_fallback": bedrockCrossProviderFallback,
			"region":                  bedrockRegion,
		},
		OpenaiConfig: map[string]interface{}{
			"provider":                "openai",
			"model_id":                openaiModel,
			"fallback_models":         openaiFallbacks,
			"cross_provider_fallback": openaiCrossProviderFallback,
			"api_key":                 openaiAPIKey, // Prefill from environment if available
		},
		AvailableModels: map[string][]string{
			"bedrock":    getBedrockAvailableModels(),
			"openrouter": getOpenRouterAvailableModels(),
			"openai":     getOpenAIAvailableModels(),
		},
	}
}

// ValidateAPIKey validates API keys for OpenRouter, OpenAI, Bedrock, and Vertex
func ValidateAPIKey(req APIKeyValidationRequest) APIKeyValidationResponse {
	// Use fmt.Printf for logging in validation functions
	fmt.Printf("[API KEY VALIDATION] Request received for provider: %s\n", req.Provider)

	var isValid bool
	var message string
	var err error

	fmt.Printf("[API KEY VALIDATION] Validating %s API key\n", req.Provider)
	switch req.Provider {
	case "openrouter":
		isValid, message, err = validateOpenRouterAPIKey(req.APIKey)
	case "openai":
		isValid, message, err = validateOpenAIAPIKey(req.APIKey)
	case "bedrock":
		// Bedrock uses AWS credentials, test them instead of API key
		fmt.Printf("[API KEY VALIDATION] Testing AWS Bedrock credentials\n")
		isValid, message, err = validateBedrockCredentials(req.ModelID)
	case "vertex":
		// Vertex uses Google API key, optionally test with model ID
		fmt.Printf("[API KEY VALIDATION] Testing Vertex AI API key\n")
		isValid, message, err = validateVertexAPIKey(req.APIKey, req.ModelID)
	case "anthropic":
		// Anthropic validation can be added here if needed
		fmt.Printf("[API KEY VALIDATION WARN] Anthropic validation not yet implemented\n")
		return APIKeyValidationResponse{
			Valid: false,
			Error: "Anthropic API key validation not yet implemented",
		}
	default:
		fmt.Printf("[API KEY VALIDATION WARN] Unsupported provider: %s\n", req.Provider)
		return APIKeyValidationResponse{
			Valid: false,
			Error: "Unsupported provider",
		}
	}

	// Handle validation errors
	if err != nil {
		fmt.Printf("[API KEY VALIDATION ERROR] %s validation failed: %v\n", req.Provider, err)
		return APIKeyValidationResponse{
			Valid: false,
			Error: fmt.Sprintf("Validation failed: %w", err),
		}
	}

	// Return validation result
	if isValid {
		fmt.Printf("[API KEY VALIDATION SUCCESS] %s: %s\n", req.Provider, message)
	} else {
		fmt.Printf("[API KEY VALIDATION FAILED] %s: %s\n", req.Provider, message)
	}

	return APIKeyValidationResponse{
		Valid:   isValid,
		Message: message,
	}
}

// validateOpenRouterAPIKey validates an OpenRouter API key
func validateOpenRouterAPIKey(apiKey string) (bool, string, error) {
	fmt.Printf("[OPENROUTER VALIDATION] Starting API key validation\n")

	// Basic format validation
	if !strings.HasPrefix(apiKey, "sk-or-") {
		fmt.Printf("[OPENROUTER VALIDATION WARN] Format validation failed - missing sk-or- prefix\n")
		return false, "Invalid OpenRouter API key format", nil
	}
	fmt.Printf("[OPENROUTER VALIDATION] Format validation passed\n")
	// Test the API key by making a request to OpenRouter
	fmt.Printf("[OPENROUTER VALIDATION] Making request to OpenRouter API\n")
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		fmt.Printf("[OPENROUTER VALIDATION ERROR] Failed to create request: %w\n", err)
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("[OPENROUTER VALIDATION] Sending request to OpenRouter API\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[OPENROUTER VALIDATION ERROR] Request failed: %w\n", err)
		return false, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[OPENROUTER VALIDATION] Response status: %d\n", resp.StatusCode)
	switch resp.StatusCode {
	case 200:
		fmt.Printf("[OPENROUTER VALIDATION SUCCESS] API key is valid\n")
		return true, "OpenRouter API key is valid", nil
	case 401:
		fmt.Printf("[OPENROUTER VALIDATION FAILED] Unauthorized - invalid API key\n")
		return false, "Invalid OpenRouter API key", nil
	case 429:
		fmt.Printf("[OPENROUTER VALIDATION FAILED] Rate limit exceeded\n")
		return false, "OpenRouter API rate limit exceeded", nil
	default:
		fmt.Printf("[OPENROUTER VALIDATION FAILED] Unexpected status: %d\n", resp.StatusCode)
		return false, fmt.Sprintf("OpenRouter API returned status %d", resp.StatusCode), nil
	}
}

// validateOpenAIAPIKey validates an OpenAI API key
func validateOpenAIAPIKey(apiKey string) (bool, string, error) {
	fmt.Printf("[OPENAI VALIDATION] Starting API key validation\n")
	// Basic format validation
	if !strings.HasPrefix(apiKey, "sk-") {
		fmt.Printf("[OPENAI VALIDATION WARN] Format validation failed - missing sk- prefix\n")
		return false, "Invalid OpenAI API key format", nil
	}
	fmt.Printf("[OPENAI VALIDATION] Format validation passed\n")
	// Test the API key by making a request to OpenAI
	fmt.Printf("[OPENAI VALIDATION] Making request to OpenAI API\n")
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		fmt.Printf("[OPENAI VALIDATION ERROR] Failed to create request: %w\n", err)
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("[OPENAI VALIDATION] Sending request to OpenAI API\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[OPENAI VALIDATION ERROR] Request failed: %w\n", err)
		return false, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[OPENAI VALIDATION] Response status: %d", resp.StatusCode)

	switch resp.StatusCode {
	case 200:
		fmt.Printf("[OPENAI VALIDATION SUCCESS] API key is valid\n")
		return true, "OpenAI API key is valid", nil
	case 401:
		fmt.Printf("[OPENAI VALIDATION FAILED] Unauthorized - invalid API key\n")
		return false, "Invalid OpenAI API key", nil
	case 429:
		fmt.Printf("[OPENAI VALIDATION FAILED] Rate limit exceeded\n")
		return false, "OpenAI API rate limit exceeded", nil
	default:
		fmt.Printf("[OPENAI VALIDATION FAILED] Unexpected status: %d", resp.StatusCode)
		return false, fmt.Sprintf("OpenAI API returned status %d", resp.StatusCode), nil
	}
}

// validateVertexAPIKey validates a Vertex AI (Google Gemini) API key
func validateVertexAPIKey(apiKey string, modelID string) (bool, string, error) {
	fmt.Printf("[VERTEX VALIDATION] Starting API key validation\n")
	// Basic validation - Google API keys don't have a specific prefix
	if apiKey == "" {
		fmt.Printf("[VERTEX VALIDATION WARN] API key is empty\n")
		return false, "API key is empty", nil
	}
	fmt.Printf("[VERTEX VALIDATION] API key format check passed\n")
	// If model ID is provided, test with that specific model
	// Otherwise, test by listing available models
	var testURL string
	if modelID != "" {
		// Test with specific model
		testURL = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s?key=%s", modelID, apiKey)
		fmt.Printf("[VERTEX VALIDATION] Testing with model: %s", modelID)
	} else {
		// Test by listing models
		testURL = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)
		fmt.Printf("[VERTEX VALIDATION] Testing by listing available models\n")
	}

	// Test the API key by making a request to Gemini API
	fmt.Printf("[VERTEX VALIDATION] Making request to Gemini API\n")
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		fmt.Printf("[VERTEX VALIDATION ERROR] Failed to create request: %w\n", err)
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("[VERTEX VALIDATION] Sending request to Gemini API\n")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[VERTEX VALIDATION ERROR] Request failed: %w\n", err)
		return false, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[VERTEX VALIDATION] Response status: %d", resp.StatusCode)

	switch resp.StatusCode {
	case 200:
		fmt.Printf("[VERTEX VALIDATION SUCCESS] API key is valid\n")
		if modelID != "" {
			return true, fmt.Sprintf("Vertex AI API key is valid for model %s", modelID), nil
		}
		return true, "Vertex AI API key is valid", nil
	case 400:
		fmt.Printf("[VERTEX VALIDATION FAILED] Bad request - invalid API key or model\n")
		return false, "Invalid API key or model ID", nil
	case 401:
		fmt.Printf("[VERTEX VALIDATION FAILED] Unauthorized - invalid API key\n")
		return false, "Invalid Vertex AI API key", nil
	case 403:
		fmt.Printf("[VERTEX VALIDATION FAILED] Forbidden - API key lacks required permissions\n")
		return false, "API key lacks required permissions", nil
	case 404:
		if modelID != "" {
			fmt.Printf("[VERTEX VALIDATION FAILED] Model not found: %s", modelID)
			return false, fmt.Sprintf("Model %s not found", modelID), nil
		}
		fmt.Printf("[VERTEX VALIDATION FAILED] Resource not found\n")
		return false, "Resource not found", nil
	case 429:
		fmt.Printf("[VERTEX VALIDATION FAILED] Rate limit exceeded\n")
		return false, "Vertex AI API rate limit exceeded", nil
	default:
		fmt.Printf("[VERTEX VALIDATION FAILED] Unexpected status: %d", resp.StatusCode)
		return false, fmt.Sprintf("Vertex AI API returned status %d", resp.StatusCode), nil
	}
}

// noopLoggerImpl is a no-op logger implementation for validation functions
type noopLoggerImpl struct{}

func (n *noopLoggerImpl) Infof(format string, v ...any)                        {}
func (n *noopLoggerImpl) Errorf(format string, v ...any)                       {}
func (n *noopLoggerImpl) Info(args ...interface{})                             {}
func (n *noopLoggerImpl) Error(args ...interface{})                            {}
func (n *noopLoggerImpl) Debug(args ...interface{})                            {}
func (n *noopLoggerImpl) Debugf(format string, args ...interface{})            {}
func (n *noopLoggerImpl) Warn(args ...interface{})                             {}
func (n *noopLoggerImpl) Warnf(format string, args ...interface{})             {}
func (n *noopLoggerImpl) Fatal(args ...interface{})                            {}
func (n *noopLoggerImpl) Fatalf(format string, args ...interface{})            {}
func (n *noopLoggerImpl) WithField(key string, value interface{}) interface{}  { return nil }
func (n *noopLoggerImpl) WithFields(fields map[string]interface{}) interface{} { return nil }
func (n *noopLoggerImpl) WithError(err error) interface{}                      { return nil }
func (n *noopLoggerImpl) Close() error                                         { return nil }

// validateBedrockCredentials validates AWS Bedrock credentials and region
func validateBedrockCredentials(modelID string) (bool, string, error) {
	fmt.Printf("[BEDROCK VALIDATION] Starting AWS Bedrock credentials validation\n")
	// Check if AWS region is configured
	region := os.Getenv("AWS_REGION")
	if region == "" {
		fmt.Printf("[BEDROCK VALIDATION WARN] AWS_REGION environment variable not set\n")
		return false, "AWS_REGION environment variable not set", nil
	}
	fmt.Printf("[BEDROCK VALIDATION] AWS region: %s", region)

	// Check if AWS credentials are configured
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if accessKey == "" || secretKey == "" {
		fmt.Printf("[BEDROCK VALIDATION WARN] AWS credentials not configured\n")
		return false, "AWS credentials not configured (AWS_ACCESS_KEY_ID or AWS_SECRET_ACCESS_KEY missing)", nil
	}
	fmt.Printf("[BEDROCK VALIDATION] AWS credentials configured\n")
	// Use provided model ID or fallback to default
	if modelID == "" {
		modelID = "us.anthropic.claude-3-haiku-20240307-v1:0" // fallback default
		fmt.Printf("[BEDROCK VALIDATION] Using fallback model ID: %s\n", modelID)
	} else {
		fmt.Printf("[BEDROCK VALIDATION] Using provided model ID: %s\n", modelID)
	}

	// Test Bedrock access by creating a Bedrock LLM instance
	fmt.Printf("[BEDROCK VALIDATION] Testing Bedrock access by creating LLM instance\n")
	// Load AWS SDK configuration
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(region))
	if err != nil {
		fmt.Printf("[BEDROCK VALIDATION ERROR] Failed to load AWS config: %w\n", err)
		return false, "Failed to load AWS configuration", err
	}

	// Create Bedrock runtime client
	client := bedrockruntime.NewFromConfig(cfg)

	// Create a simple no-op logger for validation
	noopLog := &noopLoggerImpl{}
	// Create Bedrock adapter instance
	llm := bedrockadapter.NewBedrockAdapter(client, modelID, noopLog)

	// Test the LLM with a simple generation call
	fmt.Printf("[BEDROCK VALIDATION] Making test generation call to Bedrock\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "test"}},
		},
	})
	if err != nil {
		fmt.Printf("[BEDROCK VALIDATION ERROR] Bedrock test generation failed: %w\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "AccessDenied") {
			return false, "AWS credentials do not have permission to access Bedrock", nil
		}
		if strings.Contains(err.Error(), "InvalidUserID.NotFound") {
			return false, "AWS credentials are invalid", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "Bedrock service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("Bedrock test generation failed: %w", err), nil
	}

	fmt.Printf("[BEDROCK VALIDATION SUCCESS] AWS Bedrock credentials are valid\n")
	return true, "AWS Bedrock credentials are valid", nil
}

// Helper functions to get available models from environment variables

// getBedrockAvailableModels returns available Bedrock models from environment variables
func getBedrockAvailableModels() []string {
	// Get from environment variable
	modelsStr := os.Getenv("BEDROCK_AVAILABLE_MODELS")
	if modelsStr == "" {
		// Fallback to old naming
		modelsStr = os.Getenv("BEDROCK_MODELS")
	}
	if modelsStr == "" {
		// Return empty array if no environment variable is set
		return []string{}
	}

	// Parse comma-separated models
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// getOpenRouterAvailableModels returns available OpenRouter models from environment variables
func getOpenRouterAvailableModels() []string {
	// Get from environment variable
	modelsStr := os.Getenv("OPENROUTER_AVAILABLE_MODELS")
	if modelsStr == "" {
		// Fallback to old naming
		modelsStr = os.Getenv("OPEN_ROUTER_MODELS")
	}
	if modelsStr == "" {
		// Return empty array if no environment variable is set
		return []string{}
	}

	// Parse comma-separated models
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// getOpenAIAvailableModels returns available OpenAI models from environment variables
func getOpenAIAvailableModels() []string {
	// Get from environment variable
	modelsStr := os.Getenv("OPENAI_AVAILABLE_MODELS")
	if modelsStr == "" {
		// Fallback to old naming
		modelsStr = os.Getenv("OPENAI_MODELS")
	}
	if modelsStr == "" {
		// Return empty array if no environment variable is set
		return []string{}
	}

	// Parse comma-separated models
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}
