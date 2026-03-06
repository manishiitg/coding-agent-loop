package llmproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	anthropicadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/anthropic"
	azureadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
	bedrockadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/bedrock"
	claudecodeadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"
	geminicli "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/geminicli"
	minimaxadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/minimax"
	openaiadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
	vertexadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"

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
	ProviderAzure      Provider = "azure"
	ProviderClaudeCode Provider = "claude-code"
	ProviderGeminiCLI  Provider = "gemini-cli"
	ProviderMiniMax    Provider = "minimax"
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
	// API keys for providers (optional, falls back to environment variables if not provided)
	APIKeys *ProviderAPIKeys
}

// ProviderAPIKeys holds API keys for different providers
type ProviderAPIKeys struct {
	OpenRouter *string
	OpenAI     *string
	Anthropic  *string
	Vertex     *string
	GeminiCLI  *string
	MiniMax    *string
	Bedrock    *BedrockConfig
	Azure      *AzureAPIConfig
}

// AzureAPIConfig holds Azure-specific configuration
type AzureAPIConfig struct {
	Endpoint   string // Azure AI endpoint URL
	APIKey     string // Azure API key
	APIVersion string // API version (optional, defaults to 2024-10-21)
	Region     string // Azure region (optional, for logging)
}

// BedrockConfig holds Bedrock-specific configuration
type BedrockConfig struct {
	Region string
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
	case ProviderAzure:
		llm, err = initializeAzureWithFallback(config)
	case ProviderClaudeCode:
		llm, err = initializeClaudeCode(config)
	case ProviderGeminiCLI:
		llm, err = initializeGeminiCLI(config)
	case ProviderMiniMax:
		llm, err = initializeMiniMax(config)
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

// InitializeImageGenerationModel creates and initializes an image generation model.
// Provider must be "vertex". Model selection determines the API path:
//   - "imagen-*" models use the Imagen GenerateImages API
//   - "gemini-*" models use GenerateContent with IMAGE response modality
//
// Requires GEMINI_API_KEY environment variable.
func InitializeImageGenerationModel(config Config) (llmtypes.ImageGenerationModel, error) {
	switch config.Provider {
	case ProviderVertex:
		return initializeVertexImagen(config)
	case ProviderMiniMax:
		return initializeMiniMaxImagen(config)
	default:
		return nil, fmt.Errorf("image generation not supported for provider: %s. Supported providers: vertex, minimax", config.Provider)
	}
}

// initializeMiniMaxImagen creates a MiniMax image generation adapter.
func initializeMiniMaxImagen(config Config) (llmtypes.ImageGenerationModel, error) {
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.MiniMax != nil && *config.APIKeys.MiniMax != "" {
		apiKey = *config.APIKeys.MiniMax
	} else {
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("MINIMAX_API_KEY is required for MiniMax image generation")
	}

	modelID := config.ModelID
	if modelID == "" {
		modelID = minimaxadapter.ModelMiniMaxImage01
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	logger.Infof("Initializing MiniMax Image Generation with model: %s", modelID)
	return minimaxadapter.NewMiniMaxImageAdapter(apiKey, modelID, logger), nil
}

// initializeVertexImagen creates an image generation adapter using the Gemini API.
// If the model starts with "gemini-", uses GenerateContent (native Gemini image output).
// Otherwise assumes an Imagen model and uses the GenerateImages API.
// Uses GEMINI_API_KEY with the Gemini Developer API backend.
func initializeVertexImagen(config Config) (llmtypes.ImageGenerationModel, error) {
	modelID := config.ModelID
	if modelID == "" {
		modelID = "gemini-2.5-flash-image"
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

	// Check config APIKeys first, then fall back to environment variables
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.Vertex != nil && *config.APIKeys.Vertex != "" {
		apiKey = *config.APIKeys.Vertex
	}
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("VERTEX_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required for Imagen image generation (or provide api_key in config)")
	}

	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GenAI client for Imagen: %w", err)
	}

	logger.Infof("Initialized Gemini image model - model_id: %s", modelID)
	return vertexadapter.NewGeminiImageAdapter(client, modelID, logger), nil
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
		if logger == nil {
			logger = &noopLoggerImpl{}
		}
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
		if logger == nil {
			logger = &noopLoggerImpl{}
		}
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
		if logger == nil {
			logger = &noopLoggerImpl{}
		}
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
		if logger == nil {
			logger = &noopLoggerImpl{}
		}
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

	// Get region from config first, then environment (default to us-east-1)
	region := ""
	if config.APIKeys != nil && config.APIKeys.Bedrock != nil && config.APIKeys.Bedrock.Region != "" {
		region = config.APIKeys.Bedrock.Region
		logger.Infof("Using region from config: %s", region)
	} else {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = "us-east-1"
			logger.Infof("AWS_REGION not set, using default: %s", region)
		} else {
			logger.Infof("Using region from environment: %s", region)
		}
	}

	logger.Infof("AWS_REGION: %s", region)
	logger.Infof("AWS_ACCESS_KEY_ID: %s", os.Getenv("AWS_ACCESS_KEY_ID"))
	logger.Infof("AWS_SECRET_ACCESS_KEY: %s", os.Getenv("AWS_SECRET_ACCESS_KEY"))

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

func emitToolCallDetected(emitter interfaces.EventEmitter, provider string, modelID string, toolCallID string, toolName string, arguments string, traceID interfaces.TraceID, metadata LLMMetadata) {
	if emitter != nil {
		emitter.EmitToolCallDetected(provider, modelID, toolCallID, toolName, arguments, traceID, metadata)
	}
}

// initializeOpenAI creates and configures an OpenAI LLM instance
func initializeOpenAI(config Config) (llmtypes.Model, error) {
	// Check for API key from config first, then environment
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.OpenAI != nil && *config.APIKeys.OpenAI != "" {
		apiKey = *config.APIKeys.OpenAI
	} else {
		// Try environment variable
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required for OpenAI provider (not found in config or environment)")
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
		option.WithAPIKey(apiKey),
	)

	// Create OpenAI adapter
	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
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

	// Check for API key from config first, then environment
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.Anthropic != nil && *config.APIKeys.Anthropic != "" {
		apiKey = *config.APIKeys.Anthropic
	} else {
		// Try environment variable
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required (not found in config or environment)")
	}

	// Use provided model or default
	modelID := config.ModelID
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
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

// initializeMiniMax creates and configures a MiniMax LLM instance using the Anthropic-compatible API
func initializeMiniMax(config Config) (llmtypes.Model, error) {
	// Check for API key from config first, then environment
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.MiniMax != nil && *config.APIKeys.MiniMax != "" {
		apiKey = *config.APIKeys.MiniMax
	} else {
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("MINIMAX_API_KEY is required for MiniMax provider (not found in config or environment)")
	}

	modelID := config.ModelID
	if modelID == "" {
		modelID = minimaxadapter.ModelMiniMaxM25
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
	logger.Infof("Initializing MiniMax LLM with model: %s", modelID)

	llmMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "minimax_user",
		CustomFields: map[string]string{
			"provider":  "minimax",
			"operation": "llm_initialization",
		},
	}
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), modelID, config.Temperature, config.TraceID, llmMetadata)

	llm := minimaxadapter.NewMiniMaxAdapter(apiKey, modelID, logger)

	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "minimax_user",
		CustomFields: map[string]string{
			"provider":     "minimax",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized MiniMax LLM - model_id: %s", modelID)
	return llm, nil
}

// initializeAzureWithFallback creates an Azure LLM with fallback models for rate limiting
func initializeAzureWithFallback(config Config) (llmtypes.Model, error) {
	// Try primary model first
	llm, err := initializeAzure(config)
	if err == nil {
		return llm, nil
	}

	// If primary fails and we have fallback models, try them
	if len(config.FallbackModels) > 0 {
		logger := config.Logger
		if logger == nil {
			logger = &noopLoggerImpl{}
		}
		logger.Infof("Primary Azure model failed, trying fallback models - primary_model: %s, fallback_models: %v, error: %s", config.ModelID, config.FallbackModels, err.Error())

		for _, fallbackModel := range config.FallbackModels {
			fallbackConfig := config
			fallbackConfig.ModelID = fallbackModel

			llm, err := initializeAzure(fallbackConfig)
			if err == nil {
				logger.Infof("Successfully initialized fallback Azure model - fallback_model: %s", fallbackModel)
				return llm, nil
			}

			logger.Infof("Fallback Azure model failed - fallback_model: %s, error: %s", fallbackModel, err.Error())
		}
	}

	// If all models fail, return the original error
	return nil, fmt.Errorf("all Azure models failed: %w", err)
}

// initializeAzure creates and configures an Azure AI LLM instance
func initializeAzure(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time
		TopP:         config.Temperature,
		User:         "azure_user",
		CustomFields: map[string]string{
			"provider":  "azure",
			"operation": OperationLLMInitialization,
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Check for Azure config from APIKeys or environment
	var endpoint, apiKey, apiVersion, region string

	if config.APIKeys != nil && config.APIKeys.Azure != nil {
		endpoint = config.APIKeys.Azure.Endpoint
		apiKey = config.APIKeys.Azure.APIKey
		apiVersion = config.APIKeys.Azure.APIVersion
		region = config.APIKeys.Azure.Region
	}

	// Fallback to environment variables
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	if apiKey == "" {
		apiKey = os.Getenv("AZURE_AI_API_KEY")
	}
	if apiVersion == "" {
		apiVersion = os.Getenv("AZURE_AI_API_VERSION")
	}
	if region == "" {
		region = os.Getenv("AZURE_AI_REGION")
	}

	// Validate required fields
	if endpoint == "" {
		return nil, fmt.Errorf("AZURE_AI_ENDPOINT is required for Azure provider (not found in config or environment)")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_AI_API_KEY is required for Azure provider (not found in config or environment)")
	}

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "gpt-4o"
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
	logger.Infof("Initializing Azure AI LLM - model_id: %s, endpoint: %s, region: %s", modelID, endpoint, region)

	// Create Azure adapter config
	azureConfig := azureadapter.AzureConfig{
		Endpoint:   endpoint,
		APIKey:     apiKey,
		APIVersion: apiVersion,
		Region:     region,
	}

	// Create Azure adapter
	llm := azureadapter.NewAzureAdapter(azureConfig, modelID, logger)

	// Emit LLM initialization success event
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "azure_user",
		CustomFields: map[string]string{
			"provider":     "azure",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
			"endpoint":     endpoint,
			"region":       region,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Azure AI LLM - model_id: %s", modelID)
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

	// Check for API key from config first, then environment
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.OpenRouter != nil && *config.APIKeys.OpenRouter != "" {
		apiKey = *config.APIKeys.OpenRouter
	} else {
		// Try environment variables (check both naming conventions)
		apiKey = os.Getenv("OPENROUTER_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("OPEN_ROUTER_API_KEY")
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY or OPEN_ROUTER_API_KEY is required for OpenRouter provider (not found in config or environment)")
	}

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
	logger.Infof("🔧 Initializing OpenRouter LLM - model_id: %s, base_url: https://openrouter.ai/api/v1", modelID)

	// 🆕 DETAILED OPENROUTER INITIALIZATION LOGGING
	logger.Infof("🔧 [DEBUG] Creating OpenRouter LLM with OpenAI client...")
	logger.Infof("🔧 [DEBUG] Model: %s", modelID)
	logger.Infof("🔧 [DEBUG] Base URL: https://openrouter.ai/api/v1")
	logger.Infof("🔧 [DEBUG] API Key present: %v", apiKey != "")

	// Create OpenAI SDK client with OpenRouter base URL
	clientOptions := []option.RequestOption{
		option.WithAPIKey(apiKey),
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
	if logger == nil {
		logger = &noopLoggerImpl{}
	}

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

	// Check for API key from config first, then environment
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.Vertex != nil && *config.APIKeys.Vertex != "" {
		apiKey = *config.APIKeys.Vertex
		logger.Infof("🔑 [VERTEX AUTH] Using API key from config")
	} else {
		// Try environment variables
		apiKey = os.Getenv("VERTEX_API_KEY")
		if apiKey == "" {
			// Try alternative environment variable names
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("VERTEX_API_KEY or GOOGLE_API_KEY is required for Gemini models (not found in config or environment)")
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

// initializeClaudeCode creates and configures a Claude Code CLI adapter instance
func initializeClaudeCode(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0, // Will be set at call time or by CLI
		TopP:         config.Temperature,
		User:         "claude_code_user",
		CustomFields: map[string]string{
			"provider":  "claude-code",
			"operation": OperationLLMInitialization,
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Set default model if not specified
	modelID := config.ModelID
	if modelID == "" {
		modelID = "claude-code" // Default ID representing the CLI
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
	logger.Infof("Initializing Claude Code CLI adapter - model_id: %s", modelID)

	// Create Claude Code adapter
	// Note: API key is not used by the CLI adapter as it uses local auth
	llm := claudecodeadapter.NewClaudeCodeAdapter("", modelID, logger)

	// Emit LLM initialization success event
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "claude_code_user",
		CustomFields: map[string]string{
			"provider":     "claude-code",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling, // CLI has native tools
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Claude Code CLI adapter - model_id: %s", modelID)
	return llm, nil
}

// initializeGeminiCLI creates and configures a Gemini CLI adapter instance
func initializeGeminiCLI(config Config) (llmtypes.Model, error) {
	// LLM Initialization event data
	llmMetadata := LLMMetadata{
		ModelVersion: config.ModelID,
		MaxTokens:    0,
		TopP:         config.Temperature,
		User:         "gemini_cli_user",
		CustomFields: map[string]string{
			"provider":  "gemini-cli",
			"operation": OperationLLMInitialization,
		},
	}

	// Emit LLM initialization start event
	emitLLMInitializationStart(config.EventEmitter, string(config.Provider), config.ModelID, config.Temperature, config.TraceID, llmMetadata)

	// Set default model if not specified
	// Gemini CLI supports aliases: "auto" (default), "pro", "flash", "flash-lite"
	modelID := config.ModelID
	if modelID == "" {
		modelID = "auto"
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
	logger.Infof("Initializing Gemini CLI adapter - model_id: %s", modelID)

	// Resolve API key: explicit config > environment variable
	apiKey := ""
	if config.APIKeys != nil && config.APIKeys.GeminiCLI != nil {
		apiKey = *config.APIKeys.GeminiCLI
		logger.Infof("Gemini CLI: using API key from config (length=%d)", len(apiKey))
	} else if envKey := os.Getenv("GEMINI_API_KEY"); envKey != "" {
		apiKey = envKey
		logger.Infof("Gemini CLI: using API key from GEMINI_API_KEY env var (length=%d)", len(apiKey))
	} else {
		logger.Infof("Gemini CLI: no API key found in config or environment")
	}

	// Create Gemini CLI adapter — pass API key so it can set GEMINI_API_KEY on the subprocess
	llm := geminicli.NewGeminiCLIAdapter(apiKey, modelID, logger)

	// Emit LLM initialization success event
	successMetadata := LLMMetadata{
		ModelVersion: modelID,
		User:         "gemini_cli_user",
		CustomFields: map[string]string{
			"provider":     "gemini-cli",
			"status":       StatusLLMInitialized,
			"capabilities": CapabilityTextGeneration + "," + CapabilityToolCalling,
		},
	}
	emitLLMInitializationSuccess(config.EventEmitter, string(config.Provider), modelID, CapabilityTextGeneration+","+CapabilityToolCalling, config.TraceID, successMetadata)

	logger.Infof("Initialized Gemini CLI adapter - model_id: %s", modelID)
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
	case ProviderAzure:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("AZURE_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "gpt-4o"
	case ProviderClaudeCode:
		// Get primary model from environment variable
		if primaryModel := os.Getenv("CLAUDE_CODE_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "claude-code"
	case ProviderGeminiCLI:
		// Get primary model from environment variable
		// Supports aliases: "auto" (default), "pro", "flash", "flash-lite"
		// or full names: "gemini-2.5-flash", "gemini-2.5-pro", etc.
		if primaryModel := os.Getenv("GEMINI_CLI_PRIMARY_MODEL"); primaryModel != "" {
			return primaryModel
		}
		return "auto"
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
	case ProviderAzure:
		// Get fallback models from environment variable
		fallbackModelsEnv := os.Getenv("AZURE_FALLBACK_MODELS")
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
	case ProviderBedrock, ProviderOpenAI, ProviderAnthropic, ProviderOpenRouter, ProviderVertex, ProviderAzure, ProviderClaudeCode, ProviderGeminiCLI, ProviderMiniMax:
		return Provider(provider), nil
	default:
		return "", fmt.Errorf("unsupported provider: %s. Supported providers: bedrock, openai, anthropic, openrouter, vertex, azure, claude-code, gemini-cli, minimax", provider)
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
	// Use no-op logger if nil is provided
	if logger == nil {
		logger = &noopLoggerImpl{}
	}
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
// extractTextFromParts extracts text content from message parts
func extractTextFromParts(parts []llmtypes.ContentPart) string {
	var textParts []string
	for _, part := range parts {
		if textPart, ok := part.(llmtypes.TextContent); ok {
			textParts = append(textParts, textPart.Text)
		}
	}
	return strings.Join(textParts, " ")
}

func (p *ProviderAwareLLM) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Note: LLM generation start event is now emitted at the agent level to avoid duplication

	// Automatically add usage parameter for OpenRouter requests to get cache token information
	if p.provider == ProviderOpenRouter {
		options = append(options, WithOpenRouterUsage())
	}

	// 🆕 USEFUL LOGGING - System prompts, messages, and tools
	// Parse call options to extract tools
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Extract and log system prompts
	var systemPrompts []string
	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeSystem {
			text := extractTextFromParts(msg.Parts)
			if text != "" {
				systemPrompts = append(systemPrompts, text)
			}
		}
	}
	if len(systemPrompts) > 0 {
		p.logger.Infof("📋 SYSTEM PROMPTS (%d):", len(systemPrompts))
		for i, prompt := range systemPrompts {
			p.logger.Infof("   [%d] %s", i+1, prompt)
		}
	} else {
		p.logger.Infof("📋 SYSTEM PROMPTS: None")
	}

	// Log all messages
	p.logger.Infof("💬 MESSAGES (%d):", len(messages))
	for i, msg := range messages {
		text := extractTextFromParts(msg.Parts)
		// Truncate very long messages for readability
		displayText := text
		if len(displayText) > 500 {
			displayText = displayText[:500] + "... [truncated]"
		}
		p.logger.Infof("   [%d] Role: %s, Content: %s", i+1, msg.Role, displayText)
	}

	// Log tools if provided
	if len(opts.Tools) > 0 {
		p.logger.Infof("🔧 TOOLS (%d):", len(opts.Tools))
		for i, tool := range opts.Tools {
			if tool.Function != nil {
				toolJSON, err := json.MarshalIndent(tool, "      ", "  ")
				if err != nil {
					p.logger.Infof("   [%d] %s (error marshaling: %v)", i+1, tool.Function.Name, err)
				} else {
					p.logger.Infof("   [%d] %s:\n%s", i+1, tool.Function.Name, string(toolJSON))
				}
			} else {
				p.logger.Infof("   [%d] Tool with nil Function", i+1)
			}
		}
	} else {
		p.logger.Infof("🔧 TOOLS: None")
	}

	// Log request timing
	requestStartTime := time.Now()
	p.logger.Infof("⏱️  LLM REQUEST START - Time: %s", requestStartTime.Format(time.RFC3339))

	// Call the underlying LLM
	resp, err := p.Model.GenerateContent(ctx, messages, options...)

	// Log response timing
	requestEndTime := time.Now()
	duration := requestEndTime.Sub(requestStartTime)
	p.logger.Infof("⏱️  LLM RESPONSE RECEIVED - Time: %s, Duration: %v", requestEndTime.Format(time.RFC3339), duration)

	// Check if we have a valid response
	if err != nil {
		p.logger.Infof("❌ LLM generation failed - provider: %s, model: %s, error: %v", string(p.provider), p.modelID, err)

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

	// Validate response structure
	if resp == nil {
		p.logger.Infof("❌ Response is nil")

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
		p.logger.Infof("❌ Response.Choices is nil")

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
				functionName := "N/A"
				arguments := "{}"
				if toolCall.FunctionCall != nil {
					functionName = toolCall.FunctionCall.Name
					if toolCall.FunctionCall.Arguments != "" {
						arguments = toolCall.FunctionCall.Arguments
					}
				}
				p.logger.Infof("   Tool Call %d: ID=%s, Type=%s, Function=%s, Arguments=%s",
					i+1, toolCall.ID, toolCall.Type, functionName, arguments)
			}
			// Note: Tool call events are emitted later in the function (line ~1594) to avoid duplication
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

		// Log tool calls if present (even when content is also present)
		if len(choice.ToolCalls) > 0 {
			p.logger.Infof("🔧 TOOL CALLS IN RESPONSE (%d):", len(choice.ToolCalls))
			for i, toolCall := range choice.ToolCalls {
				functionName := "N/A"
				arguments := "{}"
				if toolCall.FunctionCall != nil {
					functionName = toolCall.FunctionCall.Name
					if toolCall.FunctionCall.Arguments != "" {
						arguments = toolCall.FunctionCall.Arguments
					}
				}
				p.logger.Infof("   Tool Call %d: ID=%s, Type=%s, Function=%s, Arguments=%s",
					i+1, toolCall.ID, toolCall.Type, functionName, arguments)
			}
		}

		// Emit tool call events for all tool calls (even when content is present)
		if len(choice.ToolCalls) > 0 {
			for _, toolCall := range choice.ToolCalls {
				toolName := ""
				arguments := "{}"
				if toolCall.FunctionCall != nil {
					toolName = toolCall.FunctionCall.Name
					if toolCall.FunctionCall.Arguments != "" {
						arguments = toolCall.FunctionCall.Arguments
					}
				}

				toolCallMetadata := LLMMetadata{
					User: "tool_call_user",
					CustomFields: map[string]string{
						"provider":     string(p.provider),
						"model_id":     p.modelID,
						"tool_call_id": toolCall.ID,
						"tool_type":    toolCall.Type,
						"tool_name":    toolName,
					},
				}
				emitToolCallDetected(p.eventEmitter, string(p.provider), p.modelID, toolCall.ID, toolName, arguments, p.traceID, toolCallMetadata)
			}
		}
	}

	// Extract token usage using unified Usage struct (comprehensive extraction)
	var usage *llmtypes.Usage
	if resp.Usage != nil {
		// Use unified Usage field (already populated by adapters with all token types)
		usage = resp.Usage
	} else if len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil {
		// Fallback: Extract from GenerationInfo using comprehensive extraction
		usage = llmtypes.ExtractUsageFromGenerationInfo(resp.Choices[0].GenerationInfo)
	}

	if usage != nil {
		// Calculate total tokens if not provided by the provider
		if usage.TotalTokens == 0 && usage.InputTokens > 0 && usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}

		// Build comprehensive log message with all token types
		logMsg := fmt.Sprintf("Token usage extracted: Input=%d, Output=%d, Total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		if usage.CacheTokens != nil && *usage.CacheTokens > 0 {
			logMsg += fmt.Sprintf(", Cached=%d", *usage.CacheTokens)
		}
		if usage.ThoughtsTokens != nil && *usage.ThoughtsTokens > 0 {
			logMsg += fmt.Sprintf(", Thoughts=%d", *usage.ThoughtsTokens)
		}
		if usage.ReasoningTokens != nil && *usage.ReasoningTokens > 0 {
			logMsg += fmt.Sprintf(", Reasoning=%d", *usage.ReasoningTokens)
		}
		p.logger.Infof(logMsg)

		// Emit LLM generation success event with comprehensive token usage
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
			},
		}

		// Add optional token types to metadata if present
		if usage.CacheTokens != nil && *usage.CacheTokens > 0 {
			successMetadata.CustomFields["cache_tokens"] = fmt.Sprintf("%d", *usage.CacheTokens)
		}
		if usage.ThoughtsTokens != nil && *usage.ThoughtsTokens > 0 {
			successMetadata.CustomFields["thoughts_tokens"] = fmt.Sprintf("%d", *usage.ThoughtsTokens)
		}
		if usage.ReasoningTokens != nil && *usage.ReasoningTokens > 0 {
			successMetadata.CustomFields["reasoning_tokens"] = fmt.Sprintf("%d", *usage.ReasoningTokens)
		}

		successMetadata.CustomFields["note"] = "Token usage extracted from unified Usage struct"
		emitLLMGenerationSuccess(p.eventEmitter, string(p.provider), p.modelID, OperationLLMGeneration, len(messages), getTemperatureFromOptions(options), extractMessageContentAsString(messages), len(resp.Choices[0].Content), len(resp.Choices), p.traceID, successMetadata)
	} else {
		// No token usage available, emit success event without usage
		p.logger.Infof("No token usage available (neither resp.Usage nor GenerationInfo)")

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

// WithMCPConfig sets the MCP configuration JSON string for the Claude Code adapter session.
func WithMCPConfig(config string) llmtypes.CallOption {
	return claudecodeadapter.WithMCPConfig(config)
}

// WithDangerouslySkipPermissions enables the --dangerously-skip-permissions flag for the Claude Code CLI.
// CAUTION: This allows the agent to execute any tool without user confirmation.
func WithDangerouslySkipPermissions() llmtypes.CallOption {
	return claudecodeadapter.WithDangerouslySkipPermissions()
}

// WithClaudeCodeSettings sets the --settings flag for the Claude Code CLI.
// It accepts either a JSON string or a file path.
func WithClaudeCodeSettings(settings string) llmtypes.CallOption {
	return claudecodeadapter.WithClaudeCodeSettings(settings)
}

// WithClaudeCodeTools sets the --tools flag for the Claude Code CLI.
// Use "" to disable all built-in tools.
func WithClaudeCodeTools(tools string) llmtypes.CallOption {
	return claudecodeadapter.WithClaudeCodeTools(tools)
}

// WithAllowedTools sets the --allowed-tools flag for the Claude Code CLI.
// Example: "mcp__api-bridge__*" to allow all tools from the bridge.
func WithAllowedTools(tools string) llmtypes.CallOption {
	return claudecodeadapter.WithAllowedTools(tools)
}

// WithMaxTurns sets the --max-turns flag for the Claude Code CLI.
// Limits the number of agentic turns. Claude Code exits with an error when the limit is reached.
func WithMaxTurns(maxTurns int) llmtypes.CallOption {
	return claudecodeadapter.WithMaxTurns(maxTurns)
}

// WithResumeSessionID sets the --resume flag so the Claude Code CLI resumes
// an existing session instead of starting a new one.
func WithResumeSessionID(id string) llmtypes.CallOption {
	return claudecodeadapter.WithResumeSessionID(id)
}

// --- Gemini CLI Wrapper Functions ---

// WithGeminiModel sets the --model flag for the Gemini CLI.
func WithGeminiModel(model string) llmtypes.CallOption {
	return geminicli.WithGeminiModel(model)
}

// WithGeminiResumeSessionID sets the --resume flag so the Gemini CLI resumes
// an existing session instead of starting a new one.
func WithGeminiResumeSessionID(id string) llmtypes.CallOption {
	return geminicli.WithResumeSessionID(id)
}

// WithGeminiApprovalMode sets the --approval-mode flag for the Gemini CLI.
func WithGeminiApprovalMode(mode string) llmtypes.CallOption {
	return geminicli.WithApprovalMode(mode)
}

// WithGeminiSystemPromptFile sets the GEMINI_SYSTEM_MD environment variable path.
func WithGeminiSystemPromptFile(path string) llmtypes.CallOption {
	return geminicli.WithSystemPromptFile(path)
}

// WithGeminiProjectSettings writes a .gemini/settings.json in a temp directory
// and runs the Gemini CLI from there. This controls tool restrictions (tools.core),
// MCP server configuration (mcpServers), and other project-level settings.
func WithGeminiProjectSettings(settingsJSON string) llmtypes.CallOption {
	return geminicli.WithProjectSettings(settingsJSON)
}

// WithGeminiAllowedTools sets the --allowed-tools flag for the Gemini CLI.
// These tools bypass the confirmation dialog.
func WithGeminiAllowedTools(tools string) llmtypes.CallOption {
	return geminicli.WithAllowedTools(tools)
}

// WithGeminiProjectDirID sets an explicit project directory ID for the Gemini CLI.
// This ensures resume calls use the same isolated project directory as the original invocation.
func WithGeminiProjectDirID(id string) llmtypes.CallOption {
	return geminicli.WithProjectDirID(id)
}

// LLM Configuration Management Functions

// LLMDefaultsResponse represents the response structure for LLM defaults
type LLMDefaultsResponse struct {
	PrimaryConfig    map[string]interface{} `json:"primary_config"`
	OpenrouterConfig map[string]interface{} `json:"openrouter_config"`
	BedrockConfig    map[string]interface{} `json:"bedrock_config"`
	OpenaiConfig     map[string]interface{} `json:"openai_config"`
	AnthropicConfig  map[string]interface{} `json:"anthropic_config"`
	AzureConfig      map[string]interface{} `json:"azure_config"`
	MinimaxConfig    map[string]interface{} `json:"minimax_config"`
	AvailableModels  map[string][]string    `json:"available_models"`
}

// APIKeyValidationRequest represents a request to validate an API key
type APIKeyValidationRequest struct {
	Provider string                 `json:"provider"`
	APIKey   string                 `json:"api_key"`
	ModelID  string                 `json:"model_id,omitempty"` // Optional model ID for Bedrock validation
	Options  map[string]interface{} `json:"options,omitempty"`
}

// APIKeyValidationResponse represents the response for API key validation
type APIKeyValidationResponse struct {
	Valid            bool                   `json:"valid"`
	Message          string                 `json:"message,omitempty"`
	Error            string                 `json:"error,omitempty"`
	CorrectedOptions map[string]interface{} `json:"corrected_options,omitempty"`
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

	// Anthropic configuration
	anthropicModel := os.Getenv("ANTHROPIC_PRIMARY_MODEL")
	if anthropicModel == "" {
		anthropicModel = "claude-sonnet-4-20250514"
	}
	anthropicAPIKey := os.Getenv("ANTHROPIC_API_KEY")

	// Azure configuration
	azureModel := os.Getenv("AZURE_PRIMARY_MODEL")
	azureAPIKey := os.Getenv("AZURE_AI_API_KEY")
	azureEndpoint := os.Getenv("AZURE_AI_ENDPOINT")

	// MiniMax configuration
	minimaxModel := os.Getenv("MINIMAX_PRIMARY_MODEL")
	if minimaxModel == "" {
		minimaxModel = "MiniMax-M2.5"
	}
	minimaxAPIKey := os.Getenv("MINIMAX_API_KEY")

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
			"api_key":                 openrouterAPIKey,
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
			"api_key":                 openaiAPIKey,
		},
		AnthropicConfig: map[string]interface{}{
			"provider":        "anthropic",
			"model_id":        anthropicModel,
			"fallback_models": []string{},
			"api_key":         anthropicAPIKey,
		},
		AzureConfig: map[string]interface{}{
			"provider":        "azure",
			"model_id":        azureModel,
			"fallback_models": []string{},
			"api_key":         azureAPIKey,
			"endpoint":        azureEndpoint,
		},
		MinimaxConfig: map[string]interface{}{
			"provider":        "minimax",
			"model_id":        minimaxModel,
			"fallback_models": []string{},
			"api_key":         minimaxAPIKey,
		},
		AvailableModels: map[string][]string{
			"bedrock":    getBedrockAvailableModels(),
			"openrouter": getOpenRouterAvailableModels(),
			"openai":     getOpenAIAvailableModels(),
			"anthropic":  getAnthropicAvailableModels(),
			"azure":      getAzureAvailableModels(),
			"minimax":    getMiniMaxAvailableModels(),
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
	var correctedOptions map[string]interface{}

	fmt.Printf("[API KEY VALIDATION] Validating %s API key\n", req.Provider)
	switch req.Provider {
	case "openrouter":
		isValid, message, err = validateOpenRouterAPIKey(req.APIKey, req.ModelID, req.Options)
	case "openai":
		isValid, message, err = validateOpenAIAPIKey(req.APIKey, req.ModelID, req.Options)
	case "bedrock":
		// Bedrock uses AWS credentials, test them instead of API key
		fmt.Printf("[API KEY VALIDATION] Testing AWS Bedrock credentials\n")
		isValid, message, err = validateBedrockCredentials(req.ModelID, req.Options)
	case "vertex":
		// Vertex supports both API key and OAuth authentication
		if req.APIKey == "" {
			// Test OAuth authentication (gcloud/service account/ADC)
			fmt.Printf("[API KEY VALIDATION] Testing Vertex AI OAuth credentials\n")
			isValid, message, err = validateVertexCredentials(req.ModelID, req.Options)
		} else {
			// Test API key authentication
			fmt.Printf("[API KEY VALIDATION] Testing Vertex AI API key\n")
			isValid, message, err = validateVertexAPIKey(req.APIKey, req.ModelID, req.Options)
		}
	case "anthropic":
		// Anthropic validation with real GenerateContent call
		fmt.Printf("[API KEY VALIDATION] Testing Anthropic API key\n")
		isValid, message, err = validateAnthropicAPIKey(req.APIKey, req.ModelID, req.Options)
	case "minimax":
		// MiniMax validation with real GenerateContent call
		fmt.Printf("[API KEY VALIDATION] Testing MiniMax API key\n")
		isValid, message, err = validateMinimaxAPIKey(req.APIKey, req.ModelID, req.Options)
	case "azure":
		// Azure AI validation with real GenerateContent call
		fmt.Printf("[API KEY VALIDATION] Testing Azure AI API key\n")
		isValid, message, correctedOptions, err = validateAzureAPIKey(req.APIKey, req.ModelID, req.Options)
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
			Error: fmt.Sprintf("Validation failed: %v", err),
		}
	}

	// Return validation result
	if isValid {
		fmt.Printf("[API KEY VALIDATION SUCCESS] %s: %s\n", req.Provider, message)
	} else {
		fmt.Printf("[API KEY VALIDATION FAILED] %s: %s\n", req.Provider, message)
	}

	return APIKeyValidationResponse{
		Valid:            isValid,
		Message:          message,
		CorrectedOptions: correctedOptions,
	}
}

// validateOpenRouterAPIKey validates an OpenRouter API key by making a real GenerateContent call
func validateOpenRouterAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[OPENROUTER VALIDATION] Starting API key validation\n")

	// Basic format validation
	if !strings.HasPrefix(apiKey, "sk-or-") {
		fmt.Printf("[OPENROUTER VALIDATION WARN] Format validation failed - missing sk-or- prefix\n")
		return false, "Invalid OpenRouter API key format", nil
	}
	fmt.Printf("[OPENROUTER VALIDATION] Format validation passed\n")

	// Use a default model if none provided
	if modelID == "" {
		modelID = "moonshotai/kimi-k2"
		fmt.Printf("[OPENROUTER VALIDATION] Using default model: %s\n", modelID)
	}

	// Set API key in environment temporarily for initialization
	originalKey := os.Getenv("OPEN_ROUTER_API_KEY")
	os.Setenv("OPEN_ROUTER_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("OPEN_ROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPEN_ROUTER_API_KEY")
		}
	}()

	// Create a no-op logger for validation
	noopLog := &noopLoggerImpl{}

	// Extract temperature from options (no default - let the model use its own default)
	temperature := extractTemperatureFromOptions(options)

	// Create OpenRouter LLM instance
	fmt.Printf("[OPENROUTER VALIDATION] Creating OpenRouter LLM instance (temperature: %v)\n", temperature)
	config := Config{
		Provider:    ProviderOpenRouter,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      noopLog,
		Context:     context.Background(),
	}

	llm, err := initializeOpenRouter(config)
	if err != nil {
		fmt.Printf("[OPENROUTER VALIDATION ERROR] Failed to create LLM instance: %v\n", err)
		return false, fmt.Sprintf("Failed to create OpenRouter LLM instance: %v", err), nil
	}

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[OPENROUTER VALIDATION] Making test generation call to OpenRouter\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[OPENROUTER VALIDATION ERROR] OpenRouter test generation failed: %v\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") {
			return false, "Invalid OpenRouter API key", nil
		}
		if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
			return false, "OpenRouter API rate limit exceeded", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "OpenRouter service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("OpenRouter test generation failed: %v", err), nil
	}

	fmt.Printf("[OPENROUTER VALIDATION SUCCESS] OpenRouter API key is valid\n")
	return true, fmt.Sprintf("OpenRouter API key is valid for model %s", modelID), nil
}

// validateOpenAIAPIKey validates an OpenAI API key by making a real GenerateContent call
func validateOpenAIAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[OPENAI VALIDATION] Starting API key validation\n")
	// Basic format validation
	if !strings.HasPrefix(apiKey, "sk-") {
		fmt.Printf("[OPENAI VALIDATION WARN] Format validation failed - missing sk- prefix\n")
		return false, "Invalid OpenAI API key format", nil
	}
	fmt.Printf("[OPENAI VALIDATION] Format validation passed\n")

	// Use a default model if none provided
	if modelID == "" {
		modelID = "gpt-4o-mini"
		fmt.Printf("[OPENAI VALIDATION] Using default model: %s\n", modelID)
	}

	// Set API key in environment temporarily for initialization
	originalKey := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENAI_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	// Create a no-op logger for validation
	noopLog := &noopLoggerImpl{}

	// Extract temperature from options (no default - let the model use its own default)
	temperature := extractTemperatureFromOptions(options)

	// Create OpenAI LLM instance
	fmt.Printf("[OPENAI VALIDATION] Creating OpenAI LLM instance (temperature: %v)\n", temperature)
	config := Config{
		Provider:    ProviderOpenAI,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      noopLog,
		Context:     context.Background(),
	}

	llm, err := initializeOpenAI(config)
	if err != nil {
		fmt.Printf("[OPENAI VALIDATION ERROR] Failed to create LLM instance: %v\n", err)
		return false, fmt.Sprintf("Failed to create OpenAI LLM instance: %v", err), nil
	}

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[OPENAI VALIDATION] Making test generation call to OpenAI\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[OPENAI VALIDATION ERROR] OpenAI test generation failed: %v\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") {
			return false, "Invalid OpenAI API key", nil
		}
		if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
			return false, "OpenAI API rate limit exceeded", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "OpenAI service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("OpenAI test generation failed: %v", err), nil
	}

	fmt.Printf("[OPENAI VALIDATION SUCCESS] OpenAI API key is valid\n")
	return true, fmt.Sprintf("OpenAI API key is valid for model %s", modelID), nil
}

// validateAnthropicAPIKey validates an Anthropic API key by making a real GenerateContent call
func validateAnthropicAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[ANTHROPIC VALIDATION] Starting API key validation\n")

	// Basic format validation - Anthropic API keys start with "sk-ant-"
	if !strings.HasPrefix(apiKey, "sk-ant-") {
		fmt.Printf("[ANTHROPIC VALIDATION WARN] Format validation failed - missing sk-ant- prefix\n")
		return false, "Invalid Anthropic API key format", nil
	}
	fmt.Printf("[ANTHROPIC VALIDATION] Format validation passed\n")

	// Use a default model if none provided
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
		fmt.Printf("[ANTHROPIC VALIDATION] Using default model: %s\n", modelID)
	}

	// Set API key in environment temporarily for initialization
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalKey)
		} else {
			os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	// Create a no-op logger for validation
	noopLog := &noopLoggerImpl{}

	// Extract temperature from options (no default - let the model use its own default)
	temperature := extractTemperatureFromOptions(options)

	// Create Anthropic LLM instance
	fmt.Printf("[ANTHROPIC VALIDATION] Creating Anthropic LLM instance (temperature: %v)\n", temperature)
	config := Config{
		Provider:    ProviderAnthropic,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      noopLog,
		Context:     context.Background(),
	}

	llm, err := initializeAnthropic(config)
	if err != nil {
		fmt.Printf("[ANTHROPIC VALIDATION ERROR] Failed to create LLM instance: %v\n", err)
		return false, fmt.Sprintf("Failed to create Anthropic LLM instance: %v", err), nil
	}

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[ANTHROPIC VALIDATION] Making test generation call to Anthropic\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[ANTHROPIC VALIDATION ERROR] Anthropic test generation failed: %v\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") {
			return false, "Invalid Anthropic API key", nil
		}
		if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
			return false, "Anthropic API rate limit exceeded", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "Anthropic service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("Anthropic test generation failed: %v", err), nil
	}

	fmt.Printf("[ANTHROPIC VALIDATION SUCCESS] Anthropic API key is valid\n")
	return true, fmt.Sprintf("Anthropic API key is valid for model %s", modelID), nil
}

