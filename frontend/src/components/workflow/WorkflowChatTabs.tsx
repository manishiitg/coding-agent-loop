import React, { useMemo, useEffect, useCallback, useRef } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { ArrowDown, ListTree, Radio, Terminal } from 'lucide-react'
import { normalizeEventViewMode, useChatStore, type ChatTab, type TabSessionStatus } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

// ---------------------------------------------------------------------------
// WorkflowTabItem — per-tab component with narrow store subscriptions
// ---------------------------------------------------------------------------

interface WorkflowTabItemProps {
  tab: ChatTab
  isActive: boolean
  sessionStatus: TabSessionStatus | undefined
  onTabClick: (tabId: string) => void
}

const WorkflowTabItem = React.memo<WorkflowTabItemProps>(({
  tab,
  isActive,
  sessionStatus,
  onTabClick,
}) => {
  const displayName = tab.metadata?.phaseId === 'workflow-builder' && tab.name === 'Workflow Builder'
    ? 'Chat'
    : tab.name
  const indicatorColor = useMemo(() => {
    if (tab.isStreaming) return 'bg-green-500 animate-pulse'

    if (sessionStatus?.status) {
      switch (sessionStatus.status) {
        case 'running':  return 'bg-blue-500'
        case 'paused':   return 'bg-yellow-500'
        case 'completed': return 'bg-gray-400'
        case 'stopped':  return 'bg-gray-500'
        case 'error':    return 'bg-red-500'
        default:         return 'bg-gray-400'
      }
    }

    if (tab.isCompleted) return 'bg-gray-400'
    return 'bg-gray-400'
  }, [tab.isStreaming, tab.isCompleted, sessionStatus?.status])

  return (
    <div
      onClick={() => onTabClick(tab.tabId)}
      onKeyDown={(e) => e.key === 'Enter' && onTabClick(tab.tabId)}
      role="button"
      tabIndex={0}
      className={`
        flex min-w-0 items-center gap-1.5 px-2 py-1 rounded-t-md text-xs font-medium transition-colors cursor-pointer outline-none
        ${isActive
          ? 'bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 border-b-2 border-blue-500'
          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100'
        }
      `}
    >
      {/* Status Indicator */}
      <div className={`w-1.5 h-1.5 rounded-full ${indicatorColor}`} />

      {/* Scheduled-run badge — distinguishes read-only schedule observer tabs */}
      {tab.metadata?.isScheduledRun && (
        <Radio className="w-3 h-3 text-green-500 animate-pulse" />
      )}

      {/* Tab Name */}
      <span className="min-w-0 max-w-[14rem] truncate whitespace-nowrap">{displayName}</span>
    </div>
  )
})

WorkflowTabItem.displayName = 'WorkflowTabItem'

// ---------------------------------------------------------------------------
// WorkflowChatTabs — parent component
// ---------------------------------------------------------------------------

/**
 * Mini ChatTabs component for workflow mode chat area
 * Only shows workflow tabs that are active (have sessionId or isStreaming)
 */
