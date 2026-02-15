package mcpagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// isContextCanceledError checks if an error is due to context cancellation or deadline exceeded
func isContextCanceledError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "context deadline exceeded")
}

// retryOriginalModel handles retry logic for throttling and zero_candidates errors
// Returns: shouldRetry (bool), delay (time.Duration), error
func retryOriginalModel(a *Agent, ctx context.Context, errorType string, attempt, maxRetries int, baseDelay, maxDelay time.Duration, turn int, logger loggerv2.Logger, usage observability.UsageMetrics) (bool, time.Duration, error) {
	// Exponential backoff: 10s, 20s, 40s, 80s, 160s...
	delay := baseDelay * time.Duration(1<<attempt)
	if delay > maxDelay {
		delay = maxDelay
	}

	// Emit retry attempt event with proper model/provider info for UI display
	retryAttemptEvent := events.NewFallbackAttemptEvent(
		turn, attempt+1, maxRetries,
		a.ModelID, string(a.provider), "retry", // Use "retry" phase to distinguish from actual fallbacks
		false, delay, fmt.Sprintf("%s - retrying original model", errorType),
	)
	a.EmitTypedEvent(ctx, retryAttemptEvent)

	var logMsg string
	if errorType == "zero_candidates_error" {
		logMsg = fmt.Sprintf("🔄 [ZERO_CANDIDATES] Retrying original model FIRST (before fallbacks). Waiting %v before retry (attempt %d/%d)...", delay, attempt+1, maxRetries)
	} else {
		logMsg = fmt.Sprintf("🔄 [THROTTLING] Retrying original model FIRST (before fallbacks). Waiting %v before retry (attempt %d/%d)...", delay, attempt+1, maxRetries)
	}
	logger.Info(logMsg)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	// Wait for delay or context cancellation
	select {
	case <-ctx.Done():
		return false, delay, ctx.Err()
	case <-timer.C:
	}

	var retryLogMsg string
	if errorType == "zero_candidates_error" {
		retryLogMsg = fmt.Sprintf("🔄 [ZERO_CANDIDATES] Retrying with original model (turn %d, attempt %d/%d)...", turn, attempt+2, maxRetries)
	} else {
		retryLogMsg = fmt.Sprintf("🔄 [THROTTLING] Retrying with original model (turn %d, attempt %d/%d)...", turn, attempt+2, maxRetries)
	}
	logger.Info(retryLogMsg)
	return true, delay, nil
}

// isMaxTokenError checks if an error is due to reaching maximum token limit
func isMaxTokenError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Exclude context cancellation from max token errors
	if isContextCanceledError(err) {
		return false
	}
	return strings.Contains(msg, "max_token") ||
		strings.Contains(msg, "max tokens") ||
		strings.Contains(msg, "Input is too long") ||
		strings.Contains(msg, "ValidationException") ||
		strings.Contains(msg, "too long")
}

// isThrottlingError checks if an error is due to API throttling
func isThrottlingError(err error) bool {
	if err == nil {
		return false
	}
	// Exclude context cancellation from throttling errors
	if isContextCanceledError(err) {
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
		strings.Contains(errStr, "throttled") ||
		strings.Contains(errStr, "overloaded") ||
		strings.Contains(errStr, "model is overloaded") ||
		strings.Contains(errStr, "UNAVAILABLE") ||
		(strings.Contains(errStr, "503") && strings.Contains(errStr, "overloaded"))
}

// isEmptyContentError checks if an error is due to empty content in response
func isEmptyContentError(err error) bool {
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

// isZeroCandidatesError checks if an error is due to zero candidates returned
func isZeroCandidatesError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "zero candidates") ||
		strings.Contains(msg, "returned zero candidates") ||
		strings.Contains(msg, "no candidates")
}

