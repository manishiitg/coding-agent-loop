import React, { useMemo, useEffect, useCallback, useRef } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { X, ArrowDown, Square, Maximize2, Minimize2 } from 'lucide-react'
import { useChatStore, type ChatTab, type TabSessionStatus } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { shouldShowEventByMode } from '../events/eventModeUtils'
import { agentApi } from '../../services/api'
import { logger } from '../../utils/logger'
import type { PollingEvent } from '../../services/api-types'

const EMPTY_EVENTS: PollingEvent[] = []

// ---------------------------------------------------------------------------
// WorkflowTabItem — per-tab component with narrow store subscriptions
// ---------------------------------------------------------------------------

interface WorkflowTabItemProps {
  tab: ChatTab
  isActive: boolean
  sessionStatus: TabSessionStatus | undefined
  onTabClick: (tabId: string) => void
  onTabClose: (e: React.MouseEvent, tabId: string) => void
  onStopSession: (e: React.MouseEvent, tab: ChatTab) => void
}

const WorkflowTabItem = React.memo<WorkflowTabItemProps>(({
  tab,
  isActive,
  sessionStatus,
  onTabClick,
  onTabClose,
  onStopSession,
}) => {
  // Narrow selector: only re-renders when THIS tab's events change
  const events = useChatStore(
    state => tab.sessionId ? state.tabEvents[tab.sessionId] || EMPTY_EVENTS : EMPTY_EVENTS
  )

  // Narrow selector for streaming status (inlined from getTabStreamingStatus)
  const isTabStreaming = useChatStore(state => {
    const storeTab = state.chatTabs[tab.tabId]
    if (!storeTab || storeTab.isCompleted) return false
    const isPolling = state.pollingInterval !== null
    return isPolling ? storeTab.isStreaming !== false : false
  })

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

  const newEventCount = useMemo(() => {
    if (isActive || !tab.sessionId) return 0
    const visibleEvents = events.filter(
      e => e.type && shouldShowEventByMode(e.type)
    )
    const lastViewedCount = tab.lastViewedEventCounts?.micro ?? tab.lastViewedEventCount ?? 0
    return Math.max(0, visibleEvents.length - lastViewedCount)
  }, [isActive, tab.sessionId, tab.lastViewedEventCounts, tab.lastViewedEventCount, events])

  const canStop = tab.sessionId && (isTabStreaming || tab.isStreaming)

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

      {/* Tab Name */}
      <span className="whitespace-nowrap">{tab.name}</span>

      {/* New Events Badge - show for inactive tabs with new events */}
      {!isActive && newEventCount > 0 && (
        <span className="flex items-center justify-center min-w-[18px] h-4 px-1.5 text-xs font-semibold text-white bg-red-500 dark:bg-red-600 rounded-full">
          {newEventCount > 99 ? '99+' : newEventCount}
        </span>
      )}


      {/* Stop Button - show for tabs with sessionId that are streaming/running */}
      {canStop && (
        <button
          onClick={(e) => onStopSession(e, tab)}
          className={`
            ml-0.5 p-0.5 rounded hover:bg-red-100 dark:hover:bg-red-900/30
            ${isActive ? 'opacity-70 hover:opacity-100' : 'opacity-0 hover:opacity-70'}
            transition-opacity
          `}
          title="Stop session"
        >
          <Square className="w-3 h-3 text-red-600 dark:text-red-400" fill="currentColor" />
        </button>
      )}

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
    setTabStreaming,
  } = useChatStore(useShallow(state => ({
    chatTabs: state.chatTabs,
    activeTabId: state.activeTabId,
    switchTab: state.switchTab,
    closeTab: state.closeTab,
    tabSessionStatus: state.tabSessionStatus,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    setTabStreaming: state.setTabStreaming,
  })))

  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  const chatAreaExpanded = useWorkflowStore(state => state.chatAreaExpanded)
  const setChatAreaExpanded = useWorkflowStore(state => state.setChatAreaExpanded)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)

  // Filter to only show workflow tabs for the active preset (have sessionId or isStreaming)
  const activeWorkflowTabs = useMemo(() => {
    return Object.values(chatTabs)
      .filter(tab =>
        tab.metadata?.mode === 'workflow' &&
        (tab.sessionId || tab.isStreaming) &&
        // Strict preset match — only show tabs explicitly tagged with the current preset
        tab.metadata?.presetQueryId === activePresetId
      )
      .sort((a, b) => a.createdAt - b.createdAt)
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

  const handleStopSession = useCallback(async (e: React.MouseEvent, tab: ChatTab) => {
    e.stopPropagation()

    if (!tab.sessionId) {
      logger.warn('WorkflowChatTabs', 'No session ID to stop for tab:', tab.tabId)
      return
    }

    try {
      await agentApi.stopSession(tab.sessionId)
      logger.debug('WorkflowChatTabs', `Stopped session ${tab.sessionId} for tab ${tab.tabId}`)
      setTabStreaming(tab.tabId, false)
    } catch (error) {
      logger.error('WorkflowChatTabs', 'Failed to stop session:', error)
    }
  }, [setTabStreaming])

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
          onStopSession={handleStopSession}
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

          {/* Expand/Collapse Button */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setChatAreaExpanded(!chatAreaExpanded)
                }}
                className="flex items-center justify-center p-1.5 rounded text-xs font-medium transition-colors bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100"
                title={chatAreaExpanded ? "Restore width" : "Expand width"}
              >
                {chatAreaExpanded ? (
                  <Minimize2 className="w-3.5 h-3.5" />
                ) : (
                  <Maximize2 className="w-3.5 h-3.5" />
                )}
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>{chatAreaExpanded ? "Restore width" : "Expand width"}</p>
            </TooltipContent>
          </Tooltip>

          {/* Close Button - closes the entire chat area panel */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setShowChatArea(false)
                }}
                className="flex items-center justify-center p-1.5 rounded text-xs font-medium transition-colors bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100"
                title="Close chat area"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Close chat area</p>
            </TooltipContent>
          </Tooltip>
        </div>
      )}
    </div>
  )
}
