import React, { useEffect, useState } from 'react'
import { MessageSquare, X } from 'lucide-react'
import type { ChatTab } from '../../stores/useChatStore'
import { useAuthStore } from '../../stores/useAuthStore'
import { isWorkflowReadOnly } from '../../utils/workflowPermissions'

export interface AgentWorksChatTabItemProps {
  tab: ChatTab
  isActive: boolean
  canClose: boolean
  isBlank: boolean
  displayName?: string
  onTabClick: (tabId: string) => void
  onCloseTab: (tabId: string) => void
  onMakeInteractive?: (tabId: string) => void
}

const ALLOW_MAKE_SCHEDULE_INTERACTIVE = false
const TAB_STATUS_DOT: Record<'busy' | 'idle' | 'stopped', { cls: string; label: string }> = {
  busy: { cls: 'bg-[hsl(var(--info))] animate-pulse', label: 'Busy' },
  idle: { cls: 'bg-[hsl(var(--success))]', label: 'Idle' },
  stopped: { cls: 'bg-muted-foreground/60', label: 'Stopped' },
}

/** The shared AgentWorks Builder/Chat tab pill used by workflows and Work. */
export const AgentWorksChatTabItem = React.memo<AgentWorksChatTabItemProps>(({
  tab, isActive, canClose, isBlank, displayName: displayNameOverride,
  onTabClick, onCloseTab, onMakeInteractive,
}) => {
  const isReadOnlyUser = useAuthStore(state => isWorkflowReadOnly(state.user, state.isMultiUserMode))
  const displayName = displayNameOverride ?? tab.name
  const rawStatus: 'busy' | 'idle' | 'stopped' = tab.isStreaming || tab.hasRunningBgAgents
    ? 'busy'
    : tab.isCompleted ? 'stopped' : 'idle'
  const [status, setStatus] = useState(rawStatus)
  useEffect(() => {
    if (rawStatus === status) return
    if (rawStatus === 'busy') {
      setStatus('busy')
      return
    }
    const timer = setTimeout(() => setStatus(rawStatus), 1200)
    return () => clearTimeout(timer)
  }, [rawStatus, status])
  const dot = TAB_STATUS_DOT[status]
  const isBusy = status === 'busy'

  return (
    <div
      onClick={() => onTabClick(tab.tabId)}
      onKeyDown={(event) => event.key === 'Enter' && onTabClick(tab.tabId)}
      role="button"
      tabIndex={0}
      className={`group flex min-w-0 cursor-pointer items-center gap-1.5 rounded-t-md px-2 py-1 text-xs font-medium outline-none transition-colors ${
        isActive
          ? 'border-b-2 border-blue-500 bg-white text-gray-900 dark:bg-gray-900 dark:text-gray-100'
          : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
      }`}
    >
      {!isBlank && <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dot.cls}`} title={dot.label} aria-label={dot.label} />}
      <span
        className="min-w-0 max-w-[14rem] truncate whitespace-nowrap"
        title={displayName !== tab.name ? tab.name : undefined}
      >
        {displayName}
      </span>
      {ALLOW_MAKE_SCHEDULE_INTERACTIVE && onMakeInteractive && tab.metadata?.isViewOnly && (tab.metadata?.isScheduledRun || tab.metadata?.isBotRun) && !isReadOnlyUser && (
        <button type="button" onClick={(event) => { event.stopPropagation(); onMakeInteractive(tab.tabId) }} className="ml-0.5 rounded p-0.5 text-blue-600 opacity-80 hover:bg-blue-100 hover:opacity-100 dark:text-blue-300 dark:hover:bg-blue-900/40" title="Interact in Automation Builder" aria-label="Interact in Automation Builder">
          <MessageSquare className="h-3 w-3" />
        </button>
      )}
      {canClose && (
        <button
          type="button"
          disabled={isBusy}
          onClick={(event) => { event.stopPropagation(); if (!isBusy) onCloseTab(tab.tabId) }}
          className={`ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded text-gray-400 transition-colors ${isBusy ? 'cursor-not-allowed opacity-40' : 'hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-gray-700 dark:hover:text-gray-200'}`}
          aria-label={isBusy ? `${displayName} is still running — stop it before closing` : `Close ${displayName}`}
          title={isBusy ? 'Still running — stop the run before closing' : 'Close tab'}
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  )
})

AgentWorksChatTabItem.displayName = 'AgentWorksChatTabItem'
