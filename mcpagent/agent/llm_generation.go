package mcpagent

import (
	"context"
	"fmt"
	"mcpagent/events"
	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GenerateContentWithRetry handles LLM generation with robust retry logic for throttling errors
func GenerateContentWithRetry(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, opts []llmtypes.CallOption, turn int, sendMessage func(string)) (*llmtypes.ContentResponse, observability.UsageMetrics, error) {
	// 🆕 DETAILED GENERATECONTENTWITHRETRY DEBUG LOGGING
	logger := getLogger(a)
	logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry START - Time: %v", time.Now()))
	logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry params - Messages: %d, Options: %d, Turn: %d", len(messages), len(opts), turn))
	logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry context - Err: %v, Done: %v", ctx.Err(), ctx.Done()))

	maxRetries := 5
	baseDelay := 30 * time.Second // Start with 30s for throttling
	maxDelay := 5 * time.Minute   // Maximum 5 minutes
	var lastErr error
	var usage observability.UsageMetrics

	// Helper functions for error classification
	isMaxTokenError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		return strings.Contains(msg, "max_token") ||
			strings.Contains(msg, "context") ||
			strings.Contains(msg, "max tokens") ||
			strings.Contains(msg, "Input is too long") ||
			strings.Contains(msg, "ValidationException") ||
			strings.Contains(msg, "too long")
	}

	isThrottlingError := func(err error) bool {
		if err == nil {
			return false
		}
		errStr := err.Error()
		return strings.Contains(errStr, "ThrottlingException") ||
			strings.Contains(errStr, "Too many tokens") ||
			strings.Contains(errStr, "StatusCode: 429") ||
			strings.Contains(errStr, "API returned unexpected status code: 429") ||
			strings.Contains(errStr, "status code: 429") ||
			strings.Contains(errStr, "status code 429") ||
			strings.Contains(errStr, "429") ||
			strings.Contains(errStr, "rate limit") ||
			strings.Contains(errStr, "throttled")
	}

	isEmptyContentError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		if strings.Contains(msg, "MALFORMED_FUNCTION_CALL") {
			return false
		}
		return strings.Contains(msg, "Choice.Content is empty string") ||
			strings.Contains(msg, "empty content error") ||
			strings.Contains(msg, "choice.Content is empty") ||
			strings.Contains(msg, "empty response")
	}

	isConnectionError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		return strings.Contains(msg, "EOF") ||
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
	}

	isStreamError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		return strings.Contains(msg, "stream error") ||
			strings.Contains(msg, "stream ID") ||
			strings.Contains(msg, "streaming") ||
			strings.Contains(msg, "stream closed") ||
			strings.Contains(msg, "stream interrupted") ||
			strings.Contains(msg, "stream timeout") ||
			strings.Contains(msg, "streaming error")
	}

	isInternalError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
		return strings.Contains(msg, "INTERNAL_ERROR") ||
			strings.Contains(msg, "internal error") ||
			strings.Contains(msg, "server error") ||
			strings.Contains(msg, "unexpected error") ||
			strings.Contains(msg, "received from peer") ||
			strings.Contains(msg, "peer error") ||
			strings.Contains(msg, "internal server error") ||
			strings.Contains(msg, "service error") ||
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
	}

	// Get fallback models for the current provider
	v2Logger := a.Logger
	v2Logger.Debug("Getting fallback models", loggerv2.String("provider_field", string(a.provider)))

	// Validate and fallback provider
	var provider llm.Provider
	var err error
	if a.provider != "" {
		provider, err = llm.ValidateProvider(string(a.provider))
		if err != nil {
			// Log the error and use a default provider
			v2Logger.Warn("Invalid provider, using default provider 'bedrock'",
				loggerv2.String("provider", string(a.provider)),
				loggerv2.Error(err))
			provider = llm.ProviderBedrock
		}
	} else {
		// If no provider specified, default to bedrock
		v2Logger.Debug("No provider specified, using default provider 'bedrock'")
		provider = llm.ProviderBedrock
	}

	v2Logger.Debug("Validated provider", loggerv2.String("provider", string(provider)))

	// Determine fallback models
	sameProviderFallbacks := llm.GetDefaultFallbackModels(provider)
	var crossProviderFallbacks []string
	var crossProviderName string

	if a.CrossProviderFallback != nil {
		crossProviderFallbacks = a.CrossProviderFallback.Models
		crossProviderName = a.CrossProviderFallback.Provider
		v2Logger.Debug("Using frontend cross-provider fallback",
			loggerv2.String("provider", crossProviderName),
			loggerv2.Any("models", crossProviderFallbacks))
	} else {
		crossProviderFallbacks = llm.GetCrossProviderFallbackModels(provider)
		if len(crossProviderFallbacks) > 0 {
			crossProviderName = string(detectProviderFromModelID(crossProviderFallbacks[0]))
		} else {
			if provider == llm.ProviderVertex {
				crossProviderName = "anthropic"
			} else {
				crossProviderName = "openai"
			}
		}
		v2Logger.Debug("Using default cross-provider fallback",
			loggerv2.String("provider", crossProviderName),
			loggerv2.Any("models", crossProviderFallbacks))
	}

	v2Logger.Debug("Fallback models loaded",
		loggerv2.Any("same_provider", sameProviderFallbacks),
		loggerv2.Any("cross_provider", crossProviderFallbacks))

	// Emit start event
	llmGenerationStartEvent := &events.LLMGenerationWithRetryEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Turn:                   turn,
		MaxRetries:             maxRetries,
		PrimaryModel:           a.ModelID,
		CurrentLLM:             a.ModelID,
		SameProviderFallbacks:  sameProviderFallbacks,
		CrossProviderFallbacks: crossProviderFallbacks,
		Provider:               string(a.provider),
		Operation:              "llm_generation_with_fallback",
		Status:                 "started",
	}
	a.EmitTypedEvent(ctx, llmGenerationStartEvent)

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, usage, ctx.Err()
		default:
		}

		logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry attempt %d - About to call a.LLM.GenerateContent", attempt+1))

		llmCallStart := time.Now()
		resp, err := a.LLM.GenerateContent(ctx, messages, opts...)
		llmCallDuration := time.Since(llmCallStart)
		logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry attempt %d - Duration: %v, Error: %v", attempt+1, llmCallDuration, err != nil))

		if err == nil {
			logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry attempt %d - SUCCESS", attempt+1))
			usage = extractUsageMetricsWithMessages(resp, messages)
			return resp, usage, nil
		}

		// Error handling
		logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry attempt %d - ERROR: %v", attempt+1, err))
		lastErr = err

		// Emit error event
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

		// Determine error type
		var errorType string
		if isMaxTokenError(err) {
			errorType = "max_token_error"
		} else if isThrottlingError(err) {
			errorType = "throttling_error"
		} else if isEmptyContentError(err) {
			errorType = "empty_content_error"
		} else if isConnectionError(err) {
			errorType = "connection_error"
		} else if isStreamError(err) {
			errorType = "stream_error"
		} else if isInternalError(err) {
			errorType = "internal_error"
		}

		if errorType != "" {
			// Use unified fallback logic
			fResp, fUsage, fErr := handleErrorWithFallback(a, ctx, err, errorType, turn, attempt, maxRetries, sameProviderFallbacks, crossProviderFallbacks, sendMessage, messages, opts)

			if fErr == nil {
				return fResp, fUsage, nil
			}

			lastErr = fErr

			// Special handling for throttling: retry with original model if fallbacks failed
			if errorType == "throttling_error" && attempt < maxRetries-1 {
				delay := time.Duration(float64(baseDelay) * (1.5 + float64(attempt)*0.5))
				if delay > maxDelay {
					delay = maxDelay
				}

				// Emit and log delay
				retryDelayEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{Timestamp: time.Now()},
					Data: map[string]interface{}{
						"delay_duration": delay.String(),
						"attempt":        attempt + 1,
						"max_retries":    maxRetries,
						"operation":      "retry_delay",
						"error_type":     "throttling",
					},
				}
				a.EmitTypedEvent(ctx, retryDelayEvent)
				sendMessage(fmt.Sprintf("\n⏳ All fallback models failed. Waiting %v before retry with original model...", delay))

				// Wait for delay or context cancellation
				select {
				case <-ctx.Done():
					return nil, usage, ctx.Err()
				case <-time.After(delay):
				}

				sendMessage(fmt.Sprintf("\n🔄 Retrying with original model (turn %d, attempt %d/%d)...", turn, attempt+2, maxRetries))
				continue
			}

			// For other error types, or if max retries reached for throttling, break loops
			break
		}

		// Unknown error type - break and return last error
		lastErr = err
		break
	}

	sendMessage(fmt.Sprintf("\n❌ LLM generation failed after %d attempts (turn %d): %v", maxRetries, turn, lastErr))
	return nil, usage, lastErr
}

