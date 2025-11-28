import React from 'react'

interface OrchestratorExplanationProps {
  agentMode: 'simple' | 'workflow'
}

export const OrchestratorExplanation: React.FC<OrchestratorExplanationProps> = () => {
  // Orchestrator mode removed - always return null
  return null
}
