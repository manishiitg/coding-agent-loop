import React, { useState, useEffect, useCallback, useRef } from 'react'
import { Share2, Copy, Check, Loader2 } from 'lucide-react'
import { agentApi, sessionShareApi } from '../../services/api'
import type { ChatHistorySummary, ActiveSessionInfo } from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { useModeStore } from '../../stores/useModeStore'
import { useChatStore } from '../../stores/useChatStore'

interface ChatHistorySectionProps {
  onSessionSelect?: (sessionId: string, sessionTitle?: string, sessionType?: 'active' | 'completed', activeSessionInfo?: ActiveSessionInfo) => void
  minimized?: boolean
}

export default function ChatHistorySection({ 
  onSessionSelect, 
  minimized = false
}: ChatHistorySectionProps) {
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(true)
  const [presetCache, setPresetCache] = useState<Record<string, string>>({})
  
  // Get chat history cache methods from store (persists across mount/unmount)
  const getChatHistory = useChatStore((state) => state.getChatHistory)
  const loadMoreChatHistory = useChatStore((state) => state.loadMoreChatHistory)
  const getChatHistoryHasMore = useChatStore((state) => state.getChatHistoryHasMore)
  
  // Get active sessions from cache (shared across all components)
  const activeSessions = useChatStore((state) => state.activeSessionsCache)
  
  // Mode store subscription
  const { selectedModeCategory } = useModeStore()
  
  // Local sessions state (filtered by mode)
  const [sessions, setSessions] = useState<ChatHistorySummary[]>([])

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

  // Active sessions are now managed by the centralized cache in useChatStore
  // No need for local polling - the store handles it

  // Check if a session is active
  const isSessionActive = useCallback((sessionId: string) => {
    return activeSessions.some(session => session.session_id === sessionId)
  }, [activeSessions])

  // Get active session info
  const getActiveSessionInfo = useCallback((sessionId: string) => {
    return activeSessions.find(session => session.session_id === sessionId)
  }, [activeSessions])

  // Load chat sessions using store cache
  const loadSessions = useCallback(async (forceRefresh: boolean = false) => {
    // Skip loading in workflow mode since we hide the entire section
    if (selectedModeCategory === 'workflow') {
      setSessions([])
      return
    }

    setLoading(true)
    setError(null)
    try {
      // Use 'chat' as default mode for API call
      const modeCategory = selectedModeCategory || 'chat'
      const allSessions = await getChatHistory(modeCategory, forceRefresh)

      // Filter by current mode: chat shows only chat, multi-agent shows only multi-agent
      const filteredSessions = allSessions.filter(s => {
        if ((s.agent_mode || '').toLowerCase() === 'workflow') return false
        const isMultiAgent = s.config?.delegation_mode === 'plan'
        if (selectedModeCategory === 'multi-agent') return isMultiAgent
        // In chat mode, show only non-multi-agent sessions
        return !isMultiAgent
      })
      setSessions(filteredSessions)

      // Fetch preset details for sessions that have preset_query_id
      const presetPromises = filteredSessions
        .filter(session => session.preset_query_id && !presetCache[session.preset_query_id])
        .map(session => fetchPresetQuery(session.preset_query_id!))

      if (presetPromises.length > 0) {
        await Promise.all(presetPromises)
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to load chat history'
      setError(errorMessage)
    } finally {
      setLoading(false)
    }
  }, [presetCache, fetchPresetQuery, selectedModeCategory, getChatHistory])

  // Load sessions on mount or when mode category changes
  // The store cache handles preventing unnecessary API calls
  useEffect(() => {
    loadSessions(false)
  }, [loadSessions, selectedModeCategory])

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
      case 'workflow':
        return 'Workflow'
      default:
        return agentMode
    }
  }

  // Format preset query for display
  const formatPresetQuery = (presetQueryId: string) => {
    return presetCache[presetQueryId] || 'Preset'
  }

  // Handle session click
  const handleSessionClick = async (session: ChatHistorySummary) => {
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
  const handleDeleteSession = async (e: React.MouseEvent, session: ChatHistorySummary) => {
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

  // Share popover state
  const [shareSessionId, setShareSessionId] = useState<string | null>(null)
  const [shareUrl, setShareUrl] = useState('')
  const [isCreatingShare, setIsCreatingShare] = useState(false)
  const [copied, setCopied] = useState(false)
  const sharePopoverRef = useRef<HTMLDivElement>(null)
  const shareButtonRef = useRef<HTMLButtonElement>(null)

  // Close share popover on outside click or Escape
  useEffect(() => {
    if (!shareSessionId) return
    const handleClick = (e: MouseEvent) => {
      if (
        sharePopoverRef.current && !sharePopoverRef.current.contains(e.target as Node) &&
        shareButtonRef.current && !shareButtonRef.current.contains(e.target as Node)
      ) {
        setShareSessionId(null)
      }
    }
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setShareSessionId(null)
    }
    document.addEventListener('mousedown', handleClick)
    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      document.removeEventListener('keydown', handleKey)
    }
  }, [shareSessionId])

  const handleShareSession = useCallback(async (e: React.MouseEvent, session: ChatHistorySummary) => {
    e.stopPropagation()
    if (shareSessionId === session.session_id) {
      setShareSessionId(null)
      return
    }
    console.log(`[RESTORE_DEBUG] Share: starting for session ${session.session_id}`)
    setIsCreatingShare(true)
    setShareUrl('')
    setCopied(false)
    setShareSessionId(session.session_id)
    try {
      const t0 = performance.now()
      const res = await sessionShareApi.createShare(session.session_id)
      const url = `${window.location.origin}/shared/${res.token}`
      console.log(`[RESTORE_DEBUG] Share: API returned in ${(performance.now() - t0).toFixed(0)}ms, token=${res.token}`)
      setShareUrl(url)
      // Auto-copy to clipboard
      try {
        await navigator.clipboard.writeText(url)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      } catch {
        // Fallback for non-HTTPS contexts
        const textarea = document.createElement('textarea')
        textarea.value = url
        textarea.style.position = 'fixed'
        textarea.style.opacity = '0'
        document.body.appendChild(textarea)
        textarea.select()
        document.execCommand('copy')
        document.body.removeChild(textarea)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }
    } catch (err) {
      console.error(`[RESTORE_DEBUG] Share: API error`, err)
      setShareUrl('')
      setShareSessionId(null)
    } finally {
      setIsCreatingShare(false)
    }
  }, [shareSessionId])

  const handleCopyShareUrl = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!shareUrl) return
    await navigator.clipboard.writeText(shareUrl)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [shareUrl])

  // Hide entire section in workflow mode
  if (selectedModeCategory === 'workflow') {
    return null
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
          Previous Chats
        </h3>
        <div className="flex items-center gap-1">
          <button
            onClick={() => loadSessions(true)}
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
                  {formatDate(session.last_activity || session.created_at)}
                  {session.config?.delegation_mode === 'plan' ? (
                    <span className="ml-2 px-1.5 py-0.5 rounded text-xs bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300">
                      Multi Agent
                    </span>
                  ) : session.agent_mode && session.agent_mode !== 'simple' && (
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
              <div className="flex items-center gap-0.5 relative">
                <button
                  ref={shareSessionId === session.session_id ? shareButtonRef : undefined}
                  onClick={(e) => handleShareSession(e, session)}
                  className="p-1 text-gray-400 hover:text-blue-600 dark:text-gray-500 dark:hover:text-blue-400 transition-all"
                  title="Share session"
                >
                  <Share2 size={12} />
                </button>
                <button
                  onClick={(e) => handleDeleteSession(e, session)}
                  className="opacity-0 group-hover:opacity-100 p-1 text-gray-400 hover:text-red-600 dark:text-gray-500 dark:hover:text-red-400 transition-all"
                  title="Delete session"
                >
                  <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>

                {/* Share popover */}
                {shareSessionId === session.session_id && (
                  <div
                    ref={sharePopoverRef}
                    onClick={(e) => e.stopPropagation()}
                    className="absolute right-0 top-full mt-1 z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-2 w-64"
                  >
                    {isCreatingShare ? (
                      <div className="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                        <Loader2 size={12} className="animate-spin" />
                        Creating share link...
                      </div>
                    ) : (
                      <div className="flex items-center gap-1.5">
                        <input
                          type="text"
                          readOnly
                          value={shareUrl}
                          className="flex-1 text-xs bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-1 text-gray-700 dark:text-gray-300 outline-none min-w-0"
                        />
                        <button
                          onClick={handleCopyShareUrl}
                          className="flex-shrink-0 p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
                          title="Copy link"
                        >
                          {copied ? <Check size={12} className="text-green-500" /> : <Copy size={12} />}
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
            )
          })}

          {/* Load More Button */}
          {!loading && !error && getChatHistoryHasMore() && (
            <div className="flex justify-center pt-2">
              <button
                onClick={async () => {
                  setLoadingMore(true)
                  setError(null)
                  try {
                    const modeCategory = selectedModeCategory || 'chat'
                    const allSessions = await loadMoreChatHistory(modeCategory)
                    const filteredSessions = allSessions.filter(session => {
                      if ((session.agent_mode || '').toLowerCase() === 'workflow') return false
                      const isMultiAgent = session.config?.delegation_mode === 'plan'
                      if (selectedModeCategory === 'multi-agent') return isMultiAgent
                      return !isMultiAgent
                    })
                    setSessions(filteredSessions)
                    
                    // Fetch preset details for newly loaded sessions
                    const presetPromises = filteredSessions
                      .filter(session => session.preset_query_id && !presetCache[session.preset_query_id])
                      .map(session => fetchPresetQuery(session.preset_query_id!))
                    
                    if (presetPromises.length > 0) {
                      await Promise.all(presetPromises)
                    }
                  } catch (err) {
                    const errorMessage = err instanceof Error ? err.message : 'Failed to load more chats'
                    setError(errorMessage)
                    console.error('[ChatHistory] Failed to load more chat sessions:', err)
                  } finally {
                    setLoadingMore(false)
                  }
                }}
                disabled={loadingMore}
                className="text-xs text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 px-3 py-1.5 rounded-md border border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {loadingMore ? 'Loading...' : 'Load More'}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