export const WorkflowChatTabs: React.FC = () => {
  const {
    chatTabs,
    activeTabId,
    switchTab,
    tabSessionStatus,
    autoScroll,
    setAutoScroll,
    setTabViewMode,
  } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
    switchTab: state.switchTab,
    tabSessionStatus: state.tabSessionStatus,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    setTabViewMode: state.setTabViewMode,
  })))

  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)

  // Layout mode for the active tab — tree groups related events, flat shows the old feed.
  const activeViewMode = useChatStore(state => {
    const tab = activeTabId ? state.chatTabs[activeTabId] : null
    return normalizeEventViewMode(tab?.viewMode)
  })
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)

  // Filter to workflow tabs for the active preset, but always keep the active
  // workflow tab visible. Scheduled-run restores can briefly lack a preset match
  // while the tab is being created/switched, and hiding the active tab makes the
  // restore look like it failed.
  const activeWorkflowTabs = useMemo(() => {
    const allTabs = Object.values(chatTabs)
    const matched = allTabs.filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata.presetQueryId === activePresetId
    )
    const activeTab = activeTabId ? chatTabs[activeTabId] : undefined
    const activeWorkflowTab = activeTab?.metadata?.mode === 'workflow' ? activeTab : undefined

    const visibleById = new Map<string, ChatTab>()
    matched.forEach(tab => visibleById.set(tab.tabId, tab))
    if (activeWorkflowTab) visibleById.set(activeWorkflowTab.tabId, activeWorkflowTab)

    const visible = visibleById.size > 0
      ? Array.from(visibleById.values())
      : allTabs.filter(tab =>
          tab.metadata?.mode === 'workflow' &&
          tab.metadata?.phaseId === 'workflow-builder' &&
          !tab.metadata?.presetQueryId
        )
    return visible.sort((a, b) => a.createdAt - b.createdAt)
  }, [chatTabs, activePresetId, activeTabId])

  // Skip auto-close on initial mount
  const hasRenderedRef = useRef(false)

  const handleTabClick = useCallback((tabId: string) => {
    switchTab(tabId)
  }, [switchTab])

  // Close chat area when all workflow tabs are closed (but not on first render)
  useEffect(() => {
    if (!hasRenderedRef.current) {
      hasRenderedRef.current = true
      return
    }
    if (activeWorkflowTabs.length === 0) {
      setShowChatArea(false)
    }
  }, [activeWorkflowTabs.length, setShowChatArea])

  // Don't render if no active workflow tabs
  if (activeWorkflowTabs.length === 0) {
    return null
  }

  return (
    <div className="shrink-0 border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800">
      <div className="flex min-w-0 items-center gap-1 px-2 py-1">
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {activeWorkflowTabs.map((tab) => (
            <WorkflowTabItem
              key={tab.tabId}
              tab={tab}
              isActive={tab.tabId === activeTabId}
              sessionStatus={tabSessionStatus[tab.tabId]}
              onTabClick={handleTabClick}
            />
          ))}
        </div>

        {/* Auto-scroll and layout controls - only show when there are workflow tabs */}
        {activeWorkflowTabs.length > 0 && (
          <div className="flex shrink-0 items-center gap-1 border-l border-gray-200 pl-2 dark:border-gray-700">
            {activeViewMode !== 'terminal' && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                const newAutoScrollState = !autoScroll
                setAutoScroll(newAutoScrollState)

                if (newAutoScrollState) {
                  setTimeout(() => {
                    const chatAreaContainer = document.querySelector('[data-testid="chat-area-container"]')
                    if (chatAreaContainer) {
                      const scrollableElement = chatAreaContainer.querySelector('.overflow-y-auto')
                      if (scrollableElement) {
                        scrollableElement.scrollTo({
                          top: scrollableElement.scrollHeight,
                          behavior: 'smooth'
                        })
                      }
                    }
                  }, 50)
                }
              }}
              className={`
                flex items-center gap-1 px-2 py-1 rounded text-xs font-medium transition-colors
                ${autoScroll
                  ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-gray-100'
                  : 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'
                }
                hover:bg-gray-200 dark:hover:bg-gray-700
              `}
            >
              <ArrowDown className={`w-3.5 h-3.5 ${autoScroll ? 'opacity-70' : 'opacity-40'}`} />
              <span className="hidden sm:inline">
                {autoScroll ? 'Auto-scroll' : 'Manual'}
              </span>
            </button>
            )}

            {/* Layout Mode */}
            <div
              data-tour="event-view-mode"
              data-testid="tour-event-view-mode"
              className="inline-flex items-center rounded-full border border-gray-200 bg-gray-100 p-0.5 dark:border-gray-700 dark:bg-gray-800"
              role="group"
              aria-label="Event layout mode"
            >
              {([
                { mode: 'tree' as const, Icon: ListTree, label: 'Tree', tip: 'Tree view — group events by workflow and agent' },
                { mode: 'terminal' as const, Icon: Terminal, label: 'Terminal', tip: 'Terminal view — show only the terminal panes, no events' },
              ]).map(({ mode, Icon, label, tip }) => (
                <Tooltip key={mode}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation()
                        if (activeTabId) {
                          setTabViewMode(activeTabId, mode)
                        }
                      }}
                      aria-label={label}
                      aria-pressed={activeViewMode === mode}
                      className={`flex h-6 w-6 items-center justify-center rounded-full transition-colors ${
                        activeViewMode === mode
                          ? 'bg-blue-600 text-white shadow-sm'
                          : 'text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                      }`}
                    >
                      <Icon className="h-3.5 w-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{tip}</p>
                  </TooltipContent>
                </Tooltip>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
