import { memo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Users } from 'lucide-react'
import type { CrewStepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import { isCrewStep } from '../../../utils/stepConfigMatching'
import { useCrewAttachmentAlias, useCrewTrigger } from '../canvas/useCrewStepLookups'

interface CrewNodeProps {
  data: CrewStepNodeData
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

export const CrewNode = memo(({ data, selected }: CrewNodeProps) => {
  const { title, status, changeType, isOrphan, step } = data
  const crew = isCrewStep(step) ? step : null
  const projectId = crew?.crew_project_id || (typeof data.crew_project_id === 'string' ? data.crew_project_id : '')
  const triggerId = crew?.trigger_id || (typeof data.trigger_id === 'string' ? data.trigger_id : '')
  const rawOutput = crew?.context_output
  const outputFile = Array.isArray(rawOutput) ? rawOutput[0] : rawOutput || 'response.md'
  const profileId = crew?.crew_profile_id || (typeof data.crew_profile_id === 'string' ? data.crew_profile_id : '') || 'work'
  const trigger = useCrewTrigger(profileId, projectId, triggerId)
  const crewAlias = useCrewAttachmentAlias(typeof data.workspacePath === 'string' ? data.workspacePath : null, projectId)
  const crewLabel = crewAlias ?? projectId
  const triggerLabel = trigger?.name ?? triggerId
  const statusIcon = statusIcons[status]

  return (
    <div className={`
      relative w-[280px] rounded-xl border-2 bg-white dark:bg-gray-900 shadow-lg overflow-hidden
      ${statusBorderColors[status]}
      ${isOrphan ? 'border-dashed border-amber-400 dark:border-amber-500' : ''}
      ${selected ? 'ring-2 ring-purple-500/60' : ''}
      ${changeType ? changeHighlightStyles[changeType] : ''}
    `}>
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

      {changeType && (
        <div className={`absolute ${status === 'running' || status === 'failed' ? 'top-6 right-0' : 'top-0 right-0'} z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      {isOrphan && (
        <div className="absolute top-0 left-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-br-lg rounded-tl-xl bg-amber-500 text-white text-[10px] font-medium shadow-lg">
          <span>Orphan · unused</span>
        </div>
      )}

      <Handle
        type="target"
        position={Position.Top}
        className="!w-3 !h-3 !border-2 !border-white dark:!border-gray-900 !bg-gray-400 dark:!bg-gray-500"
        style={{ top: '-6px', left: '50%' }}
      />

      <div className="px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-start gap-3 mb-2">
          <div className="flex items-center justify-center w-8 h-8 rounded-md bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 flex-shrink-0" title="Crew step">
            <Users className="w-4 h-4" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white leading-relaxed">
              {title}
            </h3>
            <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Crew</div>
          </div>
          {statusIcon}
        </div>
        <dl className="space-y-1 text-xs">
          <div className="flex items-baseline gap-2 min-w-0">
            <dt className="shrink-0 text-muted-foreground">Crew</dt>
            <dd className="truncate font-mono text-foreground/90" title={projectId}>{crewLabel || '—'}</dd>
          </div>
          <div className="flex items-baseline gap-2 min-w-0">
            <dt className="shrink-0 text-muted-foreground">Trigger</dt>
            <dd className="truncate font-mono text-foreground/90" title={triggerId}>{triggerLabel || '—'}</dd>
          </div>
          <div className="flex items-baseline gap-2 min-w-0">
            <dt className="shrink-0 text-muted-foreground">Output</dt>
            <dd className="truncate font-mono text-foreground/90" title={outputFile}>{outputFile}</dd>
          </div>
        </dl>
      </div>

      <Handle type="source" position={Position.Bottom} className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-900" />

      <Handle
        type="target"
        position={Position.Top}
        id="retry"
        className="!w-2 !h-2 !bg-transparent !border-0"
      />
    </div>
  )
})

CrewNode.displayName = 'CrewNode'
export default CrewNode
