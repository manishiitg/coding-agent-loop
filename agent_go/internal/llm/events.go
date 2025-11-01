package llm

import (
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
)

// LLM Event Types - Constants for event type names
const (
	EventTypeLLMInitializationStart   = "llm_initialization_start"
	EventTypeLLMInitializationSuccess = "llm_initialization_success"
	EventTypeLLMInitializationError   = "llm_initialization_error"
	EventTypeLLMGenerationStart       = "llm_generation_start"
	EventTypeLLMGenerationSuccess     = "llm_generation_success"
	EventTypeLLMGenerationError       = "llm_generation_error"
)

// LLM Operation Types - Constants for operation names
const (
	OperationLLMInitialization = "llm_initialization"
	OperationLLMGeneration     = "llm_generation"
	OperationLLMToolCalling    = "llm_tool_calling"
)

// LLM Status Types - Constants for status values
const (
	StatusLLMInitialized = "initialized"
	StatusLLMFailed      = "failed"
	StatusLLMSuccess     = "success"
	StatusLLMInProgress  = "in_progress"
)

// LLM Capabilities - Constants for capability strings
const (
	CapabilityTextGeneration = "text_generation"
	CapabilityToolCalling    = "tool_calling"
	CapabilityStreaming      = "streaming"
)

// TokenUsage represents token consumption information
type TokenUsage struct {
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	TotalTokens  int    `json:"total_tokens,omitempty"`
	Unit         string `json:"unit,omitempty"`
	Cost         string `json:"cost,omitempty"`
}

