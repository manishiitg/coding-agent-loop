import { useState, useEffect, useCallback, useRef } from 'react'
import { History, Loader2, Search } from 'lucide-react'
import { useChatStore } from '../stores'
import { useAppStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import type { ChatHistorySummary } from '../services/api-types'
import { truncateTabTitle } from '../utils/textUtils'

interface ResumeSessionDialogProps {
  onClose: () => void
}

export default function ResumeSessionDialog({ onClose }: ResumeSessionDialogProps) {
  const [sessions, setSessions] = useState<ChatHistorySummary[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const { getChatHistory, loadMoreChatHistory } = useChatStore()

  const hasMore = () => {
    const state = useChatStore.getState()
    if (state.chatHistoryTotalCount === null) return false
    return state.chatHistoryLoadedCount < state.chatHistoryTotalCount
  }

  const loadSessions = useCallback(async () => {
    setIsLoading(true)
    try {
      const allSessions = await getChatHistory('chat', true)
      setSessions(allSessions)
    } catch (err) {
      console.error('[ResumeSessionDialog] Failed to load sessions:', err)
    } finally {
      setIsLoading(false)
    }
  }, [getChatHistory])

  useEffect(() => {
    loadSessions()
  }, [loadSessions])

  const handleLoadMore = useCallback(async () => {
    if (isLoadingMore || !hasMore()) return
    setIsLoadingMore(true)
    try {
      const moreSessions = await loadMoreChatHistory('chat')
      setSessions(moreSessions)
    } catch (err) {
      console.error('[ResumeSessionDialog] Failed to load more sessions:', err)
    } finally {
      setIsLoadingMore(false)
    }
  }, [isLoadingMore, loadMoreChatHistory])

  // Intersection observer for infinite scroll
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !isLoading && !isLoadingMore && hasMore()) {
          handleLoadMore()
        }
      },
      { root: listRef.current, threshold: 0.1 }
    )

    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [isLoading, isLoadingMore, handleLoadMore])

  const handleSelectSession = async (session: ChatHistorySummary) => {
    const chatStore = useChatStore.getState()
    const appStore = useAppStore.getState()

    const existingTab = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === session.session_id)

    // Detect if this was a multi-agent session
    const isMultiAgent = session.config?.delegation_mode === 'plan'
    const tabMode = isMultiAgent ? 'multi-agent' as const : 'chat' as const

    if (existingTab) {
      chatStore.switchTab(existingTab.tabId)
    } else {
      // Switch to multi-agent mode if restoring a multi-agent session
      if (isMultiAgent) {
        useModeStore.getState().setModeCategory('multi-agent')
      }

      const newTabId = await chatStore.createChatTab(
        truncateTabTitle(session.title || 'Chat'),
        { mode: tabMode },
        session.session_id,
        'tiny'
      )

      // Restore delegation tier config to the new tab if present
      if (isMultiAgent && session.config?.delegation_tier_config) {
        chatStore.setTabConfig(newTabId, {
          delegationTierConfig: session.config.delegation_tier_config
        })
      }

      chatStore.switchTab(newTabId)
      appStore.setChatSessionId(session.session_id)
      appStore.setChatSessionTitle(session.title || '')
    }

    onClose()
  }

  const filteredSessions = sessions.filter(s => {
    if (!searchQuery.trim()) return true
    const q = searchQuery.toLowerCase()
    return (s.title || '').toLowerCase().includes(q) ||
           (s.agent_mode || '').toLowerCase().includes(q)
  })

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredSessions.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      } else if (e.key === 'Enter') {
        e.preventDefault()
        if (filteredSessions.length > 0 && selectedIndex >= 0 && selectedIndex < filteredSessions.length) {
          handleSelectSession(filteredSessions[selectedIndex])
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredSessions, selectedIndex, onClose])

  // Reset selection when search changes
  useEffect(() => {
    setSelectedIndex(0)
  }, [searchQuery])

  const formatTime = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMins = Math.floor(diffMs / 60000)
    const diffHours = Math.floor(diffMs / 3600000)
    const diffDays = Math.floor(diffMs / 86400000)

    if (diffMins < 1) return 'just now'
    if (diffMins < 60) return `${diffMins}m ago`
    if (diffHours < 24) return `${diffHours}h ago`
    if (diffDays < 7) return `${diffDays}d ago`
    return date.toLocaleDateString()
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-lg max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-secondary">
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium text-foreground">Resume Conversation</span>
          </div>
          <button
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground transition-colors"
          >
            ✕
          </button>
        </div>

        {/* Search */}
        <div className="px-4 py-2 border-b border-border">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search conversations..."
              className="w-full pl-8 pr-3 py-1.5 text-sm bg-secondary border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              autoFocus
            />
          </div>
        </div>

        {/* Session List */}
        <div ref={listRef} className="flex-1 overflow-y-auto">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredSessions.length === 0 ? (
            <div className="text-center py-12 text-sm text-muted-foreground">
              {searchQuery ? 'No matching conversations' : 'No previous conversations'}
            </div>
          ) : (
            <>
              {filteredSessions.map((session, index) => (
                <div
                  key={session.session_id}
                  onClick={() => handleSelectSession(session)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => e.key === 'Enter' && handleSelectSession(session)}
                  className={`w-full px-4 py-3 text-left cursor-pointer transition-colors border-b border-border/50 last:border-b-0 ${
                    index === selectedIndex
                      ? 'bg-primary/10 border-l-2 border-l-primary'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-foreground truncate mr-2">
                      {session.title || 'Untitled'}
                    </span>
                    <span className="text-xs text-muted-foreground whitespace-nowrap">
                      {formatTime(session.last_activity || session.created_at)}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 mt-1">
                    {session.config?.delegation_mode === 'plan' && (
                      <span className="text-xs px-1.5 py-0.5 rounded bg-indigo-500/15 text-indigo-600 dark:text-indigo-400">
                        Multi Agent
                      </span>
                    )}
                    <span className={`text-xs px-1.5 py-0.5 rounded ${
                      session.status === 'active' || session.status === 'running'
                        ? 'bg-green-500/15 text-green-600 dark:text-green-400'
                        : session.status === 'error'
                          ? 'bg-destructive/15 text-destructive'
                          : 'bg-secondary text-muted-foreground'
                    }`}>
                      {session.status || 'completed'}
                    </span>
                    {session.config?.plan_folder && (
                      <span className="text-xs text-muted-foreground truncate max-w-[120px]">
                        {session.config.plan_folder}
                      </span>
                    )}
                    {session.total_turns > 0 && (
                      <span className="text-xs text-muted-foreground">
                        {session.total_turns} turns
                      </span>
                    )}
                  </div>
                </div>
              ))}

              {/* Sentinel for infinite scroll */}
              <div ref={sentinelRef} className="h-1" />

              {/* Loading more indicator */}
              {isLoadingMore && (
                <div className="flex items-center justify-center py-3">
                  <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
                  <span className="ml-2 text-xs text-muted-foreground">Loading more...</span>
                </div>
              )}

              {/* No more sessions */}
              {!hasMore() && filteredSessions.length > 0 && !searchQuery && (
                <div className="text-center py-2 text-xs text-muted-foreground">
                  All conversations loaded
                </div>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        <div className="px-4 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
          <div className="flex items-center justify-between">
            <span>↑↓ to navigate</span>
            <span>Enter to select · Esc to close</span>
          </div>
        </div>
      </div>
    </div>
  )
}
