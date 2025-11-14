package llm

import (
	"context"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"

	llmproviders "llm-providers"
	"llm-providers/interfaces"
	llmprovidertypes "llm-providers/llmtypes"
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
	Logger utils.ExtendedLogger
	// Context for LLM initialization (optional, uses background with timeout if not provided)
	Context context.Context
}

// LoggerAdapter adapts utils.ExtendedLogger to interfaces.Logger
type LoggerAdapter struct {
	logger utils.ExtendedLogger
}

// NewLoggerAdapter creates a new logger adapter
func NewLoggerAdapter(logger utils.ExtendedLogger) *LoggerAdapter {
	return &LoggerAdapter{logger: logger}
}

// Infof implements interfaces.Logger
func (l *LoggerAdapter) Infof(format string, v ...any) {
	l.logger.Infof(format, v...)
}

// Errorf implements interfaces.Logger
func (l *LoggerAdapter) Errorf(format string, v ...any) {
	l.logger.Errorf(format, v...)
}

// Debugf implements interfaces.Logger
func (l *LoggerAdapter) Debugf(format string, args ...interface{}) {
	l.logger.Debugf(format, args...)
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
		logger = &LoggerAdapter{logger: nil}
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
	}
}

// InitializeLLM creates and initializes an LLM based on the provider configuration
// This function maintains backward compatibility by accepting agent_go Config
// and converting it to llm-providers Config internally
func InitializeLLM(config Config) (llmtypes.Model, error) {
	// Convert agent_go Config to llm-providers Config
	externalConfig := convertConfig(config)

	// Call llm-providers InitializeLLM
	llm, err := llmproviders.InitializeLLM(externalConfig)
	if err != nil {
		return nil, err
	}

	// Wrap the returned LLM to maintain backward compatibility
	// The llm-providers version already returns ProviderAwareLLM, but we need to
	// wrap it to maintain the same interface that agent_go expects
	return wrapProviderAwareLLM(llm, config.Provider, config.ModelID, config.Tracers, config.TraceID, config.Logger), nil
}

// modelAdapter adapts llm-providers Model to agent_go llmtypes.Model
type modelAdapter struct {
	model llmprovidertypes.Model
}

// GenerateContent converts types and calls the underlying model
func (m *modelAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Convert messages from agent_go types to llm-providers types
	providerMessages := make([]llmprovidertypes.MessageContent, len(messages))
	for i, msg := range messages {
		providerMessages[i] = convertMessageContent(msg)
	}

	// Convert options from agent_go types to llm-providers types
	providerOptions := make([]llmprovidertypes.CallOption, len(options))
	for i, opt := range options {
		providerOptions[i] = convertCallOption(opt)
	}

	// Call the underlying model
	resp, err := m.model.GenerateContent(ctx, providerMessages, providerOptions...)
	if err != nil {
		return nil, err
	}

	// Convert response from llm-providers types to agent_go types
	return convertContentResponse(resp), nil
}

// convertMessageContent converts agent_go MessageContent to llm-providers MessageContent
func convertMessageContent(msg llmtypes.MessageContent) llmprovidertypes.MessageContent {
	parts := make([]llmprovidertypes.ContentPart, len(msg.Parts))
	for i, part := range msg.Parts {
		parts[i] = convertContentPart(part)
	}
	return llmprovidertypes.MessageContent{
		Role:  llmprovidertypes.ChatMessageType(msg.Role),
		Parts: parts,
	}
}

// convertContentPart converts agent_go ContentPart to llm-providers ContentPart
func convertContentPart(part llmtypes.ContentPart) llmprovidertypes.ContentPart {
	switch p := part.(type) {
	case llmtypes.TextContent:
		return llmprovidertypes.TextContent{Text: p.Text}
	case llmtypes.ImageContent:
		return llmprovidertypes.ImageContent{
			SourceType: p.SourceType,
			MediaType:  p.MediaType,
			Data:       p.Data,
		}
	default:
		return part
	}
}

