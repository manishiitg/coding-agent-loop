import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowUpRight, Bot, CalendarClock, ChevronDown, ChevronRight, Code2, LayoutList, Loader2, MessageSquare, Paperclip } from 'lucide-react'
import { agentApi } from '../services/api'
import {
  type ChatHistoryConversation,
  type ChatHistoryMessage,
  type ChatHistoryPreviewMessage,
  type ChatHistorySession,
} from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'

const PAGE_SIZE = 5
const FETCH_LIMIT = 100

type PreviousChatFilter = 'chat' | 'schedule' | 'bot' | 'all'

export function chatHistorySessionTitle(session: ChatHistorySession, maxLength = 110): string {
  const query = session.query?.replace(/\s+/g, ' ').trim()
  if (query) return query.length > maxLength ? `${query.slice(0, maxLength)}...` : query
  return `${(session.agent_mode || 'chat').replace(/_/g, ' ')} ${session.session_id.slice(0, 8)}`
}

export function chatHistoryConversationPath(session: ChatHistorySession): string {
  if (session.conversation_path) return session.conversation_path
  const userId = session.user_id || 'default'
  return `_users/${userId}/chat_history/${session.session_id}/conversation.json`
}

export function chatHistoryRuntimeLabel(session: ChatHistorySession): string | undefined {
  const runtime = session.runtime
  const provider = runtime?.provider?.trim()
  if (runtime?.kind !== 'coding_agent' || !provider) return undefined

  const model = runtime.model_id?.trim()
  if (model && model !== provider) return `${provider} · ${model}`
  return provider
}