// isConnectionError checks if an error is due to connection issues
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	// Exclude context cancellation from connection errors
	if isContextCanceledError(err) {
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

// isStreamError checks if an error is due to streaming issues
func isStreamError(err error) bool {
	if err == nil {
		return false
	}
	// Exclude context cancellation from stream errors
	if isContextCanceledError(err) {
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

// isInternalError checks if an error is due to internal server issues
func isInternalError(err error) bool {
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

// classifyLLMError categorizes the given error into a known LLM error type
func classifyLLMError(err error) string {
	if isMaxTokenError(err) {
		return "max_token_error"
	} else if isThrottlingError(err) {
		return "throttling_error"
	} else if isZeroCandidatesError(err) {
		return "zero_candidates_error"
	} else if isEmptyContentError(err) {
		return "empty_content_error"
	} else if isConnectionError(err) {
		return "connection_error"
	} else if isStreamError(err) {
		return "stream_error"
	} else if isInternalError(err) {
		return "internal_error"
	}
	return ""
}

// streamingManager handles streaming state and goroutine management
type streamingManager struct {
	streamChan        chan llmtypes.StreamChunk
	streamingDone     chan bool
	contentChunkIndex int
	totalChunks       int
	startTime         time.Time
}

// startStreaming initializes streaming if enabled and on the first attempt
func (a *Agent) startStreaming(ctx context.Context, attempt int, opts *[]llmtypes.CallOption) *streamingManager {
	if !a.EnableStreaming || attempt != 0 {
		return nil
	}

	sm := &streamingManager{
		streamChan:    make(chan llmtypes.StreamChunk, 100),
		streamingDone: make(chan bool, 1),
		startTime:     time.Now(),
	}

	*opts = append(*opts, llmtypes.WithStreamingChan(sm.streamChan))

	a.EmitTypedEvent(ctx, &events.StreamingStartEvent{
		BaseEventData: events.BaseEventData{Timestamp: time.Now()},
		Model:         a.ModelID,
		Provider:      string(a.provider),
	})

	go sm.processChunks(ctx, a)
	return sm
}

// processChunks runs in a goroutine to handle incoming streaming chunks
func (sm *streamingManager) processChunks(ctx context.Context, a *Agent) {
	defer func() {
		sm.streamingDone <- true
	}()

	for chunk := range sm.streamChan {
		if chunk.Type == llmtypes.StreamChunkTypeContent && chunk.Content != "" {
			sm.contentChunkIndex++
			sm.totalChunks++

			a.EmitTypedEvent(ctx, &events.StreamingChunkEvent{
				BaseEventData: events.BaseEventData{Timestamp: time.Now()},
				Content:       chunk.Content,
				ChunkIndex:    sm.contentChunkIndex,
				IsToolCall:    false,
			})

			if a.StreamingCallback != nil {
				a.StreamingCallback(chunk)
			}
		}
	}
}

// finishStreaming waits for streaming to complete and emits the end event
func (a *Agent) finishStreaming(ctx context.Context, sm *streamingManager, resp *llmtypes.ContentResponse) {
	if sm == nil {
		return
	}

	<-sm.streamingDone

	endEvent := &events.StreamingEndEvent{
		BaseEventData: events.BaseEventData{Timestamp: time.Now()},
		TotalChunks:   sm.totalChunks,
		Duration:      time.Since(sm.startTime).String(),
	}

	if resp != nil && len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil {
		if resp.Choices[0].GenerationInfo.TotalTokens != nil {
			endEvent.TotalTokens = *resp.Choices[0].GenerationInfo.TotalTokens
		}
		if resp.Choices[0].StopReason != "" {
			endEvent.FinishReason = resp.Choices[0].StopReason
		}
	}
	a.EmitTypedEvent(ctx, endEvent)
}

// getEffectiveLLMConfig returns a unified LLM configuration, compatible with legacy settings
func (a *Agent) getEffectiveLLMConfig() AgentLLMConfiguration {
	// If the new config is populated, use it
	if a.LLMConfig.Primary.ModelID != "" && a.LLMConfig.Primary.Provider != "" {
		return a.LLMConfig
	}

	// Otherwise, build from legacy fields
	config := AgentLLMConfiguration{
		Primary: LLMModel{
			Provider: string(a.provider),
			ModelID:  a.ModelID,
			// Note: API Key not easily accessible from legacy Agent struct without introspection
			// but executeLLM will handle this by checking Agent.APIKeys if model.APIKey is nil
		},
		Fallbacks: []LLMModel{},
	}

	// Add legacy cross-provider fallbacks if available
	if a.CrossProviderFallback != nil {
		for _, model := range a.CrossProviderFallback.Models {
			config.Fallbacks = append(config.Fallbacks, LLMModel{
				Provider: a.CrossProviderFallback.Provider,
				ModelID:  model,
			})
		}
	}

	return config
}

// executeLLM creates an LLM instance and executes it
func (a *Agent) executeLLM(ctx context.Context, model LLMModel, messages []llmtypes.MessageContent, opts []llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Create LLM instance with model's own auth
	apiKeys := &llm.ProviderAPIKeys{}

	// First, set up agent-level keys as base (so Azure and Bedrock configs are always available)
	if a.APIKeys != nil {
		apiKeys = &llm.ProviderAPIKeys{
			OpenRouter: a.APIKeys.OpenRouter,
			OpenAI:     a.APIKeys.OpenAI,
			Anthropic:  a.APIKeys.Anthropic,
			Vertex:     a.APIKeys.Vertex,
		}
		if a.APIKeys.Bedrock != nil {
			apiKeys.Bedrock = &llm.BedrockConfig{
				Region: a.APIKeys.Bedrock.Region,
			}
		}
		if a.APIKeys.Azure != nil {
			apiKeys.Azure = &llm.AzureAPIConfig{
				Endpoint:   a.APIKeys.Azure.Endpoint,
				APIKey:     a.APIKeys.Azure.APIKey,
				APIVersion: a.APIKeys.Azure.APIVersion,
				Region:     a.APIKeys.Azure.Region,
			}
		}
	}

	// Override with model-specific key if available (for simple API key providers)
	if model.APIKey != nil {
		switch llmproviders.Provider(model.Provider) {
		case llmproviders.ProviderOpenRouter:
			apiKeys.OpenRouter = model.APIKey
		case llmproviders.ProviderOpenAI:
			apiKeys.OpenAI = model.APIKey
		case llmproviders.ProviderAnthropic:
			apiKeys.Anthropic = model.APIKey
		case llmproviders.ProviderVertex:
			apiKeys.Vertex = model.APIKey
		}
	}

	if model.Region != nil && llmproviders.Provider(model.Provider) == llmproviders.ProviderBedrock {
		if apiKeys.Bedrock == nil {
			apiKeys.Bedrock = &llm.BedrockConfig{}
		}
		apiKeys.Bedrock.Region = *model.Region
	}

	// Use model's temperature if available, otherwise fallback to agent's temperature
	temperature := a.Temperature
	if model.Temperature != nil {
		temperature = *model.Temperature
	}

	llmInstance, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.Provider(model.Provider),
		ModelID:     model.ModelID,
		Temperature: temperature,
		Logger:      a.Logger,
		APIKeys:     apiKeys,
		Tracers:     a.Tracers,
		TraceID:     a.TraceID,
		Context:     ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	return llmInstance.GenerateContent(ctx, messages, opts...)
}

// GenerateContentWithRetry handles LLM generation with robust retry logic and tiered fallback
func GenerateContentWithRetry(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, opts []llmtypes.CallOption, turn int) (*llmtypes.ContentResponse, observability.UsageMetrics, error) {
	logger := getLogger(a)
	logger.Info(fmt.Sprintf("🔄 [DEBUG] GenerateContentWithRetry START - Messages: %d, Options: %d, Turn: %d", len(messages), len(opts), turn))

	maxRetries := 5
	if env := os.Getenv("LLM_MAX_RETRIES"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			maxRetries = val
		}
	}

	maxRetriesZeroCandidates := 3 // Limit retries for zero_candidates errors to 3 before fallback

	baseDelaySeconds := 10
	if env := os.Getenv("LLM_RETRY_BASE_DELAY_SECONDS"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			baseDelaySeconds = val
		}
	}
	baseDelay := time.Duration(baseDelaySeconds) * time.Second

	maxDelaySeconds := 300 // 5 minutes
	if env := os.Getenv("LLM_RETRY_MAX_DELAY_SECONDS"); env != "" {
		if val, err := strconv.Atoi(env); err == nil && val > 0 {
			maxDelaySeconds = val
		}
	}
	maxDelay := time.Duration(maxDelaySeconds) * time.Second
	var lastErr error
	var usage observability.UsageMetrics

	// Get effective configuration (supports new and legacy)
	llmConfig := a.getEffectiveLLMConfig()

	// Build list of models to try: Primary + Fallbacks
	modelsToTry := []LLMModel{llmConfig.Primary}
	modelsToTry = append(modelsToTry, llmConfig.Fallbacks...)

	generationStartTime := time.Now()

	// Emit start event
	a.EmitTypedEvent(ctx, &events.LLMGenerationWithRetryEvent{
		BaseEventData: events.BaseEventData{Timestamp: generationStartTime},
		Turn:          turn,
		MaxRetries:    maxRetries,
		PrimaryModel:  llmConfig.Primary.ModelID,
		CurrentLLM:    llmConfig.Primary.ModelID,
		// SameProviderFallbacks:  sameProviderFallbacks, // Deprecated/merged
		// CrossProviderFallbacks: crossProviderFallbacks, // Deprecated/merged
		Provider:  llmConfig.Primary.Provider,
		Operation: "llm_generation_with_fallback",
		Status:    "started",
	})

	// Iterate through models
	for modelIndex, model := range modelsToTry {
		isFallback := modelIndex > 0
		if isFallback {
			logger.Info(fmt.Sprintf("🔄 Trying fallback %d/%d: %s/%s",
				modelIndex, len(llmConfig.Fallbacks), model.Provider, model.ModelID))

			// Emit fallback model used event
			fallbackEvent := events.NewFallbackModelUsedEvent(turn, llmConfig.Primary.ModelID, model.ModelID, model.Provider, "fallback_chain", time.Since(generationStartTime))
			a.EmitTypedEvent(ctx, fallbackEvent)

			// Temporarily update agent's model ID for consistent event logging
			// This is important because EmitTypedEvent uses a.ModelID in some places
			// We revert it later if we fail, or keep it if we succeed and want to stick to it?
			// The original logic kept it on success.
			a.ModelID = model.ModelID
			a.provider = llm.Provider(model.Provider)
		}

		// Try executing with retries (throttling/transient error handling)
		for attempt := 0; attempt < maxRetries; attempt++ {
			if ctx.Err() != nil {
				return nil, usage, a.handleContextCancellation(ctx, turn, generationStartTime)
			}

			// Create a copy of options for this attempt
			currentOpts := make([]llmtypes.CallOption, len(opts))
			copy(currentOpts, opts)

			// Start streaming (only on first attempt of primary model, or maybe disable for fallbacks?)
			// Original logic: streaming enabled for primary, disabled for fallbacks in loop
			// Here we can enable it if the agent supports it, but fallback logic usually disables it for simplicity
			// For now, let's keep it enabled if it's the first model, or if we want streaming on fallbacks too
			// The original code passed `opts` to fallback generation which might include streaming channel?
			// Actually `startStreaming` modifies `currentOpts` to add the channel.
			// If we are in fallback, we probably shouldn't use the SAME channel if the previous one closed?
			// `startStreaming` creates a NEW channel every time it's called.
			// So streaming on fallback is fine if the frontend can handle it.
			// However, the original code used "non-streaming approach for all agents during fallback".
			// Let's stick to that for safety: only stream on primary model (modelIndex == 0).
			var sm *streamingManager
			if modelIndex == 0 {
				sm = a.startStreaming(ctx, attempt, &currentOpts)
			}

			// Execute LLM
			resp, err := a.executeLLM(ctx, model, messages, currentOpts)

			if modelIndex == 0 {
				a.finishStreaming(ctx, sm, resp)
			}

			if err == nil {
				usage = extractUsageMetricsWithMessages(resp, messages)

				if isFallback {
					// Emit fallback success event
					fallbackAttemptEvent := events.NewFallbackAttemptEvent(
						turn, modelIndex, len(llmConfig.Fallbacks),
						model.ModelID, model.Provider, "fallback_chain",
						true, time.Since(generationStartTime), "",
					)
					a.EmitTypedEvent(ctx, fallbackAttemptEvent)

					// Emit model change event to track the permanent model change
					modelChangeEvent := events.NewModelChangeEvent(turn, llmConfig.Primary.ModelID, model.ModelID, "fallback_success", model.Provider, time.Since(generationStartTime))
					a.EmitTypedEvent(ctx, modelChangeEvent)

					// Update agent's config to use this working model as primary for future calls?
					// The original code did: a.ModelID = fallbackModelID; a.LLM = fallbackLLM
					// For this refactor, we are not storing the LLM instance permanently for fallbacks in the same way,
					// but we should probably update a.ModelID and a.provider for consistency.
					// We already did that at the start of the loop.
					// We should also update LLMConfig.Primary to this model to avoid retrying failed primary next turn?
					// That's a behavior change. Let's strictly follow the "permanent update" behavior of original code.
					a.ModelID = model.ModelID
					a.provider = llm.Provider(model.Provider)
					// Note: a.LLM is not updated here because we create it on the fly in executeLLM.
					// If we want to persist it, we'd need to re-initialize a.LLM.
					// But since we use executeLLM now, we don't strictly rely on a.LLM for generation anymore in this function.
					// However, other parts of Agent might use a.LLM (e.g. token counting metadata).
					// Ideally we should update a.LLM.
					// For now, let's leave a.LLM as is or update it if possible.
					// Re-initializing a.LLM here might be expensive or unnecessary if we always use executeLLM.
				} else {
					// Primary succeeded
					logger.Info(fmt.Sprintf("✅ Primary LLM succeeded: %s/%s", model.Provider, model.ModelID))
				}

				return resp, usage, nil
			}

			// Handle context cancellation specifically
			if isContextCanceledError(err) || ctx.Err() != nil {
				return nil, usage, a.handleContextCancellation(ctx, turn, generationStartTime)
			}

			// Emit error event for actual errors
			a.EmitTypedEvent(ctx, &events.LLMGenerationErrorEvent{
				BaseEventData: events.BaseEventData{Timestamp: time.Now()},
				Turn:          turn + 1,
				ModelID:       model.ModelID,
				Error:         err.Error(),
				Duration:      time.Since(generationStartTime),
			})

			errorType := classifyLLMError(err)
			lastErr = err

			// Special handling for retrying SAME model (throttling/zero candidates)
			// For zero_candidates errors: limit to 3 retries before fallback
			// For throttling errors: use full 5 retries
			shouldRetrySameModel := false
			if errorType == "zero_candidates_error" {
				// Zero candidates: retry up to 3 times (attempts 0, 1, 2 = 3 retries total)
				if attempt < maxRetriesZeroCandidates-1 {
					shouldRetrySameModel = true
				} else {
					logger.Info(fmt.Sprintf("🔄 [ZERO_CANDIDATES] Reached max retries (%d) for zero_candidates error, moving to fallback models", maxRetriesZeroCandidates))
					// Break immediately - don't continue the loop
					logger.Warn(fmt.Sprintf("❌ Model failed after %d retries: %s/%s - %v", maxRetriesZeroCandidates, model.Provider, model.ModelID, err))
					break // Break retry loop, proceed to next model
				}
			} else if errorType == "throttling_error" {
				// Throttling: retry up to 5 times (existing behavior)
				if attempt < maxRetries-1 {
					shouldRetrySameModel = true
				}
			}

			if shouldRetrySameModel {
				// Use maxRetriesZeroCandidates for zero_candidates, maxRetries for throttling
				retryLimit := maxRetries
				if errorType == "zero_candidates_error" {
					retryLimit = maxRetriesZeroCandidates
				}
				shouldRetry, _, retryErr := retryOriginalModel(a, ctx, errorType, attempt, retryLimit, baseDelay, maxDelay, turn, logger, usage)
				if retryErr != nil {
					return nil, usage, retryErr
				}
				if shouldRetry {
					continue // Retry same model
				}
			}

			// If not a retryable error on same model, or max retries reached:
			// Break inner loop to try next model in fallback list
			logger.Warn(fmt.Sprintf("❌ Model failed: %s/%s - %v", model.Provider, model.ModelID, err))

			// Emit failure event for this model
			if isFallback {
				failureEvent := events.NewFallbackAttemptEvent(
					turn, modelIndex, len(llmConfig.Fallbacks),
					model.ModelID, model.Provider, "fallback_chain",
					false, time.Since(generationStartTime), err.Error(),
				)
				a.EmitTypedEvent(ctx, failureEvent)
			}

			break // Break retry loop, proceed to next model
		}
	}

	// If all models failed
	return nil, usage, fmt.Errorf("all LLMs failed (primary + %d fallbacks): %w", len(llmConfig.Fallbacks), lastErr)
}

// handleContextCancellation emits cancellation event and returns the error
func (a *Agent) handleContextCancellation(ctx context.Context, turn int, startTime time.Time) error {
	err := ctx.Err()
	if err == nil {
		err = context.Canceled
	}
	a.EmitTypedEvent(ctx, events.NewContextCancelledEvent(turn, err.Error(), time.Since(startTime)))
	return err
}