// convertCallOption converts agent_go CallOption to llm-providers CallOption
func convertCallOption(opt llmtypes.CallOption) llmprovidertypes.CallOption {
	return func(opts *llmprovidertypes.CallOptions) {
		// Create a temporary agent_go CallOptions to apply the option
		agentOpts := &llmtypes.CallOptions{}
		opt(agentOpts)

		// Copy the values to llm-providers CallOptions
		if agentOpts.Model != "" {
			opts.Model = agentOpts.Model
		}
		if agentOpts.Temperature != 0 {
			opts.Temperature = agentOpts.Temperature
		}
		if agentOpts.MaxTokens != 0 {
			opts.MaxTokens = agentOpts.MaxTokens
		}
		if agentOpts.JSONMode {
			opts.JSONMode = agentOpts.JSONMode
		}
		if agentOpts.Tools != nil {
			// Convert tools
			tools := make([]llmprovidertypes.Tool, len(agentOpts.Tools))
			for i, tool := range agentOpts.Tools {
				tools[i] = convertTool(tool)
			}
			opts.Tools = tools
		}
		if agentOpts.ToolChoice != nil {
			opts.ToolChoice = convertToolChoice(agentOpts.ToolChoice)
		}
		if agentOpts.StreamingFunc != nil {
			// Convert StreamingFunc to StreamChan
			ch := make(chan llmprovidertypes.StreamChunk, 100)
			opts.StreamChan = ch
			go func() {
				for chunk := range ch {
					// Convert StreamChunk to string for agent_go's StreamingFunc
					if chunk.Content != "" {
						agentOpts.StreamingFunc(chunk.Content)
					}
				}
			}()
		}
		if agentOpts.Metadata != nil {
			opts.Metadata = convertMetadata(agentOpts.Metadata)
		}
	}
}

// convertTool converts agent_go Tool to llm-providers Tool
func convertTool(tool llmtypes.Tool) llmprovidertypes.Tool {
	return llmprovidertypes.Tool{
		Type:     tool.Type,
		Function: convertFunctionDefinition(tool.Function),
	}
}

// convertFunctionDefinition converts agent_go FunctionDefinition to llm-providers FunctionDefinition
func convertFunctionDefinition(fn *llmtypes.FunctionDefinition) *llmprovidertypes.FunctionDefinition {
	if fn == nil {
		return nil
	}
	// Convert Parameters struct
	var params *llmprovidertypes.Parameters
	if fn.Parameters != nil {
		params = &llmprovidertypes.Parameters{
			Type:                 fn.Parameters.Type,
			Properties:           fn.Parameters.Properties,
			Required:             fn.Parameters.Required,
			AdditionalProperties: fn.Parameters.AdditionalProperties,
			PatternProperties:    fn.Parameters.PatternProperties,
			MinProperties:        fn.Parameters.MinProperties,
			MaxProperties:        fn.Parameters.MaxProperties,
			Additional:           fn.Parameters.Additional,
		}
	}
	return &llmprovidertypes.FunctionDefinition{
		Name:        fn.Name,
		Description: fn.Description,
		Parameters:  params,
	}
}

// convertToolChoice converts agent_go ToolChoice to llm-providers ToolChoice
func convertToolChoice(tc *llmtypes.ToolChoice) *llmprovidertypes.ToolChoice {
	if tc == nil {
		return nil
	}
	var function *llmprovidertypes.FunctionName
	if tc.Function != nil {
		function = &llmprovidertypes.FunctionName{
			Name: tc.Function.Name,
		}
	}
	return &llmprovidertypes.ToolChoice{
		Type:     tc.Type,
		Function: function,
		Any:      tc.Any,
		None:     tc.None,
	}
}

// convertMetadata converts agent_go Metadata to llm-providers Metadata
func convertMetadata(md *llmtypes.Metadata) *llmprovidertypes.Metadata {
	if md == nil {
		return nil
	}
	result := &llmprovidertypes.Metadata{}
	if md.Usage != nil {
		result.Usage = &llmprovidertypes.UsageMetadata{
			Include: md.Usage.Include,
		}
	}
	return result
}