// validateMinimaxAPIKey validates a MiniMax API key by making a real GenerateContent call
func validateMinimaxAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[MINIMAX VALIDATION] Starting API key validation\n")

	if apiKey == "" {
		return false, "MiniMax API key is required", nil
	}

	// Use a default model if none provided
	if modelID == "" {
		modelID = "MiniMax-M2.5"
		fmt.Printf("[MINIMAX VALIDATION] Using default model: %s\n", modelID)
	}

	// Set API key in environment temporarily for initialization
	originalKey := os.Getenv("MINIMAX_API_KEY")
	os.Setenv("MINIMAX_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("MINIMAX_API_KEY", originalKey)
		} else {
			os.Unsetenv("MINIMAX_API_KEY")
		}
	}()

	noopLog := &noopLoggerImpl{}
	temperature := extractTemperatureFromOptions(options)

	config := Config{
		Provider:    ProviderMiniMax,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      noopLog,
		Context:     context.Background(),
	}

	llm, err := initializeMiniMax(config)
	if err != nil {
		fmt.Printf("[MINIMAX VALIDATION ERROR] Failed to create LLM instance: %v\n", err)
		return false, fmt.Sprintf("Failed to create MiniMax LLM instance: %v", err), nil
	}

	callOptions := createCallOptionsFromMap(options)

	fmt.Printf("[MINIMAX VALIDATION] Making test generation call\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[MINIMAX VALIDATION ERROR] MiniMax test generation failed: %v\n", err)
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "1004") {
			return false, "Invalid MiniMax API key", nil
		}
		if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
			return false, "MiniMax API rate limit exceeded", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "MiniMax service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("MiniMax test generation failed: %v", err), nil
	}

	fmt.Printf("[MINIMAX VALIDATION SUCCESS] MiniMax API key is valid\n")
	return true, fmt.Sprintf("MiniMax API key is valid for model %s", modelID), nil
}

