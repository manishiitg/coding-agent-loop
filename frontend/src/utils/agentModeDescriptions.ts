/**
 * Centralized utility for agent mode descriptions
 * This eliminates code duplication across components
 */

export type AgentMode = 'simple' | 'ReAct' | 'orchestrator' | 'workflow'

export const getAgentModeDescription = (agentMode: AgentMode): string => {
  switch (agentMode) {
    case 'ReAct':
      return 'Step-by-step reasoning do more indepth reasoning and has access to memory.'
    case 'orchestrator':
      return 'Deep Search: Create multi-step plans with long term memory and might take hours'
    case 'workflow':
      return 'Todo-list-based workflow execution with human verification and sequential task completion'
    case 'simple':
    default:
      return 'Ask simple questions across multiple MCP servers'
  }
}
