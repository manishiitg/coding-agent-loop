import { memo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, ListOrdered, MessageSquare, Code2, ShieldCheck, Repeat } from 'lucide-react'
import type { MessageSequenceNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'
import type { MessageSequenceItem } from '../../../utils/stepConfigMatching'

interface MessageSequenceNodeProps {
  data: MessageSequenceNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-violet-300 dark:border-violet-700',
  running: 'border-violet-500 dark:border-violet-400',
  executing: 'border-violet-500 dark:border-violet-400',
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
  running: <Loader2 className="w-4 h-4 text-violet-500 animate-spin" />,
  executing: <Loader2 className="w-4 h-4 text-violet-500 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />
}

// Per-item-type presentation: short label, badge colors, and icon. Falls back
// to a neutral "step" treatment for any item type the backend adds later.
function itemPresentation(item: MessageSequenceItem): { label: string; badge: string; icon: ReactElement } {
  switch (item.type) {
    case 'user_message':
      return {
        label: item.kind && item.kind !== 'execution' ? item.kind.replace(/_/g, ' ') : 'message',
        badge: 'bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300',
        icon: <MessageSquare className="w-3 h-3 flex-shrink-0" />
      }
    case 'code':
      return {
        label: 'code',
        badge: 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300',
        icon: <Code2 className="w-3 h-3 flex-shrink-0" />
      }
    case 'prevalidation':
      return {
        label: 'prevalidation',
        badge: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
        icon: <ShieldCheck className="w-3 h-3 flex-shrink-0" />
      }
    case 'foreach':
      return {
        label: 'foreach',
        badge: 'bg-teal-100 text-teal-700 dark:bg-teal-900/30 dark:text-teal-300',
        icon: <Repeat className="w-3 h-3 flex-shrink-0" />
      }
    default:
      return {
        label: item.type || 'step',
        badge: 'bg-gray-100 text-gray-600 dark:bg-gray-700/50 dark:text-gray-300',
        icon: <MessageSquare className="w-3 h-3 flex-shrink-0" />
      }
  }
}

function itemPrimaryText(item: MessageSequenceItem): string {
  const raw = item.title || item.message || item.script_path || item.source || item.id || ''
  const flat = raw.replace(/\s+/g, ' ').trim()
  return flat.length > 52 ? flat.substring(0, 49) + '…' : flat
}

export const MessageSequenceNode = memo(({ data, selected }: MessageSequenceNodeProps) => {
  const { title, description, items, status, stepIndex, changeType, isOrphan } = data

  const borderColor = statusBorderColors[status] || statusBorderColors.pending
  const statusIcon = statusIcons[status] || null
  const changeStyle = changeType ? changeHighlightStyles[changeType] : ''
  const changeBadge = changeType ? changeBadgeStyles[changeType] : null

  const seqItems = items || []
  const displayTitle = title || `Message Sequence ${stepIndex + 1}`
  // Keep the node compact: show the first handful of items, then a "+N more"
  // summary line so a long queue doesn't blow up the canvas node height.
  const MAX_VISIBLE = 6
  const visibleItems = seqItems.slice(0, MAX_VISIBLE)
  const hiddenCount = seqItems.length - visibleItems.length

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
      <Handle type="target" position={Position.Top} className="w-3 h-3 !bg-violet-500" />

      {/* Header */}
      <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <ListOrdered className="w-4 h-4 text-violet-500 flex-shrink-0" />
          <span className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide truncate">
            Message Sequence
          </span>
        </div>
        <span className="text-[10px] font-medium text-violet-600 dark:text-violet-300 bg-violet-100 dark:bg-violet-900/30 rounded px-1.5 py-0.5 flex-shrink-0">
          {seqItems.length} {seqItems.length === 1 ? 'item' : 'items'}
        </span>
      </div>

      {/* Title + description */}
      <div className="px-4 pt-3">
        <div className="text-sm font-medium text-gray-900 dark:text-gray-100 line-clamp-2">
          {displayTitle}
        </div>
        {description && (
          <div className="mt-1 text-xs text-gray-500 dark:text-gray-400 line-clamp-2">
            {description}
          </div>
        )}
      </div>

      {/* Ordered item queue */}
      <div className="px-4 py-3 space-y-1.5">
        {visibleItems.length === 0 && (
          <div className="text-xs italic text-gray-400 dark:text-gray-500">No items defined</div>
        )}
        {visibleItems.map((item, idx) => {
          const pres = itemPresentation(item)
          const primary = itemPrimaryText(item)
          return (
            <div key={item.id || idx} className="flex items-center gap-2 min-w-0">
              <span className="text-[10px] font-mono text-gray-400 dark:text-gray-500 w-4 flex-shrink-0 text-right">
                {idx + 1}
              </span>
              <span className={`flex items-center gap-1 text-[10px] font-medium rounded px-1.5 py-0.5 flex-shrink-0 ${pres.badge}`}>
                {pres.icon}
                {pres.label}
              </span>
              <span className="text-xs text-gray-700 dark:text-gray-300 truncate min-w-0">
                {primary}
              </span>
            </div>
          )
        })}
        {hiddenCount > 0 && (
          <div className="text-[11px] text-gray-400 dark:text-gray-500 pl-6">
            +{hiddenCount} more {hiddenCount === 1 ? 'item' : 'items'}
          </div>
        )}
      </div>

      {/* Footer metadata */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700 flex items-center justify-between bg-gray-50 dark:bg-gray-900/50">
        <div className="text-xs text-gray-500 dark:text-gray-400">
          Step {stepIndex + 1}
        </div>
        {statusIcon && <div className="flex-shrink-0">{statusIcon}</div>}
      </div>

      {/* Bottom handle — sequence is linear, single exit */}
      <Handle type="source" position={Position.Bottom} className="w-3 h-3 !bg-violet-500" />
    </div>
  )
})

MessageSequenceNode.displayName = 'MessageSequenceNode'
