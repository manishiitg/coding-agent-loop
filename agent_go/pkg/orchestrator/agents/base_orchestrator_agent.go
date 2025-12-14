package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	loggerv2 "mcpagent/logger/v2"
	"reflect"
	"regexp"
	"time"

	mcpagent "mcpagent/agent"
	"mcpagent/events"
	"mcpagent/llm"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// NonStructuredResponseError represents a case where the agent returned a text response
// instead of structured output. This should be handled by displaying the text to the user
// and asking for further feedback.
type NonStructuredResponseError struct {
	TextResponse   string
	UpdatedHistory []llmtypes.MessageContent
	OriginalError  error
}

func (e *NonStructuredResponseError) Error() string {
	if e.OriginalError != nil {
		return e.OriginalError.Error()
	}
	return fmt.Sprintf("non-structured response received: %s", e.TextResponse)
}

// Unwrap returns the original error for error unwrapping
func (e *NonStructuredResponseError) Unwrap() error {
	return e.OriginalError
}

// IsNonStructuredResponseError checks if an error is a NonStructuredResponseError
func IsNonStructuredResponseError(err error) bool {
	var nonStructuredErr *NonStructuredResponseError
	return errors.As(err, &nonStructuredErr)
}

// OrchestratorContext holds context information for event emission
// Removed: OrchestratorContext and related context-specific fields are now handled by the context-aware bridge.

// BaseOrchestratorAgent provides common functionality for all orchestrator agents
type BaseOrchestratorAgent struct {
	config               *OrchestratorAgentConfig
	logger               loggerv2.Logger
	baseAgent            *BaseAgent // set during init
	tracer               observability.Tracer
	agentType            AgentType
	systemPrompt         string
	eventBridge          mcpagent.AgentEventListener    // Event bridge for auto events
	userMessageProcessor func(map[string]string) string // Optional processor for user messages (replaces inputProcessor)
	agentSessionID       string                         // Agent session ID for correlating orchestrator_agent_start and orchestrator_agent_end events
}

// NewBaseOrchestratorAgentWithEventBridge creates a new base orchestrator agent with event bridge
func NewBaseOrchestratorAgentWithEventBridge(
	config *OrchestratorAgentConfig,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	agentType AgentType,
	eventBridge mcpagent.AgentEventListener,
) *BaseOrchestratorAgent {
	return &BaseOrchestratorAgent{
		config:       config,
		logger:       logger,
		tracer:       tracer,
		agentType:    agentType,
		systemPrompt: "", // Not used for base orchestrator
		eventBridge:  eventBridge,
	}
}

// Initialize initializes the base orchestrator agent
func (boa *BaseOrchestratorAgent) Initialize(ctx context.Context) error {
	// Create LLM instance
	llmInstance, err := boa.createLLM()
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}

	// Create traceID
	traceID := observability.TraceID(fmt.Sprintf("%s-agent-%s-%d",
		boa.agentType,
		boa.config.Model,
		time.Now().UnixNano()))

	// Determine agent name: use unique AgentName from config if available, otherwise fall back to agent type
	agentName := string(boa.agentType)
	if boa.config.AgentName != "" {
		agentName = boa.config.AgentName
	} else {
	}

	// Create base agent
	baseAgent, err := NewBaseAgent(
		ctx,
		boa.agentType,
		agentName, // Use unique agent name if available, otherwise agent type
		llmInstance,
		boa.systemPrompt,
		boa.config.ServerNames,
		boa.config.SelectedTools,        // NEW: Pass selected tools
		boa.config.UseCodeExecutionMode, // NEW: Pass code execution mode
		boa.config.Mode,
		boa.tracer,
		traceID,
		boa.config.MCPConfigPath,
		boa.config.Model,
		boa.config.Temperature,
		boa.config.ToolChoice,
		boa.config.MaxTurns,
		boa.config.Provider,
		boa.logger,
		false,                                    // cacheOnly - not used in orchestrator agents
		boa.config.EnableLargeOutputVirtualTools, // NEW: Pass large output virtual tools setting
		boa.config.EnableContextSummarization,    // Context summarization configuration
		boa.config.SummarizeOnTokenThreshold,
		boa.config.TokenThresholdPercent,
		boa.config.SummaryKeepLastMessages,
	)
	if err != nil {
		return fmt.Errorf("failed to create base agent: %w", err)
	}

	boa.baseAgent = baseAgent

	// Append the agent-specific prompt to the existing system prompt
	boa.baseAgent.agent.AppendSystemPrompt(boa.systemPrompt)

	// Removed verbose logging
	return nil
}