// validateAzureAPIKey validates an Azure AI API key by making a real GenerateContent call
// Returns: isValid, message, correctedOptions, error
func validateAzureAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, map[string]interface{}, error) {
	fmt.Printf("[AZURE VALIDATION] Starting API key validation for model: %s\n", modelID)

	// Check for required endpoint in options
	var endpoint string
	if options != nil {
		if e, ok := options["endpoint"].(string); ok {
			endpoint = e
		}
	}
	if endpoint == "" {
		// Fallback to environment variable
		endpoint = os.Getenv("AZURE_AI_ENDPOINT")
	}
	if endpoint == "" {
		fmt.Printf("[AZURE VALIDATION WARN] No endpoint provided\n")
		return false, "Azure endpoint URL is required", nil, nil
	}
	fmt.Printf("[AZURE VALIDATION] Endpoint: %s\n", endpoint)

	// Basic validation - API key should not be empty
	if apiKey == "" {
		fmt.Printf("[AZURE VALIDATION WARN] API key is empty\n")
		return false, "Azure API key is required", nil, nil
	}
	fmt.Printf("[AZURE VALIDATION] API key format validation passed\n")

	// Use a default model if none provided
	if modelID == "" {
		modelID = "gpt-4o"
		fmt.Printf("[AZURE VALIDATION] Using default model: %s\n", modelID)
	}

	// Helper to try validation with a specific endpoint and modelID
	tryValidation := func(testEndpoint string, testModelID string) (bool, string, error) {
		// Extract optional fields from options
		var apiVersion, region string
		if options != nil {
			if v, ok := options["api_version"].(string); ok {
				apiVersion = v
			}
			if r, ok := options["region"].(string); ok {
				region = r
			}
		}

		// Set environment variables temporarily for initialization
		originalEndpoint := os.Getenv("AZURE_AI_ENDPOINT")
		originalKey := os.Getenv("AZURE_AI_API_KEY")
		originalVersion := os.Getenv("AZURE_AI_API_VERSION")
		originalRegion := os.Getenv("AZURE_AI_REGION")

		os.Setenv("AZURE_AI_ENDPOINT", testEndpoint)
		os.Setenv("AZURE_AI_API_KEY", apiKey)
		if apiVersion != "" {
			os.Setenv("AZURE_AI_API_VERSION", apiVersion)
		}
		if region != "" {
			os.Setenv("AZURE_AI_REGION", region)
		}
		defer func() {
			// Restore original environment variables
			if originalEndpoint != "" {
				os.Setenv("AZURE_AI_ENDPOINT", originalEndpoint)
			} else {
				os.Unsetenv("AZURE_AI_ENDPOINT")
			}
			if originalKey != "" {
				os.Setenv("AZURE_AI_API_KEY", originalKey)
			} else {
				os.Unsetenv("AZURE_AI_API_KEY")
			}
			if originalVersion != "" {
				os.Setenv("AZURE_AI_API_VERSION", originalVersion)
			} else {
				os.Unsetenv("AZURE_AI_API_VERSION")
			}
			if originalRegion != "" {
				os.Setenv("AZURE_AI_REGION", originalRegion)
			} else {
				os.Unsetenv("AZURE_AI_REGION")
			}
		}()

		// Create a no-op logger for validation
		noopLog := &noopLoggerImpl{}

		// Extract temperature from options
		temperature := extractTemperatureFromOptions(options)

		// Create Azure LLM instance
		fmt.Printf("[AZURE VALIDATION] Creating Azure LLM instance (endpoint: %s, model: %s)\n", testEndpoint, testModelID)
		config := Config{
			Provider:    ProviderAzure,
			ModelID:     testModelID,
			Temperature: temperature,
			Logger:      noopLog,
			Context:     context.Background(),
		}

		llm, err := initializeAzure(config)
		if err != nil {
			return false, fmt.Sprintf("Failed to create Azure LLM instance: %v", err), nil
		}

		// Create call options from map
		callOptions := createCallOptionsFromMap(options)

		// Test the LLM with a simple generation call
		fmt.Printf("[AZURE VALIDATION] Making test generation call to Azure AI\n")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
			},
		}, callOptions...)
		if err != nil {
			// Check for specific error types
			if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") {
				return false, "Invalid Azure API key", nil
			}
			if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
				return false, "Azure API rate limit exceeded", nil
			}
			if strings.Contains(err.Error(), "timeout") {
				return false, "Azure service timeout - check network connectivity", nil
			}
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "unknown_model") || strings.Contains(err.Error(), "Unknown model") || strings.Contains(err.Error(), "DeploymentNotFound") {
				return false, fmt.Sprintf("Model '%s' not found or endpoint incorrect", testModelID), nil
			}
			return false, fmt.Sprintf("Azure test generation failed: %v", err), nil
		}

		return true, fmt.Sprintf("Azure API key is valid for model %s", testModelID), nil
	}

	// Clean Model ID: strip date suffix like -2026-01-14
	cleanModelID := modelID
	if parts := strings.Split(modelID, "-20"); len(parts) > 1 {
		cleanModelID = parts[0]
	}

	// Try combinations
	endpoints := []string{endpoint}
	
	// Case 1: services.ai.azure.com -> cognitiveservices.azure.com
	if strings.Contains(endpoint, "services.ai.azure.com") {
		parts := strings.Split(endpoint, "services.ai.azure.com")
		if len(parts) > 0 {
			prefix := parts[0]
			if strings.HasPrefix(prefix, "https://") {
				resourceName := strings.TrimPrefix(prefix, "https://")
				resourceName = strings.TrimSuffix(resourceName, ".")
				if resourceName != "" {
					derivedEndpoint := fmt.Sprintf("https://%s.cognitiveservices.azure.com/", resourceName)
					endpoints = append(endpoints, derivedEndpoint)
				}
			}
		}
	}

	models := []string{modelID}
	if cleanModelID != modelID {
		models = append(models, cleanModelID)
	}

	var lastMessage string
	var lastErr error

	for _, testEndpoint := range endpoints {
		for _, testModel := range models {
			isValid, message, err := tryValidation(testEndpoint, testModel)
			if isValid {
				// Success! Check if we need to return corrected options
				correctedOptions := make(map[string]interface{})
				if options != nil {
					for k, v := range options {
						correctedOptions[k] = v
					}
				}
				
				isCorrected := false
				if testEndpoint != endpoint {
					correctedOptions["endpoint"] = testEndpoint
					isCorrected = true
				}
				if testModel != modelID {
					correctedOptions["model_id"] = testModel
					isCorrected = true
				}

				if isCorrected {
					msg := fmt.Sprintf("%s (Note: We automatically optimized your configuration to endpoint: %s, model: %s)", message, testEndpoint, testModel)
					return true, msg, correctedOptions, nil
				}
				return true, message, nil, nil
			}
			lastMessage = message
			lastErr = err
		}
	}

	return false, lastMessage, nil, lastErr
}

