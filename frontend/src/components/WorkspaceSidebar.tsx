import React, { useState, useEffect } from 'react'
import SidebarHeader from './sidebar/SidebarHeader'
import LLMConfigurationSummary from './sidebar/LLMConfigurationSummary'
import HumanFeedbackConnectorsSection from './sidebar/HumanFeedbackConnectorsSection'
import MCPServersSection from './sidebar/MCPServersSection'
import { SkillsSection } from './skills'
import { SecretsSection } from './secrets'
import { SubAgentsSection } from './subagents'
import ChatHistorySection from './sidebar/ChatHistorySection'
import LLMConfigurationModal from './LLMConfigurationModal'
import DelegationTierConfigModal from './DelegationTierConfigModal'
import type { ActiveSessionInfo } from '../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useMCPStore, useLLMStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { Layers, LogOut, User, Bell, BellOff, Play } from 'lucide-react'
import { RunningWorkflowsIndicator } from './workflow/RunningWorkflowsIndicator'
import { useAuthStore } from '../stores/useAuthStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { playNotificationSound } from '../utils/sound'

interface WorkspaceSidebarProps {
  // Chat session selection
  onChatSessionSelect?: (sessionId: string, sessionTitle?: string, sessionType?: 'active' | 'completed', activeSessionInfo?: ActiveSessionInfo) => void
  
  // Minimize functionality
  minimized: boolean
  onToggleMinimize: () => void
}