// ExecuteStructuredWithInputProcessor executes the agent with structured output and proper event emission
func ExecuteStructuredWithInputProcessor[T any](boa *BaseOrchestratorAgent, ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llmtypes.MessageContent, schema string, systemPrompt string, overwriteSystemPrompt bool) (T, []llmtypes.MessageContent, error) {
	startTime := time.Now()

	// Auto-emit agent start event
	boa.emitAgentStartEvent(ctx, templateVars)

	// Use userMessageProcessor if set, otherwise use provided inputProcessor
	var userMessage string
	if boa.userMessageProcessor != nil {
		userMessage = boa.userMessageProcessor(templateVars)
	} else {
		userMessage = inputProcessor(templateVars)
	}

	// Get the base agent for structured output
	baseAgent := boa.baseAgent

	// Check if baseAgent is initialized
	if baseAgent == nil {
		var zero T
		return zero, nil, fmt.Errorf("base agent is not initialized - Initialize() must be called before executing agent %s", boa.agentType)
	}

	// Use the agent's built-in structured output capability
	// First, prepare messages with conversation history and user message
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Add user message
	userMessageContent := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
	}
	messages = append(messages, userMessageContent)

	// Set system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			baseAgent.agent.SetSystemPrompt(systemPrompt)
		} else {
			baseAgent.agent.AppendSystemPrompt(systemPrompt)
		}
	}

	// Use AskWithHistoryStructured from mcpagent
	// Note: schema parameter needs to be a zero value of type T for the schema type, and schemaString is the JSON schema string
	var schemaType T
	result, updatedHistory, err := mcpagent.AskWithHistoryStructured[T](baseAgent.agent, ctx, messages, schemaType, schema)

	duration := time.Since(startTime)

	// Auto-emit agent end event with structured response
	// Convert structured response to map for event emission
	var resultStr string
	var structuredResponse map[string]interface{}
	if err != nil {
		resultStr = "Error: " + err.Error()
	} else {
		// Marshal structured response to JSON for both Result field and StructuredResponse map
		resultBytes, marshalErr := json.Marshal(result)
		if marshalErr == nil {
			// Set Result field to the JSON string of the structured response
			resultStr = string(resultBytes)

			// Also unmarshal to map for StructuredResponse field
			var responseMap map[string]interface{}
			if unmarshalErr := json.Unmarshal(resultBytes, &responseMap); unmarshalErr == nil {
				structuredResponse = responseMap
			} else {
				boa.logger.Warn(fmt.Sprintf("⚠️ Failed to unmarshal structured response for event: %v", unmarshalErr), loggerv2.Field{Key: "error", Value: unmarshalErr})
			}
		} else {
			// Fallback to generic message if marshaling fails
			resultStr = fmt.Sprintf("Generated %s structured output (marshaling failed: %v)", boa.agentType, marshalErr)
			boa.logger.Warn(fmt.Sprintf("⚠️ Failed to marshal structured response for event: %v", marshalErr), loggerv2.Field{Key: "error", Value: marshalErr})
		}
	}
	boa.emitAgentEndEventWithStructuredResponse(ctx, templateVars, resultStr, structuredResponse, err, duration)

	if err != nil {
		var zero T
		return zero, nil, fmt.Errorf("structured execution failed: %w", err)
	}

	return result, updatedHistory, nil
}

