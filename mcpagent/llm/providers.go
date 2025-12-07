package llm

import (
	"context"

	"fmt"

	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/interfaces"
)

// Re-export Provider type and constants from llm-providers
type Provider = llmproviders.Provider

const (
	ProviderBedrock    = llmproviders.ProviderBedrock
	ProviderOpenAI     = llmproviders.ProviderOpenAI
	ProviderAnthropic  = llmproviders.ProviderAnthropic
	ProviderOpenRouter = llmproviders.ProviderOpenRouter
	ProviderVertex     = llmproviders.ProviderVertex
)

// Config holds configuration for LLM initialization (agent_go version)
// This is kept for backward compatibility and converted to llm-providers Config internally
type Config struct {
	Provider    Provider
	ModelID     string
	Temperature float64
	Tracers     []observability.Tracer
	TraceID     observability.TraceID
	// Fallback configuration for rate limiting
	FallbackModels []string
	MaxRetries     int
	// Logger for structured logging
	Logger loggerv2.Logger
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
	Bedrock    *BedrockConfig
}

// BedrockConfig holds Bedrock-specific configuration
type BedrockConfig struct {
	Region string
}

// LoggerAdapter adapts v2.Logger to interfaces.Logger
type LoggerAdapter struct {
	logger loggerv2.Logger
}

// NewLoggerAdapter creates a new logger adapter
func NewLoggerAdapter(logger loggerv2.Logger) *LoggerAdapter {
	return &LoggerAdapter{logger: logger}
}

// Infof implements interfaces.Logger
func (l *LoggerAdapter) Infof(format string, v ...any) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger.Info(fmt.Sprintf(format, v...))
}

// Errorf implements interfaces.Logger
func (l *LoggerAdapter) Errorf(format string, v ...any) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger.Error(fmt.Sprintf(format, v...), nil)
}

// Debugf implements interfaces.Logger
func (l *LoggerAdapter) Debugf(format string, args ...interface{}) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger.Debug(fmt.Sprintf(format, args...))
}

// convertConfig converts agent_go Config to llm-providers Config
func convertConfig(config Config) llmproviders.Config {
	// Create EventEmitterAdapter from tracers
	var eventEmitter interfaces.EventEmitter
	if len(config.Tracers) > 0 {
		eventEmitter = NewEventEmitterAdapter(config.Tracers)
	} else {
		// Create a no-op event emitter if no tracers
		eventEmitter = NewEventEmitterAdapter(nil)
	}

	// Create LoggerAdapter from ExtendedLogger
	var logger interfaces.Logger
	if config.Logger != nil {
		logger = NewLoggerAdapter(config.Logger)
	} else {
		// Create a no-op logger if none provided
		logger = NewLoggerAdapter(nil)
	}

	// Convert API keys if provided
	var providerAPIKeys *llmproviders.ProviderAPIKeys
	if config.APIKeys != nil {
		providerAPIKeys = &llmproviders.ProviderAPIKeys{
			OpenRouter: config.APIKeys.OpenRouter,
			OpenAI:     config.APIKeys.OpenAI,
			Anthropic:  config.APIKeys.Anthropic,
			Vertex:     config.APIKeys.Vertex,
		}
		if config.APIKeys.Bedrock != nil {
			providerAPIKeys.Bedrock = &llmproviders.BedrockConfig{
				Region: config.APIKeys.Bedrock.Region,
			}
		}
	}

	return llmproviders.Config{
		Provider:       llmproviders.Provider(config.Provider),
		ModelID:        config.ModelID,
		Temperature:    config.Temperature,
		EventEmitter:   eventEmitter,
		TraceID:        interfaces.TraceID(config.TraceID),
		FallbackModels: config.FallbackModels,
		MaxRetries:     config.MaxRetries,
		Logger:         logger,
		Context:        config.Context,
		APIKeys:        providerAPIKeys,
	}
}

// InitializeLLM creates and initializes an LLM based on the provider configuration
// This function maintains backward compatibility by accepting agent_go Config
// and converting it to llm-providers Config internally
func InitializeLLM(config Config) (llmtypes.Model, error) {
	// Convert agent_go Config to llm-providers Config
	externalConfig := convertConfig(config)

	// Call llm-providers InitializeLLM (already returns llmtypes.Model)
	llm, err := llmproviders.InitializeLLM(externalConfig)
	if err != nil {
		return nil, err
	}

	// Wrap the returned LLM to maintain backward compatibility with agent_go-specific fields
	return wrapProviderAwareLLM(llm, config.Provider, config.ModelID, config.Tracers, config.TraceID, config.Logger), nil
}