// LLMMetadata represents common metadata for LLM events
type LLMMetadata struct {
	ModelVersion     string            `json:"model_version,omitempty"`
	MaxTokens        int               `json:"max_tokens,omitempty"`
	TopP             float64           `json:"top_p,omitempty"`
	FrequencyPenalty float64           `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64           `json:"presence_penalty,omitempty"`
	StopSequences    []string          `json:"stop_sequences,omitempty"`
	User             string            `json:"user,omitempty"`
	CustomFields     map[string]string `json:"custom_fields,omitempty"`
}

// LLMInitializationStartEvent represents the start of LLM initialization
type LLMInitializationStartEvent struct {
	ModelID     string      `json:"model_id"`
	Temperature float64     `json:"temperature"`
	Provider    string      `json:"provider"`
	Operation   string      `json:"operation"`
	Timestamp   time.Time   `json:"timestamp"`
	TraceID     string      `json:"trace_id"`
	Metadata    LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMInitializationStartEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMInitializationStartEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMInitializationStartEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMInitializationStartEvent) GetTraceID() string { return e.TraceID }

// LLMInitializationSuccessEvent represents successful LLM initialization
type LLMInitializationSuccessEvent struct {
	ModelID      string      `json:"model_id"`
	Provider     string      `json:"provider"`
	Status       string      `json:"status"`
	Capabilities []string    `json:"capabilities"`
	Timestamp    time.Time   `json:"timestamp"`
	TraceID      string      `json:"trace_id"`
	Metadata     LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMInitializationSuccessEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMInitializationSuccessEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMInitializationSuccessEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMInitializationSuccessEvent) GetTraceID() string { return e.TraceID }

// LLMInitializationErrorEvent represents failed LLM initialization
type LLMInitializationErrorEvent struct {
	ModelID   string      `json:"model_id"`
	Provider  string      `json:"provider"`
	Operation string      `json:"operation"`
	Error     string      `json:"error"`
	ErrorType string      `json:"error_type"`
	Status    string      `json:"status"`
	Timestamp time.Time   `json:"timestamp"`
	TraceID   string      `json:"trace_id"`
	Metadata  LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMInitializationErrorEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMInitializationErrorEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMInitializationErrorEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMInitializationErrorEvent) GetTraceID() string { return e.TraceID }

// LLMGenerationStartEvent represents the start of LLM generation
type LLMGenerationStartEvent struct {
	ModelID        string      `json:"model_id"`
	Provider       string      `json:"provider"`
	Operation      string      `json:"operation"`
	Messages       int         `json:"messages"`
	Temperature    float64     `json:"temperature"`
	MessageContent string      `json:"message_content"`
	Timestamp      time.Time   `json:"timestamp"`
	TraceID        string      `json:"trace_id"`
	Metadata       LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMGenerationStartEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMGenerationStartEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMGenerationStartEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMGenerationStartEvent) GetTraceID() string { return e.TraceID }

// LLMGenerationSuccessEvent represents successful LLM generation
type LLMGenerationSuccessEvent struct {
	ModelID        string      `json:"model_id"`
	Provider       string      `json:"provider"`
	Operation      string      `json:"operation"`
	Messages       int         `json:"messages"`
	Temperature    float64     `json:"temperature"`
	MessageContent string      `json:"message_content"`
	ResponseLength int         `json:"response_length"`
	ChoicesCount   int         `json:"choices_count"`
	TokenUsage     TokenUsage  `json:"token_usage,omitempty"`
	Timestamp      time.Time   `json:"timestamp"`
	TraceID        string      `json:"trace_id"`
	Metadata       LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMGenerationSuccessEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMGenerationSuccessEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMGenerationSuccessEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMGenerationSuccessEvent) GetTraceID() string { return e.TraceID }

// LLMGenerationErrorEvent represents failed LLM generation
type LLMGenerationErrorEvent struct {
	ModelID        string      `json:"model_id"`
	Provider       string      `json:"provider"`
	Operation      string      `json:"operation"`
	Messages       int         `json:"messages"`
	Temperature    float64     `json:"temperature"`
	MessageContent string      `json:"message_content"`
	Error          string      `json:"error"`
	ErrorType      string      `json:"error_type"`
	Timestamp      time.Time   `json:"timestamp"`
	TraceID        string      `json:"trace_id"`
	Metadata       LLMMetadata `json:"metadata,omitempty"`
}

// GetModelID returns the model ID
func (e *LLMGenerationErrorEvent) GetModelID() string { return e.ModelID }

// GetProvider returns the provider name
func (e *LLMGenerationErrorEvent) GetProvider() string { return e.Provider }

// GetTimestamp returns the event timestamp
func (e *LLMGenerationErrorEvent) GetTimestamp() time.Time { return e.Timestamp }

// GetTraceID returns the trace ID
func (e *LLMGenerationErrorEvent) GetTraceID() string { return e.TraceID }

// emitLLMInitializationStart emits a typed start event for LLM initialization
func emitLLMInitializationStart(tracers []observability.Tracer, provider string, modelID string, temperature float64, traceID observability.TraceID, metadata LLMMetadata) {
	if len(tracers) == 0 {
		return
	}

	event := &LLMInitializationStartEvent{
		ModelID:     modelID,
		Temperature: temperature,
		Provider:    provider,
		Operation:   OperationLLMInitialization,
		Timestamp:   time.Now(),
		TraceID:     string(traceID),
		Metadata:    metadata,
	}

	for _, tracer := range tracers {
		if err := tracer.EmitLLMEvent(event); err != nil {
			// Log error but continue with other tracers
			continue
		}
	}
}

// emitLLMInitializationSuccess emits a typed success event for LLM initialization
func emitLLMInitializationSuccess(tracers []observability.Tracer, provider string, modelID string, capabilities string, traceID observability.TraceID, metadata LLMMetadata) {
	if len(tracers) == 0 {
		return
	}

	// Convert capabilities string to slice
	capabilitiesSlice := strings.Split(capabilities, ",")
	for i, cap := range capabilitiesSlice {
		capabilitiesSlice[i] = strings.TrimSpace(cap)
	}

	event := &LLMInitializationSuccessEvent{
		ModelID:      modelID,
		Provider:     provider,
		Status:       StatusLLMInitialized,
		Capabilities: capabilitiesSlice,
		Timestamp:    time.Now(),
		TraceID:      string(traceID),
		Metadata:     metadata,
	}

	for _, tracer := range tracers {
		if err := tracer.EmitLLMEvent(event); err != nil {
			// Log error but continue with other tracers
			continue
		}
	}
}

// emitLLMInitializationError emits a typed error event for LLM initialization
func emitLLMInitializationError(tracers []observability.Tracer, provider string, modelID string, operation string, err error, traceID observability.TraceID, metadata LLMMetadata) {
	if len(tracers) == 0 {
		return
	}

	event := &LLMInitializationErrorEvent{
		ModelID:   modelID,
		Provider:  provider,
		Operation: operation,
		Error:     err.Error(),
		ErrorType: fmt.Sprintf("%T", err),
		Status:    StatusLLMFailed,
		Timestamp: time.Now(),
		TraceID:   string(traceID),
		Metadata:  metadata,
	}

	for _, tracer := range tracers {
		if err := tracer.EmitLLMEvent(event); err != nil {
			// Log error but continue with other tracers
			continue
		}
	}
}

// emitLLMGenerationSuccess emits a typed success event for LLM generation
func emitLLMGenerationSuccess(tracers []observability.Tracer, provider string, modelID string, operation string, messages int, temperature float64, messageContent string, responseLength int, choicesCount int, traceID observability.TraceID, metadata LLMMetadata) {
	if len(tracers) == 0 {
		return
	}

	// Extract token usage from metadata if available
	var tokenUsage TokenUsage
	if metadata.CustomFields != nil {
		if _, ok := metadata.CustomFields["generation_info"]; ok {
			// For now, we'll create a simple TokenUsage structure
			// In the future, we could enhance this to parse more complex token usage data
			tokenUsage = TokenUsage{
				Unit: "TOKENS",
			}
		}
	}

	event := &LLMGenerationSuccessEvent{
		ModelID:        modelID,
		Provider:       provider,
		Operation:      operation,
		Messages:       messages,
		Temperature:    temperature,
		MessageContent: messageContent,
		ResponseLength: responseLength,
		ChoicesCount:   choicesCount,
		TokenUsage:     tokenUsage,
		Timestamp:      time.Now(),
		TraceID:        string(traceID),
		Metadata:       metadata,
	}

	for _, tracer := range tracers {
		if err := tracer.EmitLLMEvent(event); err != nil {
			// Log error but continue with other tracers
			continue
		}
	}
}

// emitLLMGenerationError emits a typed error event for LLM generation
func emitLLMGenerationError(tracers []observability.Tracer, provider string, modelID string, operation string, messages int, temperature float64, messageContent string, err error, traceID observability.TraceID, metadata LLMMetadata) {
	if len(tracers) == 0 {
		return
	}

	event := &LLMGenerationErrorEvent{
		ModelID:        modelID,
		Provider:       provider,
		Operation:      operation,
		Messages:       messages,
		Temperature:    temperature,
		MessageContent: messageContent,
		Error:          err.Error(),
		ErrorType:      fmt.Sprintf("%T", err),
		Timestamp:      time.Now(),
		TraceID:        string(traceID),
		Metadata:       metadata,
	}

	for _, tracer := range tracers {
		if err := tracer.EmitLLMEvent(event); err != nil {
			// Log error but continue with other tracers
			continue
		}
	}
}

// extractTokenUsageFromGenerationInfo extracts token usage from GenerationInfo
func extractTokenUsageFromGenerationInfo(generationInfo *llmtypes.GenerationInfo) observability.UsageMetrics {
	usage := observability.UsageMetrics{Unit: "TOKENS"}

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

// ExtractTokenUsageWithCacheInfo extracts token usage with OpenRouter cache information
func ExtractTokenUsageWithCacheInfo(generationInfo *llmtypes.GenerationInfo) (observability.UsageMetrics, float64, int, map[string]interface{}) {
	usage := extractTokenUsageFromGenerationInfo(generationInfo)

	var cacheDiscount float64
	var reasoningTokens int

	if generationInfo != nil {
		// Extract OpenRouter cache discount
		if generationInfo.CacheDiscount != nil {
			cacheDiscount = *generationInfo.CacheDiscount
		}

		// Extract reasoning tokens (for models like o3)
		if generationInfo.ReasoningTokens != nil {
			reasoningTokens = *generationInfo.ReasoningTokens
		}
	}

	// Create a copy of generationInfo for logging (convert to map for backward compatibility)
	infoCopy := make(map[string]interface{})
	if generationInfo != nil {
		// Convert typed fields to map for backward compatibility
		if generationInfo.InputTokens != nil {
			infoCopy["input_tokens"] = *generationInfo.InputTokens
		}
		if generationInfo.OutputTokens != nil {
			infoCopy["output_tokens"] = *generationInfo.OutputTokens
		}
		if generationInfo.TotalTokens != nil {
			infoCopy["total_tokens"] = *generationInfo.TotalTokens
		}
		if generationInfo.CacheDiscount != nil {
			infoCopy["cache_discount"] = *generationInfo.CacheDiscount
		}
		if generationInfo.ReasoningTokens != nil {
			infoCopy["ReasoningTokens"] = *generationInfo.ReasoningTokens
		}
		// Add any additional fields from the Additional map
		for k, v := range generationInfo.Additional {
			infoCopy[k] = v
		}
	}

	return usage, cacheDiscount, reasoningTokens, infoCopy
}