// ExecuteStructuredWithInputProcessorViaTool executes the agent with structured output via tool calls
func ExecuteStructuredWithInputProcessorViaTool[T any](boa *BaseOrchestratorAgent, ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llmtypes.MessageContent, schema string, systemPrompt string, overwriteSystemPrompt bool, toolName string, toolDescription string) (T, []llmtypes.MessageContent, error) {
	startTime := time.Now()

	// Auto-emit agent start event
	boa.emitAgentStartEvent(ctx, templateVars)

	// Use userMessageProcessor if set, otherwise use provided inputProcessor
	var userMessage string
	if boa.userMessageProcessor != nil {
		userMessage = boa.userMessageProcessor(templateVars)
	} else {
		userMessage = inputProcessor(templateVars)
	}

	// Get the base agent for structured output
	baseAgent := boa.baseAgent

	// Check if baseAgent is initialized
	if baseAgent == nil {
		var zero T
		return zero, nil, fmt.Errorf("base agent is not initialized - Initialize() must be called before executing agent %s", boa.agentType)
	}

	// Prepare messages with conversation history and user message
	messages := make([]llmtypes.MessageContent, len(conversationHistory))
	copy(messages, conversationHistory)

	// Add user message
	userMessageContent := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
	}
	messages = append(messages, userMessageContent)

	// Set system prompt if provided
	if systemPrompt != "" {
		if overwriteSystemPrompt {
			baseAgent.agent.SetSystemPrompt(systemPrompt)
		} else {
			baseAgent.agent.AppendSystemPrompt(systemPrompt)
		}
	}

	// Use AskWithHistoryStructuredViaTool from mcpagent
	result, err := mcpagent.AskWithHistoryStructuredViaTool[T](baseAgent.agent, ctx, messages, toolName, toolDescription, schema)
	updatedHistory := result.Messages

	duration := time.Since(startTime)

	// Auto-emit agent end event with structured response
	var resultStr string
	var structuredResponse map[string]interface{} // Will be nil for conversational responses
	var finalErr error

	if err != nil {
		resultStr = "Error: " + err.Error()
		finalErr = err
		// structuredResponse remains nil for errors
	} else if !result.HasStructuredOutput {
		// Conversational response - no structured output
		// structuredResponse remains nil (explicitly)
		conversationalInput := result.TextResponse
		if conversationalInput == "" {
			conversationalInput = "LLM returned empty response (no tool call detected)"
		}
		resultStr = conversationalInput // Use conversational input directly, not wrapped

		// Log for debugging

		// Emit agent end event with conversational response before returning error
		// This ensures the frontend shows the conversational output, not the previous tool
		// Explicitly pass nil for structuredResponse to ensure it's not set
		boa.emitAgentEndEventWithStructuredResponse(ctx, templateVars, resultStr, nil, nil, duration)

		// Return a special error type that includes the text response and updated history
		// This allows callers to handle non-structured responses gracefully by displaying
		// the text to the user and asking for further feedback
		var zero T
		return zero, updatedHistory, &NonStructuredResponseError{
			TextResponse:   conversationalInput,
			UpdatedHistory: updatedHistory,
			OriginalError:  fmt.Errorf("conversational input detected - LLM response: %s", conversationalInput),
		}
	} else {
		// Structured output: marshal to JSON for result field and map for structuredResponse field
		// This applies generically to all structured responses (conditional, validation, etc.)
		resultBytes, marshalErr := json.Marshal(result.StructuredResult)
		if marshalErr == nil {
			// Set Result field to the JSON string of the structured response
			resultStr = string(resultBytes)

			// Also unmarshal to map for StructuredResponse field
			var responseMap map[string]interface{}
			if unmarshalErr := json.Unmarshal(resultBytes, &responseMap); unmarshalErr == nil {
				structuredResponse = responseMap
			} else {
				boa.logger.Warn(fmt.Sprintf("⚠️ Failed to unmarshal structured response for event: %v", unmarshalErr), loggerv2.Field{Key: "error", Value: unmarshalErr})
			}
		} else {
			// Fallback to generic message if marshaling fails
			resultStr = fmt.Sprintf("Generated %s structured output (marshaling failed: %v)", boa.agentType, marshalErr)
			boa.logger.Warn(fmt.Sprintf("⚠️ Failed to marshal structured response for event: %v", marshalErr), loggerv2.Field{Key: "error", Value: marshalErr})
		}
	}

	boa.emitAgentEndEventWithStructuredResponse(ctx, templateVars, resultStr, structuredResponse, finalErr, duration)

	if err != nil {
		var zero T
		return zero, nil, fmt.Errorf("structured execution failed: %w", err)
	}

	// NonStructuredResponseError is already handled above (line 273), so we can proceed to return the result
	return result.StructuredResult, updatedHistory, nil
}