const formatChatTime = (value?: string): string => {
  if (!value) return 'Unknown time'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown time'
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const getChatKind = (session: ChatHistorySession): Exclude<PreviousChatFilter, 'all'> => {
  if (session.session_id.startsWith('schedule-cron--')) return 'schedule'
  if (session.session_id.startsWith('bot-')) return 'bot'
  return 'chat'
}

const previewMessages = (session: ChatHistorySession): ChatHistoryPreviewMessage[] => {
  return (session.preview_messages || [])
    .filter(message => message.text?.trim())
    .slice(-6)
}

const messageRole = (message: ChatHistoryMessage): string => {
  return String(message.Role || message.role || '').toLowerCase().trim()
}

const cleanMessageText = (text: string): string => {
  const markers = [
    '\n\nPrevious workflow-builder conversation file:',
    '\n\nPrevious builder chat file available:',
  ]
  for (const marker of markers) {
    const markerIndex = text.indexOf(marker)
    if (markerIndex >= 0) return text.slice(0, markerIndex).trim()
  }
  return text.trim()
}

const messageText = (message: ChatHistoryMessage): string => {
  const parts = message.Parts || message.parts || []
  const text = parts
    .map(part => part.Text || part.text || part.Content || part.content || '')
    .filter(Boolean)
    .join('\n')
    .trim()
  return cleanMessageText(text)
}

const shouldSkipMessageText = (text: string): boolean => {
  return text.startsWith('[AUTO-NOTIFICATION]') ||
    text.startsWith('[Previous tool call') ||
    text.startsWith('[Previous tool result')
}

const conversationMessages = (conversation: ChatHistoryConversation): ChatHistoryPreviewMessage[] => {
  return (conversation.conversation_history || [])
    .map(message => ({
      role: messageRole(message),
      text: messageText(message),
    }))
    .filter(message => {
      if (!message.text || shouldSkipMessageText(message.text)) return false
      return message.role === 'human' ||
        message.role === 'user' ||
        message.role === 'ai' ||
        message.role === 'assistant'
    })
}

const mergeSessions = (current: ChatHistorySession[], next: ChatHistorySession[]): ChatHistorySession[] => {
  const byId = new Map<string, ChatHistorySession>()
  for (const session of [...current, ...next]) {
    byId.set(session.session_id, session)
  }
  return Array.from(byId.values()).sort((a, b) =>
    Date.parse(b.updated_at || b.created_at || '') - Date.parse(a.updated_at || a.created_at || '')
  )
}

interface PreviousChatHistoryPanelProps {
  workspacePath?: string
  activeSessionId?: string
  title?: string
  emptyText?: string
  actionLabel?: string
  onHasChatsChange?: (hasChats: boolean) => void
  onSelectSession: (session: ChatHistorySession) => void | Promise<void>
}

export const PreviousChatHistoryPanel: React.FC<PreviousChatHistoryPanelProps> = ({
  workspacePath,
  activeSessionId,
  title = 'Previous chats',
  emptyText = 'No previous chats yet.',
  actionLabel = 'Open',
  onHasChatsChange,
  onSelectSession,
}) => {
  const [sessions, setSessions] = useState<ChatHistorySession[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [activeFilter, setActiveFilter] = useState<PreviousChatFilter>('chat')
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE)
  const [expandedSessionIds, setExpandedSessionIds] = useState<Set<string>>(() => new Set())
  const [expandedMessagesBySession, setExpandedMessagesBySession] = useState<Record<string, ChatHistoryPreviewMessage[]>>({})
  const [loadingExpandedSessionIds, setLoadingExpandedSessionIds] = useState<Set<string>>(() => new Set())
  const expandedMessagesRef = useRef(expandedMessagesBySession)
  const loadingExpandedSessionIdsRef = useRef(loadingExpandedSessionIds)
  const addToast = useChatStore(state => state.addToast)

  useEffect(() => {
    expandedMessagesRef.current = expandedMessagesBySession
  }, [expandedMessagesBySession])

  useEffect(() => {
    loadingExpandedSessionIdsRef.current = loadingExpandedSessionIds
  }, [loadingExpandedSessionIds])

  useEffect(() => {
    let cancelled = false
    setSessions([])
    setActiveFilter('chat')
    setVisibleCount(PAGE_SIZE)
    setExpandedSessionIds(new Set())
    setExpandedMessagesBySession({})
    setLoadingExpandedSessionIds(new Set())
    setIsLoading(true)

    agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath)
      .then(response => {
        if (cancelled) return
        setSessions(mergeSessions([], response.sessions || []))
      })
      .catch(() => {
        if (cancelled) return
        setSessions([])
        addToast('Failed to load previous chats', 'error')
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false)
      })

    return () => { cancelled = true }
  }, [addToast, workspacePath])

  const visibleSessions = useMemo(
    () => sessions.filter(session => session.session_id !== activeSessionId),
    [activeSessionId, sessions]
  )

  const filterCounts = useMemo(() => {
    const counts: Record<PreviousChatFilter, number> = {
      chat: 0,
      schedule: 0,
      bot: 0,
      all: visibleSessions.length,
    }
    for (const session of visibleSessions) {
      counts[getChatKind(session)] += 1
    }
    return counts
  }, [visibleSessions])

  const filteredSessions = useMemo(
    () => activeFilter === 'all'
      ? visibleSessions
      : visibleSessions.filter(session => getChatKind(session) === activeFilter),
    [activeFilter, visibleSessions]
  )

  const displayedSessions = useMemo(
    () => filteredSessions.slice(0, visibleCount),
    [filteredSessions, visibleCount]
  )

  useEffect(() => {
    setVisibleCount(PAGE_SIZE)
  }, [activeFilter])

  useEffect(() => {
    onHasChatsChange?.(!isLoading && visibleSessions.length > 0)
  }, [isLoading, onHasChatsChange, visibleSessions.length])

  const loadExpandedMessages = useCallback(async (session: ChatHistorySession) => {
    const sessionId = session.session_id
    if (expandedMessagesRef.current[sessionId] || loadingExpandedSessionIdsRef.current.has(sessionId)) return

    const nextLoading = new Set(loadingExpandedSessionIdsRef.current)
    nextLoading.add(sessionId)
    loadingExpandedSessionIdsRef.current = nextLoading
    setLoadingExpandedSessionIds(nextLoading)
    try {
      const conversation = await agentApi.getChatHistoryConversation(sessionId, workspacePath)
      const messages = conversationMessages(conversation)
      setExpandedMessagesBySession(current => {
        const next = {
          ...current,
          [sessionId]: messages.length > 0 ? messages : previewMessages(session),
        }
        expandedMessagesRef.current = next
        return next
      })
    } catch {
      setExpandedMessagesBySession(current => {
        const next = {
          ...current,
          [sessionId]: previewMessages(session),
        }
        expandedMessagesRef.current = next
        return next
      })
      addToast('Failed to load full chat details', 'error')
    } finally {
      setLoadingExpandedSessionIds(current => {
        const next = new Set(current)
        next.delete(sessionId)
        loadingExpandedSessionIdsRef.current = next
        return next
      })
    }
  }, [addToast, workspacePath])

  const toggleExpanded = useCallback((session: ChatHistorySession) => {
    const sessionId = session.session_id
    const wasExpanded = expandedSessionIds.has(sessionId)
    setExpandedSessionIds(current => {
      const next = new Set(current)
      if (next.has(sessionId)) {
        next.delete(sessionId)
      } else {
        next.add(sessionId)
      }
      return next
    })
    if (!wasExpanded) {
      void loadExpandedMessages(session)
    }
  }, [expandedSessionIds, loadExpandedMessages])

  const handleSelect = useCallback((session: ChatHistorySession) => {
    void onSelectSession(session)
  }, [onSelectSession])

  const ActionIcon = actionLabel.toLowerCase() === 'attach' ? Paperclip : ArrowUpRight
  const filterItems = [
    { filter: 'chat' as const, label: 'Chat', icon: MessageSquare },
    { filter: 'schedule' as const, label: 'Schedules', icon: CalendarClock },
    { filter: 'bot' as const, label: 'Bots', icon: Bot },
    { filter: 'all' as const, label: 'All', icon: LayoutList },
  ]

  return (
    <div className="shrink-0 border-b border-border bg-background">
      <div className="w-full">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-2">
          <div className="flex min-w-0 items-center gap-2 text-sm">
            <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground/80" />
            <span className="truncate font-medium text-foreground">{title}</span>
          </div>

          {isLoading ? (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
          ) : visibleSessions.length > 0 ? (
            <div className="flex max-w-full items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-muted/30 p-0.5">
              {filterItems.map(({ filter, label, icon: Icon }) => {
              const isActive = activeFilter === filter
              return (
                <button
                  key={filter}
                  type="button"
                  onClick={() => setActiveFilter(filter)}
                  className={`inline-flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-xs font-medium transition-colors ${
                    isActive
                      ? 'bg-background text-foreground shadow-sm ring-1 ring-border/40'
                      : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{label}</span>
                  <span className={`min-w-4 rounded-full px-1 py-0.5 text-center text-[10px] leading-none ${
                    isActive
                      ? 'bg-muted text-foreground'
                      : 'bg-background/60 text-muted-foreground'
                  }`}>
                    {filterCounts[filter]}
                  </span>
                </button>
              )
              })}
            </div>
          ) : null}
        </div>

        {isLoading ? (
          <div className="px-3 py-3 text-xs text-muted-foreground">Loading previous chats...</div>
        ) : visibleSessions.length === 0 ? (
          <div className="px-3 py-3 text-xs text-muted-foreground">{emptyText}</div>
        ) : filteredSessions.length === 0 ? (
          <div className="px-3 py-3 text-xs text-muted-foreground">No previous {activeFilter === 'schedule' ? 'schedule' : activeFilter} chats yet.</div>
        ) : (
          <div className="divide-y divide-border">
            {displayedSessions.map(session => {
              const messages = expandedMessagesBySession[session.session_id] || previewMessages(session)
              const isExpanded = expandedSessionIds.has(session.session_id)
              const isLoadingDetails = loadingExpandedSessionIds.has(session.session_id)
              const runtimeLabel = chatHistoryRuntimeLabel(session)

              return (
                <div key={session.session_id} className="group bg-background transition-colors hover:bg-muted/20">
                  <div className="flex items-start gap-2 px-3 py-2.5">
                    <button
                      type="button"
                      onClick={() => toggleExpanded(session)}
                      disabled={messages.length === 0 && !session.message_count}
                      className="mt-0.5 rounded p-1 text-muted-foreground transition-colors hover:bg-background hover:text-foreground disabled:cursor-default disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
                      aria-label={isExpanded ? 'Hide chat details' : 'Show chat details'}
                    >
                      {isExpanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    </button>

                    <button
                      type="button"
                      onClick={() => handleSelect(session)}
                      className="min-w-0 flex-1 text-left"
                    >
                      <div className="line-clamp-1 text-sm font-medium text-foreground">{chatHistorySessionTitle(session)}</div>
                      <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
                        <span>{formatChatTime(session.updated_at || session.created_at)}</span>
                        {typeof session.message_count === 'number' && <span>{session.message_count} messages</span>}
                        {session.agent_mode && <span>{session.agent_mode.replace(/_/g, ' ')}</span>}
                        {runtimeLabel && (
                          <span className="inline-flex items-center gap-1 rounded border border-border bg-muted/40 px-1.5 py-0.5 font-medium text-foreground">
                            <Code2 className="h-3 w-3" />
                            {runtimeLabel}
                          </span>
                        )}
                      </div>
                    </button>

                    <button
                      type="button"
                      onClick={() => handleSelect(session)}
                      className="inline-flex shrink-0 items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground opacity-80 transition-colors hover:border-primary/40 hover:text-foreground group-hover:opacity-100"
                    >
                      <ActionIcon className="h-3.5 w-3.5" />
                      <span>{actionLabel}</span>
                    </button>
                  </div>

                  {isExpanded && (
                    <div className="px-10 pb-3">
                      <div className="max-h-80 space-y-2 overflow-y-auto rounded-md border border-border bg-muted/20 p-2 text-xs text-foreground">
                        {isLoadingDetails && (
                          <div className="flex items-center gap-2 text-muted-foreground">
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            <span>Loading full chat...</span>
                          </div>
                        )}
                        {!isLoadingDetails && messages.length === 0 && (
                          <div className="text-muted-foreground">No displayable messages found.</div>
                        )}
                        {!isLoadingDetails && messages.map((message, index) => {
                          const normalizedRole = message.role === 'ai' || message.role === 'assistant' ? 'Assistant' : 'User'
                          const roleClass = normalizedRole === 'Assistant'
                            ? 'text-emerald-600 dark:text-emerald-400'
                            : 'text-sky-600 dark:text-sky-400'

                          return (
                            <div key={`${session.session_id}-previous-preview-${index}`} className="space-y-1 rounded bg-background/70 px-2 py-1.5">
                              <div className={`text-[10px] font-semibold uppercase leading-none ${roleClass}`}>
                                {normalizedRole}
                              </div>
                              <div className="whitespace-pre-wrap break-words leading-relaxed text-muted-foreground">
                                {message.text}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}

        {!isLoading && filteredSessions.length > displayedSessions.length && (
          <div className="border-t border-border px-3 py-2">
            <button
              type="button"
              onClick={() => setVisibleCount(count => count + PAGE_SIZE)}
              className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
            >
              <span>Load {PAGE_SIZE} more</span>
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
