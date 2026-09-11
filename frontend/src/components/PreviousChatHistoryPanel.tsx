import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowUpRight, Bot, CalendarClock, ChevronDown, ChevronRight, Code2, Eye, Loader2, MessageSquare, Paperclip, Trash2, UserRound, type LucideIcon } from 'lucide-react'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import {
  type ChatHistoryConversation,
  type ChatHistoryMessage,
  type ChatHistoryPreviewMessage,
  type ChatHistorySession,
  type ScheduledJob,
  type ScheduledJobRun,
} from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'
import { isScheduledChatHistorySession } from '../utils/chatHistoryOpenDisposition'
import { chatHistoryWorkshopMode } from '../utils/chatHistoryWorkshopMode'
import { chatHistoryRuntimeLabel, chatHistoryRuntimeShortLabel } from '../utils/chatHistoryRuntimeLabel'
import { type ScheduleActivityItem } from '../utils/scheduleRunPresentation'
import { ScheduleRunCard } from './ScheduleRunCard'
import { ChatSessionIdCopyButton } from './ChatSessionIdCopyButton'
import { ConversationMarkdownRenderer } from './ui/MarkdownRenderer'
import {
  CHAT_HISTORY_CLEANUP_AGE_OPTIONS,
  type ChatHistoryCleanupAgeDays,
  CleanupOldChatsDropdown,
} from './CleanupOldChatsDropdown'

const PAGE_SIZE = 5
// Keep the first paint cheap. Workflow builder conversations can be large, and
// the backend has to parse each returned file to build previews. The panel only
// renders five rows at a time, so loading 100 upfront makes the spinner feel
// stuck on workflows with months of schedule/pulse history.
const FETCH_LIMIT = 25
// How many stored messages the first expand pulls. Every fetched message is
// rendered (assistant prose, tool calls, tool results), so this is the whole
// budget rather than a fetch-more-than-you-show buffer -- "Load more" walks
// further back through the conversation in increments.
const EXPANDED_FETCH_LIMIT = 16
const EXPANDED_FETCH_INCREMENT = 30
// Fallback rows built from the session-list metadata (session.preview_messages),
// shown before the details fetch lands or if it fails.
const PREVIEW_MESSAGE_LIMIT = 14

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

function ChatHistoryRuntimeBadge({ session }: { session: ChatHistorySession }) {
  const fullLabel = chatHistoryRuntimeLabel(session)
  const shortLabel = chatHistoryRuntimeShortLabel(session)
  if (!fullLabel || !shortLabel) return null

  return (
    <span
      className="group/runtime inline-flex min-w-0 max-w-full items-center gap-1 rounded border border-border/70 bg-muted/30 px-1.5 py-0.5 text-[10px]"
      title={`Coding agent and model: ${fullLabel}`}
      aria-label={`Coding agent and model: ${fullLabel}`}
    >
      <Code2 className="h-3 w-3 shrink-0" />
      <span className="whitespace-nowrap group-hover/runtime:hidden">{shortLabel}</span>
      <span className="hidden max-w-64 truncate whitespace-nowrap group-hover/runtime:inline">{fullLabel}</span>
    </span>
  )
}

export { chatHistoryRuntimeLabel, chatHistoryRuntimeShortLabel } from '../utils/chatHistoryRuntimeLabel'

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
  return chatHistoryWorkshopMode(session) === 'run' ? 'Run' : 'Workshop'
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

const sameWorkspace = (left?: string, right?: string): boolean => {
  const normalize = (value?: string) => (value || '').trim().replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
  return Boolean(normalize(left) && normalize(left) === normalize(right))
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
  if (isScheduledChatHistorySession(session)) return 'schedule'
  if (session.session_id.startsWith('bot-')) return 'bot'
  return 'chat'
}

