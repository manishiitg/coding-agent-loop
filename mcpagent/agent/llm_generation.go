package mcpagent

import (
	"context"
	"fmt"
	"mcpagent/events"
	"mcpagent/llm"
	"mcpagent/observability"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ═══════════════════════════════════════════════════════════════════
// UNIFIED FALLBACK HELPERS
// ═══════════════════════════════════════════════════════════════════

// getFallbackModelsInPriority returns fallback models sorted by priority (ascending)
func (a *Agent) getFallbackModelsInPriority() []FallbackModel {
	if len(a.FallbackModels) == 0 {
		return nil
	}
	// Create a copy to avoid modifying the original
	sorted := make([]FallbackModel, len(a.FallbackModels))
	copy(sorted, a.FallbackModels)
	// Sort by priority (lower number = higher priority)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// convertToEventFallbackModels converts agent FallbackModels to event FallbackModelInfo
func convertToEventFallbackModels(models []FallbackModel) []events.FallbackModelInfo {
	result := make([]events.FallbackModelInfo, len(models))
	for i, m := range models {
		result[i] = events.FallbackModelInfo{
			ModelID:  m.ModelID,
			Provider: m.Provider,
			Priority: m.Priority,
		}
	}
	return result
}

// tryUnifiedFallback attempts all fallback models in priority order
// Returns: response, error, usage metrics
func (a *Agent) tryUnifiedFallback(ctx context.Context, errorType string, turn int, fallbackModels []FallbackModel, sendMessage func(string), messages []llmtypes.MessageContent, opts []llmtypes.CallOption) (*llmtypes.ContentResponse, error, observability.UsageMetrics) {
	logger := getLogger(a)
	fallbackStartTime := time.Now()

	if len(fallbackModels) == 0 {
		return nil, fmt.Errorf("no fallback models configured"), observability.UsageMetrics{}
	}

	sendMessage(fmt.Sprintf("\n🔄 Trying %d fallback models in priority order...", len(fallbackModels)))

	for i, fb := range fallbackModels {
		// Skip if this is the same as the current model
		if fb.ModelID == a.ModelID && fb.Provider == string(a.provider) {
			logger.Infof("Skipping fallback model %s (same as current)", fb.ModelID)
			continue
		}

		attemptStartTime := time.Now()
		sendMessage(fmt.Sprintf("\n🔄 Trying fallback %d/%d: %s (%s)", i+1, len(fallbackModels), fb.ModelID, fb.Provider))

		// Emit fallback attempt event
		fallbackAttemptEvent := &events.FallbackAttemptEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:          turn + 1,
			AttemptIndex:  i + 1,
			TotalAttempts: len(fallbackModels),
			ModelID:       fb.ModelID,
			Provider:      fb.Provider,
			Phase:         "unified", // New unified phase
			Success:       false,
			Duration:      "",
		}
		a.EmitTypedEvent(ctx, fallbackAttemptEvent)

		// Create fallback LLM using explicit provider from FallbackModel
		origModelID := a.ModelID
		origProvider := a.provider
		a.ModelID = fb.ModelID

		fallbackLLM, ferr := a.createFallbackLLMFromModel(ctx, fb)
		if ferr != nil {
			a.ModelID = origModelID
			// Emit fallback initialization failure event
			initFailureEvent := &events.FallbackAttemptEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Turn:          turn + 1,
				AttemptIndex:  i + 1,
				TotalAttempts: len(fallbackModels),
				ModelID:       fb.ModelID,
				Provider:      fb.Provider,
				Phase:         "unified",
				Success:       false,
				Duration:      time.Since(attemptStartTime).String(),
				Error:         ferr.Error(),
			}
			a.EmitTypedEvent(ctx, initFailureEvent)
			sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fb.ModelID, ferr))
			continue
		}

		origLLM := a.LLM
		a.LLM = fallbackLLM

		// Try generation with fallback model
		fresp, ferr2 := a.LLM.GenerateContent(ctx, messages, opts...)

		a.LLM = origLLM
		a.ModelID = origModelID
		a.provider = origProvider

		if ferr2 == nil {
			usage := extractUsageMetricsWithMessages(fresp, messages)

			// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
			a.ModelID = fb.ModelID
			a.LLM = fallbackLLM
			// Update provider to match the fallback model
			if validProvider, err := llm.ValidateProvider(fb.Provider); err == nil {
				a.provider = validProvider
			}

			// Emit successful fallback events
			successEvent := events.NewFallbackAttemptEvent(
				turn, i+1, len(fallbackModels),
				fb.ModelID, fb.Provider, "unified",
				true, time.Since(attemptStartTime), "",
			)
			a.EmitTypedEvent(ctx, successEvent)

			fallbackUsedEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fb.ModelID, fb.Provider, errorType, time.Since(fallbackStartTime))
			a.EmitTypedEvent(ctx, fallbackUsedEvent)

			modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fb.ModelID, "fallback_success", fb.Provider, time.Since(fallbackStartTime))
			a.EmitTypedEvent(ctx, modelChangeEvent)

			sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fb.ModelID, fb.Provider))
			return fresp, nil, usage
		}

		// Emit fallback attempt failure event
		failureEvent := events.NewFallbackAttemptEvent(
			turn, i+1, len(fallbackModels),
			fb.ModelID, fb.Provider, "unified",
			false, time.Since(attemptStartTime), ferr2.Error(),
		)
		a.EmitTypedEvent(ctx, failureEvent)
		sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fb.ModelID, ferr2))
	}

	// All fallbacks failed
	allFailedEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"turn":                 turn + 1,
			"all_fallbacks_failed": true,
			"fallback_count":       len(fallbackModels),
			"error_type":           errorType,
			"operation":            "unified_fallback",
			"duration":             time.Since(fallbackStartTime).String(),
		},
	}
	a.EmitTypedEvent(ctx, allFailedEvent)

	return nil, fmt.Errorf("all %d fallback models failed for %s", len(fallbackModels), errorType), observability.UsageMetrics{}
}