// validateVertexCredentials validates Vertex AI OAuth credentials (gcloud/service account/ADC)
func validateVertexCredentials(modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[VERTEX VALIDATION] Starting OAuth credentials validation\n")

	// Check for required environment variables
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("VERTEX_PROJECT_ID")
	}
	if projectID == "" {
		fmt.Printf("[VERTEX VALIDATION WARN] GOOGLE_CLOUD_PROJECT or VERTEX_PROJECT_ID not set\n")
		return false, "GOOGLE_CLOUD_PROJECT or VERTEX_PROJECT_ID environment variable is required for OAuth authentication", nil
	}

	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = os.Getenv("VERTEX_LOCATION_ID")
	}
	if location == "" {
		location = "us-central1"
		fmt.Printf("[VERTEX VALIDATION] Using default location: %s\n", location)
	}
	// Vertex AI doesn't support "global" location
	if location == "global" {
		location = "us-central1"
		fmt.Printf("[VERTEX VALIDATION] Location 'global' is not valid for Vertex AI, using: %s\n", location)
	}

	fmt.Printf("[VERTEX VALIDATION] Testing OAuth with project: %s, location: %s\n", projectID, location)

	// Use a default model if none provided
	if modelID == "" {
		modelID = "gemini-2.5-flash"
		fmt.Printf("[VERTEX VALIDATION] Using default model: %s\n", modelID)
	}

	// Test OAuth by creating an LLM instance and making a real API call
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a no-op logger for validation
	noopLog := &noopLoggerImpl{}

	// Detect if this is an Anthropic model (starts with "claude-")
	isAnthropicModel := strings.HasPrefix(modelID, "claude-")

	var llm llmtypes.Model
	var err error

	if isAnthropicModel {
		// Create Vertex Anthropic adapter for OAuth
		fmt.Printf("[VERTEX VALIDATION] Creating Vertex Anthropic adapter for model: %s\n", modelID)
		llm = vertexadapter.NewVertexAnthropicAdapter(projectID, location, modelID, noopLog)
	} else {
		// For Gemini models with OAuth, use Vertex AI backend (not Gemini Developer API)
		fmt.Printf("[VERTEX VALIDATION] Creating Vertex AI Gemini adapter for model: %s with OAuth\n", modelID)

		// Set environment variables for Vertex AI OAuth (genai library reads these)
		originalProject := os.Getenv("GOOGLE_CLOUD_PROJECT")
		originalLocation := os.Getenv("GOOGLE_CLOUD_LOCATION")
		originalUseVertex := os.Getenv("GOOGLE_GENAI_USE_VERTEXAI")

		os.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
		os.Setenv("GOOGLE_CLOUD_LOCATION", location)
		os.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")

		defer func() {
			if originalProject != "" {
				os.Setenv("GOOGLE_CLOUD_PROJECT", originalProject)
			} else {
				os.Unsetenv("GOOGLE_CLOUD_PROJECT")
			}
			if originalLocation != "" {
				os.Setenv("GOOGLE_CLOUD_LOCATION", originalLocation)
			} else {
				os.Unsetenv("GOOGLE_CLOUD_LOCATION")
			}
			if originalUseVertex != "" {
				os.Setenv("GOOGLE_GENAI_USE_VERTEXAI", originalUseVertex)
			} else {
				os.Unsetenv("GOOGLE_GENAI_USE_VERTEXAI")
			}
		}()

		// Create Google GenAI client with OAuth (no API key, uses BackendVertexAI via env vars)
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			Backend: genai.BackendVertexAI,
		})
		if err != nil {
			fmt.Printf("[VERTEX VALIDATION ERROR] Failed to create Vertex AI client: %v\n", err)
			return false, fmt.Sprintf("Failed to create Vertex AI client for Gemini model '%s': %v", modelID, err), nil
		}

		// Create Gemini adapter with Vertex AI backend
		llm = vertexadapter.NewGoogleGenAIAdapter(client, modelID, noopLog)
	}

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[VERTEX VALIDATION] Making test generation call to Vertex AI\n")
	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[VERTEX VALIDATION ERROR] Vertex AI test generation failed: %v\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "authentication") || strings.Contains(err.Error(), "unauthorized") {
			return false, "OAuth authentication failed. Make sure you have run 'gcloud auth application-default login' or set up service account credentials.", nil
		}
		if strings.Contains(err.Error(), "permission") || strings.Contains(err.Error(), "forbidden") {
			return false, "OAuth credentials do not have permission to access Vertex AI", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "Vertex AI service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("Vertex AI test generation failed: %v", err), nil
	}

	fmt.Printf("[VERTEX VALIDATION SUCCESS] Vertex AI OAuth credentials are valid\n")
	return true, fmt.Sprintf("Vertex AI OAuth authentication successful (project: %s, location: %s)", projectID, location), nil
}

