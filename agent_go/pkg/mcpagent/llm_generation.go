package mcpagent

import (
	"context"
	"fmt"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/pkg/events"
	"os"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/bedrock"
	"github.com/tmc/langchaingo/llms/openai"
)

// GenerateContentWithRetry handles LLM generation with robust retry logic for throttling errors
func GenerateContentWithRetry(a *Agent, ctx context.Context, messages []llms.MessageContent, opts []llms.CallOption, turn int, sendMessage func(string)) (*llms.ContentResponse, error, observability.UsageMetrics) {
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
			strings.Contains(errStr, "throttled") ||
			// Add server errors (5xx) to trigger fallback
			strings.Contains(errStr, "502") ||
			strings.Contains(errStr, "503") ||
			strings.Contains(errStr, "504") ||
			strings.Contains(errStr, "500") ||
			strings.Contains(errStr, "API returned unexpected status code: 5") ||
			strings.Contains(errStr, "Provider returned error") ||
			strings.Contains(errStr, "Bad Gateway") ||
			strings.Contains(errStr, "Service Unavailable") ||
			strings.Contains(errStr, "Gateway Timeout")

		// Enhanced debugging for throttling error detection
		if isThrottling {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isThrottling
	}

	// Helper function to check if an error is an empty content error
	isEmptyContentError := func(err error) bool {
		if err == nil {
			return false
		}
		msg := err.Error()
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
			strings.Contains(msg, "service error")

		// Enhanced debugging for internal error detection
		if isInternal {
			// Note: logger will be available in the main function scope
			// This will be logged when the error is actually processed
		}

		return isInternal
	}

	// Get fallback models for the current provider
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
	sameProviderFallbacks := llm.GetDefaultFallbackModels(provider)

	// Use actual cross-provider fallback configuration if available, otherwise fall back to hardcoded function
	var crossProviderFallbacks []string
	var crossProviderName string
	if a.CrossProviderFallback != nil {
		crossProviderFallbacks = a.CrossProviderFallback.Models
		crossProviderName = a.CrossProviderFallback.Provider
		logger.Infof("🔍 Using frontend cross-provider fallback - Provider: %s, Models: %v", crossProviderName, crossProviderFallbacks)
	} else {
		crossProviderFallbacks = llm.GetCrossProviderFallbackModels(provider)
		crossProviderName = "openai" // Default fallback provider
		logger.Infof("🔍 Using default cross-provider fallback - Provider: %s, Models: %v", crossProviderName, crossProviderFallbacks)
	}

	logger.Infof("🔍 Fallback models loaded - same_provider: %v, cross_provider: %v", sameProviderFallbacks, crossProviderFallbacks)

	// Create LLM generation with retry event (replaced span-based tracing)
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
			// Emit LLM generation success event (replaced span-based tracing)
			llmAttemptEndEvent := &events.LLMGenerationEndEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Turn:      turn + 1,
				Content:   resp.Choices[0].Content,
				ToolCalls: len(resp.Choices[0].ToolCalls),
				Duration:  time.Since(llmGenerationStartEvent.Timestamp),
				UsageMetrics: events.UsageMetrics{
					PromptTokens:     usage.InputTokens,
					CompletionTokens: usage.OutputTokens,
					TotalTokens:      usage.TotalTokens,
				},
			}
			a.EmitTypedEvent(ctx, llmAttemptEndEvent)
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
			// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events
			if a.AgentMode == ReActAgent && a.reasoningTracker != nil {
				a.reasoningTracker.Reset()
			}

			// Create max token fallback event (replaced span-based tracing)
			maxTokenFallbackEvent := &events.LLMGenerationErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
					EventID:   events.GenerateEventID(),
				},
				Turn:     turn + 1,
				ModelID:  a.ModelID,
				Error:    err.Error(),
				Duration: 0, // Will be calculated when fallback completes
			}
			a.EmitTypedEvent(ctx, maxTokenFallbackEvent)

			sendMessage(fmt.Sprintf("\n⚠️ LLM generation failed due to max_token/context error (turn %d). Trying fallback models...", turn))

			// Store original error for final fallback
			originalError := err

			// Phase 1: Try same-provider fallbacks first
			sendMessage(fmt.Sprintf("\n🔄 Phase 1: Trying %d same-provider (%s) fallback models...", len(sameProviderFallbacks), string(a.provider)))
			for i, fallbackModelID := range sameProviderFallbacks {
				// Create fallback attempt event (replaced span-based tracing)
				fallbackAttemptEvent := &events.FallbackAttemptEvent{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Turn:          turn + 1,
					AttemptIndex:  i + 1,
					TotalAttempts: len(sameProviderFallbacks),
					ModelID:       fallbackModelID,
					Provider:      string(a.provider),
					Phase:         "same_provider",
					Success:       false, // Will be updated when attempt completes
					Duration:      "",    // Will be updated when attempt completes
				}
				a.EmitTypedEvent(ctx, fallbackAttemptEvent)

				sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(a.provider), i+1, len(sameProviderFallbacks), fallbackModelID))

				// Track fallback attempt start time
				fallbackStartTime := time.Now()

				origModelID := a.ModelID
				a.ModelID = fallbackModelID
				fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
				if ferr != nil {
					a.ModelID = origModelID
					// Emit fallback attempt event for initialization failure (replaced span-based tracing)
					fallbackInitFailureEvent := &events.FallbackAttemptEvent{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Turn:          turn + 1,
						AttemptIndex:  i + 1,
						TotalAttempts: len(sameProviderFallbacks),
						ModelID:       fallbackModelID,
						Provider:      string(a.provider),
						Phase:         "same_provider",
						Success:       false,
						Duration:      time.Since(fallbackStartTime).String(),
						Error:         ferr.Error(),
					}
					a.EmitTypedEvent(ctx, fallbackInitFailureEvent)

					// Emit fallback attempt event for initialization failure
					fallbackAttemptEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(sameProviderFallbacks),
						fallbackModelID, string(a.provider), "same_provider",
						false, time.Since(fallbackStartTime), ferr.Error(),
					)
					a.EmitTypedEvent(ctx, fallbackAttemptEvent)

					sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
					continue
				}

				origLLM := a.LLM
				a.LLM = fallbackLLM

				// For ReAct agents, use streaming in fallback as well
				var fresp *llms.ContentResponse
				var ferr2 error
				if a.AgentMode == ReActAgent {
					streamingOpts := append(opts, llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
						chunkStr := string(chunk)
						sendMessage(chunkStr)
						return nil
					}))
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, streamingOpts...)
				} else {
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)
				}

				a.LLM = origLLM
				a.ModelID = origModelID

				if ferr2 == nil {
					usage = extractUsageMetricsWithMessages(fresp, messages)

					// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
					a.ModelID = fallbackModelID
					a.LLM = fallbackLLM
					// Note: We don't restore origModelID and origLLM anymore

					// Emit fallback attempt event for successful attempt
					fallbackAttemptEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(sameProviderFallbacks),
						fallbackModelID, string(a.provider), "same_provider",
						true, time.Since(fallbackStartTime), "",
					)
					a.EmitTypedEvent(ctx, fallbackAttemptEvent)

					// Emit fallback model used event
					fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(a.provider), "max_token_error", time.Since(fallbackStartTime))
					a.EmitTypedEvent(ctx, fallbackEvent)

					// Emit model change event to track the permanent model change
					modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", string(a.provider), time.Since(fallbackStartTime))
					a.EmitTypedEvent(ctx, modelChangeEvent)

					// Emit fallback attempt success event (replaced span-based tracing)
					fallbackSuccessEvent := &events.FallbackAttemptEvent{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Turn:          turn + 1,
						AttemptIndex:  i + 1,
						TotalAttempts: len(sameProviderFallbacks),
						ModelID:       fallbackModelID,
						Provider:      string(a.provider),
						Phase:         "same_provider",
						Success:       true,
						Duration:      time.Since(fallbackStartTime).String(),
					}
					a.EmitTypedEvent(ctx, fallbackSuccessEvent)
					// Emit max token fallback success event (replaced span-based tracing)
					maxTokenSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":                turn + 1,
							"successful_fallback": fallbackModelID,
							"attempts":            i + 1,
							"successful_llm":      fallbackModelID,
							"successful_provider": string(a.provider),
							"successful_phase":    "same_provider",
							"duration":            time.Since(fallbackStartTime).String(),
						},
					}
					a.EmitTypedEvent(ctx, maxTokenSuccessEvent)
					sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, string(a.provider)))
					return fresp, nil, usage
				}

				// Emit fallback attempt event for generation failure
				fallbackAttemptEvent = events.NewFallbackAttemptEvent(
					turn, i+1, len(sameProviderFallbacks),
					fallbackModelID, string(a.provider), "same_provider",
					false, time.Since(fallbackStartTime), ferr2.Error(),
				)
				a.EmitTypedEvent(ctx, fallbackAttemptEvent)

				// Emit fallback attempt failure event
				fallbackAttemptFailureEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(sameProviderFallbacks),
					fallbackModelID, string(a.provider), "same_provider",
					false, time.Since(fallbackStartTime), ferr2.Error(),
				)
				a.EmitTypedEvent(ctx, fallbackAttemptFailureEvent)
				// Provide more specific error messages for context length issues
				errorMsg := ferr2.Error()
				if strings.Contains(errorMsg, "Input is too long") || strings.Contains(errorMsg, "ValidationException") {
					sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: Input too long for this model's context window", fallbackModelID))
				} else {
					sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
				}
			}

			// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (%s) fallback models...", len(crossProviderFallbacks), strings.Title(crossProviderName)))
				for i, fallbackModelID := range crossProviderFallbacks {
					// Create cross-provider fallback attempt event (replaced span-based tracing)
					crossProviderFallbackEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"llm_model":         fallbackModelID,
							"fallback_provider": "openai",
							"fallback_phase":    "cross_provider",
							"total_fallbacks":   len(crossProviderFallbacks),
							"error_type":        "max_token",
							"operation":         "fallback_attempt",
						},
					}
					a.EmitTypedEvent(ctx, crossProviderFallbackEvent)

					sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", strings.Title(crossProviderName), i+1, len(crossProviderFallbacks), fallbackModelID))

					// Track fallback attempt start time
					fallbackStartTime := time.Now()

					origModelID := a.ModelID
					a.ModelID = fallbackModelID
					fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
					if ferr != nil {
						a.ModelID = origModelID
						// Emit cross-provider fallback initialization failure event (replaced span-based tracing)
						crossProviderInitFailureEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           false,
								"error":             ferr.Error(),
								"stage":             "initialization",
								"fallback_model":    fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"duration":          time.Since(fallbackStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderInitFailureEvent)

						// Emit fallback attempt event for initialization failure
						fallbackAttemptEvent := events.NewFallbackAttemptEvent(
							turn, i+1, len(crossProviderFallbacks),
							fallbackModelID, "openai", "cross_provider",
							false, time.Since(fallbackStartTime), ferr.Error(),
						)
						a.EmitTypedEvent(ctx, fallbackAttemptEvent)

						sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
						continue
					}

					origLLM := a.LLM
					a.LLM = fallbackLLM

					// For ReAct agents, use streaming in fallback as well
					var fresp *llms.ContentResponse
					var ferr2 error
					// Use non-streaming approach for all agents, including ReAct agents during fallback
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

					a.LLM = origLLM
					a.ModelID = origModelID

					if ferr2 == nil {
						usage = extractUsageMetricsWithMessages(fresp, messages)

						// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
						a.ModelID = fallbackModelID
						a.LLM = fallbackLLM
						// Note: We don't restore origModelID and origLLM anymore

						// Emit fallback attempt event for successful attempt
						fallbackAttemptEvent := events.NewFallbackAttemptEvent(
							turn, i+1, len(crossProviderFallbacks),
							fallbackModelID, "openai", "cross_provider",
							true, time.Since(fallbackStartTime), "",
						)
						a.EmitTypedEvent(ctx, fallbackAttemptEvent)

						// Emit fallback model used event
						fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, "openai", "max_token_error", time.Since(fallbackStartTime))
						a.EmitTypedEvent(ctx, fallbackEvent)

						// Emit model change event to track the permanent model change
						modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", "openai", time.Since(fallbackStartTime))
						a.EmitTypedEvent(ctx, modelChangeEvent)

						// Emit cross-provider fallback success event (replaced span-based tracing)
						crossProviderSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           true,
								"usage":             usage,
								"stage":             "generation",
								"llm_model":         fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"duration":          time.Since(fallbackStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderSuccessEvent)
						// Emit max token fallback success event for cross-provider (replaced span-based tracing)
						maxTokenCrossProviderSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":                turn + 1,
								"successful_fallback": fallbackModelID,
								"attempts":            i + 1,
								"successful_llm":      fallbackModelID,
								"successful_provider": "openai",
								"successful_phase":    "cross_provider",
								"duration":            time.Since(fallbackStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, maxTokenCrossProviderSuccessEvent)
						sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, strings.Title(crossProviderName)))
						return fresp, nil, usage
					}

					// Emit fallback attempt event for generation failure
					fallbackAttemptEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(crossProviderFallbacks),
						fallbackModelID, "openai", "cross_provider",
						false, time.Since(fallbackStartTime), ferr2.Error(),
					)
					a.EmitTypedEvent(ctx, fallbackAttemptEvent)

					// Emit cross-provider fallback attempt failure event
					crossProviderFallbackFailureEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(crossProviderFallbacks),
						fallbackModelID, crossProviderName, fmt.Sprintf("cross_provider_%s", crossProviderName),
						false, time.Since(fallbackStartTime), ferr2.Error(),
					)
					a.EmitTypedEvent(ctx, crossProviderFallbackFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
				}
			}

			// Provide a detailed summary of all failed fallback attempts
			sendMessage(fmt.Sprintf("\n❌ All fallback models failed for context length error (turn %d):", turn))
			sendMessage(fmt.Sprintf("   - Tried %d Bedrock models: %v", len(sameProviderFallbacks), sameProviderFallbacks))
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("   - Tried %d OpenAI models: %v", len(crossProviderFallbacks), crossProviderFallbacks))
			}
			sendMessage(fmt.Sprintf("   - Original error: %v", originalError))
			sendMessage(fmt.Sprintf("   - Suggestion: Try reducing conversation history or input length"))

			// Emit max token fallback all failed event (replaced span-based tracing)
			maxTokenAllFailedEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"turn":                    turn + 1,
					"all_fallbacks_failed":    true,
					"same_provider_attempts":  len(sameProviderFallbacks),
					"cross_provider_attempts": len(crossProviderFallbacks),
					"failed_models":           append(sameProviderFallbacks, crossProviderFallbacks...),
					"error_type":              "max_token",
					"operation":               "max_token_fallback",
					"final_error":             err.Error(),
				},
			}
			a.EmitTypedEvent(ctx, maxTokenAllFailedEvent)
			lastErr = fmt.Errorf("all fallback models failed for max_token error: %v", originalError)
			break
		}

		// Handle throttling errors with fallback models
		if isThrottlingError(err) {
			// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events
			if a.AgentMode == ReActAgent && a.reasoningTracker != nil {
				a.reasoningTracker.Reset()
			}

			// Track throttling start time
			throttlingStartTime := time.Now()

			// Emit throttling detected event
			throttlingEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(throttlingStartTime))
			a.EmitTypedEvent(ctx, throttlingEvent)

			// Create throttling fallback event (replaced span-based tracing)
			throttlingFallbackEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"error_type":               "throttling_error",
					"original_error":           err.Error(),
					"same_provider_fallbacks":  len(sameProviderFallbacks),
					"cross_provider_fallbacks": len(crossProviderFallbacks),
					"turn":                     turn,
					"attempt":                  attempt + 1,
					"operation":                "throttling_fallback",
				},
			}
			a.EmitTypedEvent(ctx, throttlingFallbackEvent)

			sendMessage(fmt.Sprintf("\n⚠️ AWS Bedrock throttling detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))

			// Phase 1: Try same-provider fallbacks first
			sendMessage(fmt.Sprintf("\n🔄 Phase 1: Trying %d same-provider (Bedrock) fallback models...", len(sameProviderFallbacks)))
			for i, fallbackModelID := range sameProviderFallbacks {
				// Create throttling fallback attempt event (replaced span-based tracing)
				throttlingFallbackAttemptEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Data: map[string]interface{}{
						"fallback_index":    i + 1,
						"fallback_model":    fallbackModelID,
						"llm_model":         fallbackModelID,
						"fallback_provider": string(a.provider),
						"fallback_phase":    "same_provider",
						"total_fallbacks":   len(sameProviderFallbacks),
						"error_type":        "throttling",
						"operation":         "fallback_attempt",
					},
				}
				a.EmitTypedEvent(ctx, throttlingFallbackAttemptEvent)

				sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(a.provider), i+1, len(sameProviderFallbacks), fallbackModelID))

				origModelID := a.ModelID
				a.ModelID = fallbackModelID
				fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
				if ferr != nil {
					a.ModelID = origModelID
					// Emit throttling fallback initialization failure event (replaced span-based tracing)
					throttlingInitFailureEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":              turn + 1,
							"success":           false,
							"error":             ferr.Error(),
							"stage":             "initialization",
							"fallback_model":    fallbackModelID,
							"fallback_provider": string(a.provider),
							"fallback_phase":    "same_provider",
							"error_type":        "throttling",
						},
					}
					a.EmitTypedEvent(ctx, throttlingInitFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
					continue
				}

				origLLM := a.LLM
				a.LLM = fallbackLLM

				// Use non-streaming approach for all agents during fallback
				var fresp *llms.ContentResponse
				var ferr2 error
				// Use non-streaming approach for all agents, including ReAct agents during fallback
				fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

				a.LLM = origLLM
				a.ModelID = origModelID

				if ferr2 == nil {
					usage = extractUsageMetricsWithMessages(fresp, messages)

					// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
					a.ModelID = fallbackModelID
					a.LLM = fallbackLLM
					// Note: We don't restore origModelID and origLLM anymore

					// Emit fallback model used event
					fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(a.provider), "throttling", time.Since(throttlingStartTime))
					a.EmitTypedEvent(ctx, fallbackEvent)

					// Emit model change event to track the permanent model change
					modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", string(a.provider), time.Since(throttlingStartTime))
					a.EmitTypedEvent(ctx, modelChangeEvent)

					// Emit throttling fallback success event (replaced span-based tracing)
					throttlingFallbackSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":              turn + 1,
							"success":           true,
							"usage":             usage,
							"stage":             "generation",
							"llm_model":         fallbackModelID,
							"fallback_provider": string(a.provider),
							"fallback_phase":    "same_provider",
							"error_type":        "throttling",
							"duration":          time.Since(throttlingStartTime).String(),
						},
					}
					a.EmitTypedEvent(ctx, throttlingFallbackSuccessEvent)
					// Emit throttling fallback overall success event (replaced span-based tracing)
					throttlingOverallSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":                turn + 1,
							"successful_fallback": fallbackModelID,
							"attempts":            i + 1,
							"successful_llm":      fallbackModelID,
							"successful_provider": string(a.provider),
							"successful_phase":    "same_provider",
							"error_type":          "throttling",
							"duration":            time.Since(throttlingStartTime).String(),
						},
					}
					a.EmitTypedEvent(ctx, throttlingOverallSuccessEvent)
					sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, string(a.provider)))
					return fresp, nil, usage
				}

				// Emit throttling fallback attempt failure event
				throttlingFallbackFailureEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(sameProviderFallbacks),
					fallbackModelID, string(a.provider), "same_provider",
					false, time.Since(throttlingStartTime), ferr2.Error(),
				)
				a.EmitTypedEvent(ctx, throttlingFallbackFailureEvent)
				sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
			}

			// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (%s) fallback models...", len(crossProviderFallbacks), strings.Title(crossProviderName)))
				for i, fallbackModelID := range crossProviderFallbacks {
					// Create cross-provider throttling fallback attempt event (replaced span-based tracing)
					crossProviderThrottlingFallbackEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"llm_model":         fallbackModelID,
							"fallback_provider": "openai",
							"fallback_phase":    "cross_provider",
							"total_fallbacks":   len(crossProviderFallbacks),
							"error_type":        "throttling",
							"operation":         "fallback_attempt",
						},
					}
					a.EmitTypedEvent(ctx, crossProviderThrottlingFallbackEvent)

					sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", strings.Title(crossProviderName), i+1, len(crossProviderFallbacks), fallbackModelID))

					origModelID := a.ModelID
					a.ModelID = fallbackModelID
					fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
					if ferr != nil {
						a.ModelID = origModelID
						// Emit cross-provider throttling fallback initialization failure event (replaced span-based tracing)
						crossProviderThrottlingInitFailureEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           false,
								"error":             ferr.Error(),
								"stage":             "initialization",
								"fallback_model":    fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"error_type":        "throttling",
							},
						}
						a.EmitTypedEvent(ctx, crossProviderThrottlingInitFailureEvent)
						sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
						continue
					}

					origLLM := a.LLM
					a.LLM = fallbackLLM

					// Use non-streaming approach for all agents during fallback
					var fresp *llms.ContentResponse
					var ferr2 error
					// Use non-streaming approach for all agents, including ReAct agents during fallback
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

					a.LLM = origLLM
					a.ModelID = origModelID

					if ferr2 == nil {
						usage = extractUsageMetricsWithMessages(fresp, messages)

						// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
						a.ModelID = fallbackModelID
						a.LLM = fallbackLLM
						// Note: We don't restore origModelID and origLLM anymore

						// Emit fallback model used event
						fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, "openai", "throttling", time.Since(throttlingStartTime))
						a.EmitTypedEvent(ctx, fallbackEvent)

						// Emit model change event to track the permanent model change
						modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", "openai", time.Since(throttlingStartTime))
						a.EmitTypedEvent(ctx, modelChangeEvent)

						// Emit cross-provider throttling fallback success event (replaced span-based tracing)
						crossProviderThrottlingSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           true,
								"usage":             usage,
								"stage":             "generation",
								"llm_model":         fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"error_type":        "throttling",
								"duration":          time.Since(throttlingStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderThrottlingSuccessEvent)
						// Emit cross-provider throttling fallback overall success event (replaced span-based tracing)
						crossProviderThrottlingOverallSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":                turn + 1,
								"successful_fallback": fallbackModelID,
								"attempts":            i + 1,
								"successful_llm":      fallbackModelID,
								"successful_provider": "openai",
								"successful_phase":    "cross_provider",
								"error_type":          "throttling",
								"duration":            time.Since(throttlingStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderThrottlingOverallSuccessEvent)
						sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, strings.Title(crossProviderName)))
						return fresp, nil, usage
					}

					// Emit cross-provider throttling fallback attempt failure event
					crossProviderThrottlingFallbackFailureEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(crossProviderFallbacks),
						fallbackModelID, crossProviderName, fmt.Sprintf("cross_provider_%s", crossProviderName),
						false, time.Since(throttlingStartTime), ferr2.Error(),
					)
					a.EmitTypedEvent(ctx, crossProviderThrottlingFallbackFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
				}
			}

			// If all fallback models failed, try waiting and retrying with original model
			if attempt < maxRetries-1 {
				delay := time.Duration(float64(baseDelay) * (1.5 + float64(attempt)*0.5))
				if delay > maxDelay {
					delay = maxDelay
				}

				// Create retry delay event (replaced span-based tracing)
				retryDelayEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
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

				go func() {
					ticker := time.NewTicker(15 * time.Second)
					defer ticker.Stop()
					remaining := delay
					for remaining > 0 {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							remaining -= 15 * time.Second
							if remaining > 0 {
								sendMessage(fmt.Sprintf("\n⏳ Still waiting... %v remaining (turn %d)", remaining, turn))
							}
						}
					}
				}()

				select {
				case <-ctx.Done():
					// Emit retry delay cancellation event (replaced span-based tracing)
					retryDelayCancelledEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":       turn + 1,
							"cancelled":  true,
							"error":      ctx.Err().Error(),
							"error_type": "throttling",
							"operation":  "retry_delay",
						},
					}
					a.EmitTypedEvent(ctx, retryDelayCancelledEvent)
					return nil, ctx.Err(), usage
				case <-time.After(delay):
				}

				// Emit retry delay completion event (replaced span-based tracing)
				retryDelayCompletedEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Data: map[string]interface{}{
						"turn":       turn + 1,
						"completed":  true,
						"error_type": "throttling",
						"operation":  "retry_delay",
					},
				}
				a.EmitTypedEvent(ctx, retryDelayCompletedEvent)
				// Emit throttling fallback all failed event (replaced span-based tracing)
				throttlingAllFailedEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Data: map[string]interface{}{
						"turn":                    turn + 1,
						"all_fallbacks_failed":    true,
						"retrying_with_original":  true,
						"same_provider_attempts":  len(sameProviderFallbacks),
						"cross_provider_attempts": len(crossProviderFallbacks),
						"error_type":              "throttling",
						"duration":                time.Since(throttlingStartTime).String(),
					},
				}
				a.EmitTypedEvent(ctx, throttlingAllFailedEvent)

				sendMessage(fmt.Sprintf("\n🔄 Retrying with original model (turn %d, attempt %d/%d)...", turn, attempt+2, maxRetries))
				continue
			}

			// Emit throttling fallback max retries reached event (replaced span-based tracing)
			throttlingMaxRetriesEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"turn":                    turn + 1,
					"all_fallbacks_failed":    true,
					"max_retries_reached":     true,
					"same_provider_attempts":  len(sameProviderFallbacks),
					"cross_provider_attempts": len(crossProviderFallbacks),
					"error_type":              "throttling",
					"duration":                time.Since(throttlingStartTime).String(),
					"final_error":             err.Error(),
				},
			}
			a.EmitTypedEvent(ctx, throttlingMaxRetriesEvent)
			lastErr = fmt.Errorf("all models failed after %d attempts: %v", maxRetries, err)
			break
		}

		// Handle empty content errors with fallback models
		if isEmptyContentError(err) {
			logger.Infof("🔍 EMPTY CONTENT ERROR HANDLING STARTED")
			logger.Infof("🔍 Error details: %s", err.Error())
			logger.Infof("🔍 Available fallbacks - same_provider: %d, cross_provider: %d", len(sameProviderFallbacks), len(crossProviderFallbacks))
			logger.Infof("🔍 Same provider fallbacks: %v", sameProviderFallbacks)
			logger.Infof("🔍 Cross provider fallbacks: %v", crossProviderFallbacks)

			// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events
			if a.AgentMode == ReActAgent && a.reasoningTracker != nil {
				a.reasoningTracker.Reset()
			}
			// Track empty content error start time
			emptyContentStartTime := time.Now()

			// Emit empty content error event
			emptyContentEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(emptyContentStartTime))
			a.EmitTypedEvent(ctx, emptyContentEvent)

			// Create empty content fallback event (replaced span-based tracing)
			emptyContentFallbackEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"error_type":               "empty_content_error",
					"original_error":           err.Error(),
					"same_provider_fallbacks":  len(sameProviderFallbacks),
					"cross_provider_fallbacks": len(crossProviderFallbacks),
					"turn":                     turn,
					"attempt":                  attempt + 1,
					"operation":                "empty_content_fallback",
				},
			}
			a.EmitTypedEvent(ctx, emptyContentFallbackEvent)

			sendMessage(fmt.Sprintf("\n⚠️ Empty content error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))

			// Phase 1: Try same-provider fallbacks first
			sendMessage(fmt.Sprintf("\n🔄 Phase 1: Trying %d same-provider (%s) fallback models...", len(sameProviderFallbacks), string(a.provider)))
			for i, fallbackModelID := range sameProviderFallbacks {
				// Create fallback attempt event (replaced span-based tracing)
				fallbackAttemptEvent := &events.FallbackAttemptEvent{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Turn:          turn + 1,
					AttemptIndex:  i + 1,
					TotalAttempts: len(sameProviderFallbacks),
					ModelID:       fallbackModelID,
					Provider:      string(a.provider),
					Phase:         "same_provider",
					Success:       false, // Will be updated when attempt completes
					Duration:      "",    // Will be updated when attempt completes
				}
				a.EmitTypedEvent(ctx, fallbackAttemptEvent)

				sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(a.provider), i+1, len(sameProviderFallbacks), fallbackModelID))

				origModelID := a.ModelID
				a.ModelID = fallbackModelID
				fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
				if ferr != nil {
					a.ModelID = origModelID
					// Emit fallback initialization failure event (replaced span-based tracing)
					fallbackInitFailureEvent := &events.FallbackAttemptEvent{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Turn:          turn + 1,
						AttemptIndex:  i + 1,
						TotalAttempts: len(sameProviderFallbacks),
						ModelID:       fallbackModelID,
						Provider:      string(a.provider),
						Phase:         "same_provider",
						Error:         ferr.Error(),
						Success:       false,
						Duration:      "",
					}
					a.EmitTypedEvent(ctx, fallbackInitFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
					continue
				}

				origLLM := a.LLM
				a.LLM = fallbackLLM

				// Use non-streaming approach for all agents during fallback
				var fresp *llms.ContentResponse
				var ferr2 error
				fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

				a.LLM = origLLM
				a.ModelID = origModelID

				if ferr2 == nil {
					usage = extractUsageMetricsWithMessages(fresp, messages)

					// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
					a.ModelID = fallbackModelID
					a.LLM = fallbackLLM
					// Note: We don't restore origModelID and origLLM anymore

					// Emit fallback model used event
					fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(a.provider), "empty_content", time.Since(emptyContentStartTime))
					a.EmitTypedEvent(ctx, fallbackEvent)

					// Emit model change event to track the permanent model change
					modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", string(a.provider), time.Since(emptyContentStartTime))
					a.EmitTypedEvent(ctx, modelChangeEvent)

					// Emit fallback success event (replaced span-based tracing)
					fallbackSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":              turn + 1,
							"success":           true,
							"usage":             usage,
							"stage":             "generation",
							"llm_model":         fallbackModelID,    // Successful LLM model
							"fallback_provider": string(a.provider), // Provider for this fallback
							"fallback_phase":    "same_provider",    // Phase of successful fallback
							"error_type":        "empty_content",
							"operation":         "fallback_attempt",
						},
					}
					a.EmitTypedEvent(ctx, fallbackSuccessEvent)
					// Emit empty content fallback success event (replaced span-based tracing)
					emptyContentFallbackSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"turn":                turn + 1,
							"successful_fallback": fallbackModelID,
							"attempts":            i + 1,
							"successful_llm":      fallbackModelID,    // Successful LLM model
							"successful_provider": string(a.provider), // Provider for successful fallback
							"successful_phase":    "same_provider",    // Phase of successful fallback
							"error_type":          "empty_content",
							"operation":           "empty_content_fallback",
							"duration":            time.Since(emptyContentStartTime).String(),
						},
					}
					a.EmitTypedEvent(ctx, emptyContentFallbackSuccessEvent)
					sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, string(a.provider)))
					return fresp, nil, usage
				}

				// Emit fallback attempt failure event
				fallbackAttemptFailureEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(sameProviderFallbacks),
					fallbackModelID, string(a.provider), "same_provider",
					false, time.Since(emptyContentStartTime), ferr2.Error(),
				)
				a.EmitTypedEvent(ctx, fallbackAttemptFailureEvent)
				sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
			}

			// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (%s) fallback models...", len(crossProviderFallbacks), strings.Title(crossProviderName)))
				for i, fallbackModelID := range crossProviderFallbacks {
					// Create cross-provider fallback attempt event (replaced span-based tracing)
					crossProviderFallbackAttemptEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"llm_model":         fallbackModelID,  // Explicit LLM model identifier
							"fallback_provider": "openai",         // Provider for this fallback
							"fallback_phase":    "cross_provider", // Phase of fallback
							"total_fallbacks":   len(crossProviderFallbacks),
							"error_type":        "empty_content",
							"operation":         "fallback_attempt",
						},
					}
					a.EmitTypedEvent(ctx, crossProviderFallbackAttemptEvent)

					sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", strings.Title(crossProviderName), i+1, len(crossProviderFallbacks), fallbackModelID))

					origModelID := a.ModelID
					a.ModelID = fallbackModelID
					fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
					if ferr != nil {
						a.ModelID = origModelID
						// Emit cross-provider fallback initialization failure event (replaced span-based tracing)
						crossProviderFallbackInitFailureEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           false,
								"error":             ferr.Error(),
								"stage":             "initialization",
								"fallback_model":    fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"error_type":        "empty_content",
								"operation":         "fallback_attempt",
							},
						}
						a.EmitTypedEvent(ctx, crossProviderFallbackInitFailureEvent)
						sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
						continue
					}

					origLLM := a.LLM
					a.LLM = fallbackLLM

					// Use non-streaming approach for all agents during fallback
					var fresp *llms.ContentResponse
					var ferr2 error
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

					a.LLM = origLLM
					a.ModelID = origModelID

					if ferr2 == nil {
						usage = extractUsageMetricsWithMessages(fresp, messages)

						// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
						a.ModelID = fallbackModelID
						a.LLM = fallbackLLM
						// Note: We don't restore origModelID and origLLM anymore

						// Emit fallback model used event
						fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, "openai", "empty_content", time.Since(emptyContentStartTime))
						a.EmitTypedEvent(ctx, fallbackEvent)

						// Emit model change event to track the permanent model change
						modelChangeEvent := events.NewModelChangeEvent(turn, origModelID, fallbackModelID, "fallback_success", "openai", time.Since(emptyContentStartTime))
						a.EmitTypedEvent(ctx, modelChangeEvent)

						// Emit cross-provider fallback success event (replaced span-based tracing)
						crossProviderFallbackSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":              turn + 1,
								"success":           true,
								"usage":             usage,
								"stage":             "generation",
								"llm_model":         fallbackModelID,  // Successful LLM model
								"fallback_provider": "openai",         // Provider for successful fallback
								"fallback_phase":    "cross_provider", // Phase of successful fallback
								"error_type":        "empty_content",
								"operation":         "fallback_attempt",
							},
						}
						a.EmitTypedEvent(ctx, crossProviderFallbackSuccessEvent)
						// Emit empty content cross-provider fallback success event (replaced span-based tracing)
						emptyContentCrossProviderSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"turn":                turn + 1,
								"successful_fallback": fallbackModelID,
								"attempts":            i + 1,
								"successful_llm":      fallbackModelID,  // Successful LLM model
								"successful_provider": "openai",         // Provider for successful fallback
								"successful_phase":    "cross_provider", // Phase of successful fallback
								"error_type":          "empty_content",
								"operation":           "empty_content_fallback",
								"duration":            time.Since(emptyContentStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, emptyContentCrossProviderSuccessEvent)
						sendMessage(fmt.Sprintf("\n✅ Fallback LLM succeeded: %s (%s) - Model updated permanently", fallbackModelID, strings.Title(crossProviderName)))
						return fresp, nil, usage
					}

					// Emit cross-provider fallback attempt failure event
					crossProviderFallbackFailureEvent := events.NewFallbackAttemptEvent(
						turn, i+1, len(crossProviderFallbacks),
						fallbackModelID, crossProviderName, fmt.Sprintf("cross_provider_%s", crossProviderName),
						false, time.Since(emptyContentStartTime), ferr2.Error(),
					)
					a.EmitTypedEvent(ctx, crossProviderFallbackFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
				}
			}

			// Provide a detailed summary of all failed fallback attempts
			sendMessage(fmt.Sprintf("\n❌ All fallback models failed for empty content error (turn %d):", turn))
			sendMessage(fmt.Sprintf("   - Tried %d %s models: %v", len(sameProviderFallbacks), string(a.provider), sameProviderFallbacks))
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("   - Tried %d OpenAI models: %v", len(crossProviderFallbacks), crossProviderFallbacks))
			}
			sendMessage(fmt.Sprintf("   - Original error: %v", err))
			sendMessage(fmt.Sprintf("   - Suggestion: Try rephrasing your question or providing more context"))

			// Emit empty content fallback all failed event (replaced span-based tracing)
			emptyContentAllFailedEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"turn":                    turn + 1,
					"all_fallbacks_failed":    true,
					"same_provider_attempts":  len(sameProviderFallbacks),
					"cross_provider_attempts": len(crossProviderFallbacks),
					"failed_models":           append(sameProviderFallbacks, crossProviderFallbacks...),
					"error_type":              "empty_content",
					"operation":               "empty_content_fallback",
					"duration":                time.Since(emptyContentStartTime).String(),
					"final_error":             err.Error(),
				},
			}
			a.EmitTypedEvent(ctx, emptyContentAllFailedEvent)
			lastErr = fmt.Errorf("all fallback models failed for empty content error: %v", err)
			break
		}

		// Handle connection/network errors with fallback models
		if isConnectionError(err) {
			// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events
			if a.AgentMode == ReActAgent && a.reasoningTracker != nil {
				a.reasoningTracker.Reset()
			}

			// Track connection error start time
			connectionErrorStartTime := time.Now()

			// Emit connection error detected event
			connectionErrorEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(connectionErrorStartTime))
			a.EmitTypedEvent(ctx, connectionErrorEvent)

			// Create connection error fallback event
			connectionErrorFallbackEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"error_type":               "connection_error",
					"original_error":           err.Error(),
					"same_provider_fallbacks":  len(sameProviderFallbacks),
					"cross_provider_fallbacks": len(crossProviderFallbacks),
					"turn":                     turn,
					"attempt":                  attempt + 1,
					"operation":                "connection_error_fallback",
				},
			}
			a.EmitTypedEvent(ctx, connectionErrorFallbackEvent)

			sendMessage(fmt.Sprintf("\n⚠️ Connection/network error detected (turn %d, attempt %d/%d). Trying fallback models...", turn, attempt+1, maxRetries))

			// Phase 1: Try same-provider fallbacks first
			sendMessage(fmt.Sprintf("\n🔄 Phase 1: Trying %d same-provider (%s) fallback models...", len(sameProviderFallbacks), string(a.provider)))
			for i, fallbackModelID := range sameProviderFallbacks {
				// Create connection error fallback attempt event
				connectionErrorFallbackAttemptEvent := &events.GenericEventData{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					Data: map[string]interface{}{
						"fallback_index":    i + 1,
						"fallback_model":    fallbackModelID,
						"llm_model":         fallbackModelID,
						"fallback_provider": string(a.provider),
						"fallback_phase":    "same_provider",
						"total_fallbacks":   len(sameProviderFallbacks),
						"error_type":        "connection_error",
						"operation":         "connection_error_fallback_attempt",
					},
				}
				a.EmitTypedEvent(ctx, connectionErrorFallbackAttemptEvent)

				sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", string(a.provider), i+1, len(sameProviderFallbacks), fallbackModelID))

				origModelID := a.ModelID
				a.ModelID = fallbackModelID
				fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
				if ferr != nil {
					a.ModelID = origModelID
					// Emit fallback initialization failure event
					fallbackInitFailureEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"fallback_provider": string(a.provider),
							"fallback_phase":    "same_provider",
							"error_type":        "connection_error",
							"operation":         "connection_error_fallback_init_failure",
							"init_error":        ferr.Error(),
						},
					}
					a.EmitTypedEvent(ctx, fallbackInitFailureEvent)
					sendMessage(fmt.Sprintf("\n❌ Failed to initialize fallback model %s: %v", fallbackModelID, ferr))
					continue
				}

				origLLM := a.LLM
				a.LLM = fallbackLLM

				// Use non-streaming approach for all agents during fallback
				var fresp *llms.ContentResponse
				var ferr2 error
				fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

				a.LLM = origLLM
				a.ModelID = origModelID

				if ferr2 == nil {
					usage = extractUsageMetricsWithMessages(fresp, messages)
					sendMessage(fmt.Sprintf("\n✅ Connection error fallback succeeded with %s model: %s", string(a.provider), fallbackModelID))

					// Emit fallback model used event
					fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, string(a.provider), "connection_error", time.Since(connectionErrorStartTime))
					a.EmitTypedEvent(ctx, fallbackEvent)

					// Emit connection error fallback success event
					connectionErrorSuccessEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"fallback_provider": string(a.provider),
							"fallback_phase":    "same_provider",
							"error_type":        "connection_error",
							"operation":         "connection_error_fallback_success",
							"duration":          time.Since(connectionErrorStartTime).String(),
						},
					}
					a.EmitTypedEvent(ctx, connectionErrorSuccessEvent)
					return fresp, nil, usage
				} else {
					sendMessage(fmt.Sprintf("\n❌ Connection error fallback model %s failed: %v", fallbackModelID, ferr2))
				}
			}

			// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (%s) fallback models...", len(crossProviderFallbacks), strings.Title(crossProviderName)))
				for i, fallbackModelID := range crossProviderFallbacks {
					// Create cross-provider connection error fallback attempt event
					crossProviderConnectionErrorFallbackEvent := &events.GenericEventData{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						Data: map[string]interface{}{
							"fallback_index":    i + 1,
							"fallback_model":    fallbackModelID,
							"llm_model":         fallbackModelID,
							"fallback_provider": "openai",
							"fallback_phase":    "cross_provider",
							"total_fallbacks":   len(crossProviderFallbacks),
							"error_type":        "connection_error",
							"operation":         "connection_error_cross_provider_fallback_attempt",
						},
					}
					a.EmitTypedEvent(ctx, crossProviderConnectionErrorFallbackEvent)

					sendMessage(fmt.Sprintf("\n🔄 Trying %s fallback model %d/%d: %s", strings.Title(crossProviderName), i+1, len(crossProviderFallbacks), fallbackModelID))

					// Track fallback attempt start time
					fallbackStartTime := time.Now()

					origModelID := a.ModelID
					a.ModelID = fallbackModelID
					fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
					if ferr != nil {
						a.ModelID = origModelID
						// Emit cross-provider fallback initialization failure event
						crossProviderInitFailureEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"fallback_index":    i + 1,
								"fallback_model":    fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"error_type":        "connection_error",
								"operation":         "connection_error_cross_provider_fallback_init_failure",
								"init_error":        ferr.Error(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderInitFailureEvent)
						sendMessage(fmt.Sprintf("\n❌ Failed to initialize cross-provider fallback model %s: %v", fallbackModelID, ferr))
						continue
					}

					origLLM := a.LLM
					a.LLM = fallbackLLM

					// Use non-streaming approach for all agents during fallback
					var fresp *llms.ContentResponse
					var ferr2 error
					fresp, ferr2 = a.LLM.GenerateContent(ctx, messages, opts...)

					a.LLM = origLLM
					a.ModelID = origModelID

					if ferr2 == nil {
						usage = extractUsageMetricsWithMessages(fresp, messages)
						sendMessage(fmt.Sprintf("\n✅ Connection error cross-provider fallback succeeded with OpenAI model: %s", fallbackModelID))

						// Emit fallback model used event
						fallbackEvent := events.NewFallbackModelUsedEvent(turn, origModelID, fallbackModelID, "openai", "connection_error", time.Since(fallbackStartTime))
						a.EmitTypedEvent(ctx, fallbackEvent)

						// Emit cross-provider connection error fallback success event
						crossProviderConnectionErrorSuccessEvent := &events.GenericEventData{
							BaseEventData: events.BaseEventData{
								Timestamp: time.Now(),
							},
							Data: map[string]interface{}{
								"fallback_index":    i + 1,
								"fallback_model":    fallbackModelID,
								"fallback_provider": "openai",
								"fallback_phase":    "cross_provider",
								"error_type":        "connection_error",
								"operation":         "connection_error_cross_provider_fallback_success",
								"duration":          time.Since(fallbackStartTime).String(),
							},
						}
						a.EmitTypedEvent(ctx, crossProviderConnectionErrorSuccessEvent)
						return fresp, nil, usage
					} else {
						sendMessage(fmt.Sprintf("\n❌ Connection error cross-provider fallback model %s failed: %v", fallbackModelID, ferr2))
					}
				}
			}

			// Provide a detailed summary of all failed fallback attempts
			sendMessage(fmt.Sprintf("\n❌ All fallback models failed for connection error (turn %d):", turn))
			sendMessage(fmt.Sprintf("   - Tried %d %s models: %v", len(sameProviderFallbacks), string(a.provider), sameProviderFallbacks))
			if len(crossProviderFallbacks) > 0 {
				sendMessage(fmt.Sprintf("   - Tried %d OpenAI models: %v", len(crossProviderFallbacks), crossProviderFallbacks))
			}
			sendMessage(fmt.Sprintf("   - Original error: %v", err))
			sendMessage(fmt.Sprintf("   - Suggestion: Check network connectivity and try again"))

			// Emit connection error fallback all failed event
			connectionErrorAllFailedEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{
					"turn":                    turn + 1,
					"all_fallbacks_failed":    true,
					"same_provider_attempts":  len(sameProviderFallbacks),
					"cross_provider_attempts": len(crossProviderFallbacks),
					"failed_models":           append(sameProviderFallbacks, crossProviderFallbacks...),
					"error_type":              "connection_error",
					"operation":               "connection_error_fallback",
					"duration":                time.Since(connectionErrorStartTime).String(),
					"final_error":             err.Error(),
				},
			}
			a.EmitTypedEvent(ctx, connectionErrorAllFailedEvent)
			lastErr = fmt.Errorf("all fallback models failed for connection error: %v", err)
			break
		}

		// Handle stream errors with fallback models
		if isStreamError(err) {
			resp, fallbackErr, fallbackUsage := handleErrorWithFallback(a, ctx, err, "stream_error", turn, attempt, maxRetries, sameProviderFallbacks, crossProviderFallbacks, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}
			lastErr = fallbackErr
			break
		}

		// Handle internal server errors with fallback models
		if isInternalError(err) {
			resp, fallbackErr, fallbackUsage := handleErrorWithFallback(a, ctx, err, "internal_error", turn, attempt, maxRetries, sameProviderFallbacks, crossProviderFallbacks, sendMessage, messages, opts)
			if fallbackErr == nil {
				return resp, nil, fallbackUsage
			}
			lastErr = fallbackErr
			break
		}

		// For any other errors, just return the error
		lastErr = err
		break
	}

	sendMessage(fmt.Sprintf("\n❌ LLM generation failed after %d attempts (turn %d): %v", maxRetries, turn, lastErr))
	return nil, lastErr, usage
}

