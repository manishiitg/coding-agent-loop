/**
 * Centralized utility for agent mode descriptions
 * This eliminates code duplication across components
 */

export type AgentMode = 'simple' | 'workflow'

export const getAgentModeDescription = (agentMode: AgentMode): string => {
  switch (agentMode) {
    case 'workflow':
      return 'Todo-list-based workflow execution with human verification and sequential task completion'
    case 'simple':
    default:
      return 'Ask simple questions across multiple MCP servers'
  }
}
