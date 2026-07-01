package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

type workflowCostSummary struct {
	TotalCostUSD      float64                 `json:"total_cost_usd"`
	ExecutionCostUSD  float64                 `json:"execution_cost_usd"`
	EvaluationCostUSD float64                 `json:"evaluation_cost_usd"`
	BuilderCostUSD    float64                 `json:"builder_cost_usd"`
	RunCount          int                     `json:"run_count"`
	LatestRun         *workflowRunCostSummary `json:"latest_run,omitempty"`
	TopDrivers        []workflowCostDriver    `json:"top_drivers,omitempty"`
}

type workflowRunCostSummary struct {
	RunFolder         string  `json:"run_folder"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	ExecutionCostUSD  float64 `json:"execution_cost_usd"`
	EvaluationCostUSD float64 `json:"evaluation_cost_usd"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

type workflowCostDriver struct {
	Source          string  `json:"source"`
	Kind            string  `json:"kind"`
	Name            string  `json:"name"`
	Provider        string  `json:"provider,omitempty"`
	CostUSD         float64 `json:"cost_usd"`
	InputTokens     int     `json:"input_tokens,omitempty"`
	OutputTokens    int     `json:"output_tokens,omitempty"`
	CacheReadTokens int     `json:"cache_read_tokens,omitempty"`
	ReasoningTokens int     `json:"reasoning_tokens,omitempty"`
	Calls           int     `json:"calls,omitempty"`
}

type workflowCostDriverKey struct {
	source   string
	kind     string
	name     string
	provider string
}

func buildWorkflowCostSummary(runs []workflowRunCostEntry, phaseTokenUsage *orchestrator.PhaseTokenUsageFile, phaseDailyCosts []workflowPhaseDailyCostEntry) workflowCostSummary {
	summary := workflowCostSummary{
		RunCount: len(runs),
	}

	drivers := make(map[workflowCostDriverKey]workflowCostDriver)
	for i := range runs {
		run := workflowRunCostSummaryFromEntry(runs[i])
		summary.ExecutionCostUSD += run.ExecutionCostUSD
		summary.EvaluationCostUSD += run.EvaluationCostUSD
		if summary.LatestRun == nil {
			runCopy := run
			summary.LatestRun = &runCopy
		}
		addTokenUsageCostDrivers(drivers, "execution", runs[i].TokenUsage)
		addTokenUsageCostDrivers(drivers, "evaluation", runs[i].EvaluationTokenUsage)
	}

	if phaseTokenUsage != nil {
		summary.BuilderCostUSD = phaseTokenUsageTotalCostUSD(phaseTokenUsage)
		addPhaseTokenUsageCostDrivers(drivers, "builder", phaseTokenUsage)
	} else {
		for _, entry := range phaseDailyCosts {
			summary.BuilderCostUSD += phaseTokenUsageTotalCostUSD(entry.TokenUsage)
			addPhaseTokenUsageCostDrivers(drivers, "builder", entry.TokenUsage)
		}
	}

	summary.TotalCostUSD = summary.ExecutionCostUSD + summary.EvaluationCostUSD + summary.BuilderCostUSD
	summary.TopDrivers = topWorkflowCostDrivers(drivers, 5)
	return summary
}

func workflowRunCostSummaryFromEntry(entry workflowRunCostEntry) workflowRunCostSummary {
	executionCost := orchestrator.TokenUsageTotalCostUSD(entry.TokenUsage)
	evaluationCost := orchestrator.TokenUsageTotalCostUSD(entry.EvaluationTokenUsage)
	updatedAt := workflowRunCostEntryTime(entry)
	updatedAtText := ""
	if !updatedAt.IsZero() {
		updatedAtText = updatedAt.UTC().Format(time.RFC3339)
	}
	return workflowRunCostSummary{
		RunFolder:         entry.RunFolder,
		ExecutionCostUSD:  executionCost,
		EvaluationCostUSD: evaluationCost,
		TotalCostUSD:      executionCost + evaluationCost,
		UpdatedAt:         updatedAtText,
	}
}

func phaseTokenUsageTotalCostUSD(tokenFile *orchestrator.PhaseTokenUsageFile) float64 {
	if tokenFile == nil {
		return 0
	}
	var total float64
	for _, usage := range tokenFile.ByModel {
		if usage != nil {
			total += usage.TotalCost
		}
	}
	return total
}

func addTokenUsageCostDrivers(drivers map[workflowCostDriverKey]workflowCostDriver, source string, tokenFile *orchestrator.TokenUsageFile) {
	if tokenFile == nil {
		return
	}
	for modelID, usage := range tokenFile.ByModel {
		if usage == nil || usage.TotalCost <= 0 {
			continue
		}
		name := strings.TrimSpace(modelID)
		if name == "" {
			name = "unknown model"
		}
		mergeWorkflowCostDriver(drivers, workflowCostDriver{
			Source:          source,
			Kind:            "model",
			Name:            name,
			Provider:        strings.TrimSpace(usage.Provider),
			CostUSD:         usage.TotalCost,
			InputTokens:     usage.InputTokens,
			OutputTokens:    usage.OutputTokens,
			CacheReadTokens: usage.CacheReadTokens,
			ReasoningTokens: usage.ReasoningTokens,
			Calls:           usage.LLMCallCount,
		})
	}
	for toolKey, usage := range tokenFile.ByTool {
		if usage == nil || usage.TotalCost <= 0 {
			continue
		}
		name := strings.TrimSpace(usage.ToolName)
		if name == "" {
			name = strings.TrimSpace(toolKey)
		}
		if name == "" {
			name = "unknown tool"
		}
		mergeWorkflowCostDriver(drivers, workflowCostDriver{
			Source:   source,
			Kind:     "tool",
			Name:     name,
			Provider: strings.TrimSpace(usage.Provider),
			CostUSD:  usage.TotalCost,
			Calls:    usage.Count,
		})
	}
}

func addPhaseTokenUsageCostDrivers(drivers map[workflowCostDriverKey]workflowCostDriver, source string, tokenFile *orchestrator.PhaseTokenUsageFile) {
	if tokenFile == nil {
		return
	}
	for modelID, usage := range tokenFile.ByModel {
		if usage == nil || usage.TotalCost <= 0 {
			continue
		}
		name := strings.TrimSpace(modelID)
		if name == "" {
			name = "unknown model"
		}
		mergeWorkflowCostDriver(drivers, workflowCostDriver{
			Source:          source,
			Kind:            "model",
			Name:            name,
			Provider:        strings.TrimSpace(usage.Provider),
			CostUSD:         usage.TotalCost,
			InputTokens:     usage.InputTokens,
			OutputTokens:    usage.OutputTokens,
			CacheReadTokens: usage.CacheReadTokens,
			ReasoningTokens: usage.ReasoningTokens,
			Calls:           usage.LLMCallCount,
		})
	}
}

func mergeWorkflowCostDriver(drivers map[workflowCostDriverKey]workflowCostDriver, driver workflowCostDriver) {
	key := workflowCostDriverKey{
		source:   driver.Source,
		kind:     driver.Kind,
		name:     driver.Name,
		provider: driver.Provider,
	}
	existing := drivers[key]
	existing.Source = driver.Source
	existing.Kind = driver.Kind
	existing.Name = driver.Name
	existing.Provider = driver.Provider
	existing.CostUSD += driver.CostUSD
	existing.InputTokens += driver.InputTokens
	existing.OutputTokens += driver.OutputTokens
	existing.CacheReadTokens += driver.CacheReadTokens
	existing.ReasoningTokens += driver.ReasoningTokens
	existing.Calls += driver.Calls
	drivers[key] = existing
}

func topWorkflowCostDrivers(drivers map[workflowCostDriverKey]workflowCostDriver, limit int) []workflowCostDriver {
	if len(drivers) == 0 || limit <= 0 {
		return nil
	}
	result := make([]workflowCostDriver, 0, len(drivers))
	for _, driver := range drivers {
		if driver.CostUSD > 0 {
			result = append(result, driver)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CostUSD != result[j].CostUSD {
			return result[i].CostUSD > result[j].CostUSD
		}
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Name < result[j].Name
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func workflowRunCostSummaryForFolder(runs []workflowRunCostEntry, runFolder string) (*workflowRunCostSummary, []workflowCostDriver) {
	if len(runs) == 0 {
		return nil, nil
	}
	var selected *workflowRunCostEntry
	for i := range runs {
		if workflowRunFolderMatches(runs[i].RunFolder, runFolder) {
			selected = &runs[i]
			break
		}
	}
	if selected == nil {
		selected = &runs[0]
	}
	summary := workflowRunCostSummaryFromEntry(*selected)
	if summary.TotalCostUSD <= 0 {
		return &summary, nil
	}
	drivers := make(map[workflowCostDriverKey]workflowCostDriver)
	addTokenUsageCostDrivers(drivers, "execution", selected.TokenUsage)
	addTokenUsageCostDrivers(drivers, "evaluation", selected.EvaluationTokenUsage)
	return &summary, topWorkflowCostDrivers(drivers, 1)
}

func workflowCostNotificationMetadata(ctx context.Context, workspacePath, runFolder string) map[string]string {
	workspacePath = strings.TrimSpace(workspacePath)
	runFolder = strings.TrimSpace(runFolder)
	if workspacePath == "" || runFolder == "" {
		return nil
	}

	costs := loadWorkflowCosts(ctx, workspacePath)
	runSummary, topDrivers := workflowRunCostSummaryForFolder(costs.Runs, runFolder)
	if runSummary == nil || runSummary.TotalCostUSD <= 0 {
		return nil
	}

	meta := map[string]string{
		"cost_total_usd":      fmt.Sprintf("%.6f", runSummary.TotalCostUSD),
		"cost_execution_usd":  fmt.Sprintf("%.6f", runSummary.ExecutionCostUSD),
		"cost_evaluation_usd": fmt.Sprintf("%.6f", runSummary.EvaluationCostUSD),
		"cost_summary":        formatWorkflowRunCostNotification(*runSummary, topDrivers),
	}
	return meta
}

func mergeBackgroundAgentMetadata(base, update map[string]string) map[string]string {
	if len(base) == 0 && len(update) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(update))
	for k, v := range base {
		if strings.TrimSpace(k) != "" {
			merged[k] = v
		}
	}
	for k, v := range update {
		if strings.TrimSpace(k) != "" {
			merged[k] = v
		}
	}
	return merged
}

func autoNotificationCostSummary(meta map[string]string) string {
	if meta == nil {
		return ""
	}
	summary := strings.TrimSpace(meta["cost_summary"])
	if summary == "" {
		return ""
	}
	return "\n" + summary
}

func autoNotificationCostSummaryFromMetadata(ctx context.Context, meta map[string]string) string {
	if summary := autoNotificationCostSummary(meta); summary != "" {
		return summary
	}
	if meta == nil {
		return ""
	}
	return autoNotificationCostSummary(workflowCostNotificationMetadata(ctx, meta["workflow_path"], meta["run_folder"]))
}

func formatWorkflowRunCostNotification(summary workflowRunCostSummary, topDrivers []workflowCostDriver) string {
	parts := []string{
		fmt.Sprintf("total %s", formatWorkflowCostUSD(summary.TotalCostUSD)),
		fmt.Sprintf("execution %s", formatWorkflowCostUSD(summary.ExecutionCostUSD)),
	}
	if summary.EvaluationCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("evaluation %s", formatWorkflowCostUSD(summary.EvaluationCostUSD)))
	}
	line := "Cost: " + strings.Join(parts, ", ")
	if len(topDrivers) > 0 {
		driver := topDrivers[0]
		line += fmt.Sprintf(". Top driver: %s %s %s (%s)", driver.Source, driver.Kind, driver.Name, formatWorkflowCostUSD(driver.CostUSD))
	}
	return line + "."
}

func formatWorkflowCostUSD(value float64) string {
	if value <= 0 {
		return "$0"
	}
	if value < 0.01 {
		return fmt.Sprintf("$%.4f", value)
	}
	return fmt.Sprintf("$%.2f", value)
}
