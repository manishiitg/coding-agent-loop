import React from 'react'

interface OrchestratorExplanationProps {
  agentMode: 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
}

export const OrchestratorExplanation: React.FC<OrchestratorExplanationProps> = ({ agentMode }) => {
  // Only show when in Deep Search mode
  if (agentMode !== 'orchestrator') {
    return null
  }

  return (
    <div className="flex items-center justify-center py-12">
      <div className="text-center max-w-2xl">
        {/* Main Icon */}
        <div className="w-20 h-20 mx-auto mb-6 bg-primary/10 rounded-full flex items-center justify-center">
          <svg className="w-10 h-10 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
          </svg>
        </div>

        {/* Title */}
        <h3 className="text-xl font-semibold text-foreground mb-4">
          🎯 5-Agent Deep Search System
        </h3>

        {/* Description */}
        <p className="text-sm text-muted-foreground mb-6">
          The Deep Search system uses a sophisticated 5-agent system with long-term memory to execute complex multi-step plans that can take hours to complete. <strong className="text-foreground">Fully automatic execution</strong> - runs continuously without user intervention.
        </p>

        {/* Agent Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
          {/* Planning Agent */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">🏗️</span>
              <h4 className="font-medium text-card-foreground">Planning Agent</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Creates comprehensive multi-step plans with dependencies and MCP server assignments
            </p>
          </div>

          {/* Execution Agent */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">⚡</span>
              <h4 className="font-medium text-card-foreground">Execution Agent</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Executes plans using MCP tools (AWS, GitHub, Database, Kubernetes, etc.)
            </p>
          </div>

          {/* Validation Agent */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">🔍</span>
              <h4 className="font-medium text-card-foreground">Validation Agent</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Validates results, prevents hallucinations, and ensures factual accuracy
            </p>
          </div>

          {/* Organizer Agent */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">📊</span>
              <h4 className="font-medium text-card-foreground">Organizer Agent</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Organizes and structures results, manages memory and workspace cleanup
            </p>
          </div>

          {/* Report Agent */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">📋</span>
              <h4 className="font-medium text-card-foreground">Report Agent</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Generates comprehensive reports that directly answer the original objective
            </p>
          </div>
        </div>

        {/* Key Features */}
        <div className="bg-muted border border-border rounded-lg p-4 mb-4">
          <h4 className="font-medium text-foreground mb-3 text-sm">Key Features:</h4>
          <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
            <div className="flex items-center gap-1">
              <span>🤖</span>
              <span>Fully Automatic</span>
            </div>
            <div className="flex items-center gap-1">
              <span>🧠</span>
              <span>Long-term Memory</span>
            </div>
            <div className="flex items-center gap-1">
              <span>⏱️</span>
              <span>Hours-long Execution</span>
            </div>
            <div className="flex items-center gap-1">
              <span>🔄</span>
              <span>Adaptive Planning</span>
            </div>
            <div className="flex items-center gap-1">
              <span>✅</span>
              <span>Fact-checking</span>
            </div>
            <div className="flex items-center gap-1">
              <span>⚡</span>
              <span>Continuous Operation</span>
            </div>
          </div>
        </div>

      </div>
    </div>
  )
}