// validateVertexAPIKey validates a Vertex AI (Google Gemini) API key by making a real GenerateContent call
func validateVertexAPIKey(apiKey string, modelID string, options map[string]interface{}) (bool, string, error) {
	fmt.Printf("[VERTEX VALIDATION] Starting API key validation\n")
	// Basic validation - Google API keys don't have a specific prefix
	if apiKey == "" {
		fmt.Printf("[VERTEX VALIDATION WARN] API key is empty\n")
		return false, "API key is empty", nil
	}
	fmt.Printf("[VERTEX VALIDATION] API key format check passed\n")

	// Use a default model if none provided
	if modelID == "" {
		modelID = "gemini-2.5-flash"
		fmt.Printf("[VERTEX VALIDATION] Using default model: %s\n", modelID)
	}

	// Set API key in environment temporarily for initialization
	originalKey := os.Getenv("VERTEX_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	os.Setenv("VERTEX_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("VERTEX_API_KEY", originalKey)
		} else {
			os.Unsetenv("VERTEX_API_KEY")
		}
		if originalGoogleKey != "" {
			os.Setenv("GOOGLE_API_KEY", originalGoogleKey)
		}
	}()

	// Create a no-op logger for validation
	noopLog := &noopLoggerImpl{}

	// Extract temperature from options (no default - let the model use its own default)
	temperature := extractTemperatureFromOptions(options)

	// Create Vertex LLM instance (for Gemini models with API key)
	fmt.Printf("[VERTEX VALIDATION] Creating Vertex Gemini LLM instance (temperature: %v)\n", temperature)
	config := Config{
		Provider:    ProviderVertex,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      noopLog,
		Context:     context.Background(),
		APIKeys: &ProviderAPIKeys{
			Vertex: &apiKey,
		},
	}

	llm, err := initializeVertexGemini(config, modelID, noopLog)
	if err != nil {
		fmt.Printf("[VERTEX VALIDATION ERROR] Failed to create LLM instance: %v\n", err)
		return false, fmt.Sprintf("Failed to create Vertex LLM instance: %v", err), nil
	}

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[VERTEX VALIDATION] Making test generation call to Vertex AI\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[VERTEX VALIDATION ERROR] Vertex AI test generation failed: %v\n", err)
		// Check for specific error types
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "401") {
			return false, "Invalid Vertex AI API key", nil
		}
		if strings.Contains(err.Error(), "permission") || strings.Contains(err.Error(), "forbidden") || strings.Contains(err.Error(), "403") {
			return false, "API key lacks required permissions", nil
		}
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			return false, fmt.Sprintf("Model %s not found", modelID), nil
		}
		if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "429") {
			return false, "Vertex AI API rate limit exceeded", nil
		}
		if strings.Contains(err.Error(), "timeout") {
			return false, "Vertex AI service timeout - check network connectivity", nil
		}
		return false, fmt.Sprintf("Vertex AI test generation failed: %v", err), nil
	}

	fmt.Printf("[VERTEX VALIDATION SUCCESS] Vertex AI API key is valid\n")
	return true, fmt.Sprintf("Vertex AI API key is valid for model %s", modelID), nil
}

