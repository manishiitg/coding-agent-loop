import React, { useEffect, useMemo } from 'react'
import { X, Plus, ArrowDown } from 'lucide-react'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { EventModeToggle } from './events'

interface ChatTabsProps {
  // For chat mode: callback when starting a new chat
  onNewChat?: () => void
  // Auto-scroll state and toggle
  autoScroll?: boolean
  onToggleAutoScroll?: () => void
}

export const ChatTabs: React.FC<ChatTabsProps> = ({ autoScroll, onToggleAutoScroll }) => {
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const {
    chatTabs,
    activeTabId,
    switchTab,
    closeTab,
    tabSessionStatus,
    fetchAllTabSessionStatuses,
    tabEvents,
    autoScroll: storeAutoScroll,
    setAutoScroll
  } = useChatStore()
  
  // Use prop if provided, otherwise use store value
  const effectiveAutoScroll = autoScroll !== undefined ? autoScroll : storeAutoScroll
  const handleToggleAutoScroll = onToggleAutoScroll || (() => {
    setAutoScroll(!storeAutoScroll)
  })
  
  // Filter tabs by current mode
  // In workflow mode, only show chat tabs in global ChatTabs (workflow tabs show in chat area)
  const modeTabs = useMemo(() => {
    if (selectedModeCategory === 'workflow') {
      // In workflow mode, only show chat tabs (workflow tabs are shown in the chat area panel)
      return Object.values(chatTabs).filter(tab => 
        tab.metadata?.mode === 'chat'
      ).sort((a, b) => a.createdAt - b.createdAt)
    }
    // In chat mode, show all chat tabs
    return Object.values(chatTabs).filter(tab => 
      tab.metadata?.mode === selectedModeCategory
    ).sort((a, b) => a.createdAt - b.createdAt)
  }, [chatTabs, selectedModeCategory])
  
  // Get stable list of tab IDs with sessions for dependency
  const tabIdsWithSessions = useMemo(() => {
    return modeTabs
      .filter(tab => tab.sessionId)
      .map(tab => `${tab.tabId}:${tab.sessionId}`)
      .join(',')
  }, [modeTabs])
  
  // Fetch session status for tabs with session IDs
  useEffect(() => {
    // Use modeTabs directly (it's memoized, so safe to use)
    const tabsWithSessions = modeTabs.filter(tab => tab.sessionId)
    
    if (tabsWithSessions.length === 0) {
      console.log('[ChatTabs] No tabs with session IDs to fetch status for')
      return
    }
    
    const tabIds = tabsWithSessions.map(tab => tab.tabId)
    
    // Fetch status for all tabs
    const fetchStatuses = async () => {
      console.log(`[ChatTabs] Fetching session status for ${tabIds.length} tabs`)
      await fetchAllTabSessionStatuses(tabIds)
    }
    
    // Fetch immediately
    fetchStatuses()
    
    // Refresh status every 5 seconds
    const interval = setInterval(fetchStatuses, 5000)
    return () => clearInterval(interval)
    // Only depend on tabIdsWithSessions (the string) - it changes only when tabs/sessions actually change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabIdsWithSessions])

  const handleTabClick = (tabId: string) => {
    switchTab(tabId)
  }

  const handleTabClose = async (e: React.MouseEvent, tabId: string) => {
    e.stopPropagation()
    await closeTab(tabId)
  }

  const handleNewTab = async () => {
    console.log('[ChatTabs] handleNewTab called, mode:', selectedModeCategory)
    // In workflow mode, phases are started from WorkflowToolbar, not from ChatTabs
    if (selectedModeCategory === 'workflow') {
      console.log('[ChatTabs] Workflow mode: phases should be started from WorkflowToolbar, not ChatTabs')
      return
    }
    
    if (selectedModeCategory === 'chat' || selectedModeCategory === 'skill_builder') {
      // Create a new chat/skill tab
      console.log(`[ChatTabs] Creating new ${selectedModeCategory} tab...`)
      const chatStore = useChatStore.getState()
      const allChatTabs = Object.values(chatTabs).filter(tab => 
        tab.metadata?.mode === selectedModeCategory
      )
      const chatNumber = allChatTabs.length + 1
      const tabName = `Chat ${chatNumber}`
      
      console.log(`[ChatTabs] Tab name: ${tabName}, existing tabs: ${allChatTabs.length}`)
      
      try {
        console.log(`[ChatTabs] Creating new tab: ${tabName} in mode: ${selectedModeCategory}`)
        const newTabId = await chatStore.createChatTab(tabName, { mode: selectedModeCategory })
        console.log(`[ChatTabs] ✅ createChatTab returned tab ID: ${newTabId}`)
        
        // Note: Tab creation is verified inside createChatTab itself
        // The tab should now be active and visible in the UI
        // React will re-render and show the new tab automatically via the useChatStore hook
        console.log(`[ChatTabs] ✅ Tab creation completed. Tab ID: ${newTabId}`)
      } catch (error) {
        console.error('[ChatTabs] ❌ Failed to create new chat tab:', error)
        if (error instanceof Error) {
          console.error('[ChatTabs] Error details:', {
            name: error.name,
            message: error.message,
            stack: error.stack
          })
        }
        alert(`Failed to create new tab: ${error instanceof Error ? error.message : String(error)}`)
      }
    } else {
      console.warn('[ChatTabs] Unknown mode category:', selectedModeCategory)
    }
  }

  // Get tab color/indicator based on session status
  const getTabIndicator = (tab: ChatTab) => {
    const sessionStatus = tabSessionStatus[tab.tabId]
    
    // Priority: streaming > session status > completed > default
    if (tab.isStreaming) {
      return 'bg-green-500 animate-pulse' // Streaming
    }
    
    if (sessionStatus?.status) {
      switch (sessionStatus.status) {
        case 'running':
          return 'bg-blue-500' // Active/running
        case 'paused':
          return 'bg-yellow-500' // Paused
        case 'completed':
          return 'bg-gray-400' // Completed
        case 'stopped':
          return 'bg-gray-500' // Stopped
        case 'error':
          return 'bg-red-500' // Error
        default:
          return 'bg-gray-400' // Unknown status
      }
    }
    
    if (tab.isCompleted) {
      return 'bg-gray-400' // Completed
    }
    
    // Default: no session or unknown
    return 'bg-gray-400'
  }
  
  // Show tabs bar only in chat/skill_builder mode (workflow tabs are shown in WorkflowChatTabs inside ChatArea panel)
  // In workflow mode, ChatTabs should not be visible at all
  const shouldShowTabsBar = selectedModeCategory === 'chat' || selectedModeCategory === 'skill_builder'
  const hasTabs = modeTabs.length > 0
  
  // In workflow mode, don't show ChatTabs at all
  if (!shouldShowTabsBar) {
    return null
  }
  
  // Only show border when there are actual tabs (not just the "New Chat" button)
  const borderClass = hasTabs ? 'border-b border-gray-200 dark:border-gray-700' : ''
  
  return (
    <div className={`flex-shrink-0 flex items-center gap-1 bg-gray-50 dark:bg-gray-800 px-2 py-1 overflow-x-auto${borderClass ? ` ${borderClass}` : ''}`}>
      {/* Existing Tabs */}
      {modeTabs.map((tab) => {
        const isActive = tab.tabId === activeTabId
        const indicatorColor = getTabIndicator(tab)
        
        // Determine active border color based on mode
          const activeBorderClass = selectedModeCategory === 'skill_builder'
            ? 'border-emerald-500'
            : 'border-blue-500'

          // Calculate new event count for inactive tabs
          // NOTE: Events are already filtered by backend based on event_mode, so no need to filter again
          const newEventCount = (() => {
            if (isActive || !tab.sessionId) return 0
            
            // Get all events for this tab's session (already filtered by backend)
            const allEvents = tabEvents[tab.sessionId] || []
            
            // Get the last viewed count (events are already filtered, so this is the filtered count)
            const lastViewedCount = tab.lastViewedEventCount || 0
            
            // New events = current count - last viewed count
            const newCount = Math.max(0, allEvents.length - lastViewedCount)
            return newCount
          })()
          
          return (
            <div
              key={tab.tabId}
              onClick={() => handleTabClick(tab.tabId)}
              onKeyDown={(e) => e.key === 'Enter' && handleTabClick(tab.tabId)}
              role="button"
              tabIndex={0}
              data-testid={`chat-tab-${tab.tabId}`}
              className={`
                flex items-center gap-2 px-3 py-1.5 rounded-t-md text-sm font-medium transition-colors cursor-pointer outline-none
                ${isActive
                  ? `bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 border-b-2 ${activeBorderClass}`
                  : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100'
                }
              `}
            >
              {/* Status Indicator */}
              <div className={`w-2 h-2 rounded-full ${indicatorColor}`} />
              
              {/* Tab Name */}
              <span className="whitespace-nowrap">{tab.name}</span>
              
              {/* New Events Badge - show for inactive tabs with new events */}
              {!isActive && newEventCount > 0 && (
                <span className="flex items-center justify-center min-w-[18px] h-4 px-1.5 text-xs font-semibold text-white bg-red-500 dark:bg-red-600 rounded-full">
                  {newEventCount > 99 ? '99+' : newEventCount}
                </span>
              )}
              
              {/* Event Mode Toggle - show inside active tab header */}
              {isActive && (
                <div className="ml-1 flex items-center" onClick={(e) => e.stopPropagation()}>
                  <EventModeToggle />
                </div>
              )}
              
              {/* Close Button */}
              <button
                onClick={(e) => handleTabClose(e, tab.tabId)}
                data-testid={`close-tab-${tab.tabId}`}
                className={`
                  ml-1 p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-600
                  ${isActive ? 'opacity-70 hover:opacity-100' : 'opacity-0 hover:opacity-70'}
                  transition-opacity
                `}
                title="Close tab"
              >
                <X className="w-3 h-3" />
              </button>
            </div>
          )
        })}
      
      {/* New Tab Button - Only show in chat/skill_builder mode (workflow phases are started from WorkflowToolbar) */}
      {(selectedModeCategory === 'chat' || selectedModeCategory === 'skill_builder') && (
        <button
          onClick={handleNewTab}
          data-testid="new-chat-button"
          className="flex items-center gap-1 px-2 py-1.5 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
          title="New chat"
        >
          <Plus className="w-4 h-4" />
          <span className="text-xs">New Chat</span>
        </button>
      )}
      
      {/* Auto-scroll Toggle - only show when there are tabs */}
      {handleToggleAutoScroll && modeTabs.length > 0 && (
        <div className="ml-auto flex items-center border-l border-gray-200 dark:border-gray-700 pl-2">
          <button
            onClick={handleToggleAutoScroll}
            className={`
              flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors
              ${effectiveAutoScroll
                ? 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
                : 'text-gray-500 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700'
              }
            `}
          >
            <ArrowDown className={`w-3.5 h-3.5 ${effectiveAutoScroll ? 'opacity-70' : 'opacity-40'}`} />
            <span className="hidden sm:inline">
              {effectiveAutoScroll ? 'Auto-scroll' : 'Manual'}
            </span>
          </button>
        </div>
      )}
    </div>
  )
}

