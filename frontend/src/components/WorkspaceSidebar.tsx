import React, { useState, useEffect } from 'react'
import SidebarHeader from './sidebar/SidebarHeader'
import LLMConfigurationSummary from './sidebar/LLMConfigurationSummary'
import MCPServersSection from './sidebar/MCPServersSection'
import { SkillsSection } from './skills'
import { SecretsSection } from './secrets'
import { SubAgentsSection } from './subagents'
import LLMConfigurationModal from './LLMConfigurationModal'
import DelegationTierConfigModal from './DelegationTierConfigModal'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useMCPStore, useLLMStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { LogOut, User, Bell, BellOff, Play, Download } from 'lucide-react'
import { useAuthStore } from '../stores/useAuthStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { playNotificationSound } from '../utils/sound'

interface WorkspaceSidebarProps {
  // Minimize functionality
  minimized: boolean
  onToggleMinimize: () => void
}

export default function WorkspaceSidebar({
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
        body: 'This is a test notification from AgentForge',
        icon: '/logo.svg'
      })
    } else if (Notification.permission === 'default') {
      Notification.requestPermission().then(permission => {
        setOsPermission(permission)
        if (permission === 'granted') {
          new Notification('Test Notification', {
            body: 'This is a test notification from AgentForge',
            icon: '/logo.svg'
          })
        }
      })
    }
  }

  const handleNotificationClick = () => {
    if (osPermission === 'denied') {
      alert('Notifications are blocked by your system settings. Please enable them in System Settings > Notifications > AgentForge.')
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

  return (
    <TooltipProvider>
      <div className="w-full h-full bg-gray-50 dark:bg-slate-900 border-r border-gray-200 dark:border-slate-700 flex flex-col shadow-lg dark:shadow-2xl relative z-30">
      {/* Header */}
      <div className="px-4 py-3 border-b border-gray-200 dark:border-slate-700 bg-white dark:bg-slate-800/50 flex items-center justify-between h-16">
        {!minimized && <SidebarHeader />}
        <div className="flex items-center gap-1">
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
            {/* MCP Servers */}
            <MCPServersSection />

            {/* Skills */}
            <SkillsSection />

            {/* Secrets */}
            <SecretsSection />

            {/* Sub-Agent Templates */}
            <SubAgentsSection />
            {/* Download App Promo - Only in Browser */}
            {!isElectron && (
              <div className="mt-auto pt-2">
                <div className="bg-gradient-to-r from-blue-500/10 to-indigo-500/10 dark:from-blue-900/20 dark:to-indigo-900/20 rounded-lg p-3 border border-blue-100 dark:border-blue-900/30">
                  <h4 className="text-xs font-semibold text-gray-900 dark:text-gray-100 mb-1 flex items-center gap-1.5">
                    <Download className="w-3 h-3 text-blue-500" />
                    Get Mac App
                  </h4>
                  <p className="text-[10px] text-gray-500 dark:text-gray-400 mb-2 leading-relaxed">
                    Run locally without Docker. Fast, native, and easy.
                  </p>
                  <a 
                    href="https://github.com/manishiitg/mcp-agent-builder-go/releases/latest" 
                    target="_blank" 
                    rel="noopener noreferrer"
                    className="block w-full text-center px-2 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium rounded transition-colors shadow-sm"
                  >
                    Download DMG
                  </a>
                </div>
              </div>
            )}
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

          {/* Spacer to push user section to bottom */}
          <div className="flex-1" />

          {/* Download App Icon - Only in Browser */}
          {!isElectron && (
            <Tooltip>
              <TooltipTrigger asChild>
                <a
                  href="https://github.com/manishiitg/mcp-agent-builder-go/releases/latest"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="p-2 text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300 transition-colors"
                >
                  <Download className="w-5 h-5" />
                </a>
              </TooltipTrigger>
              <TooltipContent side="right">
                <p>Download Mac App</p>
              </TooltipContent>
            </Tooltip>
          )}

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
