package orchestrator

import (
	"fmt"
	"time"

	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"
)

// GetLogger returns the orchestrator's logger
func (bo *BaseOrchestrator) GetLogger() loggerv2.Logger {
	return bo.logger
}

// GetStartTime returns the start time
func (bo *BaseOrchestrator) GetStartTime() time.Time {
	return bo.startTime
}

// GetOrchestratorType returns the orchestrator type
func (bo *BaseOrchestrator) GetOrchestratorType() OrchestratorType {
	return bo.orchestratorType
}

// Workflow-specific methods (only available for workflow orchestrators)
// GetObjective returns the current objective
func (bo *BaseOrchestrator) GetObjective() string {
	return bo.objective
}

// SetObjective sets the objective
func (bo *BaseOrchestrator) SetObjective(objective string) {
	bo.objective = objective
}

// GetWorkspacePath returns the current workspace path
func (bo *BaseOrchestrator) GetWorkspacePath() string {
	return bo.workspacePath
}

// SetWorkspacePath sets the workspace path
func (bo *BaseOrchestrator) SetWorkspacePath(workspacePath string) {
	bo.workspacePath = workspacePath
}

// SetWorkspacePathForFolderGuard sets separate read and write paths for folder guard validation
// If both arrays are empty, folder guard validation is disabled (allows all paths)
func (bo *BaseOrchestrator) SetWorkspacePathForFolderGuard(readPaths []string, writePaths []string) {
	if len(readPaths) == 0 && len(writePaths) == 0 {
		// Empty arrays disable folder guard
		bo.folderGuardReadPaths = nil
		bo.folderGuardWritePaths = nil
		bo.GetLogger().Info("🔓 Folder guard disabled (empty read/write paths)")
	} else {
		bo.folderGuardReadPaths = readPaths
		bo.folderGuardWritePaths = writePaths
		bo.GetLogger().Info(fmt.Sprintf("🔒 Folder guard enabled - Read paths: %v, Write paths: %v", readPaths, writePaths))
	}
}

// GetFolderGuardPaths returns the current folder guard read and write paths
func (bo *BaseOrchestrator) GetFolderGuardPaths() (readPaths []string, writePaths []string) {
	return bo.folderGuardReadPaths, bo.folderGuardWritePaths
}

// GetContextAwareBridge returns the context-aware event bridge
func (bo *BaseOrchestrator) GetContextAwareBridge() mcpagent.AgentEventListener {
	return bo.contextAwareBridge
}

// GetMCPConfigPath returns the MCP configuration path
func (bo *BaseOrchestrator) GetMCPConfigPath() string {
	return bo.mcpConfigPath
}

// GetTemperature returns the temperature setting
func (bo *BaseOrchestrator) GetTemperature() float64 {
	return bo.temperature
}

// GetAgentMode returns the agent mode
func (bo *BaseOrchestrator) GetAgentMode() string {
	return bo.agentMode
}

// GetSelectedServers returns the selected servers
func (bo *BaseOrchestrator) GetSelectedServers() []string {
	return bo.selectedServers
}

// GetSelectedTools returns the selected tools
func (bo *BaseOrchestrator) GetSelectedTools() []string {
	return bo.selectedTools
}

// GetUseCodeExecutionMode returns the code execution mode setting
func (bo *BaseOrchestrator) GetUseCodeExecutionMode() bool {
	return bo.useCodeExecutionMode
}

// GetUseToolSearchMode returns the tool search mode setting
func (bo *BaseOrchestrator) GetUseToolSearchMode() bool {
	return bo.useToolSearchMode
}

// GetPreDiscoveredTools returns the pre-discovered tools list
func (bo *BaseOrchestrator) GetPreDiscoveredTools() []string {
	return bo.preDiscoveredTools
}

// GetLLMConfig returns the LLM configuration
func (bo *BaseOrchestrator) GetLLMConfig() *LLMConfig {
	return bo.llmConfig
}

// GetAPIKeys safely returns the API keys from the LLM configuration
// Returns nil if llmConfig is nil (prevents nil pointer dereference)
func (bo *BaseOrchestrator) GetAPIKeys() *APIKeys {
	if bo.llmConfig == nil {
		return nil
	}
	return bo.llmConfig.APIKeys
}

// GetFallbacks safely returns the fallback LLMs from the LLM configuration
// Returns nil if llmConfig is nil (prevents nil pointer dereference)
func (bo *BaseOrchestrator) GetFallbacks() []LLMModel {
	if bo.llmConfig == nil {
		return nil
	}
	return bo.llmConfig.Fallbacks
}

// GetTracer returns the tracer (not implemented - orchestrator doesn't have its own tracer)
func (bo *BaseOrchestrator) GetTracer() observability.Tracer {
	// Orchestrators don't have their own tracer - they coordinate agents that have tracers
	return nil
}

// GetMaxTurns returns the maximum turns for the orchestrator
func (bo *BaseOrchestrator) GetMaxTurns() int {
	return bo.maxTurns
}

// GetType returns the orchestrator type
func (bo *BaseOrchestrator) GetType() string {
	return string(bo.orchestratorType)
}

// SetIterationFolder sets the iteration folder and automatically applies it to the context-aware bridge
// This ensures all agents created by this orchestrator automatically get the iteration folder for token persistence
func (bo *BaseOrchestrator) SetIterationFolder(iterationFolder string) {
	bo.iterationFolder = iterationFolder
	bo.applyIterationFolderToBridge()
	bo.GetLogger().Info(fmt.Sprintf("📁 Set iteration folder for token persistence: %s (applied to all agents)", iterationFolder))
}

// GetIterationFolder returns the current iteration folder
func (bo *BaseOrchestrator) GetIterationFolder() string {
	return bo.iterationFolder
}

// GetEnableContextSummarization returns whether context summarization is enabled
func (bo *BaseOrchestrator) GetEnableContextSummarization() bool {
	return bo.enableContextSummarization
}

// GetSummarizeOnTokenThreshold returns whether token-based summarization is enabled
func (bo *BaseOrchestrator) GetSummarizeOnTokenThreshold() bool {
	return bo.summarizeOnTokenThreshold
}

// GetTokenThresholdPercent returns the token threshold percentage for summarization
func (bo *BaseOrchestrator) GetTokenThresholdPercent() float64 {
	return bo.tokenThresholdPercent
}

// GetSummarizeOnFixedTokenThreshold returns whether fixed token-based summarization is enabled
func (bo *BaseOrchestrator) GetSummarizeOnFixedTokenThreshold() bool {
	return bo.summarizeOnFixedTokenThreshold
}

// GetFixedTokenThreshold returns the fixed token threshold for summarization
func (bo *BaseOrchestrator) GetFixedTokenThreshold() int {
	return bo.fixedTokenThreshold
}

// GetSummaryKeepLastMessages returns the number of recent messages to keep when summarizing
func (bo *BaseOrchestrator) GetSummaryKeepLastMessages() int {
	return bo.summaryKeepLastMessages
}

// GetEnableContextEditing returns whether context editing is enabled
func (bo *BaseOrchestrator) GetEnableContextEditing() bool {
	return bo.enableContextEditing
}

// GetContextEditingThreshold returns the token threshold for context editing (0 = use default)
func (bo *BaseOrchestrator) GetContextEditingThreshold() int {
	return bo.contextEditingThreshold
}

// GetContextEditingTurnThreshold returns the turn age threshold for context editing (0 = use default)
func (bo *BaseOrchestrator) GetContextEditingTurnThreshold() int {
	return bo.contextEditingTurnThreshold
}