// handleErrorWithFallback is a generic function that handles any error type with fallback models
func handleErrorWithFallback(a *Agent, ctx context.Context, err error, errorType string, turn int, attempt int, maxRetries int, sameProviderFallbacks, crossProviderFallbacks []string, sendMessage func(string), messages []llmtypes.MessageContent, opts []llmtypes.CallOption) (*llmtypes.ContentResponse, observability.UsageMetrics, error) {
	// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events

	// Track error start time
	errorStartTime := time.Now()

	// Emit error detected event
	errorEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(errorStartTime), errorType, 0)
	a.EmitTypedEvent(ctx, errorEvent)

	// Create error fallback event
	errorFallbackEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"error_type":               errorType,
			"original_error":           err.Error(),
			"same_provider_fallbacks":  len(sameProviderFallbacks),
			"cross_provider_fallbacks": len(crossProviderFallbacks),
			"turn":                     turn,
			"attempt":                  attempt + 1,
			"operation":                errorType + "_fallback",
		},
	}
	a.EmitTypedEvent(ctx, errorFallbackEvent)

	// Send user message based on error type
	var userMessage string
	switch errorType {
	case "stream_error":
		userMessage = fmt.Sprintf("\n⚠️ Stream error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	case "internal_error":
		userMessage = fmt.Sprintf("\n⚠️ Internal server error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	case "connection_error":
		userMessage = fmt.Sprintf("\n⚠️ Connection/network error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	case "empty_content_error":
		userMessage = fmt.Sprintf("\n⚠️ Empty content error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	case "throttling_error":
		userMessage = fmt.Sprintf("\n⚠️ Throttling error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	case "max_token_error":
		userMessage = fmt.Sprintf("\n⚠️ Max token error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries)
	default:
		userMessage = fmt.Sprintf("\n⚠️ %s error detected (turn %d, attempt %d/%d). Trying fallback models...", errorType, turn, attempt+1, maxRetries)
	}
	sendMessage(userMessage)

	// Phase 1: Try same-provider fallbacks first
	sendMessage(fmt.Sprintf("\n🔄 Phase 1: Trying %d same-provider (%s) fallback models...", len(sameProviderFallbacks), string(a.provider)))
	for i, fallbackModelID := range sameProviderFallbacks {
		sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(a.provider), i+1, len(sameProviderFallbacks), fallbackModelID))

		origModelID := a.ModelID
		a.ModelID = fallbackModelID
		fallbackLLM, ferr := a.createFallbackLLM(ctx, fallbackModelID)
		if ferr != nil {
			a.ModelID = origModelID
			sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
			continue
		}

		origLLM := a.LLM
		a.LLM = fallbackLLM

		// Use non-streaming approach for all agents during fallback
		fresp, ferr2 := a.LLM.GenerateContent(ctx, messages, opts...)

		a.LLM = origLLM
		a.ModelID = origModelID

		if ferr2 == nil {
			usage := extractUsageMetricsWithMessages(fresp, messages)

			// Detect the actual provider for the fallback model
			fallbackProvider := detectProviderFromModelID(fallbackModelID)

			// Emit fallback attempt event for successful attempt
			fallbackAttemptEvent := events.NewFallbackAttemptEvent(
				turn, i+1, len(sameProviderFallbacks),
				fallbackModelID, string(fallbackProvider), "same_provider",
				true, time.Since(errorStartTime), "",
			)
			a.EmitTypedEvent(ctx, fallbackAttemptEvent)

			// Emit fallback model used event
			fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(fallbackProvider), errorType, time.Since(errorStartTime))
			a.EmitTypedEvent(ctx, fallbackEvent)

			// Emit model change event to track the permanent model change
			modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", string(fallbackProvider), time.Since(errorStartTime))
			a.EmitTypedEvent(ctx, modelChangeEvent)

			// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
			a.ModelID = fallbackModelID
			a.LLM = fallbackLLM

			sendMessage(fmt.Sprintf("\n✅ %s fallback successful with %s model: %s", errorType, string(fallbackProvider), fallbackModelID))
			return fresp, usage, nil
		} else {
			sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
			// Emit fallback attempt event for generation failure
			failureEvent := events.NewFallbackAttemptEvent(
				turn, i+1, len(sameProviderFallbacks),
				fallbackModelID, string(detectProviderFromModelID(fallbackModelID)), "same_provider",
				false, time.Since(errorStartTime), ferr2.Error(),
			)
			a.EmitTypedEvent(ctx, failureEvent)
		}
	}

	// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
	if len(crossProviderFallbacks) > 0 {
		// Detect provider for the first cross-provider model to show in phase message
		crossProviderName := string(detectProviderFromModelID(crossProviderFallbacks[0]))
		sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (%s) fallback models...", len(crossProviderFallbacks), crossProviderName))
		for i, fallbackModelID := range crossProviderFallbacks {
			fallbackProvider := detectProviderFromModelID(fallbackModelID)
			sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(fallbackProvider), i+1, len(crossProviderFallbacks), fallbackModelID))

			origModelID := a.ModelID
			a.ModelID = fallbackModelID
			fallbackLLM, ferr := a.createFallbackLLM(ctx, fallbackModelID)
			if ferr != nil {
				a.ModelID = origModelID
				sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
				continue
			}

			origLLM := a.LLM
			a.LLM = fallbackLLM

			// Use non-streaming approach for all agents during fallback
			fresp, ferr2 := a.LLM.GenerateContent(ctx, messages, opts...)

			a.LLM = origLLM
			a.ModelID = origModelID

			if ferr2 == nil {
				usage := extractUsageMetricsWithMessages(fresp, messages)

				// Detect the actual provider for the fallback model
				fallbackProvider := detectProviderFromModelID(fallbackModelID)

				// Emit fallback attempt event for successful attempt
				fallbackAttemptEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(crossProviderFallbacks),
					fallbackModelID, string(fallbackProvider), "cross_provider",
					true, time.Since(errorStartTime), "",
				)
				a.EmitTypedEvent(ctx, fallbackAttemptEvent)

				// Emit fallback model used event
				fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(fallbackProvider), errorType, time.Since(errorStartTime))
				a.EmitTypedEvent(ctx, fallbackEvent)

				// Emit model change event to track the permanent model change
				modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", string(fallbackProvider), time.Since(errorStartTime))
				a.EmitTypedEvent(ctx, modelChangeEvent)

				// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
				a.ModelID = fallbackModelID
				a.LLM = fallbackLLM

				sendMessage(fmt.Sprintf("\n✅ %s cross-provider fallback successful with %s model: %s", errorType, string(fallbackProvider), fallbackModelID))
				return fresp, usage, nil
			} else {
				sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
				// Emit fallback attempt event for generation failure
				failureEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(crossProviderFallbacks),
					fallbackModelID, string(detectProviderFromModelID(fallbackModelID)), "cross_provider",
					false, time.Since(errorStartTime), ferr2.Error(),
				)
				a.EmitTypedEvent(ctx, failureEvent)
			}
		}
	}

	// If all fallback models failed, emit failure event
	errorAllFailedEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"error_type":  errorType,
			"operation":   errorType + "_fallback",
			"duration":    time.Since(errorStartTime).String(),
			"final_error": err.Error(),
		},
	}
	a.EmitTypedEvent(ctx, errorAllFailedEvent)

	return nil, observability.UsageMetrics{}, fmt.Errorf("all fallback models failed for %s: %w", errorType, err)
}