// noopLoggerImpl is a no-op logger implementation for validation functions
type noopLoggerImpl struct{}

func (n *noopLoggerImpl) Infof(format string, v ...any)             {}
func (n *noopLoggerImpl) Errorf(format string, v ...any)            {}
func (n *noopLoggerImpl) Debugf(format string, args ...interface{}) {}

// DefaultLogger is a simple logger implementation that writes to stdout or a file
type DefaultLogger struct {
	output *os.File
	level  string
}

// NewDefaultLogger creates a new default logger instance
// If logFile is empty, logs to stdout. If logFile is provided, logs to that file.
// level can be "info" or "debug" - debug level enables Debugf output
func NewDefaultLogger(logFile string, level string) (interfaces.Logger, error) {
	var output *os.File
	var err error

	if logFile == "" {
		// Use stdout
		output = os.Stdout
	} else {
		// Create log directory if it doesn't exist
		logDir := filepath.Dir(logFile)
		if logDir != "." && logDir != "" {
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create log directory: %w", err)
			}
		}

		// Open log file
		output, err = os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
	}

	// Validate level
	if level != "info" && level != "debug" {
		level = "info" // Default to info if invalid
	}

	return &DefaultLogger{
		output: output,
		level:  level,
	}, nil
}

// Infof logs an info message
func (l *DefaultLogger) Infof(format string, v ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(l.output, "[%s] [INFO] %s\n", timestamp, fmt.Sprintf(format, v...))
}