// createFallbackLLMFromModel creates a fallback LLM using explicit provider from FallbackModel
func (a *Agent) createFallbackLLMFromModel(ctx context.Context, fb FallbackModel) (llmtypes.Model, error) {
	logger := getLogger(a)

	// Use explicit provider from FallbackModel
	provider, err := llm.ValidateProvider(fb.Provider)
	if err != nil {
		return nil, fmt.Errorf("invalid provider '%s' for fallback model %s: %w", fb.Provider, fb.ModelID, err)
	}

	logger.Infof("Creating fallback LLM - model_id: %s, provider: %s (explicit from config)", fb.ModelID, provider)

	// Use agent's temperature if available, otherwise default to 0.7
	temperature := a.Temperature
	if temperature == 0 {
		temperature = 0.7
	}

	// Convert Agent API keys to llm ProviderAPIKeys format
	var llmAPIKeys *llm.ProviderAPIKeys
	if a.APIKeys != nil {
		llmAPIKeys = &llm.ProviderAPIKeys{
			OpenRouter: a.APIKeys.OpenRouter,
			OpenAI:     a.APIKeys.OpenAI,
			Anthropic:  a.APIKeys.Anthropic,
			Vertex:     a.APIKeys.Vertex,
		}
		if a.APIKeys.Bedrock != nil {
			llmAPIKeys.Bedrock = &llm.BedrockConfig{
				Region: a.APIKeys.Bedrock.Region,
			}
		}
		logger.Infof("🔑 Using API keys from agent config for fallback LLM")
	} else {
		logger.Infof("⚠️ No API keys in agent config, fallback LLM will use environment variables")
	}

	llmConfig := llm.Config{
		Provider:    provider,
		ModelID:     fb.ModelID,
		Temperature: temperature,
		Tracers:     a.Tracers,
		TraceID:     a.TraceID,
		Logger:      logger,
		Context:     ctx,
		APIKeys:     llmAPIKeys,
	}

	llmModel, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create fallback LLM for provider %s, model %s: %w", provider, fb.ModelID, err)
	}
	return llmModel, nil
}

