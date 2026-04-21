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
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name"`
	Status     string `json:"status"`
	Args       string `json:"args,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationNs int64  `json:"duration_ns,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
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
func normalizeToolTimingEntries(toolCalls []orchestrator.ToolCallEntry) persistedToolTimingSummary {
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
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Status:     status,
			Args:       call.Args,
			Result:     call.Result,
			Error:      call.Error,
			DurationNs: durationNs,
			DurationMs: durationMs,
			Timestamp:  formatRFC3339UTC(call.Timestamp),
		})
	}

	if summary.Count > 0 {
		summary.AvgDurationMs = summary.TotalDurationMs / int64(summary.Count)
	}

	return summary
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