export default function WorkspaceSidebar({
  onChatSessionSelect,
  minimized,
  onToggleMinimize
}: WorkspaceSidebarProps) {
  
  // Store subscriptions
  const { showMCPDetails, setShowMCPDetails } = useMCPStore()
  const { showLLMModal, setShowLLMModal, delegationTierConfig } = useLLMStore()
  const { user, logout, isMultiUserMode } = useAuthStore()
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const showDelegationTiersDialog = useCommandDialogStore(state => state.showDelegationTiers)
  const closeDialog = useCommandDialogStore(state => state.closeDialog)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showTierModal, setShowTierModal] = useState(false)
  const [isElectron, setIsElectron] = useState(false)
  const [osPermission, setOsPermission] = useState<NotificationPermission>('default')
  const [notificationsEnabled, setNotificationsEnabled] = useState(() => {
    return localStorage.getItem('mcp_notifications_enabled') !== 'false' // Default true
  })

  useEffect(() => {
    // Check if running in Electron via preload API
    setIsElectron(!!(window as any).electronAPI)
    
    // Initial permission check
    if ('Notification' in window) {
      setOsPermission(Notification.permission)
    }

    // Re-check permission when window regains focus (user might have changed it in settings)
    const handleFocus = () => {
      if ('Notification' in window) {
        setOsPermission(Notification.permission)
      }
    }
    window.addEventListener('focus', handleFocus)
    return () => window.removeEventListener('focus', handleFocus)
  }, [])

  const testNotification = () => {
    playNotificationSound()

    // Set Dock badge for test
    if ((window as any).electronAPI) {
      (window as any).electronAPI.setDockBadge('1')
      // Clear after 5 seconds for test
      setTimeout(() => {
        (window as any).electronAPI.setDockBadge('')
      }, 5000)
    }
    
    if (!('Notification' in window)) return
    
    if (Notification.permission === 'granted') {
      new Notification('Test Notification', {
        body: 'This is a test notification from Multi Agent Builder',
        icon: '/favicon.ico'
      })
    } else if (Notification.permission === 'default') {
      Notification.requestPermission().then(permission => {
        setOsPermission(permission)
        if (permission === 'granted') {
          new Notification('Test Notification', {
            body: 'This is a test notification from Multi Agent Builder',
            icon: '/favicon.ico'
          })
        }
      })
    }
  }

  const handleNotificationClick = () => {
    if (osPermission === 'denied') {
      alert('Notifications are blocked by your system settings. Please enable them in System Settings > Notifications > Multi Agent Builder.')
      return
    }

    const nextValue = !notificationsEnabled
    setNotificationsEnabled(nextValue)
    localStorage.setItem('mcp_notifications_enabled', String(nextValue))

    if (nextValue) {
      // Just enabled: trigger test
      testNotification()
    }
  }

  // Auto-open delegation tier modal when triggered from multi-agent mode entry
  useEffect(() => {
    if (showDelegationTiersDialog && selectedModeCategory === 'multi-agent') {
      setShowTierModal(true)
      closeDialog('delegationTiers')
    }
  }, [showDelegationTiersDialog, selectedModeCategory, closeDialog])

  // Auto-show tier config modal when entering multi-agent mode without tiers configured
  useEffect(() => {
    if (selectedModeCategory !== 'multi-agent') return
    const hasTiers = delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low)
    if (!hasTiers) {
      setShowTierModal(true)
    }
  }, [selectedModeCategory, delegationTierConfig])

  // Handle ESC and Enter keys for shortcuts modal
  React.useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (showShortcuts) {
        if (event.key === 'Escape' || event.key === 'Enter') {
          event.preventDefault()
          setShowShortcuts(false)
        }
      }
    }

    if (showShortcuts) {
      window.addEventListener('keydown', handleKeyDown)
      return () => window.removeEventListener('keydown', handleKeyDown)
    }
  }, [showShortcuts])

  return (
    <TooltipProvider>
      <div className="w-full h-full bg-gray-50 dark:bg-slate-900 border-r border-gray-200 dark:border-slate-700 flex flex-col shadow-lg dark:shadow-2xl relative z-30">
      {/* Header */}
      <div className="px-4 py-3 border-b border-gray-200 dark:border-slate-700 bg-white dark:bg-slate-800/50 flex items-center justify-between h-16">
        {!minimized && <SidebarHeader />}
        <div className="flex items-center gap-1">
          {!minimized && (
            <button
              onClick={() => setShowShortcuts(true)}
              className="p-1 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors"
              title="Keyboard Shortcuts"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v14a2 2 0 002 2z" />
              </svg>
            </button>
          )}
          <span className="text-xs text-gray-400 dark:text-gray-500 font-mono">⌘5</span>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={onToggleMinimize}
                className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors relative group"
              >
          {minimized ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
          ) : (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
          )}
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>{minimized ? "Expand sidebar" : "Minimize sidebar"} (Ctrl+5)</p>
            </TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Content */}
      {!minimized && (
        <div className="flex-1 overflow-y-auto">
          <div className="p-3 space-y-3">

            {/* LLM Configuration */}
            <LLMConfigurationSummary
              minimized={minimized}
            />

            {/* Sub-Agent Models - Visible in Multi Agent Chat mode */}
            {selectedModeCategory === 'multi-agent' && (
              <div className="bg-white dark:bg-slate-800 rounded-lg shadow-sm border border-gray-200 dark:border-slate-700">
                <button
                  onClick={() => setShowTierModal(true)}
                  className="w-full p-3 flex items-center justify-between text-left hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors rounded-lg"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <Layers className="w-4 h-4 text-indigo-500 flex-shrink-0" />
                    <div className="min-w-0">
                      <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Sub-Agent Models</span>
                      {delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low) ? (
                        <div className="text-[10px] text-gray-400 dark:text-gray-500 truncate">
                          {[
                            delegationTierConfig.high && `H: ${delegationTierConfig.high.model_id.split('/').pop()}`,
                            delegationTierConfig.medium && `M: ${delegationTierConfig.medium.model_id.split('/').pop()}`,
                            delegationTierConfig.low && `L: ${delegationTierConfig.low.model_id.split('/').pop()}`,
                          ].filter(Boolean).join(' | ')}
                        </div>
                      ) : (
                        <div className="text-[10px] text-gray-400 dark:text-gray-500">Click to configure tiers</div>
                      )}
                    </div>
                  </div>
                  <svg className="w-4 h-4 text-gray-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                  </svg>
                </button>
              </div>
            )}

            {/* Human Feedback Connectors */}
            <HumanFeedbackConnectorsSection
              minimized={minimized}
            />

            {/* MCP Servers */}
            <MCPServersSection />

            {/* Skills */}
            <SkillsSection />

            {/* Secrets */}
            <SecretsSection />

            {/* Sub-Agent Templates */}
            <SubAgentsSection />

            {/* Running Workflows */}
            <div className="bg-white dark:bg-slate-800 rounded-lg shadow-sm border border-gray-200 dark:border-slate-700 p-1">
              <RunningWorkflowsIndicator variant="sidebar" minimized={false} />
            </div>

            {/* Chat History */}
            <ChatHistorySection
              onSessionSelect={(sessionId, sessionTitle, sessionType, activeSessionInfo) => {
                if (onChatSessionSelect) {
                  onChatSessionSelect(sessionId, sessionTitle, sessionType, activeSessionInfo)
                }
              }}
            />
          </div>
        </div>
      )}

      {/* User Info & Logout - Bottom Section (Expanded) */}
      {!minimized && (
        <div className="border-t border-gray-200 dark:border-slate-700 bg-white dark:bg-slate-800/50">
          <div className="p-3 flex items-center justify-between gap-2">
            {isMultiUserMode && user && (
              <div className="flex items-center gap-2 min-w-0 flex-1">
                <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                  <User className="w-4 h-4 text-primary" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">
                    {user.username || user.email || 'User'}
                  </p>
                  {user.email && user.username !== user.email && (
                    <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                      {user.email}
                    </p>
                  )}
                </div>
              </div>
            )}
            
            <div className="flex items-center gap-1">
              {isElectron && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={handleNotificationClick}
                      className={`p-2 transition-colors ${
                        osPermission === 'denied' 
                          ? 'text-red-500 hover:text-red-600' 
                          : notificationsEnabled 
                            ? 'text-indigo-500 hover:text-indigo-600 dark:text-indigo-400 dark:hover:text-indigo-300' 
                            : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300'
                      }`}
                    >
                      {osPermission === 'denied' || !notificationsEnabled ? <BellOff className="w-4 h-4" /> : <Bell className="w-4 h-4" />}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>
                      {osPermission === 'denied' 
                        ? 'Notifications blocked by system. Click to learn more.' 
                        : notificationsEnabled 
                          ? 'Disable Notifications' 
                          : 'Enable Notifications & Sound'}
                    </p>
                  </TooltipContent>
                </Tooltip>
              )}
              
              {isMultiUserMode && user && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={logout}
                      className="p-2 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors"
                    >
                      <LogOut className="w-4 h-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Sign out</p>
                  </TooltipContent>
                </Tooltip>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Minimized Icons */}
      {minimized && (
        <div 
          onClick={onToggleMinimize}
          className="flex-1 flex flex-col items-center py-4 space-y-4 cursor-pointer"
          title="Click to expand sidebar"
        >
          {/* Expand Sidebar Button */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  onToggleMinimize()
                }}
                className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                title="Expand sidebar"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
                </svg>
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Expand sidebar (Ctrl+5)</p>
            </TooltipContent>
          </Tooltip>

          {/* LLM Configuration Icon */}
          <LLMConfigurationSummary
            minimized={true}
          />

          {/* Human Feedback Connectors Icon */}
          <HumanFeedbackConnectorsSection
            minimized={true}
          />

          {/* MCP Servers Icon */}
          <button
            onClick={(e) => {
              e.stopPropagation()
              setShowMCPDetails(!showMCPDetails)
            }}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            title="MCP Servers"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
            </svg>
          </button>

          {/* Skills Icon */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  onToggleMinimize()
                }}
                className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                title="Skills"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 3v4M3 5h4M6 17v4m-2-2h4m5-16l2.286 6.857L21 12l-5.714 2.143L13 21l-2.286-6.857L5 12l5.714-2.143L13 3z" />
                </svg>
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Skills</p>
            </TooltipContent>
          </Tooltip>

          {/* Running Workflows Icon */}
          <RunningWorkflowsIndicator variant="sidebar" minimized={true} />

          {/* Chat History Icon */}
          <ChatHistorySection minimized={true} />

          {/* Spacer to push user section to bottom */}
          <div className="flex-1" />

          {/* User Info & Logout - Bottom (Minimized) */}
          <div className="border-t border-gray-200 dark:border-slate-700 pt-3 flex flex-col items-center gap-2">
            {isElectron && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      handleNotificationClick()
                    }}
                    className={`p-2 transition-colors ${
                      osPermission === 'denied' 
                        ? 'text-red-500 hover:text-red-600' 
                        : notificationsEnabled 
                          ? 'text-indigo-500 hover:text-indigo-600 dark:text-indigo-400 dark:hover:text-indigo-300' 
                          : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300'
                    }`}
                  >
                    {osPermission === 'denied' || !notificationsEnabled ? <BellOff className="w-4 h-4" /> : <Bell className="w-4 h-4" />}
                  </button>
                </TooltipTrigger>
                <TooltipContent side="right">
                  <p>
                    {osPermission === 'denied' 
                      ? 'Notifications blocked by system' 
                      : notificationsEnabled 
                        ? 'Disable Notifications' 
                        : 'Enable Notifications & Sound'}
                  </p>
                </TooltipContent>
              </Tooltip>
            )}

            {isMultiUserMode && user && (
              <>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center cursor-default">
                      <User className="w-4 h-4 text-primary" />
                    </div>
                  </TooltipTrigger>
                  <TooltipContent side="right">
                    <p>{user.username || user.email || 'User'}</p>
                  </TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        logout()
                      }}
                      className="p-2 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors"
                    >
                      <LogOut className="w-4 h-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="right">
                    <p>Sign out</p>
                  </TooltipContent>
                </Tooltip>
              </>
            )}
          </div>
        </div>
      )}

      {/* Keyboard Shortcuts Modal */}
      {showShortcuts && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 max-w-md w-full mx-4">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                Keyboard Shortcuts
              </h3>
              <button
                onClick={() => setShowShortcuts(false)}
                className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
            
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Simple Agent</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+1
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Simple Agent</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+2
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Deep Search Agent</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+3
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Workflow Agent</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+4
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Minimize Sidebar</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+5
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Minimize Workspace</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+6
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Toggle Auto-scroll</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+7
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Cycle Event Mode</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+8
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Close Shortcuts</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Esc
                </kbd>
              </div>
            </div>
            
            <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700">
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Use Ctrl on Windows/Linux or Cmd on Mac
              </p>
            </div>
          </div>
        </div>
      )}
      </div>
      
      {/* LLM Configuration Modal */}
      <LLMConfigurationModal
        isOpen={showLLMModal}
        onClose={() => setShowLLMModal(false)}
      />

      {/* Delegation Tier Configuration Modal */}
      <DelegationTierConfigModal
        isOpen={showTierModal}
        onClose={() => setShowTierModal(false)}
      />
    </TooltipProvider>
  )
}