// GenerateContentWithRetry handles LLM generation with robust retry logic for throttling errors
func GenerateContentWithRetry(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, opts []llmtypes.CallOption, turn int, sendMessage func(string)) (*llmtypes.ContentResponse, error, observability.UsageMetrics) {
	// 🆕 DETAILED GENERATECONTENTWITHRETRY DEBUG LOGGING
	logger := getLogger(a)
	logger.Infof("🔄 [DEBUG] GenerateContentWithRetry START - Time: %v", time.Now())
	logger.Infof("🔄 [DEBUG] GenerateContentWithRetry params - Messages: %d, Options: %d, Turn: %d", len(messages), len(opts), turn)
	logger.Infof("🔄 [DEBUG] GenerateContentWithRetry context - Err: %v, Done: %v", ctx.Err(), ctx.Done())

	maxRetries := 5
	baseDelay := 30 * time.Second // Start with 30s for throttling
	maxDelay := 5 * time.Minute   // Maximum 5 minutes
	var lastErr error
	var usage observability.UsageMetrics

	isMaxTokenError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		isMaxToken := strings.Contains(msg, "max_token") ||
			strings.Contains(msg, "context") ||
			strings.Contains(msg, "max tokens") ||
			strings.Contains(msg, "Input is too long") ||
			strings.Contains(msg, "ValidationException") ||
			strings.Contains(msg, "too long")

		// Enhanced debugging for max token error detection
		if isMaxToken {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isMaxToken
		// REMOVED: Empty content patterns to prevent conflict with isEmptyContentError
		// Empty content errors should only be handled by isEmptyContentError function
	}

	isThrottlingError := func(err error) bool {
		if err == nil {
			return false
		}
		errStr := err.Error()
		isThrottling := strings.Contains(errStr, "ThrottlingException") ||
			strings.Contains(errStr, "Too many tokens") ||
			strings.Contains(errStr, "StatusCode: 429") ||
			strings.Contains(errStr, "API returned unexpected status code: 429") ||
			strings.Contains(errStr, "status code: 429") ||
			strings.Contains(errStr, "status code 429") ||
			strings.Contains(errStr, "429") ||
			strings.Contains(errStr, "rate limit") ||
			strings.Contains(errStr, "throttled")

		// Enhanced debugging for throttling error detection
		if isThrottling {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isThrottling
	}

	// Helper function to check if an error is an empty content error
	// Note: Excludes MALFORMED_FUNCTION_CALL errors which have their own specific error message
	isEmptyContentError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		// Exclude MALFORMED_FUNCTION_CALL errors - they have their own specific message
		if strings.Contains(msg, "MALFORMED_FUNCTION_CALL") {
			return false
		}
		isEmptyContent := strings.Contains(msg, "Choice.Content is empty string") ||
			strings.Contains(msg, "empty content error") ||
			strings.Contains(msg, "choice.Content is empty") ||
			strings.Contains(msg, "empty response")

		// Enhanced debugging for empty content error detection
		if isEmptyContent {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isEmptyContent
	}

	// Helper function to check if an error is a connection/network error
	isConnectionError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		isConnection := strings.Contains(msg, "EOF") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "timeout") ||
			strings.Contains(msg, "network") ||
			strings.Contains(msg, "dial tcp") ||
			strings.Contains(msg, "context deadline exceeded") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "broken pipe") ||
			strings.Contains(msg, "connection lost") ||
			strings.Contains(msg, "connection closed") ||
			strings.Contains(msg, "unexpected EOF")

		// Enhanced debugging for connection error detection
		if isConnection {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isConnection
	}

	// Helper function to check if an error is a stream-related error
	isStreamError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		isStream := strings.Contains(msg, "stream error") ||
			strings.Contains(msg, "stream ID") ||
			strings.Contains(msg, "streaming") ||
			strings.Contains(msg, "stream closed") ||
			strings.Contains(msg, "stream interrupted") ||
			strings.Contains(msg, "stream timeout") ||
			strings.Contains(msg, "streaming error")

		// Enhanced debugging for stream error detection
		if isStream {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isStream
	}

	// Helper function to check if an error is an internal server error
	isInternalError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		isInternal := strings.Contains(msg, "INTERNAL_ERROR") ||
			strings.Contains(msg, "internal error") ||
			strings.Contains(msg, "server error") ||
			strings.Contains(msg, "unexpected error") ||
			strings.Contains(msg, "received from peer") ||
			strings.Contains(msg, "peer error") ||
			strings.Contains(msg, "internal server error") ||
			strings.Contains(msg, "service error") ||
			// Add server errors (5xx) to trigger fallback - these should be classified as internal errors, not throttling
			strings.Contains(msg, "status 500") ||
			strings.Contains(msg, "status code: 500") ||
			strings.Contains(msg, "status code 500") ||
			strings.Contains(msg, "StatusCode: 500") ||
			strings.Contains(msg, "500") ||
			strings.Contains(msg, "status 502") ||
			strings.Contains(msg, "status code: 502") ||
			strings.Contains(msg, "status code 502") ||
			strings.Contains(msg, "502") ||
			strings.Contains(msg, "status 503") ||
			strings.Contains(msg, "status code: 503") ||
			strings.Contains(msg, "status code 503") ||
			strings.Contains(msg, "503") ||
			strings.Contains(msg, "status 504") ||
			strings.Contains(msg, "status code: 504") ||
			strings.Contains(msg, "status code 504") ||
			strings.Contains(msg, "504") ||
			strings.Contains(msg, "API returned unexpected status code: 5") ||
			strings.Contains(msg, "Bad Gateway") ||
			strings.Contains(msg, "Service Unavailable") ||
			strings.Contains(msg, "Gateway Timeout")

		// Enhanced debugging for internal error detection
		if isInternal {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isInternal
	}

	// Get fallback models for unified fallback system
	logger.Infof("Agent provider field: '%s'", a.provider)

	// Use the agent's provider field directly since the LLM instance might not have provider info
	var provider llm.Provider
	var err error
	if a.provider != "" {
		provider, err = llm.ValidateProvider(string(a.provider))
		if err != nil {
			// Log the error and use a default provider
			logger.Infof("Invalid provider '%s', using default provider 'bedrock' - error: %v", a.provider, err)
			provider = llm.ProviderBedrock
		}
	} else {
		// If no provider specified, default to bedrock
		logger.Infof("No provider specified, using default provider 'bedrock'")
		provider = llm.ProviderBedrock
	}

	logger.Infof("Validated provider: '%s'", provider)

	// Get unified fallback models sorted by priority
	fallbackModels := a.getFallbackModelsInPriority()
	logger.Infof("🔍 Unified fallback models: %d models in priority order", len(fallbackModels))
	for i, fb := range fallbackModels {
		logger.Infof("  [%d] priority=%d, provider=%s, model=%s", i, fb.Priority, fb.Provider, fb.ModelID)
	}

	// Create LLM generation with retry event (replaced span-based tracing)
	llmGenerationStartEvent := &events.LLMGenerationWithRetryEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:                turn,
		MaxRetries:          maxRetries,
		PrimaryModel:        a.ModelID,
		CurrentLLM:          a.ModelID,
		FallbackModels:      convertToEventFallbackModels(fallbackModels),
		FallbackModelsCount: len(fallbackModels),
		Provider:            string(a.provider),
		Operation:           "llm_generation_with_fallback",
		Status:              "started",
	}
	a.EmitTypedEvent(ctx, llmGenerationStartEvent)

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err(), usage
		default:
		}

		// 🆕 DETAILED LLM CALL DEBUGGING IN RETRY LOOP
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - About to call a.LLM.GenerateContent - Time: %v", attempt+1, time.Now())
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - LLM details - Provider: %s, Model: %s", attempt+1, string(a.GetProvider()), a.ModelID)
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - Context deadline check...", attempt+1)
		if deadline, ok := ctx.Deadline(); ok {
			timeUntilDeadline := time.Until(deadline)
			logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - Context deadline: %v, Time until deadline: %v", attempt+1, deadline, timeUntilDeadline)
		} else {
			logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - Context has no deadline", attempt+1)
		}

		// Use non-streaming approach for all agents
		llmCallStart := time.Now()
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - Calling a.LLM.GenerateContent NOW - Time: %v", attempt+1, llmCallStart)

		resp, err := a.LLM.GenerateContent(ctx, messages, opts...)

		llmCallDuration := time.Since(llmCallStart)
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - a.LLM.GenerateContent completed - Duration: %v, Error: %v", attempt+1, llmCallDuration, err != nil)

		if err == nil {
			logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - SUCCESS - Response: %v", attempt+1, resp != nil)
			usage = extractUsageMetricsWithMessages(resp, messages)
			// Note: llm_generation_end event is emitted by EndLLMGeneration() in conversation.go
			// to avoid duplicate events
			return resp, nil, usage
		}

		// 🆕 DETAILED ERROR DEBUGGING
		logger.Infof("🔄 [DEBUG] GenerateContentWithRetry attempt %d - ERROR - Error: %v, Error type: %T", attempt+1, err, err)

		// Emit LLM generation error event (replaced span-based tracing)
		llmAttemptErrorEvent := &events.LLMGenerationErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Turn:     turn + 1,
			ModelID:  a.ModelID,
			Error:    err.Error(),
			Duration: time.Since(llmGenerationStartEvent.Timestamp),
		}
		a.EmitTypedEvent(ctx, llmAttemptErrorEvent)

		// Enhanced debugging: Show which error classification is being used
		logger.Infof("🔍 ERROR CLASSIFICATION DEBUG - Error: %s", err.Error())
		logger.Infof("🔍 isMaxTokenError: %v", isMaxTokenError(err))
		logger.Infof("🔍 isEmptyContentError: %v", isEmptyContentError(err))
		logger.Infof("🔍 isThrottlingError: %v", isThrottlingError(err))
		logger.Infof("🔍 isConnectionError: %v", isConnectionError(err))
		logger.Infof("🔍 isStreamError: %v", isStreamError(err))
		logger.Infof("🔍 isInternalError: %v", isInternalError(err))

		// Handle max token errors with fallback models
		if isMaxTokenError(err) {
			// Emit max token error event
			maxTokenFallbackEvent := &events.LLMGenerationErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
					EventID:   events.GenerateEventID(),
				},
				Turn:     turn + 1,
				ModelID:  a.ModelID,
				Error:    err.Error(),
				Duration: 0,
			}
			a.EmitTypedEvent(ctx, maxTokenFallbackEvent)

			sendMessage(fmt.Sprintf("\n⚠️ LLM generation failed due to max_token/context error (turn %d). Trying fallback models...", turn))

			// Use unified fallback system
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "max_token_error", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}

			sendMessage("\n❌ All fallback models failed for context length error")
			sendMessage("   - Suggestion: Try reducing conversation history or input length")
			lastErr = fmt.Errorf("all fallback models failed for max_token error: %w", err)
			break
		}

		// Handle throttling errors with fallback models
		if isThrottlingError(err) {
			throttlingStartTime := time.Now()

			// Emit throttling detected event
			throttlingEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(throttlingStartTime), "throttling", 0)
			a.EmitTypedEvent(ctx, throttlingEvent)

			sendMessage(fmt.Sprintf("\n⚠️ %s throttling detected (turn %d, attempt %d/%d). Trying fallback models...", strings.Title(string(a.provider)), turn, attempt+1, maxRetries))

			// Use unified fallback system
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "throttling", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}

			// If all fallback models failed, try waiting and retrying with original model
			if attempt < maxRetries-1 {
				delay := time.Duration(float64(baseDelay) * (1.5 + float64(attempt)*0.5))
				if delay > maxDelay {
					delay = maxDelay
				}

				sendMessage(fmt.Sprintf("\n⏳ All fallback models failed. Waiting %v before retry with original model...", delay))

				select {
				case <-ctx.Done():
					return nil, ctx.Err(), usage
				case <-time.After(delay):
				}

				sendMessage(fmt.Sprintf("\n🔄 Retrying with original model (turn %d, attempt %d/%d)...", turn, attempt+2, maxRetries))
				continue
			}

			lastErr = fmt.Errorf("all models failed after %d attempts: %w", maxRetries, err)
			break
		}

		// Handle empty content errors - go directly to fallback models (no retry delay)
		if isEmptyContentError(err) {
			logger.Infof("🔍 EMPTY CONTENT ERROR HANDLING STARTED - fallback_count: %d", len(fallbackModels))

			// Emit empty content error event
			emptyContentEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, 0, "empty_content", 0)
			a.EmitTypedEvent(ctx, emptyContentEvent)

			sendMessage("\n⚠️ Empty content error: Proceeding directly to fallback models...")

			// Use unified fallback system
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "empty_content", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}

			sendMessage("\n❌ All fallback models failed for empty content error")
			sendMessage("   - Suggestion: Try rephrasing your question or providing more context")
			lastErr = fmt.Errorf("all fallback models failed for empty content error: %w", err)
			break
		}

		// Handle connection/network errors with fallback models
		if isConnectionError(err) {
			// Emit connection error detected event
			connectionErrorEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, 0, "connection_error", 0)
			a.EmitTypedEvent(ctx, connectionErrorEvent)

			sendMessage(fmt.Sprintf("\n⚠️ Connection/network error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))

			// Use unified fallback system
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "connection_error", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}

			sendMessage("\n❌ All fallback models failed for connection error")
			sendMessage("   - Suggestion: Check network connectivity and try again")
			lastErr = fmt.Errorf("all fallback models failed for connection error: %w", err)
			break
		}

		// Handle stream errors with fallback models
		if isStreamError(err) {
			sendMessage(fmt.Sprintf("\n⚠️ Stream error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "stream_error", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}
			lastErr = fmt.Errorf("all fallback models failed for stream_error: %w", err)
			break
		}

		// Handle internal server errors with fallback models
		if isInternalError(err) {
			sendMessage(fmt.Sprintf("\n⚠️ Internal server error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))
			resp, fallbackErr, fallbackUsage := a.tryUnifiedFallback(ctx, "internal_error", turn, fallbackModels, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}
			lastErr = fmt.Errorf("all fallback models failed for internal_error: %w", err)
			break
		}

		// For any other errors, just return the error
		lastErr = err
		break
	}

	sendMessage(fmt.Sprintf("\n❌ LLM generation failed after %d attempts (turn %d): %v", maxRetries, turn, lastErr))
	return nil, lastErr, usage
}
