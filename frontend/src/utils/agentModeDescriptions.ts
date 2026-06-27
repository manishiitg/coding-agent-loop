/**
 * Centralized utility for agent mode descriptions
 * This eliminates code duplication across components
 */

export type AgentMode = 'multi-agent' | 'workflow'

export const getAgentModeDescription = (agentMode: AgentMode): string => {
  switch (agentMode) {
    case 'workflow':
      return 'Todo-list-based automation execution with human verification and sequential task completion'
    case 'multi-agent':
    default:
      return 'Multi-agent chat: ask questions across multiple MCP servers, delegate to specialist sub-agents'
  }
}
