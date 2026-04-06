import { memo, useCallback, useMemo, type ReactElement, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Play, Settings, Route } from 'lucide-react'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { NodeConfigFooter } from './NodeConfigFooter'
import type { RoutingStepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface RoutingStepNodeProps {
  data: RoutingStepNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-teal-400 dark:border-teal-500',
  executing: 'border-teal-500 dark:border-teal-400',
  evaluating: 'border-purple-500 dark:border-purple-400',
  routed: 'border-green-500 dark:border-green-400',
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
  executing: <Loader2 className="w-4 h-4 text-teal-500 animate-spin" />,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 animate-spin" />,
  routed: <CheckCircle className="w-4 h-4 text-green-500" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const RoutingStepNode = memo(({ data, selected }: RoutingStepNodeProps) => {
  const { id, title, routing_question, routes, status, stepIndex, changeType, step, onRunFromStep, onOpenSidebar, isExecuting, isOrphan } = data

  // Get preset for config badges
  const activePreset = useActiveWorkflowPreset()

  const { availableLLMs } = useLLMStore()
  const stepOverride = useWorkflowStore(state => state.stepOverride)
  const layoutDirection = useWorkflowStore(state => state.layoutDirection)
  const isHorizontal = layoutDirection === 'LR'
  const inputPosition = isHorizontal ? Position.Left : Position.Top
  const outputPosition = isHorizontal ? Position.Right : Position.Bottom

  type AgentConfigsType = {
    use_code_execution_mode?: boolean
    conditional_llm?: { provider?: string; model_id?: string }
    execution_llm?: { provider?: string; model_id?: string }
    lock_learnings?: boolean
    disable_learning?: boolean
  }
  const outerStep = step as { agent_configs?: AgentConfigsType }
  const stepConfig = outerStep

  // Code execution mode
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const overrideCodeExec = stepOverride?.use_code_execution_mode
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = overrideCodeExec !== undefined
    ? overrideCodeExec === true
    : stepCodeExecSetting !== undefined
      ? stepCodeExecSetting === true
      : presetUseCodeExecutionMode

  // Conditional LLM (for routing evaluation)
  const conditionalLLM = useMemo(() => {
    const presetLLMConfig = activePreset?.llmConfig
    const stepConditionalLLM = stepConfig?.agent_configs?.conditional_llm
    const stepExecutionLLM = stepConfig?.agent_configs?.execution_llm
    const presetExecutionLLM = presetLLMConfig?.execution_llm
    const presetDefaultLLM = presetLLMConfig?.provider && presetLLMConfig?.model_id
      ? { provider: presetLLMConfig.provider, model_id: presetLLMConfig.model_id } : null

    const llmConfig = stepConditionalLLM || stepExecutionLLM || presetExecutionLLM || presetDefaultLLM
    if (!llmConfig?.provider || !llmConfig?.model_id) return null

    const llm = availableLLMs?.find(l => l.provider === llmConfig.provider && l.model === llmConfig.model_id)
    return llm?.label || `${llmConfig.provider} ${llmConfig.model_id.split('-').slice(0, 2).join('-')}`
  }, [stepConfig?.agent_configs?.conditional_llm, stepConfig?.agent_configs?.execution_llm, activePreset?.llmConfig, availableLLMs])

  // Lock learnings
  const lockLearnings = useMemo(() => {
    const locked = stepOverride?.lock_learnings !== undefined
      ? stepOverride.lock_learnings === true
      : stepConfig?.agent_configs?.lock_learnings === true
    const learningDisabled = stepConfig?.agent_configs?.disable_learning === true
    return locked && !learningDisabled
  }, [stepOverride?.lock_learnings, stepConfig?.agent_configs?.lock_learnings, stepConfig?.agent_configs?.disable_learning])

  // Handle run from step
  const handleRunClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    if (onRunFromStep && step?.id) {
      onRunFromStep(stepIndex, step.id)
    }
  }, [onRunFromStep, stepIndex, step?.id])

  // Handle settings click
  const handleSettingsClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    if (onOpenSidebar) {
      onOpenSidebar(`step-${id}`)
    }
  }, [onOpenSidebar, id])

  const borderColor = statusBorderColors[status] || statusBorderColors.pending
  const statusIcon = statusIcons[status] || null

  return (
    <div className={`relative ${changeType ? changeHighlightStyles[changeType] : ''}`}>
      {/* Change badge */}
      {changeType && (
        <div className={`absolute -top-2 -right-2 z-10 ${changeBadgeStyles[changeType].bg} text-white rounded-full w-5 h-5 flex items-center justify-center shadow-md`}>
          {changeBadgeStyles[changeType].icon}
        </div>
      )}

      {/* Input handle */}
      <Handle
        type="target"
        position={inputPosition}
        className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
      />

      {/* Main node */}
      <div
        className={`
          w-[260px] rounded-xl border-2 ${borderColor}
          bg-white dark:bg-gray-900
          shadow-lg overflow-visible transition-all duration-200
          ${isOrphan ? 'border-dashed border-amber-400 dark:border-amber-500' : ''}
          ${selected ? 'ring-2 ring-teal-500/60' : ''}
          ${status === 'executing' || status === 'evaluating' ? 'shadow-lg shadow-teal-500/30' : ''}
        `}
      >
        {/* Header */}
        <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700 flex items-center gap-2">
          <div className="flex items-center gap-1.5 min-w-0 flex-1">
            <div className="flex items-center justify-center w-6 h-6 rounded-md bg-teal-100 dark:bg-teal-900/40 flex-shrink-0">
              <Route className="w-3.5 h-3.5 text-teal-600 dark:text-teal-400" />
            </div>
            <div className="text-xs font-semibold text-gray-900 dark:text-white truncate">
              {title || `Routing ${stepIndex + 1}`}
            </div>
            {statusIcon}
          </div>

          {/* Action buttons */}
          <div className="flex items-center gap-1 flex-shrink-0">
            {!isOrphan && (
              <button
                onClick={handleRunClick}
                className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
                title="Run from this step"
                disabled={isExecuting}
              >
                <Play className="w-3 h-3 text-gray-600 dark:text-gray-400" />
              </button>
            )}
            <button
              onClick={handleSettingsClick}
              className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
              title="Step settings"
            >
              <Settings className="w-3 h-3 text-gray-600 dark:text-gray-400" />
            </button>
          </div>
        </div>

        {/* Routing question */}
        {routing_question && (
          <div className="px-3 pt-2 pb-1.5">
            <div className="p-1.5 rounded-lg bg-teal-50 dark:bg-teal-900/20 border border-teal-200 dark:border-teal-800/50">
              <div className="text-[10px] text-teal-700 dark:text-teal-300 line-clamp-2 italic">
                {routing_question}
              </div>
            </div>
          </div>
        )}

        {/* Route count + labels */}
        <div className="px-3 py-1.5">
          {routes && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400">
              {routes.length} routes
            </span>
          )}
        </div>

        {/* Route labels */}
        {routes && routes.length > 0 && (
          <div className="px-3 pb-2 flex flex-wrap gap-1">
            {routes.map((route) => (
              <span
                key={route.route_id}
                className="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400"
              >
                {route.route_name || route.route_id}
              </span>
            ))}
          </div>
        )}

        {/* Config footer */}
        <NodeConfigFooter
          useCodeExecutionMode={useCodeExecutionMode}
          evalLLM={conditionalLLM}
          lockLearnings={lockLearnings}
        />
      </div>

      {/* Output handles - one per route */}
      {routes && routes.map((route, idx) => {
        const handleOffset = routes.length > 1
          ? (idx / (routes.length - 1)) * 80 + 10 // Spread from 10% to 90%
          : 50
        return (
          <Handle
            key={`route-${route.route_id}`}
            type="source"
            position={outputPosition}
            id={`route-${route.route_id}`}
            className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
            style={isHorizontal
              ? { top: `${handleOffset}%` }
              : { left: `${handleOffset}%` }}
          />
        )
      })}

      {/* Fallback single output handle if no routes */}
      {(!routes || routes.length === 0) && (
        <Handle
          type="source"
          position={outputPosition}
          className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
        />
      )}
    </div>
  )
})

RoutingStepNode.displayName = 'RoutingStepNode'