// Errorf logs an error message
func (l *DefaultLogger) Errorf(format string, v ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(l.output, "[%s] [ERROR] %s\n", timestamp, fmt.Sprintf(format, v...))
}

// Debugf logs a debug message (only if level is "debug")
func (l *DefaultLogger) Debugf(format string, args ...interface{}) {
	if l.level == "debug" {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintf(l.output, "[%s] [DEBUG] %s\n", timestamp, fmt.Sprintf(format, args...))
	}
}

// validateBedrockCredentials validates AWS Bedrock credentials and region
func validateBedrockCredentials(modelID string, options map[string]interface{}) (bool, string, error) {
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
		fmt.Printf("[BEDROCK VALIDATION ERROR] Failed to load AWS config: %v\n", err)
		return false, "Failed to load AWS configuration", err
	}

	// Create Bedrock runtime client
	client := bedrockruntime.NewFromConfig(cfg)

	// Create a simple no-op logger for validation
	noopLog := &noopLoggerImpl{}
	// Create Bedrock adapter instance
	llm := bedrockadapter.NewBedrockAdapter(client, modelID, noopLog)

	// Create call options from map
	callOptions := createCallOptionsFromMap(options)

	// Test the LLM with a simple generation call
	fmt.Printf("[BEDROCK VALIDATION] Making test generation call to Bedrock\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = llm.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "test"}},
		},
	}, callOptions...)
	if err != nil {
		fmt.Printf("[BEDROCK VALIDATION ERROR] Bedrock test generation failed: %v\n", err)
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
		return false, fmt.Sprintf("Bedrock test generation failed: %v", err), nil
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

// getAnthropicAvailableModels returns available Anthropic models from environment variables
func getAnthropicAvailableModels() []string {
	// Get from environment variable
	modelsStr := os.Getenv("ANTHROPIC_AVAILABLE_MODELS")
	if modelsStr == "" {
		modelsStr = os.Getenv("ANTHROPIC_MODELS")
	}
	if modelsStr == "" {
		return []string{}
	}

	// Parse comma-separated models
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// getAzureAvailableModels returns available Azure models from environment variables
func getAzureAvailableModels() []string {
	// Get from environment variable
	modelsStr := os.Getenv("AZURE_AVAILABLE_MODELS")
	if modelsStr == "" {
		modelsStr = os.Getenv("AZURE_MODELS")
	}
	if modelsStr == "" {
		return []string{}
	}

	// Parse comma-separated models
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// getMiniMaxAvailableModels returns available MiniMax models from environment variables
func getMiniMaxAvailableModels() []string {
	modelsStr := os.Getenv("MINIMAX_AVAILABLE_MODELS")
	if modelsStr == "" {
		modelsStr = os.Getenv("MINIMAX_MODELS")
	}
	if modelsStr == "" {
		return []string{}
	}
	models := strings.Split(modelsStr, ",")
	for i, model := range models {
		models[i] = strings.TrimSpace(model)
	}
	return models
}

// Helper to create call options from map
// extractTemperatureFromOptions extracts temperature from options map
// Returns 0 if not found (model will use its own default)
func extractTemperatureFromOptions(options map[string]interface{}) float64 {
	if options == nil {
		return 0
	}
	if temp, ok := options["temperature"].(float64); ok {
		return temp
	}
	if temp, ok := options["temperature"].(int); ok {
		return float64(temp)
	}
	return 0
}

func createCallOptionsFromMap(options map[string]interface{}) []llmtypes.CallOption {
	var callOptions []llmtypes.CallOption
	if options == nil {
		return callOptions
	}

	// Handle temperature
	if temp, ok := options["temperature"].(float64); ok && temp >= 0 {
		callOptions = append(callOptions, llmtypes.WithTemperature(temp))
	} else if temp, ok := options["temperature"].(int); ok && temp >= 0 {
		callOptions = append(callOptions, llmtypes.WithTemperature(float64(temp)))
	}

	if effort, ok := options["reasoning_effort"].(string); ok && effort != "" {
		callOptions = append(callOptions, llmtypes.WithReasoningEffort(effort))
	}
	if verbosity, ok := options["verbosity"].(string); ok && verbosity != "" {
		callOptions = append(callOptions, llmtypes.WithVerbosity(verbosity))
	}
	if thinkingLevel, ok := options["thinking_level"].(string); ok && thinkingLevel != "" {
		callOptions = append(callOptions, llmtypes.WithThinkingLevel(thinkingLevel))
	}
	if thinkingBudget, ok := options["thinking_budget"].(float64); ok && thinkingBudget > 0 {
		callOptions = append(callOptions, llmtypes.WithThinkingBudget(int(thinkingBudget)))
	} else if thinkingBudget, ok := options["thinking_budget"].(int); ok && thinkingBudget > 0 {
		callOptions = append(callOptions, llmtypes.WithThinkingBudget(thinkingBudget))
	}
	return callOptions
}
