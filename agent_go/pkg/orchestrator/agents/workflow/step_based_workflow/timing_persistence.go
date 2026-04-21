package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

//nolint:unused // staged for the run-metadata timing persistence rollout.
type persistedToolCallTiming struct {
	ToolCallID             string `json:"tool_call_id,omitempty"`
	ToolName               string `json:"tool_name"`
	Status                 string `json:"status"`
	Args                   string `json:"args,omitempty"`
	Result                 string `json:"result,omitempty"`
	Error                  string `json:"error,omitempty"`
	DurationNs             int64  `json:"duration_ns,omitempty"`
	DurationMs             int64  `json:"duration_ms,omitempty"`
	Timestamp              string `json:"timestamp,omitempty"`
	StartedAt              string `json:"started_at,omitempty"`
	CompletedAt            string `json:"completed_at,omitempty"`
	OffsetFromAgentStartMs int64  `json:"offset_from_agent_start_ms,omitempty"`
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
type persistedToolTimingSummary struct {
	Count           int                       `json:"count"`
	SuccessfulCount int                       `json:"successful_count"`
	ErroredCount    int                       `json:"errored_count"`
	TotalDurationNs int64                     `json:"total_duration_ns"`
	TotalDurationMs int64                     `json:"total_duration_ms"`
	MaxDurationNs   int64                     `json:"max_duration_ns,omitempty"`
	MaxDurationMs   int64                     `json:"max_duration_ms,omitempty"`
	AvgDurationMs   int64                     `json:"avg_duration_ms,omitempty"`
	Calls           []persistedToolCallTiming `json:"calls,omitempty"`
}

type persistedLLMCallTiming struct {
	Turn                   int     `json:"turn,omitempty"`
	ModelID                string  `json:"model_id,omitempty"`
	Status                 string  `json:"status"`
	Error                  string  `json:"error,omitempty"`
	StartedAt              string  `json:"started_at,omitempty"`
	CompletedAt            string  `json:"completed_at,omitempty"`
	DurationNs             int64   `json:"duration_ns,omitempty"`
	DurationMs             int64   `json:"duration_ms,omitempty"`
	TimeToFirstResponseMs  int64   `json:"time_to_first_response_ms,omitempty"`
	TimeToFirstContentMs   int64   `json:"time_to_first_content_ms,omitempty"`
	TimeToFirstToolCallMs  int64   `json:"time_to_first_tool_call_ms,omitempty"`
	FirstResponseAt        string  `json:"first_response_at,omitempty"`
	FirstContentAt         string  `json:"first_content_at,omitempty"`
	FirstToolCallAt        string  `json:"first_tool_call_at,omitempty"`
	PromptTokens           int     `json:"prompt_tokens,omitempty"`
	CompletionTokens       int     `json:"completion_tokens,omitempty"`
	TotalTokens            int     `json:"total_tokens,omitempty"`
	CacheTokens            int     `json:"cache_tokens,omitempty"`
	ReasoningTokens        int     `json:"reasoning_tokens,omitempty"`
	ToolCalls              int     `json:"tool_calls,omitempty"`
	ContextUsagePercent    float64 `json:"context_usage_percent,omitempty"`
	ModelContextWindow     int     `json:"model_context_window,omitempty"`
	FixedThresholdPercent  float64 `json:"fixed_threshold_percent,omitempty"`
	OffsetFromAgentStartMs int64   `json:"offset_from_agent_start_ms,omitempty"`
}

type persistedLLMTimingSummary struct {
	Count                 int                      `json:"count"`
	SuccessfulCount       int                      `json:"successful_count"`
	ErroredCount          int                      `json:"errored_count"`
	CanceledCount         int                      `json:"canceled_count"`
	TotalDurationNs       int64                    `json:"total_duration_ns"`
	TotalDurationMs       int64                    `json:"total_duration_ms"`
	MaxDurationNs         int64                    `json:"max_duration_ns,omitempty"`
	MaxDurationMs         int64                    `json:"max_duration_ms,omitempty"`
	AvgDurationMs         int64                    `json:"avg_duration_ms,omitempty"`
	FirstStartAt          string                   `json:"first_start_at,omitempty"`
	FirstStartOffsetMs    int64                    `json:"first_start_offset_ms,omitempty"`
	FirstResponseAt       string                   `json:"first_response_at,omitempty"`
	TimeToFirstResponseMs int64                    `json:"time_to_first_response_ms,omitempty"`
	FirstContentAt        string                   `json:"first_content_at,omitempty"`
	TimeToFirstContentMs  int64                    `json:"time_to_first_content_ms,omitempty"`
	FirstToolCallAt       string                   `json:"first_tool_call_at,omitempty"`
	TimeToFirstToolCallMs int64                    `json:"time_to_first_tool_call_ms,omitempty"`
	Calls                 []persistedLLMCallTiming `json:"calls,omitempty"`
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func durationToMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func formatRFC3339UTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func normalizeToolTimingEntries(toolCalls []orchestrator.ToolCallEntry, agentStartedAt time.Time) persistedToolTimingSummary {
	summary := persistedToolTimingSummary{
		Calls: make([]persistedToolCallTiming, 0, len(toolCalls)),
	}

	for _, call := range toolCalls {
		durationNs := int64(call.Duration)
		durationMs := durationToMillis(call.Duration)
		status := "success"
		if strings.TrimSpace(call.Error) != "" {
			status = "error"
			summary.ErroredCount++
		} else {
			summary.SuccessfulCount++
		}

		summary.Count++
		summary.TotalDurationNs += durationNs
		summary.TotalDurationMs += durationMs
		if durationNs > summary.MaxDurationNs {
			summary.MaxDurationNs = durationNs
			summary.MaxDurationMs = durationMs
		}

		summary.Calls = append(summary.Calls, persistedToolCallTiming{
			ToolCallID:             call.ToolCallID,
			ToolName:               call.ToolName,
			Status:                 status,
			Args:                   call.Args,
			Result:                 call.Result,
			Error:                  call.Error,
			DurationNs:             durationNs,
			DurationMs:             durationMs,
			Timestamp:              formatRFC3339UTC(call.Timestamp),
			StartedAt:              formatRFC3339UTC(call.StartedAt),
			CompletedAt:            formatRFC3339UTC(call.CompletedAt),
			OffsetFromAgentStartMs: offsetFrom(agentStartedAt, call.StartedAt),
		})
	}

	if summary.Count > 0 {
		summary.AvgDurationMs = summary.TotalDurationMs / int64(summary.Count)
	}

	return summary
}

func normalizeLLMTimingEntries(llmCalls []orchestrator.LLMCallEntry, agentStartedAt time.Time) persistedLLMTimingSummary {
	summary := persistedLLMTimingSummary{
		Calls: make([]persistedLLMCallTiming, 0, len(llmCalls)),
	}

	var firstStart time.Time
	var firstResponse time.Time
	var firstContent time.Time
	var firstToolCall time.Time

	for _, call := range llmCalls {
		durationNs := int64(call.Duration)
		durationMs := durationToMillis(call.Duration)

		switch call.Status {
		case "error":
			summary.ErroredCount++
		case "canceled":
			summary.CanceledCount++
		default:
			summary.SuccessfulCount++
		}

		summary.Count++
		summary.TotalDurationNs += durationNs
		summary.TotalDurationMs += durationMs
		if durationNs > summary.MaxDurationNs {
			summary.MaxDurationNs = durationNs
			summary.MaxDurationMs = durationMs
		}

		if !call.StartedAt.IsZero() && (firstStart.IsZero() || call.StartedAt.Before(firstStart)) {
			firstStart = call.StartedAt
		}
		if !call.FirstResponseAt.IsZero() && (firstResponse.IsZero() || call.FirstResponseAt.Before(firstResponse)) {
			firstResponse = call.FirstResponseAt
		}
		if !call.FirstContentAt.IsZero() && (firstContent.IsZero() || call.FirstContentAt.Before(firstContent)) {
			firstContent = call.FirstContentAt
		}
		if !call.FirstToolCallAt.IsZero() && (firstToolCall.IsZero() || call.FirstToolCallAt.Before(firstToolCall)) {
			firstToolCall = call.FirstToolCallAt
		}

		summary.Calls = append(summary.Calls, persistedLLMCallTiming{
			Turn:                   call.Turn,
			ModelID:                call.ModelID,
			Status:                 call.Status,
			Error:                  call.Error,
			StartedAt:              formatRFC3339UTC(call.StartedAt),
			CompletedAt:            formatRFC3339UTC(call.CompletedAt),
			DurationNs:             durationNs,
			DurationMs:             durationMs,
			TimeToFirstResponseMs:  durationToMillis(call.TimeToFirstResponse),
			TimeToFirstContentMs:   durationToMillis(call.TimeToFirstContent),
			TimeToFirstToolCallMs:  durationToMillis(call.TimeToFirstToolCall),
			FirstResponseAt:        formatRFC3339UTC(call.FirstResponseAt),
			FirstContentAt:         formatRFC3339UTC(call.FirstContentAt),
			FirstToolCallAt:        formatRFC3339UTC(call.FirstToolCallAt),
			PromptTokens:           call.PromptTokens,
			CompletionTokens:       call.CompletionTokens,
			TotalTokens:            call.TotalTokens,
			CacheTokens:            call.CacheTokens,
			ReasoningTokens:        call.ReasoningTokens,
			ToolCalls:              call.ToolCalls,
			ContextUsagePercent:    call.ContextUsagePercent,
			ModelContextWindow:     call.ModelContextWindow,
			FixedThresholdPercent:  call.FixedThresholdPercent,
			OffsetFromAgentStartMs: offsetFrom(agentStartedAt, call.StartedAt),
		})
	}

	if summary.Count > 0 {
		summary.AvgDurationMs = summary.TotalDurationMs / int64(summary.Count)
	}
	if !firstStart.IsZero() {
		summary.FirstStartAt = formatRFC3339UTC(firstStart)
		summary.FirstStartOffsetMs = offsetFrom(agentStartedAt, firstStart)
	}
	if !firstResponse.IsZero() {
		summary.FirstResponseAt = formatRFC3339UTC(firstResponse)
		summary.TimeToFirstResponseMs = offsetFrom(agentStartedAt, firstResponse)
	}
	if !firstContent.IsZero() {
		summary.FirstContentAt = formatRFC3339UTC(firstContent)
		summary.TimeToFirstContentMs = offsetFrom(agentStartedAt, firstContent)
	}
	if !firstToolCall.IsZero() {
		summary.FirstToolCallAt = formatRFC3339UTC(firstToolCall)
		summary.TimeToFirstToolCallMs = offsetFrom(agentStartedAt, firstToolCall)
	}

	return summary
}

func offsetFrom(start time.Time, target time.Time) int64 {
	if start.IsZero() || target.IsZero() || target.Before(start) {
		return 0
	}
	return target.Sub(start).Milliseconds()
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func workflowRunMetadataPath(runFolder string) string {
	cleaned := strings.TrimSpace(filepath.ToSlash(runFolder))
	cleaned = strings.TrimPrefix(cleaned, "../")
	cleaned = strings.TrimPrefix(cleaned, "/")
	switch {
	case cleaned == "":
		return ""
	case strings.HasPrefix(cleaned, "evaluation/runs/"):
		return filepath.Join(cleaned, "run_metadata.json")
	default:
		return filepath.Join("runs", cleaned, "run_metadata.json")
	}
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func (hcpo *StepBasedWorkflowOrchestrator) upsertRunMetadata(ctx context.Context, runFolder string, mutate func(map[string]interface{})) {
	metadataPath := workflowRunMetadataPath(runFolder)
	if metadataPath == "" {
		return
	}

	writeCtx := ctx
	if writeCtx == nil || writeCtx.Err() != nil {
		writeCtx = context.Background()
	}

	meta := make(map[string]interface{})
	if existing, err := hcpo.ReadWorkspaceFile(writeCtx, metadataPath); err == nil && strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &meta); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing run metadata %s: %v", metadataPath, err))
			meta = make(map[string]interface{})
		}
	}

	mutate(meta)

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal run metadata %s: %v", metadataPath, err))
		return
	}
	if err := hcpo.WriteWorkspaceFile(writeCtx, metadataPath, string(data)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write run metadata %s: %v", metadataPath, err))
	}
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func (hcpo *StepBasedWorkflowOrchestrator) markRunMetadataStarted(ctx context.Context, runFolder string) {
	now := time.Now().UTC()
	hcpo.upsertRunMetadata(ctx, runFolder, func(meta map[string]interface{}) {
		if _, ok := meta["created_at"]; !ok {
			meta["created_at"] = formatRFC3339UTC(now)
		}
		meta["started_at"] = formatRFC3339UTC(now)
		delete(meta, "completed_at")
		delete(meta, "duration_ms")
		meta["status"] = "running"
	})
}

//nolint:unused // staged for the run-metadata timing persistence rollout.
func (hcpo *StepBasedWorkflowOrchestrator) finalizeRunMetadata(ctx context.Context, runFolder string, status string, startedAt time.Time, completedAt time.Time) {
	hcpo.upsertRunMetadata(ctx, runFolder, func(meta map[string]interface{}) {
		if _, ok := meta["created_at"]; !ok {
			meta["created_at"] = formatRFC3339UTC(startedAt)
		}
		if _, ok := meta["started_at"]; !ok {
			meta["started_at"] = formatRFC3339UTC(startedAt)
		}
		meta["completed_at"] = formatRFC3339UTC(completedAt)
		meta["duration_ms"] = completedAt.Sub(startedAt).Milliseconds()
		meta["status"] = status
	})
}
