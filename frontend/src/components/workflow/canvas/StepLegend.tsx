import React, { useMemo, useState, useCallback } from 'react'
import { ChevronDown, ChevronUp, CheckCircle, XCircle, Loader2, ArrowRight } from 'lucide-react'
import type { WorkflowNode, StepNodeData, ConditionalNodeData, LoopNodeData } from '../hooks/usePlanToFlow'
import type { PlanStep } from '../../../utils/stepConfigMatching'

interface StepLegendProps {
  plan: { steps: PlanStep[] } | null
  nodes: WorkflowNode[]
  selectedNodeId: string | null
  onStepClick: (nodeId: string) => void
}

/**
 * Step Legend Component
 * Shows a collapsible list of all workflow steps at the bottom-left of the canvas
 * Allows quick navigation to any step by clicking on it
 */
export const StepLegend: React.FC<StepLegendProps> = ({
  plan,
  nodes,
  selectedNodeId,
  onStepClick
}) => {
  const [isCollapsed, setIsCollapsed] = useState(true)

  // Type guard to check if node has step data
  const hasStepData = (node: WorkflowNode): node is WorkflowNode & { data: StepNodeData | ConditionalNodeData | LoopNodeData } => {
    return (node.type === 'step' || node.type === 'conditional' || node.type === 'loop') &&
           'step' in node.data &&
           typeof (node.data as StepNodeData | ConditionalNodeData | LoopNodeData).step === 'object'
  }

  // Filter to only top-level steps (exclude nested branches, validation, learning nodes)
  const topLevelSteps = useMemo(() => {
    if (!plan?.steps || !nodes) return []

    // Create a map of step IDs to nodes for quick lookup
    const nodeMap = new Map<string, WorkflowNode>()
    nodes.forEach(node => {
      if (hasStepData(node)) {
        const stepId = node.data.step?.id || node.id
        nodeMap.set(stepId, node)
      }
    })

    // Get top-level steps (those that match plan.steps directly, not nested)
    return plan.steps
      .map((step, index) => {
        const stepId = step.id || `step-${index}`
        const node = nodeMap.get(stepId)
        
        // Only include if node exists and is not a nested branch node
        if (node && !node.id.includes('-true-') && !node.id.includes('-false-')) {
          return {
            step,
            stepIndex: index,
            node,
            nodeId: node.id
          }
        }
        return null
      })
      .filter((item): item is NonNullable<typeof item> => item !== null)
  }, [plan, nodes])

  const handleStepClick = useCallback((nodeId: string) => {
    onStepClick(nodeId)
    setIsCollapsed(true) // Minimize after navigating to step
  }, [onStepClick])

  if (!plan || topLevelSteps.length === 0) {
    return null
  }

  const getStatusIcon = (node: WorkflowNode) => {
    if (!hasStepData(node)) {
      return <ArrowRight className="w-3 h-3 text-muted-foreground" />
    }
    
    const status = node.data.status || 'pending'
    switch (status) {
      case 'running':
        return <Loader2 className="w-3 h-3 text-blue-500 animate-spin" />
      case 'completed':
        return <CheckCircle className="w-3 h-3 text-green-500" />
      case 'failed':
        return <XCircle className="w-3 h-3 text-red-500" />
      default:
        return <ArrowRight className="w-3 h-3 text-muted-foreground" />
    }
  }

  return (
    <div className="absolute bottom-4 left-4 z-10 w-64">
      <div className="bg-background/98 dark:bg-gray-900/98 backdrop-blur-md rounded-lg border border-border shadow-xl">
        {/* Header */}
        <button
          onClick={() => setIsCollapsed(!isCollapsed)}
          className="w-full px-3 py-1.5 flex items-center justify-between text-xs font-semibold text-foreground hover:bg-muted/50 rounded-t-lg transition-colors border-b border-border"
        >
          <span className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-primary"></span>
            Steps ({topLevelSteps.length})
          </span>
          {isCollapsed ? (
            <ChevronUp className="w-3.5 h-3.5 text-muted-foreground" />
          ) : (
            <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
          )}
        </button>

        {/* Step List */}
        {!isCollapsed && (
          <div className="max-h-[280px] overflow-y-auto [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:bg-gray-300 [&::-webkit-scrollbar-thumb]:dark:bg-gray-600 [&::-webkit-scrollbar-thumb]:rounded-full">
            {topLevelSteps.map(({ step, stepIndex, node, nodeId }) => {
              const isSelected = selectedNodeId === nodeId
              const statusIcon = getStatusIcon(node)
              const nodeType = node.type

              return (
                <button
                  key={nodeId}
                  onClick={() => handleStepClick(nodeId)}
                  className={`w-full px-2.5 py-1.5 text-left hover:bg-muted/50 transition-colors border-b border-border/50 last:border-b-0 ${
                    isSelected 
                      ? 'bg-primary/10 dark:bg-primary/20 border-l-2 border-l-primary' 
                      : ''
                  }`}
                >
                  <div className="flex items-start gap-2">
                    {/* Step Number */}
                    <div className={`flex-shrink-0 w-5 h-5 rounded flex items-center justify-center text-[10px] font-bold mt-0.5 ${
                      isSelected
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-muted-foreground'
                    }`}>
                      {stepIndex + 1}
                    </div>

                    {/* Step Title */}
                    <div className="flex-1 min-w-0">
                      <div className={`text-[11px] leading-tight ${
                        isSelected 
                          ? 'font-semibold text-foreground' 
                          : 'font-medium text-foreground/90'
                      }`}>
                        {step.title || `Step ${stepIndex + 1}`}
                      </div>
                      {(nodeType === 'conditional' || nodeType === 'loop') && (
                        <div className="text-[9px] text-muted-foreground mt-0.5 font-medium">
                          {nodeType === 'conditional' ? 'Conditional' : 'Loop'}
                        </div>
                      )}
                    </div>

                    {/* Status Icon */}
                    <div className="flex-shrink-0 mt-0.5">
                      {statusIcon}
                    </div>
                  </div>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

