import React, { useState, useEffect, useCallback } from 'react'
import { agentApi } from '../../services/api'
import type { ChatSession, ActiveSessionInfo } from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { useModeStore } from '../../stores/useModeStore'
import { useActivePresetStore } from '../../stores/useActivePresetStore'
import { ChevronDown, Plus } from 'lucide-react'

interface ChatHistorySectionProps {
  onSessionSelect?: (sessionId: string, sessionTitle?: string, sessionType?: 'active' | 'completed', activeSessionInfo?: ActiveSessionInfo) => void
  minimized?: boolean
  selectedPresetId?: string | null
  onClearFilter?: () => void
}

export default function ChatHistorySection({ 
  onSessionSelect, 
  minimized = false,
  selectedPresetId,
  onClearFilter
}: ChatHistorySectionProps) {
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(true)
  const [presetCache, setPresetCache] = useState<Record<string, string>>({})
  
  // Active session state
  const [activeSessions, setActiveSessions] = useState<ActiveSessionInfo[]>([])
  
  // Mode store subscription
  const { selectedModeCategory } = useModeStore()
  
  // Active preset query store
  const { getActivePresetQueryId } = useActivePresetStore()
  
  // Preset selector state
  const [showPresetSelector, setShowPresetSelector] = useState(false)

  // Fetch preset query details
  const fetchPresetQuery = useCallback(async (presetQueryId: string) => {
    if (presetCache[presetQueryId]) {
      return presetCache[presetQueryId]
    }
    
    try {
      const preset = await agentApi.getPresetQuery(presetQueryId)
      setPresetCache(prev => ({ ...prev, [presetQueryId]: preset.label }))
      return preset.label
    } catch (err) {
      console.error('Failed to fetch preset query:', err)
      return 'Preset'
    }
  }, [presetCache])

  // Load active sessions
  const loadActiveSessions = useCallback(async () => {
    try {
      const response = await agentApi.getActiveSessions()
      // Loaded active sessions
      setActiveSessions(response.active_sessions)
    } catch (err) {
      console.error('Failed to load active sessions:', err)
      setActiveSessions([])
    }
  }, [])

  // Check if a session is active
  const isSessionActive = useCallback((sessionId: string) => {
    return activeSessions.some(session => session.session_id === sessionId)
  }, [activeSessions])

  // Get active session info
  const getActiveSessionInfo = useCallback((sessionId: string) => {
    return activeSessions.find(session => session.session_id === sessionId)
  }, [activeSessions])

  // Helper function to get the active preset query ID for a category
  const getBackendPresetQueryId = useCallback((category: 'deep-research' | 'workflow') => {
    return getActivePresetQueryId(category)
  }, [getActivePresetQueryId])

  // Filter sessions based on mode and active preset
  const filterSessionsByMode = useCallback((sessions: ChatSession[]) => {
    if (!selectedModeCategory) return sessions

    switch (selectedModeCategory) {
      case 'chat':
        // Show all sessions where agentMode is 'simple' or 'ReAct'
        return sessions.filter(session => 
          session.agent_mode === 'simple' || session.agent_mode === 'ReAct'
        )
      
      case 'deep-research': {
        // Show sessions filtered by active preset (orchestrator category)
        const backendPresetQueryId = getBackendPresetQueryId('deep-research')
        if (backendPresetQueryId) {
          return sessions.filter(session => 
            session.agent_mode === 'orchestrator' && 
            session.preset_query_id === backendPresetQueryId
          )
        }
        return sessions.filter(session => session.agent_mode === 'orchestrator')
      }
      
      case 'workflow': {
        // Show sessions filtered by active preset (workflow category)
        const backendPresetQueryId = getBackendPresetQueryId('workflow')
        if (backendPresetQueryId) {
          return sessions.filter(session => 
            session.agent_mode === 'workflow' && 
            session.preset_query_id === backendPresetQueryId
          )
        }
        return sessions.filter(session => session.agent_mode === 'workflow')
      }
      
      default:
        return sessions
    }
  }, [selectedModeCategory, getBackendPresetQueryId])

  // Load chat sessions
  const loadSessions = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      // Use server-side filtering when an active preset is selected
      let response
      if (selectedModeCategory === 'deep-research' || selectedModeCategory === 'workflow') {
        const activePresetQueryId = getActivePresetQueryId(selectedModeCategory)
        response = await agentApi.getChatSessions(100, 0, activePresetQueryId || undefined) // server filters by preset
      } else {
        response = await agentApi.getChatSessions(100, 0)
      }
      const allSessions = response.sessions || []
      
      // Filter sessions based on current mode (for cases where server filtering isn't sufficient)
      const filteredSessions = filterSessionsByMode(allSessions)
      setSessions(filteredSessions)
      
      // Fetch preset details for sessions that have preset_query_id
      const presetPromises = filteredSessions
        .filter(session => session.preset_query_id && !presetCache[session.preset_query_id])
        .map(session => fetchPresetQuery(session.preset_query_id!))
      
      if (presetPromises.length > 0) {
        await Promise.all(presetPromises)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load chat history')
      console.error('Failed to load chat sessions:', err)
    } finally {
      setLoading(false)
    }
  }, [presetCache, fetchPresetQuery, filterSessionsByMode, selectedModeCategory, getActivePresetQueryId])

  // Load sessions and active sessions on mount
  useEffect(() => {
    loadSessions()
    loadActiveSessions()
  }, [loadSessions, loadActiveSessions])

  // Refresh active sessions periodically
  useEffect(() => {
    const interval = setInterval(loadActiveSessions, 5000) // Check every 5 seconds
    return () => clearInterval(interval)
  }, [loadActiveSessions])

  // Format date for display
  const formatDate = (dateString: string) => {
    const date = new Date(dateString)
    const now = new Date()
    const diffInHours = (now.getTime() - date.getTime()) / (1000 * 60 * 60)
    
    if (diffInHours < 24) {
      return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + 
             date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } else if (diffInHours < 24 * 7) {
      return date.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' }) + ' ' + 
             date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } else {
      return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: '2-digit' })
    }
  }

  // Truncate title for display
  const truncateTitle = (title: string, maxLength: number = 30) => {
    if (title.length <= maxLength) return title
    return title.substring(0, maxLength) + '...'
  }

  // Format agent mode for display
  const formatAgentMode = (agentMode: string) => {
    switch (agentMode.toLowerCase()) {
      case 'simple':
        return 'Simple'
      case 'react':
        return 'ReAct'
      case 'orchestrator':
        return 'Deep Research'
      default:
        return agentMode
    }
  }

  // Format preset query for display
  const formatPresetQuery = (presetQueryId: string) => {
    return presetCache[presetQueryId] || 'Preset'
  }

  // Handle session click
  const handleSessionClick = async (session: ChatSession) => {
    if (onSessionSelect) {
      // Check if session is active
      if (isSessionActive(session.session_id)) {
        const activeSession = getActiveSessionInfo(session.session_id)
        if (activeSession) {
          // Clicked on active session, reconnecting
          // The parent component will handle the reconnection
          onSessionSelect(session.session_id, session.title, 'active', activeSession)
        }
      } else {
        // Regular completed session
        onSessionSelect(session.session_id, session.title, 'completed')
      }
    }
  }

  // Handle delete session
  const handleDeleteSession = async (e: React.MouseEvent, session: ChatSession) => {
    e.stopPropagation()
    if (window.confirm('Are you sure you want to delete this chat session?')) {
      try {
        await agentApi.deleteChatSession(session.session_id)
        setSessions(sessions.filter(s => s.session_id !== session.session_id))
      } catch (err) {
        console.error('Failed to delete session:', err)
        setError('Failed to delete session')
      }
    }
  }

  if (minimized) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={(e) => {
                e.stopPropagation()
                setExpanded(!expanded)
              }}
              className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
              title="Chat History"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Chat History ({sessions.length} sessions)</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  return (
    <div className="space-y-2">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 flex items-center gap-2">
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          {selectedPresetId ? 'Filtered Chats' : 'Previous Chats'}
        </h3>
        <div className="flex items-center gap-1">
          {selectedPresetId && (
            <button
              onClick={() => {
                // Clear the filter by calling the parent callback
                onClearFilter?.()
              }}
              className="p-1 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors"
              title="Clear filter"
            >
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          )}
          <button
            onClick={loadSessions}
            disabled={loading}
            className="p-1 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors disabled:opacity-50"
            title="Refresh"
          >
            <svg className={`w-3 h-3 ${loading ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="p-1 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors"
            title={expanded ? "Collapse" : "Expand"}
          >
            <svg className={`w-3 h-3 transition-transform ${expanded ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </button>
        </div>
      </div>

      {/* Preset Selector for Deep Research and Workflow modes */}
      {(selectedModeCategory === 'deep-research' || selectedModeCategory === 'workflow') && (
        <div className="space-y-2">
          {/* Current Active Preset Display */}
          {(() => {
            const activePresetQueryId = getActivePresetQueryId(selectedModeCategory as 'deep-research' | 'workflow')
            // Note: We would need to fetch presets from usePresetsDatabase if we want to show them here
            const presets: Array<{id: string, name: string, description?: string}> = [] // Placeholder - would need to implement preset fetching
            
            return (
              <div className="space-y-2">
                {/* Active Preset */}
                {activePresetQueryId ? (
                  <div className="flex items-center justify-between p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                    <div className="flex items-center gap-2">
                      <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                      <span className="text-sm font-medium text-blue-900 dark:text-blue-100">
                        Preset Selected
                      </span>
                    </div>
                    <button
                      onClick={() => setShowPresetSelector(!showPresetSelector)}
                      className="p-1 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-200 transition-colors"
                    >
                      <ChevronDown className={`w-3 h-3 transition-transform ${showPresetSelector ? 'rotate-180' : ''}`} />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg">
                    <span className="text-sm text-gray-600 dark:text-gray-400">
                      No preset selected
                    </span>
                    <button
                      onClick={() => setShowPresetSelector(!showPresetSelector)}
                      className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                    >
                      <ChevronDown className={`w-3 h-3 transition-transform ${showPresetSelector ? 'rotate-180' : ''}`} />
                    </button>
                  </div>
                )}

                {/* Preset Selector Dropdown */}
                {showPresetSelector && (
                  <div className="border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-slate-800 shadow-lg">
                    <div className="p-2 space-y-1 max-h-48 overflow-y-auto">
                      {presets.length === 0 ? (
                        <div className="p-3 text-center text-sm text-gray-500 dark:text-gray-400">
                          No presets available
                        </div>
                      ) : (
                        presets.map((preset) => (
                          <button
                            key={preset.id}
                            onClick={() => {
                              // Note: This would need to be implemented with the new store
                              console.log('Preset selection needs to be implemented with new store')
                              setShowPresetSelector(false)
                              loadSessions() // Reload sessions with new preset filter
                            }}
                            className={`w-full text-left p-2 rounded-md text-sm transition-colors ${
                              activePresetQueryId === preset.id
                                ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                                : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                            }`}
                          >
                            <div className="font-medium">{preset.name}</div>
                            {preset.description && (
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                {preset.description}
                              </div>
                            )}
                            <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              Preset details
                            </div>
                          </button>
                        ))
                      )}
                      
                      {/* Create New Preset Button */}
                      <div className="border-t border-gray-200 dark:border-gray-700 pt-2 mt-2">
                        <button
                          onClick={() => {
                            // TODO: Open preset creation modal
                            setShowPresetSelector(false)
                          }}
                          className="w-full flex items-center gap-2 p-2 text-sm text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/20 rounded-md transition-colors"
                        >
                          <Plus className="w-3 h-3" />
                          Create New Preset
                        </button>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            )
          })()}
        </div>
      )}

      {/* Content */}
      {expanded && (
        <div className="space-y-1">
          {loading && (
            <div className="text-xs text-gray-500 dark:text-gray-400 text-center py-2">
              Loading chat history...
            </div>
          )}

          {error && (
            <div className="text-xs text-red-500 dark:text-red-400 text-center py-2">
              {error}
            </div>
          )}

          {!loading && !error && sessions.length === 0 && (
            <div className="text-xs text-gray-500 dark:text-gray-400 text-center py-2">
              No previous chats found
            </div>
          )}

          {!loading && !error && sessions.map((session) => {
            const isActive = isSessionActive(session.session_id)
            
            return (
              <div
                key={session.session_id}
                onClick={() => handleSessionClick(session)}
                className={`group flex items-center justify-between p-2 rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer transition-colors ${
                  isActive ? 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800' : ''
                }`}
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <div className="text-xs font-medium text-gray-900 dark:text-gray-100 truncate">
                      {truncateTitle(session.title || 'Untitled Chat')}
                    </div>
                    {isActive && (
                      <div className="flex items-center gap-1">
                        <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
                        <span className="text-xs text-green-600 dark:text-green-400 font-medium">LIVE</span>
                      </div>
                    )}
                  </div>
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  {formatDate(session.created_at)}
                  {session.agent_mode && (
                    <span className="ml-2 px-2 py-0.5 rounded text-xs bg-muted text-muted-foreground">
                      {formatAgentMode(session.agent_mode)}
                    </span>
                  )}
                  {session.preset_query_id && (
                    <span className="ml-2 px-2 py-0.5 rounded text-xs bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">
                      {formatPresetQuery(session.preset_query_id)}
                    </span>
                  )}
                  {(() => {
                    // Check if session is actually active (from activeSessions) vs database status
                    const isActuallyActive = isSessionActive(session.session_id)
                    const displayStatus = isActuallyActive ? 'active' : session.status
                    
                    
                    return (
                      <span className={`ml-2 px-2 py-0.5 rounded text-xs ${
                        displayStatus === 'completed' 
                          ? 'bg-muted text-muted-foreground'
                          : displayStatus === 'active'
                          ? 'bg-primary/10 text-primary'
                          : displayStatus === 'error'
                          ? 'bg-destructive/10 text-destructive'
                          : 'bg-muted text-muted-foreground'
                      }`}>
                        {displayStatus === 'active' ? 'In Progress' : 
                         displayStatus === 'completed' ? 'Completed' :
                         displayStatus === 'error' ? 'Error' : displayStatus}
                      </span>
                    )
                  })()}
                </div>
              </div>
              <button
                onClick={(e) => handleDeleteSession(e, session)}
                className="opacity-0 group-hover:opacity-100 p-1 text-gray-400 hover:text-red-600 dark:text-gray-500 dark:hover:text-red-400 transition-all"
                title="Delete session"
              >
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
