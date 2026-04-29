import { memo, useMemo, useState, useEffect, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { XCircle, Loader2, Plus, RefreshCw, GitBranch, Code, Terminal, Lock, CheckCircle } from 'lucide-react'
import { useActiveWorkflowPreset } from '../../../hooks/useActiveWorkflowPreset'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { agentApi } from '../../../services/api'

import type { ConditionalNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import type { ConditionalPlanStep } from '../../../utils/stepConfigMatching'

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
  const { title, condition_question, status, stepIndex, changeType, step, workspacePath, isOrphan } = data

  // Process text to convert escaped newlines to actual newlines
  const processText = (text: string | undefined): string | undefined => {
    if (!text) return undefined
    return text
      .replace(/\\n/g, '\n')  // Convert \n to actual newlines
      .replace(/\\t/g, '\t')  // Convert \t to actual tabs
      .replace(/\\r/g, '\r')  // Convert \r to actual carriage returns
  }

  const processedConditionQuestion = processText(condition_question)

  // Get preset for config badges
  const activePreset = useActiveWorkflowPreset()

  const stepOverride = useWorkflowStore(state => state.stepOverride)

  // Get step config (agent_configs)
  const stepConfig = step as { agent_configs?: {
    use_code_execution_mode?: boolean
    learnings_access?: 'read' | 'read-write' | 'none'
    lock_learnings?: boolean
  } }

  // Determine code execution mode: override > step config > preset default
  const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode ?? false
  const overrideCodeExec = stepOverride?.use_code_execution_mode
  const stepCodeExecSetting = stepConfig?.agent_configs?.use_code_execution_mode
  const useCodeExecutionMode = overrideCodeExec !== undefined
    ? overrideCodeExec === true
    : stepCodeExecSetting !== undefined
      ? stepCodeExecSetting === true
      : presetUseCodeExecutionMode

  // Disabled = learnings_access explicitly "none". Default and "read"/"read-write"
  // both imply the step participates in learnings.
  const learningDisabled = useMemo(() => {
    return stepConfig?.agent_configs?.learnings_access === 'none'
  }, [stepConfig?.agent_configs?.learnings_access])

  // Check if learnings exist in backend (for conditional steps, use step.ID)
  const [learningsExist, setLearningsExist] = useState<boolean | null>(null) // null = checking, true/false = result
  const stepIdForLearnings = step?.id

  useEffect(() => {
    // Only check if we have workspace path and step ID
    if (!workspacePath || !stepIdForLearnings) {
      setLearningsExist(false)
      return
    }

    // Check if learnings folder exists and has content
    const checkLearningsExist = async () => {
      try {
        const learningsPath = `${workspacePath}/learnings/${stepIdForLearnings}`
        const files = await agentApi.getPlannerFiles(learningsPath, 100)
        
        // Check if there are any learning files (exclude .learning_metadata.json)
        const hasLearningFiles = files && Array.isArray(files) && files.some((file: { filepath?: string; name?: string }) => {
          const fileName = file.filepath || file.name || ''
          return fileName.endsWith('.md') || (fileName.startsWith('code/') && /\.(go|py|sh|js|ts|jsx|tsx|bash|rb|java|rs|c|cpp|json|yaml|yml)$/.test(fileName))
        })
        
        setLearningsExist(hasLearningFiles)
      } catch (error) {
        // If folder doesn't exist or error, assume no learnings
        console.debug('[ConditionalNode] Failed to check learnings:', error)
        setLearningsExist(false)
      }
    }

    checkLearningsExist()
  }, [workspacePath, stepIdForLearnings])

  // Lock learnings: override > step config
  const isLockedInConfig = useMemo(() => {
    if (stepOverride?.lock_learnings !== undefined) return stepOverride.lock_learnings === true
    return stepConfig?.agent_configs?.lock_learnings ?? false
  }, [stepOverride?.lock_learnings, stepConfig?.agent_configs?.lock_learnings])

  const lockLearnings = useMemo(() => {
    return isLockedInConfig && (learningsExist === true) && !learningDisabled
  }, [isLockedInConfig, learningsExist, learningDisabled])

  return (
    <div className={`relative w-[240px] ${changeType ? changeHighlightStyles[changeType] : ''} ${isOrphan ? 'border-dashed border-2 border-amber-400 dark:border-amber-500 rounded-xl' : ''}`}>
      {/* Header metadata - above the diamond */}
      <div className="absolute -top-12 left-0 right-0 flex items-center justify-center gap-2 z-20">
        {/* Agent Mode Badge */}
        {useCodeExecutionMode ? (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-800" title="Code Execution Mode">
            <Terminal className="w-3.5 h-3.5" />
          </div>
        ) : (
          <div className="flex items-center justify-center w-7 h-7 rounded-md bg-slate-100 dark:bg-slate-800/60 text-slate-700 dark:text-slate-300 border border-slate-200 dark:border-slate-700" title="Simple Agent Mode">
            <Code className="w-3.5 h-3.5" />
          </div>
        )}
        {/* Lock Learnings Badge */}
        {lockLearnings && (
          <div 
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 text-[10px] font-semibold border border-purple-200 dark:border-purple-800"
            title="Learnings are locked - learning agent will not run but existing learnings will be used"
          >
            <Lock className="w-3 h-3" />
            <span>Locked</span>
          </div>
        )}
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
          ${selected ? 'ring-2 ring-purple-500/60' : ''}
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
          position={Position.Top}
          className="!w-3 !h-3 !bg-purple-400 dark:!bg-purple-500 !border-2 !border-white dark:!border-gray-900"
          style={{ top: '-6px', left: '50%' }}
        />

        {/* Content */}
        <div className="flex flex-col items-center justify-center px-12 py-5 pt-6 text-center min-h-[100px]">
          <div className="flex items-center gap-1.5 mb-2">
            {statusIcons[status]}
          </div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-tight">
            {title || `Condition ${stepIndex + 1}`}
          </h3>
        </div>

        {/* True handle - top right area */}
        <Handle
          type="source"
          position={Position.Bottom}
          id="true"
          className="!w-3 !h-3 !bg-green-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ left: '35%', bottom: '-6px' }}
        />

        {/* False handle - bottom right area */}
        <Handle
          type="source"
          position={Position.Bottom}
          id="false"
          className="!w-3 !h-3 !bg-red-500 !border-2 !border-white dark:!border-gray-900 !shadow-md"
          style={{ left: '65%', bottom: '-6px' }}
        />
      </div>

      {/* Condition question - what is being evaluated */}
      {processedConditionQuestion && (
        <div className="mx-4 mt-2 p-2 rounded-lg bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800/50">
          <p className="text-[10px] text-purple-700 dark:text-purple-300 text-center">
            <span className="font-semibold">Evaluates: </span>
            {processedConditionQuestion.length > 60
              ? `${processedConditionQuestion.substring(0, 60)}...`
              : processedConditionQuestion}
          </p>
        </div>
      )}

      {/* Branch Routing Information */}
      {(() => {
        // Type guard: check if step is a conditional step
        const conditionalStep = step.type === 'conditional' ? step as ConditionalPlanStep : null
        if (!conditionalStep) return null

        const hasRoutingInfo = conditionalStep.if_true_next_step_id || 
                               conditionalStep.if_false_next_step_id || 
                               (conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0) || 
                               (conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0)
        
        if (!hasRoutingInfo) return null
        
        return (
          <div className={`mx-4 ${condition_question ? 'mt-2' : 'mt-3'}`}>
            <div className="p-2 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700">
              <div className="text-[10px] font-semibold text-gray-700 dark:text-gray-300 mb-1.5 text-center">
                Branch Routing
              </div>
              <div className="space-y-1.5">
                {/* Yes Branch */}
                <div className="flex items-center justify-between text-[10px]">
                  <div className="flex items-center gap-1.5">
                    <span className="text-green-600 dark:text-green-400 font-medium">✓ Yes:</span>
                    {conditionalStep.if_true_steps && conditionalStep.if_true_steps.length > 0 ? (
                      <span className="text-gray-600 dark:text-gray-400">
                        {conditionalStep.if_true_steps.length} step{conditionalStep.if_true_steps.length !== 1 ? 's' : ''}
                      </span>
                    ) : (
                      <span className="text-gray-500 dark:text-gray-500 italic text-[9px]">(empty)</span>
                    )}
                  </div>
                  {conditionalStep.if_true_next_step_id && (
                    <span className="text-gray-600 dark:text-gray-400 text-[9px]">
                      → {conditionalStep.if_true_next_step_id === 'end' ? 'End' : conditionalStep.if_true_next_step_id}
                    </span>
                  )}
                </div>
                
                {/* No Branch */}
                <div className="flex items-center justify-between text-[10px]">
                  <div className="flex items-center gap-1.5">
                    <span className="text-red-600 dark:text-red-400 font-medium">✗ No:</span>
                    {conditionalStep.if_false_steps && conditionalStep.if_false_steps.length > 0 ? (
                      <span className="text-gray-600 dark:text-gray-400">
                        {conditionalStep.if_false_steps.length} step{conditionalStep.if_false_steps.length !== 1 ? 's' : ''}
                      </span>
                    ) : (
                      <span className="text-gray-500 dark:text-gray-500 italic text-[9px]">(empty)</span>
                    )}
                  </div>
                  {conditionalStep.if_false_next_step_id && (
                    <span className="text-gray-600 dark:text-gray-400 text-[9px]">
                      → {conditionalStep.if_false_next_step_id === 'end' ? 'End' : conditionalStep.if_false_next_step_id}
                    </span>
                  )}
                </div>
              </div>
            </div>
          </div>
        )
      })()}

    </div>
  )
})

ConditionalNode.displayName = 'ConditionalNode'
export default ConditionalNode
