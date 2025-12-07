import { memo, useCallback, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { HelpCircle, CheckCircle, XCircle, Loader2, Plus, RefreshCw, GitBranch, Play, Settings } from 'lucide-react'
import type { ConditionalNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface ConditionalNodeProps {
  data: ConditionalNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-purple-400 dark:border-purple-500',
  evaluating: 'border-purple-500 dark:border-purple-400',
  decided_true: 'border-green-500 dark:border-green-400',
  decided_false: 'border-red-500 dark:border-red-400',
  completed: 'border-green-500 dark:border-green-400'
}

const changeHighlightStyles: Record<ChangeType, string> = {
  added: 'ring-2 ring-emerald-500/60 shadow-emerald-500/20',
  updated: 'ring-2 ring-blue-500/60 shadow-blue-500/20',
  deleted: 'ring-2 ring-red-500/60 shadow-red-500/20 opacity-50'
}

const changeBadgeStyles: Record<ChangeType, { bg: string; icon: ReactElement }> = {
  added: { bg: 'bg-emerald-500', icon: <Plus className="w-3 h-3" /> },
  updated: { bg: 'bg-blue-500', icon: <RefreshCw className="w-3 h-3" /> },
  deleted: { bg: 'bg-red-500', icon: <XCircle className="w-3 h-3" /> }
}

const statusIcons: Record<string, ReactElement | null> = {
  pending: null,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 animate-spin" />,
  decided_true: <CheckCircle className="w-4 h-4 text-green-500" />,
  decided_false: <XCircle className="w-4 h-4 text-red-500" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const ConditionalNode = memo(({ data, selected }: ConditionalNodeProps) => {
  const { title, condition_question, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, canRun } = data

  // Button is disabled if executing, can't run (previous steps not done), or no callback
  const isRunDisabled = isExecuting || !canRun || !onRunFromStep

  // Handle run from this step button click
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    e.preventDefault() // Prevent any default behavior
    console.log('[ConditionalNode] Run button clicked:', { stepIndex, stepId: step.id, onRunFromStep: !!onRunFromStep, isExecuting, canRun, isRunDisabled })
    if (onRunFromStep && !isExecuting && canRun) {
      console.log('[ConditionalNode] Calling onRunFromStep with:', stepIndex, step.id || `step-${stepIndex}`)
      onRunFromStep(stepIndex, step.id || `step-${stepIndex}`)
    } else {
      console.warn('[ConditionalNode] Cannot run step:', { 
        hasCallback: !!onRunFromStep, 
        isExecuting, 
        canRun, 
        isRunDisabled 
      })
    }
  }, [onRunFromStep, isExecuting, canRun, stepIndex, step.id, isRunDisabled])

  // Handle settings icon click - opens the sidebar
  const handleSettingsClick = useCallback((e: MouseEvent) => {
    e.stopPropagation() // Prevent node selection
    e.preventDefault() // Prevent any default behavior
    console.log('[ConditionalNode] Settings button clicked:', { stepIndex, stepId: step.id, onOpenSidebar: !!onOpenSidebar })
    if (onOpenSidebar && typeof onOpenSidebar === 'function') {
      const nodeId = step.id || `step-${stepIndex}`
      console.log('[ConditionalNode] Calling onOpenSidebar with:', nodeId)
      onOpenSidebar(nodeId)
    } else {
      console.warn('[ConditionalNode] onOpenSidebar callback not available')
    }
  }, [onOpenSidebar, stepIndex, step.id])

  return (
    <div className={`relative w-[300px] ${changeType ? changeHighlightStyles[changeType] : ''}`}>
      {/* Header with buttons - above the diamond */}
      <div className="absolute -top-12 left-0 right-0 flex items-center justify-center gap-2 z-20">
        {/* Run from this step button */}
        {onRunFromStep ? (
          <button
            onClick={handleRunClick}
            disabled={isRunDisabled}
            className={`
              flex items-center justify-center w-7 h-7 rounded-lg transition-all relative z-10
              ${isRunDisabled
                ? 'bg-gray-200 dark:bg-gray-700 text-gray-400 cursor-not-allowed opacity-50'
                : 'bg-green-100 dark:bg-green-900/40 text-green-600 dark:text-green-400 hover:bg-green-200 dark:hover:bg-green-900/60 hover:scale-105 cursor-pointer'
              }
            `}
            title={
              isExecuting 
                ? 'Execution in progress...' 
                : !canRun 
                  ? 'Complete previous steps first' 
                  : `Run step ${stepIndex + 1} only`
            }
          >
            <Play className="w-3.5 h-3.5" />
          </button>
        ) : (
          <div className="w-7 h-7 flex items-center justify-center text-xs text-gray-400" title="Run callback not available">
            ⚠️
          </div>
        )}
        {/* Settings icon button - opens sidebar */}
        {onOpenSidebar ? (
          <button
            onClick={handleSettingsClick}
            className="flex items-center justify-center w-7 h-7 rounded-lg transition-all relative z-10 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-600 hover:scale-105 cursor-pointer"
            title="Open step settings"
          >
            <Settings className="w-3.5 h-3.5" />
          </button>
        ) : null}
      </div>

      {/* Condition Badge - Top */}
      <div className="absolute -top-2.5 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-purple-600 dark:bg-purple-500 text-white text-[11px] font-semibold shadow-lg">
        <GitBranch className="w-3.5 h-3.5" />
        <span>Condition</span>
      </div>

      {/* Change badge */}
      {/* Change badge - positioned at top-right edge */}
      {changeType && (
        <div className={`absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      {/* Diamond Shape Card */}
      <div 
        className={`
          relative rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg overflow-visible
          ${statusBorderColors[status]}
          ${selected ? 'ring-2 ring-purple-500/40' : ''}
          ${status === 'evaluating' ? 'animate-pulse' : ''}
        `}
        style={{
          clipPath: 'polygon(15% 0%, 85% 0%, 100% 50%, 85% 100%, 15% 100%, 0% 50%)',
          minHeight: '100px'
        }}
      >
        {/* Input handle */}
        <Handle 
          type="target" 
          position={Position.Left} 
          className="!w-3 !h-3 !bg-purple-400 dark:!bg-purple-500 !border-2 !border-white dark:!border-gray-900"
          style={{ left: '-6px' }}
        />

        {/* Content */}
        <div className="flex flex-col items-center justify-center px-12 py-5 pt-6 text-center min-h-[100px]">
          <div className="flex items-center gap-1.5 mb-2">
            <HelpCircle className="w-4 h-4 text-purple-500 dark:text-purple-400" />
            {statusIcons[status]}
          </div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight">
            {title || `Condition ${stepIndex + 1}`}
          </h3>
        </div>

        {/* True handle - top right area */}
        <Handle 
          type="source" 
          position={Position.Right} 
          id="true" 
          className="!w-3 !h-3 !bg-green-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ top: '30%', right: '-6px' }}
        />

        {/* False handle - bottom right area */}
        <Handle 
          type="source" 
          position={Position.Right} 
          id="false" 
          className="!w-3 !h-3 !bg-red-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ top: '70%', right: '-6px' }}
        />
      </div>

      {/* Question below the diamond */}
      {condition_question && (
        <div className="mt-3 mx-4">
          <p className="text-[11px] text-gray-600 dark:text-gray-400 text-center leading-relaxed p-2.5 rounded-lg bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800/50">
            {condition_question}
          </p>
        </div>
      )}
    </div>
  )
})

ConditionalNode.displayName = 'ConditionalNode'
export default ConditionalNode