// convertContentResponse converts llm-providers ContentResponse to agent_go ContentResponse
func convertContentResponse(resp *llmprovidertypes.ContentResponse) *llmtypes.ContentResponse {
	if resp == nil {
		return nil
	}
	choices := make([]*llmtypes.ContentChoice, len(resp.Choices))
	for i, choice := range resp.Choices {
		if choice != nil {
			choices[i] = convertContentChoice(choice)
		}
	}
	return &llmtypes.ContentResponse{
		Choices: choices,
	}
}

// convertContentChoice converts llm-providers ContentChoice to agent_go ContentChoice
func convertContentChoice(choice *llmprovidertypes.ContentChoice) *llmtypes.ContentChoice {
	result := &llmtypes.ContentChoice{
		Content:    choice.Content,
		StopReason: choice.StopReason,
	}
	if choice.ToolCalls != nil {
		result.ToolCalls = make([]llmtypes.ToolCall, len(choice.ToolCalls))
		for i, tc := range choice.ToolCalls {
			result.ToolCalls[i] = convertToolCall(tc)
		}
	}
	if choice.FuncCall != nil {
		result.FuncCall = convertFunctionCall(choice.FuncCall)
	}
	if choice.GenerationInfo != nil {
		result.GenerationInfo = convertGenerationInfo(choice.GenerationInfo)
	}
	return result
}

// convertToolCall converts llm-providers ToolCall to agent_go ToolCall
func convertToolCall(tc llmprovidertypes.ToolCall) llmtypes.ToolCall {
	result := llmtypes.ToolCall{
		ID:   tc.ID,
		Type: tc.Type,
	}
	if tc.FunctionCall != nil {
		result.FunctionCall = convertFunctionCall(tc.FunctionCall)
	}
	return result
}

// convertFunctionCall converts llm-providers FunctionCall to agent_go FunctionCall
func convertFunctionCall(fc *llmprovidertypes.FunctionCall) *llmtypes.FunctionCall {
	if fc == nil {
		return nil
	}
	return &llmtypes.FunctionCall{
		Name:      fc.Name,
		Arguments: fc.Arguments,
	}
}

// convertGenerationInfo converts llm-providers GenerationInfo to agent_go GenerationInfo
func convertGenerationInfo(gi *llmprovidertypes.GenerationInfo) *llmtypes.GenerationInfo {
	if gi == nil {
		return nil
	}
	result := &llmtypes.GenerationInfo{}
	if gi.InputTokens != nil {
		result.InputTokens = gi.InputTokens
	}
	if gi.OutputTokens != nil {
		result.OutputTokens = gi.OutputTokens
	}
	if gi.TotalTokens != nil {
		result.TotalTokens = gi.TotalTokens
	}
	if gi.CacheDiscount != nil {
		result.CacheDiscount = gi.CacheDiscount
	}
	if gi.CachedContentTokens != nil {
		result.CachedContentTokens = gi.CachedContentTokens
	}
	if gi.ReasoningTokens != nil {
		result.ReasoningTokens = gi.ReasoningTokens
	}
	if gi.Additional != nil {
		result.Additional = make(map[string]interface{})
		for k, v := range gi.Additional {
			result.Additional[k] = v
		}
	}
	return result
}

// wrapProviderAwareLLM wraps the llm-providers Model to maintain backward compatibility
func wrapProviderAwareLLM(llm llmprovidertypes.Model, provider Provider, modelID string, tracers []observability.Tracer, traceID observability.TraceID, logger utils.ExtendedLogger) *ProviderAwareLLM {
	return &ProviderAwareLLM{
		Model:    &modelAdapter{model: llm},
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
	logger   utils.ExtendedLogger
}

// NewProviderAwareLLM creates a new provider-aware LLM wrapper
// This maintains backward compatibility with existing agent_go code
func NewProviderAwareLLM(llm llmtypes.Model, provider Provider, modelID string, tracers []observability.Tracer, traceID observability.TraceID, logger utils.ExtendedLogger) *ProviderAwareLLM {
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
			p.logger.Infof("🔧 Adding OpenRouter usage parameter for cache token information")
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