// ExecuteWithInputProcessor executes the agent with a custom input processor
// This is a convenience method that delegates to ExecuteWithTemplateValidation with nil templateData
func (boa *BaseOrchestratorAgent) ExecuteWithInputProcessor(ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Delegate to ExecuteWithTemplateValidation with nil templateData to skip validation
	return boa.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, "", false)
}

// ExecuteWithTemplateValidation executes the agent with template validation
func (boa *BaseOrchestratorAgent) ExecuteWithTemplateValidation(ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llmtypes.MessageContent, templateData interface{}, systemPrompt string, overwriteSystemPrompt bool) (string, []llmtypes.MessageContent, error) {
	startTime := time.Now()

	// Auto-emit agent start event
	boa.emitAgentStartEvent(ctx, templateVars)

	// Use userMessageProcessor if set, otherwise use provided inputProcessor
	var userMessage string
	if boa.userMessageProcessor != nil {
		userMessage = boa.userMessageProcessor(templateVars)
	} else {
		userMessage = inputProcessor(templateVars)
	}

	// Validate template fields at compile time (skip validation if templateData is nil)
	if templateData != nil {
		if err := boa.validateTemplateFields(userMessage, templateData); err != nil {
			boa.logger.Error(fmt.Sprintf("❌ Template validation failed for agent %s: %v", boa.agentType, err), err)
			return "", nil, fmt.Errorf("template validation failed: %w", err)
		}
	}

	// Delegate to template's Execute method which enforces event patterns
	result, updatedConversationHistory, err := boa.baseAgent.Execute(ctx, userMessage, conversationHistory, systemPrompt, overwriteSystemPrompt)

	duration := time.Since(startTime)

	// Auto-emit agent end event
	boa.emitAgentEndEvent(ctx, templateVars, result, err, duration)

	if err != nil {
		boa.logger.Error(fmt.Sprintf("❌ Base Orchestrator Agent (%s) execution failed: %v", boa.agentType, err), err)
		return "", nil, fmt.Errorf("base orchestrator execution failed: %w", err)
	}

	// Orchestrator agent execution completed
	return result, updatedConversationHistory, nil
}

// GetType returns the agent type
func (boa *BaseOrchestratorAgent) GetType() string {
	return string(boa.agentType)
}

// GetConfig returns the agent configuration
func (boa *BaseOrchestratorAgent) GetConfig() *OrchestratorAgentConfig {
	return boa.config
}

// Close closes the base orchestrator agent
func (boa *BaseOrchestratorAgent) Close() error {
	if boa.baseAgent != nil && boa.baseAgent.agent != nil {
		boa.baseAgent.agent.Close()
	}
	return nil
}

// BaseAgent returns the base agent
func (boa *BaseOrchestratorAgent) BaseAgent() *BaseAgent {
	return boa.baseAgent
}

// GetBaseAgent returns the base agent (implements OrchestratorAgent interface)
func (boa *BaseOrchestratorAgent) GetBaseAgent() *BaseAgent {
	return boa.baseAgent
}

// SetEventBridge sets the event bridge for the agent
func (boa *BaseOrchestratorAgent) SetEventBridge(bridge mcpagent.AgentEventListener) {
	boa.eventBridge = bridge
}

// GetTracer returns the tracer
func (boa *BaseOrchestratorAgent) GetTracer() observability.Tracer {
	return boa.tracer
}

