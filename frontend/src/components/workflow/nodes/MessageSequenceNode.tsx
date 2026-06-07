import { memo, useEffect, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { BookOpen, CheckCircle, Database, FileText, XCircle, Loader2, Plus, RefreshCw, ListOrdered, MessageSquare, Code2, ShieldCheck, Repeat } from 'lucide-react'
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

// NOTE: uses the same colored-bg pattern as the item badges (which render
// correctly). The previous `bg-white/80` base rendered as empty/low-contrast
// grey boxes on the canvas.
const metadataChipBase = 'inline-flex h-5 shrink-0 items-center gap-1 rounded-md bg-gray-100 px-1.5 text-[10px] font-medium leading-none text-gray-700 dark:bg-gray-700/60 dark:text-gray-200'

function textReferencesKnowledgebase(text: string): boolean {
  return /\bknowledgebase[\\/]/i.test(text)
}

function textReferencesDatabase(text: string): boolean {
  return /\$DB_PATH\b|\bdb[\\/]|db\.sqlite|\bsqlite3?\b/i.test(text)
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
  const { id, title, description, items, status, stepIndex, changeType, isOrphan, step } = data
  const orphanReuseCount = (data as { orphanReuseCount?: number }).orphanReuseCount ?? 0

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
  const writesDb = seqItems.some(item => item.write_access?.db === true)
  const writesKnowledgebase = seqItems.some(item => item.write_access?.knowledgebase === true)
  const writesLearnings = seqItems.some(item => item.write_access?.learnings === true)
  const storeReferenceText = [
    description,
    ...seqItems.flatMap(item => [
      item.title,
      item.message,
      item.script_path,
      item.source,
      ...(item.input_files || []),
      ...(item.output_files || []),
      ...(item.validation_schema?.files?.map(file => file.file_name) || []),
      ...(item.prevalidation?.files?.map(file => file.file_name) || [])
    ])
  ].filter((value): value is string => typeof value === 'string' && value.length > 0).join('\n')
  const referencesDb = textReferencesDatabase(storeReferenceText)
  const referencesKnowledgebase = textReferencesKnowledgebase(storeReferenceText)
  const hasDbItem = writesDb || referencesDb || seqItems.some(item => item.kind === 'db')
  const hasKnowledgebaseItem = writesKnowledgebase || referencesKnowledgebase || seqItems.some(item => item.kind === 'knowledgebase')
  const dbBadgeLabel = writesDb ? 'DB write' : referencesDb ? 'Uses DB' : 'DB'
  const knowledgebaseBadgeLabel = writesKnowledgebase ? 'KB write' : referencesKnowledgebase ? 'Uses KB' : 'KB'

  // Step-level config metadata (same as a regular step card): a message_sequence
  // step can carry a learning objective + learnings/KB/DB access, not just items.
  const agentConfig = step?.agent_configs
  const learningObjective = typeof agentConfig?.learning_objective === 'string' ? agentConfig.learning_objective.trim() : ''
  const learningsAccess = agentConfig?.learnings_access
  const knowledgebaseAccess = agentConfig?.knowledgebase_access
  const dbAccess = agentConfig?.db_access
  const accessChips = [
    learningsAccess ? { icon: BookOpen, label: `Learnings: ${learningsAccess}` } : null,
    knowledgebaseAccess ? { icon: FileText, label: `KB: ${knowledgebaseAccess}` } : null,
    dbAccess ? { icon: Database, label: `DB: ${dbAccess}` } : null,
  ].filter((c): c is { icon: typeof BookOpen; label: string } => c !== null)

  useEffect(() => {
    if (!import.meta.env.DEV) return
    if (!hasDbItem && !hasKnowledgebaseItem && !writesLearnings) return

    console.log('[WorkflowMessageSequenceNode] store metadata', {
      nodeId: id,
      stepId: step?.id,
      title: displayTitle,
      writes_db: writesDb,
      writes_knowledgebase: writesKnowledgebase,
      writes_learnings: writesLearnings,
      references_database: referencesDb,
      references_knowledgebase: referencesKnowledgebase
    })
  }, [
    displayTitle,
    hasDbItem,
    hasKnowledgebaseItem,
    id,
    referencesDb,
    referencesKnowledgebase,
    step?.id,
    writesDb,
    writesKnowledgebase,
    writesLearnings
  ])

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

      {/* Orphan badge (top-left): shows whether this orphan is reused or unused */}
      {isOrphan && (
        <div className="absolute top-0 left-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-br-lg rounded-tl-lg bg-amber-500 text-white text-[10px] font-medium shadow-lg">
          <span>{orphanReuseCount > 0 ? `Orphan · reused ×${orphanReuseCount}` : 'Orphan · unused'}</span>
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
        {(hasDbItem || hasKnowledgebaseItem || writesLearnings) && (
          <div className="mt-2 flex min-w-0 flex-wrap items-center gap-1.5">
            {hasDbItem && (
              <span
                className={metadataChipBase}
                title={writesDb ? 'Message sequence writes to DB' : referencesDb ? 'Message sequence references DB paths or SQLite' : 'Message sequence has a DB item'}
              >
                <Database className="h-3 w-3" />
                <span>{dbBadgeLabel}</span>
              </span>
            )}
            {hasKnowledgebaseItem && (
              <span
                className={metadataChipBase}
                title={writesKnowledgebase ? 'Message sequence writes to KB' : referencesKnowledgebase ? 'Message sequence references knowledgebase paths' : 'Message sequence has a KB item'}
              >
                <FileText className="h-3 w-3" />
                <span>{knowledgebaseBadgeLabel}</span>
              </span>
            )}
            {writesLearnings && (
              <span
                className={metadataChipBase}
                title="Message sequence writes to learnings"
              >
                <BookOpen className="h-3 w-3" />
                <span>Learning write</span>
              </span>
            )}
          </div>
        )}
        {learningObjective && (
          <div className="mt-2 rounded-md bg-amber-500/10 px-2 py-1">
            <div className="text-[9px] font-semibold uppercase tracking-wide text-amber-700 dark:text-amber-400">Learning Objective</div>
            <div className="text-[11px] leading-snug text-gray-700 dark:text-gray-300 line-clamp-2">{learningObjective}</div>
          </div>
        )}
        {accessChips.length > 0 && (
          <div className="mt-2 flex min-w-0 flex-wrap items-center gap-1.5">
            {accessChips.map(chip => (
              <span key={chip.label} className={metadataChipBase} title={chip.label}>
                <chip.icon className="h-3 w-3" />
                <span>{chip.label}</span>
              </span>
            ))}
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
