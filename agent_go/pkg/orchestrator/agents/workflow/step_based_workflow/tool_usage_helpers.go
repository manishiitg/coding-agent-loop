package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// isCodeExecutionModeEnabled checks if code execution mode is enabled for a step's agent configs
func isCodeExecutionModeEnabled(agentConfigs *AgentConfigs, presetCodeExecMode bool) bool {
	// If step has explicit code exec mode setting, use it
	if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
		return *agentConfigs.UseCodeExecutionMode
	}

	// Otherwise, use preset default
	return presetCodeExecMode
}

// ToolUsageEntry represents a single tool's usage statistics
type ToolUsageEntry struct {
	ToolName   string `json:"tool_name"`
	UsageCount int    `json:"usage_count"`  // Number of times tool was called
	LastUsedIn string `json:"last_used_in"` // Path to last conversation file where tool was used
	HasSuccess bool   `json:"has_success"`  // Whether tool was used in a successful execution
}

// StepToolUsageSummary represents tool usage summary for a step
type StepToolUsageSummary struct {
	StepID               string           `json:"step_id"`
	ToolsUsed            []ToolUsageEntry `json:"tools_used"`            // Tools that were actually used
	TotalExecutions      int              `json:"total_executions"`      // Total number of execution attempts found
	SuccessfulExecutions int              `json:"successful_executions"` // Number of successful executions
	HasLogs              bool             `json:"has_logs"`              // Whether any logs were found for this step
}

// extractToolsFromLogsPath reads conversation history files from a logs path and extracts tool usage
func extractToolsFromLogsPath(
	ctx context.Context,
	logsPath string,
	toolUsageMap map[string]*ToolUsageEntry,
	readFile func(context.Context, string) (string, error),
	logger loggerv2.Logger,
	summary *StepToolUsageSummary,
) {
	// Try to read conversation history files
	// Pattern: execution-attempt-{N}-iteration-{M}-conversation.json
	for attempt := 1; attempt <= 5; attempt++ {
		for iteration := 0; iteration <= 5; iteration++ {
			conversationPath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d-conversation.json", logsPath, attempt, iteration)
			content, err := readFile(ctx, conversationPath)
			if err != nil {
				continue
			}

			var conversationData map[string]interface{}
			if err := json.Unmarshal([]byte(content), &conversationData); err != nil {
				logger.Warn(fmt.Sprintf("Failed to parse conversation history from %s: %v", conversationPath, err))
				continue
			}

			convHistoryRaw, ok := conversationData["conversation_history"]
			if !ok {
				continue
			}

			convHistoryJSON, err := json.Marshal(convHistoryRaw)
			if err != nil {
				continue
			}

			var convHistory []llmtypes.MessageContent
			if err := json.Unmarshal(convHistoryJSON, &convHistory); err != nil {
				continue
			}

			toolCalls := ExtractToolCallsFromMessages(convHistory)
			summary.TotalExecutions++

			// Check if this execution was successful
			executionPath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d.json", logsPath, attempt, iteration)
			execContent, execErr := readFile(ctx, executionPath)
			isSuccess := false
			if execErr == nil {
				var execData map[string]interface{}
				if err := json.Unmarshal([]byte(execContent), &execData); err == nil {
					if execResult, ok := execData["execution_result"].(string); ok {
						isSuccess = !strings.Contains(strings.ToLower(execResult), "error") &&
							!strings.Contains(strings.ToLower(execResult), "failed") &&
							!strings.Contains(strings.ToLower(execResult), "failure")
					}
				}
			}

			if isSuccess {
				summary.SuccessfulExecutions++
			}

			for _, toolName := range toolCalls {
				if entry, exists := toolUsageMap[toolName]; exists {
					entry.UsageCount++
					if isSuccess {
						entry.HasSuccess = true
					}
					entry.LastUsedIn = conversationPath
				} else {
					toolUsageMap[toolName] = &ToolUsageEntry{
						ToolName:   toolName,
						UsageCount: 1,
						LastUsedIn: conversationPath,
						HasSuccess: isSuccess,
					}
				}
			}
		}
	}
}
