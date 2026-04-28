import React, { useMemo, useEffect, useCallback, useRef } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { X, ArrowDown, List, ListTree, Radio } from 'lucide-react'
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
  onTabClose: (e: React.MouseEvent, tabId: string) => void
}

const WorkflowTabItem = React.memo<WorkflowTabItemProps>(({
  tab,
  isActive,
  sessionStatus,
  onTabClick,
  onTabClose,
}) => {
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
        flex items-center gap-1.5 px-2 py-1 rounded-t-md text-xs font-medium transition-colors cursor-pointer outline-none
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
      <span className="whitespace-nowrap">{tab.name}</span>

      {/* Close Button */}
      <button
        onClick={(e) => onTabClose(e, tab.tabId)}
        className={`
          ml-0.5 p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-600
          ${isActive ? 'opacity-70 hover:opacity-100' : 'opacity-0 hover:opacity-70'}
          transition-opacity
        `}
        title="Close tab"
      >
        <X className="w-3 h-3" />
      </button>
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
    closeTab,
    tabSessionStatus,
    autoScroll,
    setAutoScroll,
    setTabViewMode,
  } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
    switchTab: state.switchTab,
    closeTab: state.closeTab,
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

  // Filter to only show workflow tabs for the active preset.
  // Strict match — untagged tabs (no presetQueryId) are treated as orphans and hidden,
  // because the previous "include if untagged" fallback caused tabs to leak across
  // every preset whenever a session couldn't be tied back to its source preset.
  const activeWorkflowTabs = useMemo(() => {
    const allTabs = Object.values(chatTabs)
    const matched = allTabs.filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata.presetQueryId === activePresetId
    )
    return matched.sort((a, b) => a.createdAt - b.createdAt)
  }, [chatTabs, activePresetId])

  // Skip auto-close on initial mount
  const hasRenderedRef = useRef(false)

  const handleTabClick = useCallback((tabId: string) => {
    switchTab(tabId)
  }, [switchTab])

  const handleTabClose = useCallback(async (e: React.MouseEvent, tabId: string) => {
    e.stopPropagation()
    await closeTab(tabId)
  }, [closeTab])

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
    <div className="flex items-center gap-1 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 px-2 py-1 overflow-x-auto">
      {activeWorkflowTabs.map((tab) => (
        <WorkflowTabItem
          key={tab.tabId}
          tab={tab}
          isActive={tab.tabId === activeTabId}
          sessionStatus={tabSessionStatus[tab.tabId]}
          onTabClick={handleTabClick}
          onTabClose={handleTabClose}
        />
      ))}

      {/* Auto-scroll Toggle and Close Button - only show when there are workflow tabs */}
      {activeWorkflowTabs.length > 0 && (
        <div className="ml-auto flex items-center gap-1 border-l border-gray-200 dark:border-gray-700 pl-2">
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

          {/* Layout Toggle — switch between tree hierarchy and the old flat feed */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  if (activeTabId) {
                    setTabViewMode(activeTabId, activeViewMode === 'tree' ? 'flat' : 'tree')
                  }
                }}
                className={`flex items-center gap-1 p-1.5 rounded text-xs font-medium transition-colors
                  ${activeViewMode === 'tree'
                    ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'
                    : 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'
                  }
                  hover:bg-gray-200 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100
                `}
              >
                {activeViewMode === 'tree' ? (
                  <ListTree className="w-3.5 h-3.5" />
                ) : (
                  <List className="w-3.5 h-3.5" />
                )}
                <span className="hidden sm:inline">
                  {activeViewMode === 'tree' ? 'Tree' : 'Flat'}
                </span>
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>{activeViewMode === 'tree' ? 'Tree view — group events by workflow and agent' : 'Flat view — show events in chronological order'}</p>
            </TooltipContent>
          </Tooltip>

        </div>
      )}
    </div>
  )
}