// GetEventBridge returns the event bridge
func (boa *BaseOrchestratorAgent) GetEventBridge() mcpagent.AgentEventListener {
	return boa.eventBridge
}

// SetUserMessageProcessor sets the user message processor function
func (boa *BaseOrchestratorAgent) SetUserMessageProcessor(processor func(map[string]string) string) {
	boa.userMessageProcessor = processor
}

// GetUserMessageProcessor returns the user message processor if set, otherwise returns nil
func (boa *BaseOrchestratorAgent) GetUserMessageProcessor() func(map[string]string) string {
	return boa.userMessageProcessor
}

// UserMessageProcessorSetter is an interface for setting user message processor
type UserMessageProcessorSetter interface {
	SetUserMessageProcessor(func(map[string]string) string)
}

// emitEvent emits an event through the event bridge
func (boa *BaseOrchestratorAgent) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	// Check if event bridge is available
	if boa.eventBridge == nil {
		boa.logger.Debug(fmt.Sprintf("⚠️ Event bridge is nil, skipping event emission: %s", eventType))
		return
	}

	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge
	if err := boa.eventBridge.HandleEvent(ctx, agentEvent); err != nil {
		boa.logger.Warn(fmt.Sprintf("⚠️ Failed to emit event %s: %v", eventType, err), loggerv2.Field{Key: "error", Value: err})
	} else {
		boa.logger.Debug(fmt.Sprintf("✅ Successfully emitted event %s", eventType))
	}
}

// emitAgentStartEvent emits an agent start event automatically
func (boa *BaseOrchestratorAgent) emitAgentStartEvent(ctx context.Context, templateVars map[string]string) {
	// Removed verbose logging

	// Generate unique agent session ID for correlating start/end events
	boa.agentSessionID = events.GenerateEventID()

	agentName := string(boa.agentType)
	if boa.baseAgent != nil {
		agentName = boa.baseAgent.name
	}

	eventData := &events.OrchestratorAgentStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp:     time.Now(),
			CorrelationID: boa.agentSessionID, // Use shared session ID for correlation
		},
		AgentType:    string(boa.agentType),
		AgentName:    agentName,
		InputData:    templateVars,
		ModelID:      boa.config.Model,
		Provider:     boa.config.Provider,
		ServersCount: len(boa.config.ServerNames),
		MaxTurns:     boa.config.MaxTurns,
	}

	boa.emitEvent(ctx, events.OrchestratorAgentStart, eventData)
}

// emitAgentEndEvent emits an agent end event automatically
func (boa *BaseOrchestratorAgent) emitAgentEndEvent(ctx context.Context, templateVars map[string]string, result string, err error, duration time.Duration) {
	boa.emitAgentEndEventWithStructuredResponse(ctx, templateVars, result, nil, err, duration)
}

// emitAgentEndEventWithStructuredResponse emits an agent end event with optional structured response
func (boa *BaseOrchestratorAgent) emitAgentEndEventWithStructuredResponse(ctx context.Context, templateVars map[string]string, result string, structuredResponse map[string]interface{}, err error, duration time.Duration) {
	agentName := string(boa.agentType)
	if boa.baseAgent != nil {
		agentName = boa.baseAgent.name
	}

	// Get token usage from agent if available
	var promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int
	if boa.baseAgent != nil && boa.baseAgent.agent != nil {
		promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount = boa.baseAgent.agent.GetTokenUsage()
	}

	eventData := &events.OrchestratorAgentEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp:     time.Now(),
			CorrelationID: boa.agentSessionID, // Use shared session ID for correlation
		},
		AgentType:          string(boa.agentType),
		AgentName:          agentName,
		InputData:          templateVars,
		Result:             result,
		StructuredResponse: structuredResponse, // This will be nil for conversational responses
		Success:            err == nil,
		Error: func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		Duration:              duration,
		ModelID:               boa.config.Model,
		Provider:              boa.config.Provider,
		ServersCount:          len(boa.config.ServerNames),
		MaxTurns:              boa.config.MaxTurns,
		PromptTokens:          promptTokens,
		CompletionTokens:      completionTokens,
		TotalTokens:           totalTokens,
		CacheTokens:           cacheTokens,
		ReasoningTokens:       reasoningTokens,
		LLMCallCount:          llmCallCount,
		CacheEnabledCallCount: cacheEnabledCallCount,
	}

	boa.emitEvent(ctx, events.OrchestratorAgentEnd, eventData)
}

