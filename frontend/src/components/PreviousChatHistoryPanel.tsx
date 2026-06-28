import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowUpRight, Bot, CalendarClock, ChevronDown, ChevronRight, Code2, Loader2, MessageSquare, Paperclip, Trash2, type LucideIcon } from 'lucide-react'
import { agentApi } from '../services/api'
import {
  type ChatHistoryConversation,
  type ChatHistoryMessage,
  type ChatHistoryPreviewMessage,
  type ChatHistorySession,
} from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'
import {
  CHAT_HISTORY_CLEANUP_AGE_OPTIONS,
  type ChatHistoryCleanupAgeDays,
  CleanupOldChatsDropdown,
} from './CleanupOldChatsDropdown'

const PAGE_SIZE = 5
const FETCH_LIMIT = 100
const EXPANDED_MESSAGE_LIMIT = 6

type PreviousChatKind = 'chat' | 'schedule' | 'bot'
type PreviousChatFilter = PreviousChatKind
type EmptyStateIcon = LucideIcon

const emptyStateContent: Record<PreviousChatFilter, {
  icon: EmptyStateIcon
  title: string
  body: string
}> = {
  chat: {
    icon: MessageSquare,
    title: 'No chats yet',
    body: 'Start a chat from the composer below. After the first saved turn, it will appear here so you can resume it later.',
  },
  schedule: {
    icon: CalendarClock,
    title: 'No scheduled chats yet',
    body: 'Use the Schedules control in the top bar to create a recurring task. After a run starts, the latest scheduled chat will appear here.',
  },
  bot: {
    icon: Bot,
    title: 'No bot chats yet',
    body: 'Use the Bot connector button in the top bar to connect and configure a bot. Sessions started or resumed from that bot will appear here.',
  },
}

const firstRunHints: Array<{
  icon: EmptyStateIcon
  label: string
  body: string
}> = [
  {
    icon: MessageSquare,
    label: 'Chat',
    body: 'Ask anything, then return here to resume the thread.',
  },
  {
    icon: CalendarClock,
    label: 'Schedules',
    body: 'Run recurring work on a schedule and review the latest run.',
  },
  {
    icon: Bot,
    label: 'Bots',
    body: 'Use the Bot connector button to configure the external bot.',
  },
]

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
  if (!runtime || !provider) return undefined

  const model = runtime.model_id?.trim()
  if (model && model !== provider) return `${provider} · ${model}`
  return provider
}

function chatHistoryRuntimeTransport(session: ChatHistorySession): string {
  const runtime = session.runtime
  const transport = runtime?.transport?.trim().toLowerCase()
  if (transport) return transport
  return runtime?.agent_session_handle?.provider?.transport?.trim().toLowerCase() || ''
}

export function chatHistorySupportsNativeResume(session: ChatHistorySession): boolean {
  const runtime = session.runtime
  if (!runtime || runtime.kind !== 'coding_agent') return false
  if (runtime.resume_supported === false) return false
  const handle = runtime.agent_session_handle?.provider
  return Boolean(
    runtime.resume_supported ||
    runtime.external_session_id?.trim() ||
    runtime.project_dir_id?.trim() ||
    handle?.native_session_id?.trim() ||
    handle?.project_dir_id?.trim()
  )
}

export function chatHistoryUsesTerminalRestore(session: ChatHistorySession): boolean {
  const runtime = session.runtime
  if (!runtime || runtime.kind !== 'coding_agent') return false
  return chatHistoryRuntimeTransport(session) === 'tmux'
}

