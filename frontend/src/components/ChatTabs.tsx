import React, { useEffect, useMemo } from 'react'
import { X, Plus } from 'lucide-react'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { EventModeToggle } from './events'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import type { ExecutionOptions } from '../services/api-types'

interface ChatTabsProps {
  // For workflow mode: callback when starting a new phase
  onStartPhase?: (phaseId: string, executionOptions?: ExecutionOptions) => void
  // For chat mode: callback when starting a new chat
  onNewChat?: () => void
}

export const ChatTabs: React.FC<ChatTabsProps> = ({ onStartPhase }) => {
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const {
    chatTabs,
    activeTabId,
    switchTab,
    closeTab,
    tabSessionStatus,
    fetchAllTabSessionStatuses
  } = useChatStore()
  
  // For workflow mode: get phases for "New Phase" button
  const { phases } = useWorkflowStore()
  
  // Filter tabs by current mode
  const modeTabs = useMemo(() => {
    return Object.values(chatTabs).filter(tab => 
      tab.metadata?.mode === selectedModeCategory || 
      (selectedModeCategory === 'chat' && !tab.metadata?.mode) // Legacy chat tabs without metadata
    ).sort((a, b) => a.createdAt - b.createdAt)
  }, [chatTabs, selectedModeCategory])
  
  // Get stable list of tab IDs with observers for dependency
  const tabIdsWithObservers = useMemo(() => {
    return modeTabs
      .filter(tab => tab.observerId)
      .map(tab => `${tab.tabId}:${tab.observerId}`)
      .join(',')
  }, [modeTabs])
  
  // Fetch session status for tabs with observer IDs
  useEffect(() => {
    // Use modeTabs directly (it's memoized, so safe to use)
    const tabsWithObservers = modeTabs.filter(tab => tab.observerId)
    
    if (tabsWithObservers.length === 0) {
      console.log('[ChatTabs] No tabs with observer IDs to fetch status for')
      return
    }
    
    const tabIds = tabsWithObservers.map(tab => tab.tabId)
    
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
    // Only depend on tabIdsWithObservers (the string) - it changes only when tabs/observers actually change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabIdsWithObservers])

  const handleTabClick = (tabId: string) => {
    switchTab(tabId)
  }

  const handleTabClose = async (e: React.MouseEvent, tabId: string) => {
    e.stopPropagation()
    await closeTab(tabId)
  }

  const handleNewTab = async () => {
    console.log('[ChatTabs] handleNewTab called, mode:', selectedModeCategory)
    if (selectedModeCategory === 'workflow' && onStartPhase) {
      // Find first available phase (or use default)
      const defaultPhase = phases.length > 0 ? phases[0] : null
      if (defaultPhase) {
        onStartPhase(defaultPhase.id)
      }
    } else if (selectedModeCategory === 'chat') {
      // Create a new chat tab
      console.log('[ChatTabs] Creating new chat tab...')
      const chatStore = useChatStore.getState()
      const allChatTabs = Object.values(chatTabs).filter(tab => 
        tab.metadata?.mode === 'chat' || !tab.metadata?.mode
      )
      const chatNumber = allChatTabs.length + 1
      const tabName = `Chat ${chatNumber}`
      
      console.log(`[ChatTabs] Tab name: ${tabName}, existing tabs: ${allChatTabs.length}`)
      
      try {
        console.log(`[ChatTabs] Creating new chat tab: ${tabName}`)
        const newTabId = await chatStore.createChatTab(tabName, { mode: 'chat' })
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
  
  // Get tooltip text for tab
  const getTabTooltip = (tab: ChatTab): string => {
    const sessionStatus = tabSessionStatus[tab.tabId]
    const parts: string[] = [tab.name]
    
    // Always show observer ID if available
    if (tab.observerId) {
      parts.push(`Observer: ${tab.observerId.substring(0, 12)}...`)
    }
    
    if (!sessionStatus) {
      // Status not loaded yet
      if (tab.observerId) {
        parts.push('Status: Loading...')
      } else {
        parts.push('Status: No observer')
      }
      return parts.join(' • ')
    }
    
    // Show status if available
    if (sessionStatus.status) {
      const statusLabel = sessionStatus.status === 'active' ? 'Active' :
                         sessionStatus.status === 'running' ? 'Running' :
                         sessionStatus.status === 'completed' ? 'Completed' :
                         sessionStatus.status === 'paused' ? 'Paused' :
                         sessionStatus.status === 'stopped' ? 'Stopped' :
                         sessionStatus.status === 'error' ? 'Error' :
                         sessionStatus.status
      parts.push(`Status: ${statusLabel}`)
    } else {
      parts.push('Status: No session')
    }
    
    // Show agent mode if available
    if (sessionStatus.agentMode) {
      const modeLabel = sessionStatus.agentMode === 'workflow' ? 'Workflow' : 
                       sessionStatus.agentMode === 'simple' ? 'Chat (Simple)' :
                       sessionStatus.agentMode === 'orchestrator' ? 'Chat (Orchestrator)' :
                       'Chat'
      parts.push(`Mode: ${modeLabel}`)
    }
    
    // Show last activity if available
    if (sessionStatus.lastActivity) {
      try {
        const lastActivity = new Date(sessionStatus.lastActivity)
        const now = new Date()
        const diffMs = now.getTime() - lastActivity.getTime()
        const diffMins = Math.floor(diffMs / 60000)
        
        if (diffMins < 1) {
          parts.push('Last activity: Just now')
        } else if (diffMins < 60) {
          parts.push(`Last activity: ${diffMins} min ago`)
        } else {
          const diffHours = Math.floor(diffMins / 60)
          parts.push(`Last activity: ${diffHours} hour${diffHours > 1 ? 's' : ''} ago`)
        }
      } catch {
        // Invalid date, skip
      }
    }
    
    return parts.join(' • ')
  }

  // Always show tabs bar (even when empty) so users can create new tabs
  return (
    <TooltipProvider>
      <div className="flex items-center gap-1 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 px-2 py-1 overflow-x-auto">
        {/* Existing Tabs */}
        {modeTabs.map((tab) => {
          const isActive = tab.tabId === activeTabId
          const indicatorColor = getTabIndicator(tab)
          const tooltipText = getTabTooltip(tab)
          
          return (
            <Tooltip key={tab.tabId}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => handleTabClick(tab.tabId)}
                  data-testid={`chat-tab-${tab.tabId}`}
                  className={`
                    flex items-center gap-2 px-3 py-1.5 rounded-t-md text-sm font-medium transition-colors
                    ${isActive
                      ? 'bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 border-b-2 border-blue-500'
                      : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100'
                    }
                  `}
                >
                  {/* Status Indicator */}
                  <div className={`w-2 h-2 rounded-full ${indicatorColor}`} />
                  
                  {/* Tab Name */}
                  <span className="whitespace-nowrap">{tab.name}</span>
                  
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
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{tooltipText}</p>
              </TooltipContent>
            </Tooltip>
          )
        })}
      
      {/* New Tab Button */}
      <button
        onClick={handleNewTab}
        data-testid={selectedModeCategory === 'workflow' ? 'new-phase-button' : 'new-chat-button'}
        className="flex items-center gap-1 px-2 py-1.5 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
        title={selectedModeCategory === 'workflow' ? 'Start new phase' : 'New chat'}
      >
        <Plus className="w-4 h-4" />
        <span className="text-xs">
          {selectedModeCategory === 'workflow' ? 'New Phase' : 'New Chat'}
        </span>
      </button>
    </div>
    </TooltipProvider>
  )
}