const previewMessages = (session: ChatHistorySession): ChatHistoryPreviewMessage[] => {
  return (session.preview_messages || [])
    .filter(message => message.text?.trim())
    .slice(-PREVIEW_MESSAGE_LIMIT)
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

const functionCallOf = (part: unknown): Record<string, unknown> | null => {
  const call = (part as Record<string, unknown>)?.FunctionCall ?? (part as Record<string, unknown>)?.functionCall
  return call && typeof call === 'object' ? call as Record<string, unknown> : null
}

const messageToolName = (message: ChatHistoryMessage): string => {
  const parts = message.Parts || message.parts || []
  for (const part of parts) {
    const call = functionCallOf(part)
    const name = call?.Name ?? call?.name
      ?? (part as Record<string, unknown>).Name ?? (part as Record<string, unknown>).name
    if (typeof name === 'string' && name.trim()) return name.trim()
  }
  return ''
}

// The tool CALL lives as a FunctionCall part on an assistant message and holds
// no text, so messageText() came back empty and the call was filtered out
// entirely -- only its result survived. Surface the arguments so a reader can
// see what was actually run, not just what came back.
const messageToolArgs = (message: ChatHistoryMessage): string => {
  const parts = message.Parts || message.parts || []
  for (const part of parts) {
    const call = functionCallOf(part)
    const args = call?.Arguments ?? call?.arguments
    if (typeof args === 'string' && args.trim()) {
      try {
        return JSON.stringify(JSON.parse(args), null, 2)
      } catch {
        return args.trim()
      }
    }
  }
  return ''
}

const messageIsError = (message: ChatHistoryMessage): boolean => {
  const parts = message.Parts || message.parts || []
  return parts.some(part => Boolean((part as Record<string, unknown>).IsError ?? (part as Record<string, unknown>).is_error))
}

const shouldSkipMessageText = (text: string): boolean => {
  return text.startsWith('[AUTO-NOTIFICATION]') ||
    text.startsWith('[Previous tool call') ||
    text.startsWith('[Previous tool result')
}

const conversationMessages = (conversation: ChatHistoryConversation): ChatHistoryPreviewMessage[] => {
  return (conversation.conversation_history || [])
    .flatMap((message): ChatHistoryPreviewMessage[] => {
      const role = messageRole(message)
      const text = messageText(message)
      const toolName = messageToolName(message)
      const rows: ChatHistoryPreviewMessage[] = []
      if (text) rows.push({ role, text, toolName, isError: messageIsError(message) })
      // An assistant turn often has prose AND a tool call; emit the call as its
      // own row so neither hides the other.
      if (role === 'ai' || role === 'assistant') {
        const args = messageToolArgs(message)
        if (args) rows.push({ role: 'tool_call', text: args, toolName })
      }
      return rows
    })
    .filter(message => {
      if (!message.text) return false
      // Prose heuristics (auto-notification banners, replayed tool summaries)
      // must not be applied to raw tool arguments/results.
      if (message.role !== 'tool' && message.role !== 'tool_call' && shouldSkipMessageText(message.text)) return false
      return message.role === 'human' ||
        message.role === 'user' ||
        message.role === 'ai' ||
        message.role === 'assistant' ||
        // Tool calls were filtered out entirely, so a chat that spent most of
        // its turns in tools -- and any tool that failed -- read as if nothing
        // happened between one message and the next.
        message.role === 'tool' ||
        message.role === 'tool_call'
    })
}

// No local slice: the server already returns exactly the requested window, and
// trimming again here would make "Load more" fetch messages it then discarded.
const recentConversationMessages = (messages: ChatHistoryPreviewMessage[]): ChatHistoryPreviewMessage[] => {
  return messages.filter(message => message.text?.trim())
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
  // How many stored messages each expanded session has pulled so far, and
  // whether the server still had older ones to give.
  const [expandedWindowBySession, setExpandedWindowBySession] = useState<Record<string, number>>({})
  const [hasMoreBySession, setHasMoreBySession] = useState<Record<string, boolean>>({})
  const [scheduleJobs, setScheduleJobs] = useState<ScheduledJob[]>([])
  // Eagerly populated (all jobs, in parallel) once scheduleJobs loads — the
  // Schedules tab is a flat, recency-sorted feed of runs, not a per-job
  // grouping the user expands one at a time.
  const [scheduleRunsByJob, setScheduleRunsByJob] = useState<Record<string, ScheduledJobRun[]>>({})
  const [isLoadingScheduleActivity, setIsLoadingScheduleActivity] = useState(false)
  const [isLoadingScheduleRuns, setIsLoadingScheduleRuns] = useState(false)
  const [scheduleJobsWorkspacePath, setScheduleJobsWorkspacePath] = useState('')
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

    // Fetch ordinary chats separately. A schedule-heavy workflow can have many
    // newer schedule transcripts, so a mixed newest-N page would otherwise
    // hide every older chat before the Recent filter gets a chance to run.
    Promise.all([
      agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath),
      agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath, 'chat'),
    ])
      .then(([allResponse, chatResponse]) => {
        if (cancelled) return
        setSessions(mergeSessions(allResponse.sessions || [], chatResponse.sessions || []))
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

  // Load the parent schedules as soon as the workflow changes. The Schedule
  // badge must never claim zero just because its tab has not been opened yet.
  // Individual run histories remain lazy and are loaded only when expanded.
  useEffect(() => {
    if (!workspacePath) {
      setScheduleJobs([])
      setScheduleJobsWorkspacePath('')
      return
    }
    let cancelled = false
    setIsLoadingScheduleActivity(true)
    setIsLoadingScheduleRuns(false)

    void schedulerApi.listJobs({ entity_type: 'workflow', limit: 100 })
      .then(async response => {
        const jobs = (response.jobs || []).filter(job => sameWorkspace(job.workspace_path, workspacePath))
        if (cancelled) return
        setScheduleJobs(jobs)
        setScheduleRunsByJob({})
        setScheduleJobsWorkspacePath(workspacePath)
        setIsLoadingScheduleActivity(false)

        if (jobs.length === 0) return
        setIsLoadingScheduleRuns(true)
        try {
          // A workspace has a handful of schedules at most, so this stays a
          // small, bounded, one-time fan-out — not a poll. Cap per job since
          // the feed shows everything at once now (no per-job "show more"
          // gate left to bound an unbounded fetch behind).
          const runsByJob = await Promise.all(
            jobs.map(job => schedulerApi.getJobRuns(job.id, 30).then(
              response => [job.id, response.runs || []] as const,
              () => [job.id, []] as const,
            ))
          )
          if (cancelled) return
          setScheduleRunsByJob(Object.fromEntries(runsByJob))
        } finally {
          if (!cancelled) setIsLoadingScheduleRuns(false)
        }
      })
      .catch(() => {
        if (cancelled) return
        setScheduleJobs([])
        setScheduleRunsByJob({})
        setScheduleJobsWorkspacePath(workspacePath)
        setIsLoadingScheduleActivity(false)
        addToast('Failed to load schedule activity', 'error')
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

  const scheduleDataMatchesWorkspace = sameWorkspace(scheduleJobsWorkspacePath, workspacePath)

  // A flat, cross-schedule feed sorted by actual run recency — not grouped by
  // job. Mirrors the Recent tab's own sort-by-recency-and-paginate shape.
  const flattenedScheduleRuns = useMemo(() => {
    const items: { job: ScheduledJob; run: ScheduledJobRun }[] = []
    for (const job of scheduleJobs) {
      for (const run of scheduleRunsByJob[job.id] || []) {
        items.push({ job, run })
      }
    }
    return items.sort((a, b) => Date.parse(b.run.started_at || '') - Date.parse(a.run.started_at || ''))
  }, [scheduleJobs, scheduleRunsByJob])

  const displayedScheduleRuns = useMemo(
    () => flattenedScheduleRuns.slice(0, visibleCount),
    [flattenedScheduleRuns, visibleCount]
  )

  const displayFilterCounts = useMemo(() => ({
    ...filterCounts,
    // Before its durable schedule list arrives, show a loading mark rather
    // than the unrelated legacy chat-session count (often a false zero).
    schedule: scheduleDataMatchesWorkspace
      ? (isLoadingScheduleRuns ? '…' : flattenedScheduleRuns.length)
      : '…',
  }), [filterCounts, flattenedScheduleRuns.length, isLoadingScheduleRuns, scheduleDataMatchesWorkspace])

  const sessionsByID = useMemo(
    () => new Map(visibleSessions.map(session => [session.session_id, session])),
    [visibleSessions]
  )

  const totalForActiveFilter = activeFilter === 'schedule' ? flattenedScheduleRuns.length : filteredSessions.length
  const displayedCountForActiveFilter = activeFilter === 'schedule' ? displayedScheduleRuns.length : displayedSessions.length

  useEffect(() => {
    setVisibleCount(PAGE_SIZE)
  }, [activeFilter])

  useEffect(() => {
    onHasChatsChange?.(!isLoading && visibleSessions.length > 0, !isLoading)
  }, [isLoading, onHasChatsChange, visibleSessions.length])

  const loadExpandedMessages = useCallback(async (session: ChatHistorySession, requestedWindow?: number) => {
    const sessionId = session.session_id
    const window = requestedWindow ?? EXPANDED_FETCH_LIMIT
    const isLoadMore = requestedWindow !== undefined
    // A plain expand is a no-op once loaded; "Load more" deliberately re-fetches
    // the same session with a larger window.
    if (!isLoadMore && expandedMessagesRef.current[sessionId]) return
    if (loadingExpandedSessionIdsRef.current.has(sessionId)) return

    const nextLoading = new Set(loadingExpandedSessionIdsRef.current)
    nextLoading.add(sessionId)
    loadingExpandedSessionIdsRef.current = nextLoading
    setLoadingExpandedSessionIds(nextLoading)
    try {
      const conversation = await agentApi.getChatHistoryConversation(sessionId, workspacePath, window)
      const messages = conversationMessages(conversation)
      const recentMessages = recentConversationMessages(messages)
      // The server returns the tail of the conversation capped at `window`.
      // Getting back fewer than asked for means we reached the beginning.
      const returned = conversation.conversation_history?.length ?? 0
      setExpandedWindowBySession(current => ({ ...current, [sessionId]: window }))
      setHasMoreBySession(current => ({ ...current, [sessionId]: returned >= window }))
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
    if (session.can_resume === false) {
      toggleExpanded(session)
      return
    }
    void onSelectSession(session)
  }, [onSelectSession, toggleExpanded])

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

  const openScheduleActivity = useCallback((item: ScheduleActivityItem) => {
    const sessionID = item.run?.session_id
    const session = sessionID ? sessionsByID.get(sessionID) : undefined
    if (!session) {
      addToast('This run has no restorable conversation record', 'info')
      return
    }
    void onSelectSession(session)
  }, [addToast, onSelectSession, sessionsByID])


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
      const [allResponse, chatResponse] = await Promise.all([
        agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath),
        agentApi.listChatHistorySessions(FETCH_LIMIT, 0, workspacePath, 'chat'),
      ])
      setSessions(mergeSessions(allResponse.sessions || [], chatResponse.sessions || []))
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
  // This row is a content filter inside the conversation hub, not another
  // set of top-level chat tabs. "Recent" makes that distinction explicit:
  // the workspace tab owns the active/new conversation, while this list is
  // where someone returns to a saved one.
  const filterItems = [
    { filter: 'chat' as const, label: 'Recent', icon: MessageSquare },
    { filter: 'schedule' as const, label: 'Schedules', icon: CalendarClock },
    { filter: 'bot' as const, label: 'Bots', icon: Bot },
  ]

  return (
    <div className={`${fill ? 'flex min-h-0 flex-1 flex-col overflow-hidden' : 'shrink-0'} border-b border-border bg-background`}>
      <div className={`${fill ? 'flex min-h-0 flex-1 flex-col' : ''} w-full`}>
        <div className={`flex flex-wrap items-center gap-2 border-b border-border px-3 py-2 ${compact ? 'justify-end' : title ? 'justify-between' : 'justify-start'}`}>
          {/* The "Previous … chats" heading is redundant in the compact rail —
              the filter pills + list make the purpose obvious — so hide it there. */}
          {!compact && title && (
            <div className="flex min-w-0 items-center gap-2 text-sm">
              <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground/80" />
              <span className="truncate font-medium text-foreground">{activeFilter === 'schedule' ? 'Schedule activity' : title}</span>
            </div>
          )}

          {!isLoading && (
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
                      {displayFilterCounts[filter]}
                    </span>
                  </button>
                )
                })}
              </div>
              {activeFilter !== 'schedule' && hasOldVisibleSessions && (
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
        ) : activeFilter === 'schedule' ? (
          <div className={`${fill ? 'min-h-0 flex-1 overflow-y-auto' : ''}`}>
            {isLoadingScheduleActivity ? (
              <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>Loading schedule activity...</span>
              </div>
            ) : scheduleJobs.length === 0 ? (
              <div className="px-3 py-4 text-sm text-muted-foreground">No schedules are configured for this workflow yet.</div>
            ) : isLoadingScheduleRuns ? (
              <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>Loading scheduled runs...</span>
              </div>
            ) : flattenedScheduleRuns.length === 0 ? (
              <div className="px-3 py-4 text-sm text-muted-foreground">No scheduled runs recorded yet.</div>
            ) : (
              <div className="divide-y divide-border">
                {displayedScheduleRuns.map(({ job, run }) => (
                  <div key={run.id} className="px-3 py-3 transition-colors hover:bg-muted/20">
                    <ScheduleRunCard
                      job={job}
                      run={run}
                      resolveSession={r => (r.session_id ? sessionsByID.get(r.session_id) : undefined)}
                      onOpen={r => openScheduleActivity({ id: r.id, job, run: r, kind: 'run', occurredAt: r.started_at })}
                      onDelete={r => {
                        const session = r.session_id ? sessionsByID.get(r.session_id) : undefined
                        if (session) void handleDeleteSession(session)
                      }}
                      deletingRunIds={deletingSessionIds}
                      compact={compact}
                      showScheduleName
                      showCopySessionId
                    />
                  </div>
                ))}
              </div>
            )}
          </div>
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
            fallbackText={`No previous ${activeFilter} chats yet.`}
          />
        ) : (
          <div className={`${fill ? 'min-h-0 flex-1 overflow-y-auto' : ''} divide-y divide-border`}>
            {displayedSessions.map(session => {
              const messages = expandedMessagesBySession[session.session_id] || previewMessages(session)
              const isExpanded = expandedSessionIds.has(session.session_id)
              const isLoadingDetails = loadingExpandedSessionIds.has(session.session_id)
              // Distinguishes a first expand (blank, show a spinner) from a
              // "Load more" (keep the list on screen while the next page loads).
              const hasLoadedMessages = Boolean(expandedMessagesBySession[session.session_id]?.length)
              const runtimeLabel = chatHistoryRuntimeLabel(session)
              const isDeleting = deletingSessionIds.has(session.session_id)
              const canResume = session.can_resume !== false
              const canDelete = session.can_delete !== false
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
                        {session.username && (
                          <span className="inline-flex min-w-0 max-w-full items-center gap-1 rounded border border-border/70 bg-muted/30 px-1.5 py-0.5">
                            <UserRound className="h-3 w-3 shrink-0" />
                            <span className="truncate">{session.username}</span>
                          </span>
                        )}
                        {runtimeLabel && (
                          <ChatHistoryRuntimeBadge session={session} />
                        )}
                      </div>
                    </button>

                    <div className="flex shrink-0 items-center gap-1">
                      <ChatSessionIdCopyButton sessionId={session.session_id} compact />
                      {canDelete && (
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
                      )}
                      {canResume ? (
                        <button
                          type="button"
                          onClick={() => handleSelect(session)}
                          className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground opacity-80 transition-colors hover:border-primary/40 hover:text-foreground group-hover:opacity-100"
                        >
                          <ActionIcon className="h-3.5 w-3.5" />
                          {!compact && <span>{actionLabel}</span>}
                        </button>
                      ) : (
                        <span
                          className="inline-flex items-center rounded border border-border/70 bg-muted/30 p-1 text-muted-foreground"
                          title="View only"
                          aria-label="View only"
                        >
                          <Eye className="h-3.5 w-3.5" />
                          <span className="sr-only">View only</span>
                        </span>
                      )}
                    </div>
                  </div>

                  {isExpanded && (
                    <div className="px-10 pb-3">
                      <div className="max-h-80 space-y-2 overflow-y-auto rounded-md border border-border bg-muted/20 p-2 text-xs text-foreground">
                        {isLoadingDetails && !hasLoadedMessages && (
                          <div className="flex items-center gap-2 text-muted-foreground">
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            <span>Loading recent messages...</span>
                          </div>
                        )}
                        {!isLoadingDetails && messages.length === 0 && (
                          <div className="text-muted-foreground">No displayable messages found.</div>
                        )}
                        {isLoadingDetails && hasLoadedMessages && (
                          <div className="flex items-center justify-center gap-2 py-1 text-[11px] text-muted-foreground">
                            <Loader2 className="h-3 w-3 animate-spin" />
                            <span>Loading older messages...</span>
                          </div>
                        )}
                        {!isLoadingDetails && hasMoreBySession[session.session_id] && (
                          <button
                            type="button"
                            onClick={() => void loadExpandedMessages(
                              session,
                              (expandedWindowBySession[session.session_id] ?? EXPANDED_FETCH_LIMIT) + EXPANDED_FETCH_INCREMENT,
                            )}
                            className="w-full rounded border border-border/60 px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
                          >
                            Load older messages
                          </button>
                        )}
                        {(!isLoadingDetails || hasLoadedMessages) && messages.map((message, index) => {
                          const isToolCall = message.role === 'tool_call'
                          const isTool = message.role === 'tool' || isToolCall
                          const normalizedRole = isTool
                            ? `${isToolCall ? 'Tool call' : 'Tool result'}${message.toolName ? ` · ${message.toolName}` : ''}`
                            : message.role === 'ai' || message.role === 'assistant' ? 'Assistant' : 'User'
                          const roleClass = message.isError
                            ? 'text-red-600 dark:text-red-400'
                            : isTool
                              ? 'text-amber-600 dark:text-amber-400'
                              : normalizedRole === 'Assistant'
                                ? 'text-emerald-600 dark:text-emerald-400'
                                : 'text-sky-600 dark:text-sky-400'

                          return (
                            <div key={`${session.session_id}-previous-preview-${index}`} className="space-y-1 rounded bg-background/70 px-2 py-1.5">
                              <div className={`text-[10px] font-semibold uppercase leading-none ${roleClass}`}>
                                {normalizedRole}
                              </div>
                              {/* Rendered as markdown, not raw text: the stored
                                  answer is markdown (bold, headings, tables) and
                                  whitespace-pre-wrap showed it literally, e.g.
                                  "**Toptal**" with the asterisks visible. */}
                              {isTool ? (
                                <pre className={`max-h-40 overflow-auto whitespace-pre-wrap break-words rounded px-1 py-0.5 font-mono text-[11px] leading-4 ${
                                  message.isError
                                    ? 'bg-red-950/25 text-red-200'
                                    : 'bg-muted/40 text-muted-foreground'
                                }`}>
                                  {message.text}
                                </pre>
                              ) : (
                                <div className="break-words leading-relaxed text-muted-foreground">
                                  <ConversationMarkdownRenderer content={message.text} maxHeight="none" framed={false} />
                                </div>
                              )}
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

        {!isLoading && totalForActiveFilter > displayedCountForActiveFilter && (
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