// wrapProviderAwareLLM wraps the llm-providers Model to maintain backward compatibility
// Since both packages now use the same llmtypes, no conversion is needed
func wrapProviderAwareLLM(llm llmtypes.Model, provider Provider, modelID string, tracers []observability.Tracer, traceID observability.TraceID, logger loggerv2.Logger) *ProviderAwareLLM {
	return &ProviderAwareLLM{
		Model:    llm,
		provider: provider,
		modelID:  modelID,
		tracers:  tracers,
		traceID:  traceID,
		logger:   logger,
	}
}

// ProviderAwareLLM is a wrapper around LLM that preserves provider information
// This maintains backward compatibility with agent_go code
type ProviderAwareLLM struct {
	llmtypes.Model
	provider Provider
	modelID  string
	tracers  []observability.Tracer
	traceID  observability.TraceID
	logger   loggerv2.Logger
}

// NewProviderAwareLLM creates a new provider-aware LLM wrapper
// This maintains backward compatibility with existing agent_go code
func NewProviderAwareLLM(llm llmtypes.Model, provider Provider, modelID string, tracers []observability.Tracer, traceID observability.TraceID, logger loggerv2.Logger) *ProviderAwareLLM {
	return &ProviderAwareLLM{
		Model:    llm,
		provider: provider,
		modelID:  modelID,
		tracers:  tracers,
		traceID:  traceID,
		logger:   logger,
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

// GenerateContent wraps the underlying LLM's GenerateContent method
// This maintains backward compatibility and adds OpenRouter usage parameter logic
func (p *ProviderAwareLLM) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Automatically add usage parameter for OpenRouter requests to get cache token information
	if p.provider == ProviderOpenRouter {
		if p.logger != nil {
			p.logger.Info("Adding OpenRouter usage parameter for cache token information")
		}
		options = append(options, WithOpenRouterUsage())
	}

	// Call the underlying LLM (which is already a ProviderAwareLLM from llm-providers)
	return p.Model.GenerateContent(ctx, messages, options...)
}

// WithOpenRouterUsage enables usage parameter for OpenRouter requests to get cache token information
func WithOpenRouterUsage() CallOption {
	return func(opts *CallOptions) {
		// Set the usage parameter in the request metadata
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

// Re-export helper functions from llm-providers

// GetDefaultModel returns the default model for each provider from environment variables
func GetDefaultModel(provider Provider) string {
	return llmproviders.GetDefaultModel(llmproviders.Provider(provider))
}

// GetDefaultFallbackModels returns fallback models for each provider from environment variables
func GetDefaultFallbackModels(provider Provider) []string {
	return llmproviders.GetDefaultFallbackModels(llmproviders.Provider(provider))
}

// GetCrossProviderFallbackModels returns cross-provider fallback models (e.g., OpenAI for Bedrock)
func GetCrossProviderFallbackModels(provider Provider) []string {
	return llmproviders.GetCrossProviderFallbackModels(llmproviders.Provider(provider))
}

// ValidateProvider checks if the provider is supported
func ValidateProvider(provider string) (Provider, error) {
	p, err := llmproviders.ValidateProvider(provider)
	return Provider(p), err
}

// Re-export response types from llm-providers
type LLMDefaultsResponse = llmproviders.LLMDefaultsResponse
type APIKeyValidationRequest = llmproviders.APIKeyValidationRequest
type APIKeyValidationResponse = llmproviders.APIKeyValidationResponse

// GetLLMDefaults returns default LLM configurations from environment variables
func GetLLMDefaults() LLMDefaultsResponse {
	return llmproviders.GetLLMDefaults()
}

// ValidateAPIKey validates API keys for OpenRouter, OpenAI, Bedrock, and Vertex
func ValidateAPIKey(req APIKeyValidationRequest) APIKeyValidationResponse {
	return llmproviders.ValidateAPIKey(req)
}

// IsO3O4Model detects o3/o4 models (OpenAI) for conditional logic in agent
func IsO3O4Model(modelID string) bool {
	return llmproviders.IsO3O4Model(modelID)
}