// getMapKeys returns the keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// createLLM creates an LLM instance based on the agent configuration
func (boa *BaseOrchestratorAgent) createLLM() (llmtypes.Model, error) {
	// Generate trace ID for this agent session
	traceID := observability.TraceID(fmt.Sprintf("%s-agent-%d", boa.agentType, time.Now().UnixNano()))

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from frontend if provided
	if len(boa.config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, boa.config.FallbackModels...)
		// Using custom fallback models from frontend
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(boa.config.Provider))...)
		// Using default fallback models for provider
	}

	// Add cross-provider fallback models if configured
	if boa.config.CrossProviderFallback != nil && len(boa.config.CrossProviderFallback.Models) > 0 {
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.config.CrossProviderFallback.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added cross-provider fallback models
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added default cross-provider fallback models
	}

	// Convert API keys from agent config to LLM config format
	var llmAPIKeys *llm.ProviderAPIKeys
	if boa.config.APIKeys != nil {
		llmAPIKeys = &llm.ProviderAPIKeys{
			OpenRouter: boa.config.APIKeys.OpenRouter,
			OpenAI:     boa.config.APIKeys.OpenAI,
			Anthropic:  boa.config.APIKeys.Anthropic,
			Vertex:     boa.config.APIKeys.Vertex,
		}
		if boa.config.APIKeys.Bedrock != nil {
			llmAPIKeys.Bedrock = &llm.BedrockConfig{
				Region: boa.config.APIKeys.Bedrock.Region,
			}
		}
	}

	// Convert loggerv2.Logger to loggerv2.Logger for llm.Config
	var v2Logger loggerv2.Logger
	if boa.logger != nil {
		v2Logger = boa.logger
	} else {
		v2Logger = loggerv2.NewDefault()
	}

	// Create LLM configuration
	config := llm.Config{
		Provider:       llm.Provider(boa.config.Provider),
		ModelID:        boa.config.Model,
		Temperature:    boa.config.Temperature,
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     boa.config.MaxRetries,
		Logger:         v2Logger,
		APIKeys:        llmAPIKeys,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	return llmInstance, nil
}

// validateTemplateFields validates that all template field references exist in the struct
func (boa *BaseOrchestratorAgent) validateTemplateFields(templateStr string, templateData interface{}) error {
	// Extract all template field references using regex
	re := regexp.MustCompile(`\{\{\.([A-Za-z][A-Za-z0-9_]*)\}\}`)
	matches := re.FindAllStringSubmatch(templateStr, -1)

	// Get struct field names using reflection
	structFields := boa.getStructFieldNames(templateData)

	// Check if all template references exist in struct
	for _, match := range matches {
		fieldName := match[1]
		if !boa.contains(structFields, fieldName) {
			return fmt.Errorf("template references non-existent field: %s", fieldName)
		}
	}

	return nil
}

// getStructFieldNames extracts field names from a struct using reflection
func (boa *BaseOrchestratorAgent) getStructFieldNames(v interface{}) []string {
	if v == nil {
		return []string{}
	}

	val := reflect.ValueOf(v)
	typ := reflect.TypeOf(v)

	// Handle pointers
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return []string{}
		}
		val = val.Elem()
		typ = typ.Elem()
	}

	// Only handle structs
	if val.Kind() != reflect.Struct {
		return []string{}
	}

	var fieldNames []string
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		// Only include exported fields (uppercase)
		if field.PkgPath == "" {
			fieldNames = append(fieldNames, field.Name)
		}
	}

	return fieldNames
}

// contains checks if a slice contains a string
func (boa *BaseOrchestratorAgent) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
