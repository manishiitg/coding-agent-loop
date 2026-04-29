import { memo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, User } from 'lucide-react'
import type { HumanInputNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface HumanInputNodeProps {
  data: HumanInputNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-blue-400 dark:border-blue-500',
  waiting: 'border-blue-500 dark:border-blue-400',
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
  waiting: <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const HumanInputNode = memo(({ data, selected }: HumanInputNodeProps) => {
  const { title, question, response_type, options, status, stepIndex, changeType, isOrphan } = data

  const borderColor = statusBorderColors[status] || statusBorderColors.pending
  const statusIcon = statusIcons[status] || null
  const changeStyle = changeType ? changeHighlightStyles[changeType] : ''
  const changeBadge = changeType ? changeBadgeStyles[changeType] : null

  // Display question or title
  const displayText = question || title || `Human Input ${stepIndex + 1}`
  
  // Truncate long text
  const truncatedText = displayText.length > 60 ? displayText.substring(0, 57) + '...' : displayText

  // Show response type indicator
  const responseTypeLabel = response_type === 'yesno' ? 'Yes/No' 
    : response_type === 'multiple_choice' ? `Choice (${options?.length || 0} options)`
    : 'Text'

  return (
    <div
      className={`
        relative bg-white dark:bg-gray-800 rounded-lg border-2 ${borderColor}
        shadow-md hover:shadow-lg transition-all duration-200
        min-w-[320px] max-w-[320px]
        ${isOrphan ? 'border-dashed border-amber-400 dark:border-amber-500' : ''}
        ${changeStyle}
        ${selected ? 'ring-2 ring-purple-500/60' : ''}
      `}
    >
      {/* Change badge */}
      {changeBadge && (
        <div className={`absolute -top-2 -right-2 ${changeBadge.bg} rounded-full p-1 z-10`}>
          {changeBadge.icon}
        </div>
      )}

      {/* Top handle */}
      <Handle type="target" position={Position.Top} className="w-3 h-3 !bg-blue-500" />

      {/* Header */}
      <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <User className="w-4 h-4 text-blue-500 flex-shrink-0" />
          <span className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide truncate">
            Human Input
          </span>
        </div>
        <div />
      </div>

      {/* Content */}
      <div className="px-4 py-3">
        <div className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-2 line-clamp-2">
          {truncatedText}
        </div>
        <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
          <span className="px-2 py-1 bg-blue-100 dark:bg-blue-900/30 rounded">
            {responseTypeLabel}
          </span>
        </div>
      </div>

      {/* Footer metadata */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700 flex items-center justify-between bg-gray-50 dark:bg-gray-900/50">
        <div className="text-xs text-gray-500 dark:text-gray-400">
          Step {stepIndex + 1}
        </div>
        {statusIcon && <div className="flex-shrink-0">{statusIcon}</div>}
      </div>

      {/* Bottom handles - multiple for conditional routing */}
      {response_type === 'yesno' && (
        <>
          <Handle
            type="source"
            position={Position.Bottom}
            id="yes"
            className="w-3 h-3 !bg-green-500"
            style={{ left: '30%' }}
          />
          <Handle
            type="source"
            position={Position.Bottom}
            id="no"
            className="w-3 h-3 !bg-red-500"
            style={{ left: '70%' }}
          />
        </>
      )}
      {response_type === 'multiple_choice' && options && options.length > 0 && (
        <Handle
          type="source"
          position={Position.Bottom}
          className="w-3 h-3 !bg-blue-500"
        />
      )}
      {(!response_type || response_type === 'text') && (
        <Handle
          type="source"
          position={Position.Bottom}
          className="w-3 h-3 !bg-gray-500"
        />
      )}
    </div>
  )
})

HumanInputNode.displayName = 'HumanInputNode'
