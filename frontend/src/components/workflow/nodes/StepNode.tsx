import { memo, useEffect, useMemo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { BookOpen, CheckCircle, Database, FileText, XCircle, Loader2, Plus, RefreshCw, Bot, Lock } from 'lucide-react'
import type { StepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface StepNodeProps {
  data: StepNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  running: 'border-blue-500 dark:border-blue-400',
  completed: 'border-green-500 dark:border-green-400',
  failed: 'border-red-500 dark:border-red-400'
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
  running: <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />
}

const metadataPanelBase = 'rounded-md border border-l-2 border-gray-600/70 bg-gray-800/85 px-2 py-1.5 shadow-sm'
const metadataLabelBase = 'flex min-w-0 items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wide text-gray-300'
const metadataBodyBase = 'mt-0.5 text-[10px] leading-snug text-gray-100 line-clamp-2'
const accessPillBase = 'ml-auto shrink-0 rounded border border-gray-500/70 bg-gray-900/80 px-1.5 py-0.5 text-[9px] font-medium normal-case tracking-normal text-gray-200'
const metadataCompactBase = 'flex h-7 min-w-0 items-center gap-1.5 rounded-md border border-l-2 border-gray-600/70 bg-gray-800/75 px-2 text-[10px] font-medium text-gray-200 shadow-sm'

function textReferencesKnowledgebase(text: string): boolean {
  return /\bknowledgebase[\\/]/i.test(text)
}

function textReferencesDatabase(text: string): boolean {
  return /\$DB_PATH\b|\bdb[\\/]|db\.sqlite|\bsqlite3?\b/i.test(text)
}

export const StepNode = memo(({ data, selected }: StepNodeProps) => {
  const { id, title, status, stepIndex, changeType, isOrphan, isEvaluationStep, step } = data
  
  // Check if this is a sub-agent (part of a routing step)
  const isSubAgent = useMemo(() => id.includes('-sub-agent-'), [id])
  const agentConfig = step?.agent_configs
  const planReferenceText = useMemo(() => {
    if (!step) return ''

    const contextOutput = step.context_output
    const contextOutputs = Array.isArray(contextOutput)
      ? contextOutput
      : (contextOutput ? [contextOutput] : [])
    const validationFiles = step.validation_schema?.files?.map(file => file.file_name) || []

    return [
      step.description,
      step.success_criteria,
      ...(Array.isArray(step.context_dependencies) ? step.context_dependencies : []),
      ...contextOutputs,
      ...validationFiles
    ].filter((value): value is string => typeof value === 'string' && value.length > 0).join('\n')
  }, [step])
  const referencesKnowledgebase = textReferencesKnowledgebase(planReferenceText)
  const referencesDatabase = textReferencesDatabase(planReferenceText)
  const learningAccess = agentConfig?.learnings_access
  const learningObjective = typeof agentConfig?.learning_objective === 'string'
    ? agentConfig.learning_objective.trim()
    : ''
  const hasLearningObjective = learningObjective.length > 0
  const learningLocked = agentConfig?.lock_learnings === true
  const hasExplicitLearningAccess = learningAccess === 'read' || learningAccess === 'read-write' || learningAccess === 'none'
  const showLearningMetadata = hasLearningObjective || learningLocked || hasExplicitLearningAccess
  const learningAccessLabel = learningAccess === 'read-write'
    ? 'Read/write'
    : learningAccess === 'read'
      ? 'Read'
      : learningAccess === 'none'
        ? 'Off'
        : hasLearningObjective
          ? 'Objective'
          : 'Learning'
  const learningHeading = hasLearningObjective
    ? 'Learning objective'
    : learningLocked
      ? 'Learning locked'
      : 'Learning access'
  const learningDisplayText = hasLearningObjective
    ? learningObjective
    : learningLocked
      ? 'Uses existing learnings; new learning is locked for this step.'
      : learningAccess === 'none'
        ? 'Learning is disabled for this step.'
        : learningAccess === 'read'
          ? 'Reads existing learnings for this step.'
          : learningAccess === 'read-write'
            ? 'Reads and updates learnings for this step.'
            : 'Learning metadata is enabled for this step.'
  const knowledgebaseAccess = agentConfig?.knowledgebase_access
  const knowledgebaseContribution = typeof agentConfig?.knowledgebase_contribution === 'string'
    ? agentConfig.knowledgebase_contribution.trim()
    : ''
  const hasKnowledgebaseContribution = knowledgebaseContribution.length > 0
  const hasExplicitKnowledgebaseAccess = knowledgebaseAccess === 'read' || knowledgebaseAccess === 'write' || knowledgebaseAccess === 'read-write' || knowledgebaseAccess === 'none'
  const showKnowledgebaseMetadata = hasKnowledgebaseContribution || hasExplicitKnowledgebaseAccess || referencesKnowledgebase
  const knowledgebaseAccessLabel = knowledgebaseAccess === 'read-write'
    ? 'Read/write'
    : knowledgebaseAccess === 'write'
      ? 'Write'
      : knowledgebaseAccess === 'read'
        ? 'Read'
        : knowledgebaseAccess === 'none'
          ? 'Off'
          : referencesKnowledgebase
            ? 'Referenced'
            : 'KB'
  const knowledgebaseHeading = hasKnowledgebaseContribution
    ? 'KB contribution'
    : referencesKnowledgebase
      ? 'KB reference'
      : 'KB access'
  const knowledgebaseDisplayText = hasKnowledgebaseContribution
    ? knowledgebaseContribution
    : referencesKnowledgebase
      ? 'References knowledgebase files or folders in this step.'
      : knowledgebaseAccess === 'none'
        ? 'Knowledgebase access is disabled for this step.'
        : knowledgebaseAccess === 'read'
          ? 'Reads knowledgebase content for this step.'
          : knowledgebaseAccess === 'write'
            ? 'Writes results back to the knowledgebase.'
            : knowledgebaseAccess === 'read-write'
              ? 'Reads from and writes back to the knowledgebase.'
              : 'Knowledgebase metadata is enabled for this step.'
  const dbAccess = agentConfig?.db_access
  const hasExplicitDbAccess = dbAccess === 'read' || dbAccess === 'write' || dbAccess === 'read-write' || dbAccess === 'none'
  const showDbMetadata = hasExplicitDbAccess || referencesDatabase
  const dbAccessLabel = dbAccess === 'read-write'
    ? 'Read/write'
    : dbAccess === 'write'
      ? 'Write'
      : dbAccess === 'read'
        ? 'Read'
        : dbAccess === 'none'
          ? 'Off'
          : referencesDatabase
            ? 'Referenced'
            : 'DB'
  const dbDisplayText = referencesDatabase && !hasExplicitDbAccess
    ? 'References DB paths, SQLite, or $DB_PATH in this step.'
    : dbAccess === 'none'
      ? 'Database access is disabled for this step.'
      : dbAccess === 'read'
        ? 'Reads database state during this step.'
        : dbAccess === 'write'
          ? 'Writes database updates during this step.'
          : dbAccess === 'read-write'
            ? 'Reads and writes database state during this step.'
            : 'Database metadata is enabled for this step.'
  const statusIcon = statusIcons[status]
  const showFooterMetadata = isSubAgent || Boolean(statusIcon)

  useEffect(() => {
    if (!import.meta.env.DEV) return
    if (!showLearningMetadata && !showKnowledgebaseMetadata && !showDbMetadata) return

    console.log('[WorkflowStepNode] store metadata', {
      nodeId: id,
      stepId: step?.id,
      title,
      learnings_access: learningAccess,
      learning_objective: learningObjective || undefined,
      lock_learnings: learningLocked,
      knowledgebase_access: knowledgebaseAccess,
      knowledgebase_contribution: knowledgebaseContribution || undefined,
      db_access: dbAccess,
      references_knowledgebase: referencesKnowledgebase,
      references_database: referencesDatabase
    })
  }, [
    dbAccess,
    id,
    knowledgebaseAccess,
    knowledgebaseContribution,
    learningAccess,
    learningLocked,
    learningObjective,
    referencesDatabase,
    referencesKnowledgebase,
    showDbMetadata,
    showKnowledgebaseMetadata,
    showLearningMetadata,
    step?.id,
    title
  ])

  return (
    <div className={`
      relative w-[280px] rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg
      ${isSubAgent ? 'overflow-visible' : 'overflow-hidden'}
      ${statusBorderColors[status]}
      ${isSubAgent ? 'border-dashed border-indigo-400 dark:border-indigo-500' : ''}
      ${isOrphan ? 'border-dashed border-amber-400 dark:border-amber-500' : ''}
      ${selected ? 'ring-2 ring-purple-500/60' : ''}
      ${changeType ? changeHighlightStyles[changeType] : ''}
    `}>
      {/* Status badge - positioned at top-right edge (shows for running/failed status) */}
      {status === 'running' && (
        <div className="absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl bg-blue-500 text-white text-[10px] font-medium shadow-lg">
          <Loader2 className="w-3 h-3 animate-spin" />
          <span>Running</span>
        </div>
      )}
      {status === 'failed' && (
        <div className="absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl bg-red-500 text-white text-[10px] font-medium shadow-lg">
          <XCircle className="w-3 h-3" />
          <span>Failed</span>
        </div>
      )}
      
      {/* Change badge - positioned below status badge (or top-right if no status badge) */}
      {changeType && (
        <div className={`absolute ${status === 'running' || status === 'failed' ? 'top-6 right-0' : 'top-0 right-0'} z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      <Handle
        type="target"
        position={Position.Top}
        id={isSubAgent ? 'top' : undefined}
        className={`!w-3 !h-3 !border-2 !border-white dark:!border-gray-900 ${isSubAgent ? '!bg-indigo-400 dark:!bg-indigo-600' : '!bg-gray-400 dark:!bg-gray-500'}`}
        style={{ top: '-6px', left: '50%' }}
      />

      {/* Header */}
      <div className="px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        {/* First row: Step number (or sub-agent indicator) and title */}
        <div className="flex items-start gap-3 mb-2">
          {isSubAgent ? (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300 flex-shrink-0" title="Sub-agent">
              <Bot className="w-4 h-4" />
            </div>
          ) : (
            <div className="flex items-center justify-center w-8 h-8 rounded-md bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 text-sm font-bold flex-shrink-0">
              {isEvaluationStep ? `E${stepIndex + 1}` : stepIndex + 1}
            </div>
          )}
          <div className="flex-1 min-w-0">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-relaxed">
              {title}
            </h3>
          </div>
        </div>
        {(showLearningMetadata || showKnowledgebaseMetadata || showDbMetadata) && (
          <div className="mb-2 space-y-1.5">
            {showLearningMetadata && (
              <div
                className={`${metadataPanelBase} border-l-sky-500/80`}
                title={hasLearningObjective ? `Learning objective: ${learningObjective}` : learningDisplayText}
              >
                <div className={metadataLabelBase}>
                  {learningLocked ? <Lock className="h-3 w-3 shrink-0 text-sky-300" /> : <BookOpen className="h-3 w-3 shrink-0 text-sky-300" />}
                  <span className="truncate">{learningHeading}</span>
                  {(learningLocked || hasExplicitLearningAccess || hasLearningObjective) && (
                    <span className={accessPillBase}>
                      {learningLocked ? 'Locked' : learningAccessLabel}
                    </span>
                  )}
                </div>
                <div className={metadataBodyBase}>
                  {learningDisplayText}
                </div>
              </div>
            )}
            {showKnowledgebaseMetadata && (
              <div
                className={`${metadataPanelBase} border-l-emerald-500/80`}
                title={hasKnowledgebaseContribution ? `Knowledgebase contribution: ${knowledgebaseContribution}` : knowledgebaseDisplayText}
              >
                <div className={metadataLabelBase}>
                  <FileText className="h-3 w-3 shrink-0 text-emerald-300" />
                  <span className="truncate">{knowledgebaseHeading}</span>
                  {(hasExplicitKnowledgebaseAccess || referencesKnowledgebase || hasKnowledgebaseContribution) && (
                    <span className={accessPillBase}>
                      {knowledgebaseAccessLabel}
                    </span>
                  )}
                </div>
                <div className={metadataBodyBase}>
                  {knowledgebaseDisplayText}
                </div>
              </div>
            )}
            {showDbMetadata && (
              <div
                className={`${metadataCompactBase} border-l-gray-400/80`}
                title={dbDisplayText}
              >
                <Database className="h-3 w-3 shrink-0 text-gray-300" />
                <span className="truncate">DB access</span>
                <span className={accessPillBase}>{dbAccessLabel}</span>
              </div>
            )}
          </div>
        )}
        {showFooterMetadata && (
          <div className="flex items-center gap-1.5">
            {isSubAgent && (
              <span className="text-[10px] font-medium text-gray-500 dark:text-gray-400">
                Step {stepIndex + 1}
              </span>
            )}
            <div className="flex-1" />
            {statusIcon}
          </div>
        )}
      </div>

      <Handle type="source" position={Position.Bottom} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />
      
      {/* Retry Handle - for validation loop-back (hidden by default) */}
      <Handle
        type="target"
        position={Position.Top}
        id="retry"
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
    </div>
  )
})

StepNode.displayName = 'StepNode'
export default StepNode
