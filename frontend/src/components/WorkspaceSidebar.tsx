import React, { useState } from 'react'
import SidebarHeader from './sidebar/SidebarHeader'
import LLMConfigurationSummary from './sidebar/LLMConfigurationSummary'
import MCPServersSection from './sidebar/MCPServersSection'
import PresetQueriesSection from './sidebar/PresetQueriesSection'
import ChatHistorySection from './sidebar/ChatHistorySection'
import LLMConfigurationModal from './LLMConfigurationModal'
import { ModeInfoPanel } from './ModeInfoPanel'
import { ModeSwitchSection } from './ModeSwitchSection'
import type { ActiveSessionInfo } from '../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { useAppStore, useMCPStore, useChatStore, useLLMStore } from '../stores'

interface WorkspaceSidebarProps {
  // Presets (callbacks only)
  onPresetSelect: (servers: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow') => void
  onPresetFolderSelect?: (folderPath?: string) => void
  onPresetAdded?: () => void
  
  // Chat session selection
  onChatSessionSelect?: (sessionId: string, sessionTitle?: string, sessionType?: 'active' | 'completed', activeSessionInfo?: ActiveSessionInfo) => void
  onClearPresetFilter?: () => void
  
  // Minimize functionality
  minimized: boolean
  onToggleMinimize: () => void
}

export default function WorkspaceSidebar({
  onPresetSelect,
  onPresetFolderSelect,
  onPresetAdded,
  onChatSessionSelect,
  onClearPresetFilter,
  minimized,
  onToggleMinimize
}: WorkspaceSidebarProps) {
  
  // Store subscriptions
  const { agentMode, setAgentMode, setCurrentQuery, selectedPresetId } = useAppStore()
  const { getAvailableServers, showMCPDetails, setShowMCPDetails } = useMCPStore()
  const { isStreaming } = useChatStore()
  const { showLLMModal, setShowLLMModal } = useLLMStore()

  // Computed values
  const availableServers = getAvailableServers()
  const [showShortcuts, setShowShortcuts] = useState(false)

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
      <div className="w-full h-full bg-gray-50 dark:bg-slate-900 border-r border-gray-200 dark:border-slate-700 flex flex-col shadow-lg dark:shadow-2xl">
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
          <span className="text-xs text-gray-400 dark:text-gray-500 font-mono">⌘4</span>
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
              <p>{minimized ? "Expand sidebar" : "Minimize sidebar"} (Ctrl+4)</p>
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

            {/* Preset Queries */}
            <PresetQueriesSection
              availableServers={availableServers}
              onPresetSelect={onPresetSelect}
              onPresetFolderSelect={onPresetFolderSelect}
              setCurrentQuery={setCurrentQuery}
              isStreaming={isStreaming}
              onPresetAdded={onPresetAdded}
            />

            {/* Mode Information Panel */}
            <ModeInfoPanel minimized={minimized} />

            {/* Mode Switch Section */}
            <ModeSwitchSection minimized={minimized} />

            {/* Chat History */}
            <ChatHistorySection
              onSessionSelect={(sessionId, sessionTitle, sessionType, activeSessionInfo) => {
                if (onChatSessionSelect) {
                  onChatSessionSelect(sessionId, sessionTitle, sessionType, activeSessionInfo)
                }
              }}
              selectedPresetId={selectedPresetId}
              onClearFilter={onClearPresetFilter}
            />
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
                onClick={onToggleMinimize}
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

          {/* Agent Mode Icon */}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setAgentMode(agentMode === 'ReAct' ? 'simple' : 'ReAct')
                }}
                className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
                </svg>
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Agent Mode: {agentMode} ({agentMode === 'simple' ? 'Ctrl/Cmd + 1' : 'Ctrl/Cmd + 2'})</p>
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

          {/* Chat History Icon */}
          <ChatHistorySection minimized={true} />
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
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to ReAct Agent</span>
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
    </TooltipProvider>
  )
}