export function chatHistoryWorkshopModeLabel(session: ChatHistorySession): string | undefined {
  const raw = (session.runtime?.workshop_mode || session.workshop_mode || '').trim().toLowerCase()
  if (!raw) return undefined
  if (raw === 'optimizer') return 'Optimizer'
  if (raw === 'builder') return 'Builder'
  if (raw === 'run') return 'Run'
  if (raw === 'reporting') return 'Reporting'
  return raw.replace(/_/g, ' ')
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

const formatMessageCount = (count?: number): string | undefined => {
  if (typeof count !== 'number') return undefined
  const formatted = new Intl.NumberFormat().format(count)
  return `${formatted} ${count === 1 ? 'message' : 'messages'}`
}

const sessionHasMessages = (session: ChatHistorySession): boolean => {
  return (session.message_count ?? 0) > 0 || (session.preview_messages?.length ?? 0) > 0 || !!session.query?.trim()
}

const isSessionOlderThanDays = (session: ChatHistorySession, days: number): boolean => {
  const timestamp = Date.parse(session.updated_at || session.created_at || '')
  if (Number.isNaN(timestamp)) return false
  return timestamp < Date.now() - days * 24 * 60 * 60 * 1000
}

const getChatKind = (session: ChatHistorySession): PreviousChatKind => {
  if (session.session_id.startsWith('schedule-') || session.session_id.startsWith('sched_')) return 'schedule'
  if (session.session_id.startsWith('bot-')) return 'bot'
  return 'chat'
}

const previewMessages = (session: ChatHistorySession): ChatHistoryPreviewMessage[] => {
  return (session.preview_messages || [])
    .filter(message => message.text?.trim())
    .slice(-EXPANDED_MESSAGE_LIMIT)
}

const messageRole = (message: ChatHistoryMessage): string => {
  return String(message.Role || message.role || '').toLowerCase().trim()
}

const cleanMessageText = (text: string): string => {
  const markers = [
    '\n\nPrevious workflow-builder conversation file:',
    '\n\nPrevious builder chat file available:',
    '\n\nPrevious conversation file:',
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

const recentConversationMessages = (messages: ChatHistoryPreviewMessage[]): ChatHistoryPreviewMessage[] => {
  return messages
    .filter(message => message.text?.trim())
    .slice(-EXPANDED_MESSAGE_LIMIT)
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

const PreviousChatEmptyState: React.FC<{
  filter: PreviousChatFilter
  hasAnySessions: boolean
  fallbackText: string
}> = ({ filter, hasAnySessions, fallbackText }) => {
  const content = hasAnySessions
    ? emptyStateContent[filter]
    : { ...emptyStateContent[filter], body: emptyStateContent[filter].body || fallbackText }
  const Icon = content.icon

  return (
    <div className="px-3 py-4">
      <div className="rounded-md border border-dashed border-border bg-muted/15 px-3 py-4">
        <div className="flex items-start gap-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border bg-background text-muted-foreground">
            <Icon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-foreground">{content.title}</div>
            <p className="mt-1 max-w-xl text-xs leading-5 text-muted-foreground">{content.body || fallbackText}</p>

            {!hasAnySessions && (
              <div className="mt-3 grid gap-x-4 gap-y-2 border-t border-border/70 pt-3 sm:grid-cols-3">
                {firstRunHints.map(({ icon: HintIcon, label, body }) => (
                  <div key={label} className="min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
                      <HintIcon className="h-3.5 w-3.5 text-muted-foreground" />
                      <span>{label}</span>
                    </div>
                    <p className="mt-1 text-[11px] leading-4 text-muted-foreground">{body}</p>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

interface PreviousChatHistoryPanelProps {
  workspacePath?: string
  activeSessionId?: string
  title?: string
  emptyText?: string
  actionLabel?: string
  onHasChatsChange?: (hasChats: boolean, isLoaded?: boolean) => void
  onSelectSession: (session: ChatHistorySession) => void | Promise<void>
  /** Dense layout for the narrow ~360px chat rail: icon-only filters + actions,
   *  single tight meta line, no runtime/workshop chips or message preview. */
  compact?: boolean
  /** Fill the available chat surface for landing dashboards. */
  fill?: boolean
}

export const PreviousChatHistoryPanel: React.FC<PreviousChatHistoryPanelProps> = ({
  workspacePath,
  activeSessionId,
  title = 'Previous chats',
  emptyText = 'No previous chats yet.',
  actionLabel = 'Open',
  onHasChatsChange,
  onSelectSession,
  compact = false,
  fill = false,
}) => {
  const [sessions, setSessions] = useState<ChatHistorySession[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isCleanupLoading, setIsCleanupLoading] = useState(false)
  const [deletingSessionIds, setDeletingSessionIds] = useState<Set<string>>(() => new Set())
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
    }
    for (const session of visibleSessions) {
      counts[getChatKind(session)] += 1
    }
    return counts
  }, [visibleSessions])

  const filteredSessions = useMemo(
    () => visibleSessions.filter(session => getChatKind(session) === activeFilter),
    [activeFilter, visibleSessions]
  )

  const oldVisibleSessionCounts = useMemo(
    () => CHAT_HISTORY_CLEANUP_AGE_OPTIONS.reduce((counts, days) => {
      counts[days] = visibleSessions.filter(session => sessionHasMessages(session) && isSessionOlderThanDays(session, days)).length
      return counts
    }, {} as Record<ChatHistoryCleanupAgeDays, number>),
    [visibleSessions]
  )
  const hasOldVisibleSessions = useMemo(
    () => CHAT_HISTORY_CLEANUP_AGE_OPTIONS.some(days => oldVisibleSessionCounts[days] > 0),
    [oldVisibleSessionCounts]
  )

  const displayedSessions = useMemo(
    () => filteredSessions.slice(0, visibleCount),
    [filteredSessions, visibleCount]
  )

  useEffect(() => {
    setVisibleCount(PAGE_SIZE)
  }, [activeFilter])

  useEffect(() => {
    onHasChatsChange?.(!isLoading && visibleSessions.length > 0, !isLoading)
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
      const recentMessages = recentConversationMessages(messages)
      setExpandedMessagesBySession(current => {
        const next = {
          ...current,
          [sessionId]: recentMessages.length > 0 ? recentMessages : previewMessages(session),
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

  const handleDeleteSession = useCallback(async (session: ChatHistorySession) => {
    const title = chatHistorySessionTitle(session, 80)
    if (!window.confirm(`Delete this chat?\n\n${title}`)) return

    const sessionId = session.session_id
    setDeletingSessionIds(current => {
      const next = new Set(current)
      next.add(sessionId)
      return next
    })
    try {
      await agentApi.deleteChatHistorySession(sessionId, workspacePath)
      setSessions(current => current.filter(item => item.session_id !== sessionId))
      setExpandedSessionIds(current => {
        const next = new Set(current)
        next.delete(sessionId)
        return next
      })
      setExpandedMessagesBySession(current => {
        const next = { ...current }
        delete next[sessionId]
        expandedMessagesRef.current = next
        return next
      })
      addToast('Deleted previous chat', 'success')
    } catch {
      addToast('Failed to delete previous chat', 'error')
    } finally {
      setDeletingSessionIds(current => {
        const next = new Set(current)
        next.delete(sessionId)
        return next
      })
    }
  }, [addToast, workspacePath])

  const handleCleanupOldChats = useCallback(async (olderThanDays: ChatHistoryCleanupAgeDays) => {
    const scopeLabel = workspacePath || 'all chats'
    const oldSessionCount = oldVisibleSessionCounts[olderThanDays] || 0
    if (oldSessionCount === 0) {
      addToast(`No chats older than ${olderThanDays} days`, 'info')
      return
    }
    if (!window.confirm(`Delete ${oldSessionCount} chat${oldSessionCount === 1 ? '' : 's'} older than ${olderThanDays} days from ${scopeLabel}? This cannot be undone.`)) return

    setIsCleanupLoading(true)
    try {
      const response = await agentApi.cleanupChatHistorySessions(olderThanDays, workspacePath)
      const deletedCount = response.result?.deleted_count ?? 0
      const refreshed = await agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath)
      setSessions(mergeSessions([], refreshed.sessions || []))
      setExpandedSessionIds(new Set())
      setExpandedMessagesBySession({})
      expandedMessagesRef.current = {}
      addToast(
        deletedCount === 0
          ? `No chats older than ${olderThanDays} days`
          : `Deleted ${deletedCount} chat${deletedCount === 1 ? '' : 's'} older than ${olderThanDays} days`,
        'success'
      )
    } catch {
      addToast('Failed to delete old chats', 'error')
    } finally {
      setIsCleanupLoading(false)
    }
  }, [addToast, oldVisibleSessionCounts, workspacePath])

  const ActionIcon = actionLabel.toLowerCase() === 'attach' ? Paperclip : ArrowUpRight
  const filterItems = [
    { filter: 'chat' as const, label: 'Chat', icon: MessageSquare },
    { filter: 'schedule' as const, label: 'Schedules', icon: CalendarClock },
    { filter: 'bot' as const, label: 'Bots', icon: Bot },
  ]

  return (
    <div className={`${fill ? 'flex min-h-0 flex-1 flex-col overflow-hidden' : 'shrink-0'} border-b border-border bg-background`}>
      <div className={`${fill ? 'flex min-h-0 flex-1 flex-col' : ''} w-full`}>
        <div className={`flex flex-wrap items-center gap-2 border-b border-border px-3 py-2 ${compact ? 'justify-end' : 'justify-between'}`}>
          {/* The "Previous … chats" heading is redundant in the compact rail —
              the filter pills + list make the purpose obvious — so hide it there. */}
          {!compact && (
            <div className="flex min-w-0 items-center gap-2 text-sm">
              <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground/80" />
              <span className="truncate font-medium text-foreground">{title}</span>
            </div>
          )}

          {isLoading ? (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
          ) : (
            <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
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
                    {!compact && <span>{label}</span>}
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
              {hasOldVisibleSessions && (
                <CleanupOldChatsDropdown
                  counts={oldVisibleSessionCounts}
                  isLoading={isCleanupLoading || isLoading}
                  onSelect={handleCleanupOldChats}
                />
              )}
            </div>
          )}
        </div>

        {isLoading ? (
          <div className="px-3 py-3 text-xs text-muted-foreground">Loading previous chats...</div>
        ) : visibleSessions.length === 0 ? (
          <PreviousChatEmptyState
            filter={activeFilter}
            hasAnySessions={false}
            fallbackText={emptyText}
          />
        ) : filteredSessions.length === 0 ? (
          <PreviousChatEmptyState
            filter={activeFilter}
            hasAnySessions
            fallbackText={`No previous ${activeFilter === 'schedule' ? 'schedule' : activeFilter} chats yet.`}
          />
        ) : (
          <div className={`${fill ? 'min-h-0 flex-1 overflow-y-auto' : ''} divide-y divide-border`}>
            {displayedSessions.map(session => {
              const messages = expandedMessagesBySession[session.session_id] || previewMessages(session)
              const isExpanded = expandedSessionIds.has(session.session_id)
              const isLoadingDetails = loadingExpandedSessionIds.has(session.session_id)
              const runtimeLabel = chatHistoryRuntimeLabel(session)
              const isDeleting = deletingSessionIds.has(session.session_id)
              const timeLabel = formatChatTime(session.updated_at || session.created_at)
              const messageCountLabel = formatMessageCount(session.message_count)

              return (
                <div key={session.session_id} className="group bg-background transition-colors hover:bg-muted/20">
                  <div className={`flex items-start gap-2 ${compact ? 'px-2.5 py-2' : 'px-3 py-2.5'}`}>
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
                      <div className={`mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground ${compact ? 'text-[10px]' : 'text-[11px]'}`}>
                        <span className="inline-flex min-w-0 items-center gap-1">
                          <CalendarClock className="h-3 w-3 shrink-0" />
                          <span className="truncate">{timeLabel}</span>
                        </span>
                        {messageCountLabel && (
                          <span className="inline-flex items-center gap-1">
                            <MessageSquare className="h-3 w-3 shrink-0" />
                            <span>{messageCountLabel}</span>
                          </span>
                        )}
                        {runtimeLabel && (
                          <span className="inline-flex min-w-0 max-w-full items-center gap-1 rounded border border-border/70 bg-muted/30 px-1.5 py-0.5">
                            <Code2 className="h-3 w-3 shrink-0" />
                            <span className="truncate">{runtimeLabel}</span>
                          </span>
                        )}
                      </div>
                    </button>

                    <div className="flex shrink-0 items-center gap-1">
                      <button
                        type="button"
                        onClick={() => { void handleDeleteSession(session) }}
                        disabled={isDeleting}
                        className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-destructive opacity-70 transition-colors hover:bg-destructive/10 group-hover:opacity-100 disabled:cursor-not-allowed disabled:opacity-50"
                        title="Delete this chat"
                        aria-label="Delete this chat"
                      >
                        {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                      </button>
                      <button
                        type="button"
                        onClick={() => handleSelect(session)}
                        className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground opacity-80 transition-colors hover:border-primary/40 hover:text-foreground group-hover:opacity-100"
                      >
                        <ActionIcon className="h-3.5 w-3.5" />
                        {!compact && <span>{actionLabel}</span>}
                      </button>
                    </div>
                  </div>

                  {isExpanded && (
                    <div className="px-10 pb-3">
                      <div className="max-h-80 space-y-2 overflow-y-auto rounded-md border border-border bg-muted/20 p-2 text-xs text-foreground">
                        {isLoadingDetails && (
                          <div className="flex items-center gap-2 text-muted-foreground">
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            <span>Loading recent messages...</span>
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