// createFallbackLLM creates a fallback LLM instance for the given modelID
// ctx is used for cancellation/timeout during initialization
func (a *Agent) createFallbackLLM(ctx context.Context, modelID string) (llmtypes.Model, error) {
	// ✅ FIXED: Detect provider from model ID instead of using agent's provider
	provider := detectProviderFromModelID(modelID)

	// Log the fallback attempt with the detected provider
	v2Logger := a.Logger
	v2Logger.Debug("Creating fallback LLM using detected provider",
		loggerv2.String("model_id", modelID),
		loggerv2.String("detected_provider", string(provider)))

	// Use InitializeLLM from providers.go for all providers for consistency
	// This ensures proper initialization, logging, and event emission
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
		v2Logger.Debug("Using API keys from agent config for fallback LLM")
	} else {
		v2Logger.Warn("No API keys in agent config, fallback LLM will use environment variables")
	}

	llmConfig := llm.Config{
		Provider:    provider,
		ModelID:     modelID,
		Temperature: temperature,
		Tracers:     a.Tracers,
		TraceID:     a.TraceID,
		Logger:      a.Logger, // Use agent's v2.Logger (llm.Config expects v2.Logger)
		Context:     ctx,
		APIKeys:     llmAPIKeys,
	}

	llmModel, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create fallback LLM for provider %s, model %s: %w", provider, modelID, err)
	}
	return llmModel, nil
}

// detectProviderFromModelID detects the provider based on the model ID
func detectProviderFromModelID(modelID string) llm.Provider {
	// OpenAI models: gpt-*, gpt-4*, gpt-3*, o3*, o4*
	if strings.HasPrefix(modelID, "gpt-") || strings.HasPrefix(modelID, "o3") || strings.HasPrefix(modelID, "o4") {
		return llm.ProviderOpenAI
	}

	// Bedrock models: us.anthropic.* (Bedrock-specific prefix)
	if strings.HasPrefix(modelID, "us.anthropic.") {
		return llm.ProviderBedrock
	}

	// Anthropic models: claude-* (for direct API, not Bedrock)
	if strings.HasPrefix(modelID, "claude-") {
		return llm.ProviderAnthropic
	}

	// Vertex/Gemini models: gemini-* (Google Vertex AI)
	if strings.HasPrefix(modelID, "gemini-") {
		return llm.ProviderVertex
	}

	// OpenRouter models: various model names with "/" separator
	if strings.Contains(modelID, "/") {
		return llm.ProviderOpenRouter
	}

	// Default to Bedrock for unknown models (conservative approach)
	return llm.ProviderBedrock
}