// handleErrorWithFallback is a generic function that handles any error type with fallback models
func handleErrorWithFallback(a *Agent, ctx context.Context, err error, errorType string, turn int, attempt int, maxRetries int, sameProviderFallbacks, crossProviderFallbacks []string, sendMessage func(string), messages []llms.MessageContent, opts []llms.CallOption) (*llms.ContentResponse, error, observability.UsageMetrics) {
	// 🔧 FIX: Reset reasoning tracker to prevent infinite final answer events
	if a.AgentMode == ReActAgent && a.reasoningTracker != nil {
		a.reasoningTracker.Reset()
	}

	// Track error start time
	errorStartTime := time.Now()

	// Emit error detected event
	errorEvent := events.NewThrottlingDetectedEvent(turn, a.ModelID, string(a.provider), attempt+1, maxRetries, time.Since(errorStartTime))
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
		fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
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

			// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
			a.ModelID = fallbackModelID
			a.LLM = fallbackLLM

			// Emit fallback attempt event for successful attempt
			fallbackAttemptEvent := events.NewFallbackAttemptEvent(
				turn, i+1, len(sameProviderFallbacks),
				fallbackModelID, string(a.provider), "same_provider",
				true, time.Since(errorStartTime), "",
			)
			a.EmitTypedEvent(ctx, fallbackAttemptEvent)

			sendMessage(fmt.Sprintf("\n✅ %s fallback successful with %s model: %s", errorType, string(a.provider), fallbackModelID))
			return fresp, nil, usage
		} else {
			sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
		}
	}

	// Phase 2: Try cross-provider fallbacks if same-provider fallbacks failed
	if len(crossProviderFallbacks) > 0 {
		sendMessage(fmt.Sprintf("\n🔄 Phase 2: Trying %d cross-provider (openai) fallback models...", len(crossProviderFallbacks)))
		for i, fallbackModelID := range crossProviderFallbacks {
			sendMessage(fmt.Sprintf("\n🔄 Trying openai fallback model %d/%d: %s", i+1, len(crossProviderFallbacks), fallbackModelID))

			origModelID := a.ModelID
			a.ModelID = fallbackModelID
			fallbackLLM, ferr := a.createFallbackLLM(fallbackModelID)
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

				// PERMANENTLY UPDATE AGENT'S MODEL to the successful fallback
				a.ModelID = fallbackModelID
				a.LLM = fallbackLLM

				// Emit fallback attempt event for successful attempt
				fallbackAttemptEvent := events.NewFallbackAttemptEvent(
					turn, i+1, len(crossProviderFallbacks),
					fallbackModelID, "openai", "cross_provider",
					true, time.Since(errorStartTime), "",
				)
				a.EmitTypedEvent(ctx, fallbackAttemptEvent)

				sendMessage(fmt.Sprintf("\n✅ %s cross-provider fallback successful with openai model: %s", errorType, fallbackModelID))
				return fresp, nil, usage
			} else {
				sendMessage(fmt.Sprintf("\n❌ Fallback model %s failed: %v", fallbackModelID, ferr2))
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

	return nil, fmt.Errorf("all fallback models failed for %s: %v", errorType, err), observability.UsageMetrics{}
}

// createFallbackLLM creates a fallback LLM instance for the given modelID
func (a *Agent) createFallbackLLM(modelID string) (llms.Model, error) {
	// ✅ FIXED: Detect provider from model ID instead of using agent's provider
	provider := detectProviderFromModelID(modelID)

	// Log the fallback attempt with the detected provider
	logger := getLogger(a)
	logger.Infof("Creating fallback LLM using detected provider - model_id: %s, detected_provider: %s", modelID, provider)

	// Create LLM based on detected provider with better error handling
	switch provider {
	case llm.ProviderOpenAI:
		// Check for OpenAI API key
		if os.Getenv("OPENAI_API_KEY") == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required for OpenAI fallback model: %s", modelID)
		}

		llmModel, err := openai.New(openai.WithModel(modelID))
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI fallback LLM for model %s: %w", modelID, err)
		}
		return llmModel, nil

	case llm.ProviderBedrock:
		// Create Bedrock fallback LLM
		// Note: The Bedrock client has internal retries that may interfere with our fallback logic
		// but we can't disable them through the API. Our fallback logic will still work
		// when the client gives up after its internal retries.
		llmModel, err := bedrock.New(bedrock.WithModel(modelID))
		if err != nil {
			return nil, fmt.Errorf("failed to create Bedrock fallback LLM for model %s: %w", modelID, err)
		}
		return llmModel, nil

	case llm.ProviderOpenRouter:
		// Check for OpenRouter API key
		if os.Getenv("OPEN_ROUTER_API_KEY") == "" {
			return nil, fmt.Errorf("OPEN_ROUTER_API_KEY environment variable is required for OpenRouter fallback model: %s", modelID)
		}

		llmModel, err := openai.New(
			openai.WithModel(modelID),
			openai.WithBaseURL("https://openrouter.ai/api/v1"),
			openai.WithToken(os.Getenv("OPEN_ROUTER_API_KEY")),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenRouter fallback LLM for model %s: %w", modelID, err)
		}
		return llmModel, nil

	default:
		return nil, fmt.Errorf("unsupported provider '%s' for fallback model: %s", provider, modelID)
	}
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

	// OpenRouter models: various model names with "/" separator
	if strings.Contains(modelID, "/") {
		return llm.ProviderOpenRouter
	}

	// Default to Bedrock for unknown models (conservative approach)
	return llm.ProviderBedrock
}
