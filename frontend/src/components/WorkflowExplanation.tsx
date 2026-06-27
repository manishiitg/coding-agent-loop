import React from 'react'

interface WorkflowExplanationProps {
  agentMode: 'multi-agent' | 'workflow'
  selectedWorkflowPreset?: string | null
}

export const WorkflowExplanation: React.FC<WorkflowExplanationProps> = ({ agentMode, selectedWorkflowPreset }) => {
  // Only show when in workflow mode but no preset selected
  if (agentMode !== 'workflow' || selectedWorkflowPreset) {
    return null
  }

  return (
    <div className="flex items-center justify-center py-12">
      <div className="text-center max-w-2xl">
        {/* Main Icon */}
        <div className="w-20 h-20 mx-auto mb-6 bg-primary/10 rounded-full flex items-center justify-center">
          <svg className="w-10 h-10 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
        </div>

        {/* Title */}
        <h3 className="text-xl font-semibold text-foreground mb-4">
          📋 Todo-List Automation System
        </h3>

        {/* Description */}
        <p className="text-sm text-muted-foreground mb-6">
          The automation system creates structured todo-lists with human verification and sequential task completion for complex multi-step objectives. <strong className="text-foreground">Manual verification required</strong> - stops at each step for human approval.
        </p>

        {/* Workflow Phases Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
          {/* Todo Planning */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">📝</span>
              <h4 className="font-medium text-card-foreground">Todo Planning</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Creates structured todo-lists with clear objectives and sequential steps
            </p>
          </div>

          {/* Todo Execution */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">⚡</span>
              <h4 className="font-medium text-card-foreground">Todo Execution</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Executes tasks sequentially with progress tracking and validation
            </p>
          </div>

          {/* Todo Validation */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">✅</span>
              <h4 className="font-medium text-card-foreground">Todo Validation</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Validates task completion and ensures quality before proceeding
            </p>
          </div>

          {/* Workspace Update */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-lg">📁</span>
              <h4 className="font-medium text-card-foreground">Workspace Update</h4>
            </div>
            <p className="text-xs text-muted-foreground">
              Updates workspace files and maintains organized task documentation
            </p>
          </div>
        </div>

        {/* Key Features */}
        <div className="bg-muted border border-border rounded-lg p-4 mb-4">
          <h4 className="font-medium text-foreground mb-3 text-sm">Key Features:</h4>
          <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
            <div className="flex items-center gap-1">
              <span>⏸️</span>
              <span>Manual Verification</span>
            </div>
            <div className="flex items-center gap-1">
              <span>👥</span>
              <span>Human Approval</span>
            </div>
            <div className="flex items-center gap-1">
              <span>📋</span>
              <span>Structured Todo-Lists</span>
            </div>
            <div className="flex items-center gap-1">
              <span>🔄</span>
              <span>Sequential Execution</span>
            </div>
            <div className="flex items-center gap-1">
              <span>📁</span>
              <span>Workspace Integration</span>
            </div>
            <div className="flex items-center gap-1">
              <span>🎯</span>
              <span>Step-by-Step Control</span>
            </div>
          </div>
        </div>

      </div>
    </div>
  )
}
