import React from 'react'
import { useShallow } from 'zustand/react/shallow'
import { EventList } from './events'
import { Card, CardContent } from './ui/Card'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { normalizeEventViewMode, useChatStore } from '../stores/useChatStore'
import { agentApi, getApiBaseUrl } from '../services/api'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useAppStore } from '../stores/useAppStore'
import type { PollingEvent, SessionExecutionTreeResponse } from '../services/api-types'
import { useRenderLogger } from '../utils/renderLogger'
import { normalizeMarkdownContent } from './ui/MarkdownRenderer'
import { formatLiveStreamingPreview } from '../utils/streamingStatus'

interface EventDisplayProps {
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  compact?: boolean
  events?: PollingEvent[]  // Required: events should always be passed from ChatArea (tab-specific)
  executionTree?: SessionExecutionTreeResponse
  sessionId?: string | null  // Session ID for streaming text lookup
  tabId?: string  // Specific tab ID for independent tool-call toggling in multi-chat
}

// Shared markdown components factory for consistent rendering
const getMarkdownComponents = (compact: boolean) => ({
  p: ({ children }: { children?: React.ReactNode }) => <p className={`${compact ? 'mb-2 last:mb-0 text-xs' : 'mb-2.5 last:mb-0 text-xs'} text-gray-800 dark:text-gray-200 leading-relaxed`}>{children}</p>,
  h1: ({ children }: { children?: React.ReactNode }) => <h1 className={`${compact ? 'text-lg' : 'text-xl'} font-bold ${compact ? 'mb-2' : 'mb-3'} text-gray-900 dark:text-gray-100`}>{children}</h1>,
  h2: ({ children }: { children?: React.ReactNode }) => <h2 className={`${compact ? 'text-base' : 'text-lg'} font-semibold ${compact ? 'mb-2' : 'mb-2.5'} text-gray-900 dark:text-gray-100`}>{children}</h2>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className={`${compact ? 'text-sm' : 'text-base'} font-semibold ${compact ? 'mb-1' : 'mb-2'} text-gray-900 dark:text-gray-100`}>{children}</h3>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className={`list-disc list-outside ${compact ? 'pl-5 mb-2 space-y-0.5' : 'pl-6 mb-2.5 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className={`list-decimal list-outside ${compact ? 'pl-5 mb-2 space-y-0.5' : 'pl-6 mb-2.5 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className={`${compact ? 'text-xs' : 'text-xs'} pl-1 text-gray-800 dark:text-gray-200 leading-relaxed`}>{children}</li>,
  code: ({ children, className, inline }: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) => {
    const language = /language-(\w+)/.exec(className || '')?.[1]?.toLowerCase()
    const isPlainBlock = !inline && ['text', 'txt', 'plain', 'plaintext', 'terminal'].includes(language || '')
    if (isPlainBlock) {
      return <code className="font-sans text-inherit">{children}</code>
    }
    return (
      <code className={`bg-gray-100 dark:bg-neutral-800 ${compact ? 'px-1 py-0.5' : 'px-1.5 py-0.5'} rounded ${compact ? 'text-[10px]' : 'text-xs'} font-mono text-gray-800 dark:text-gray-200`}>
        {children}
      </code>
    )
  },
  pre: ({ children }: { children?: React.ReactNode }) => (
    <pre className={`bg-transparent ${compact ? 'p-0 my-2' : 'p-0 my-2.5'} ${compact ? 'text-[10px]' : 'text-[11px]'} whitespace-pre-wrap overflow-x-auto text-gray-800 dark:text-gray-200 leading-5`}>
      {children}
    </pre>
  ),
  blockquote: ({ children }: { children?: React.ReactNode }) => (
    <blockquote className={`border-l-4 border-green-300 ${compact ? 'pl-3 my-2' : 'pl-4 my-2.5'} italic text-gray-700 dark:text-gray-300`}>
      {children}
    </blockquote>
  ),
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="font-semibold text-gray-900 dark:text-gray-100">{children}</strong>,
  em: ({ children }: { children?: React.ReactNode }) => <em className="italic text-gray-800 dark:text-gray-200">{children}</em>,
  table: ({ children }: { children?: React.ReactNode }) => (
    <div className="overflow-x-auto my-4 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
      <table className="min-w-full border-collapse">{children}</table>
    </div>
  ),
  thead: ({ children }: { children?: React.ReactNode }) => (
    <thead className="bg-gray-50 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">{children}</thead>
  ),
  tbody: ({ children }: { children?: React.ReactNode }) => (
    <tbody className="bg-white dark:bg-gray-900 divide-y divide-gray-200 dark:divide-gray-700">{children}</tbody>
  ),
  tr: ({ children }: { children?: React.ReactNode }) => (
    <tr className="transition-colors hover:bg-gray-50 dark:hover:bg-gray-800">{children}</tr>
  ),
  th: ({ children }: { children?: React.ReactNode }) => (
    <th className="px-4 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">{children}</th>
  ),
  td: ({ children }: { children?: React.ReactNode }) => (
    <td className="px-4 py-3 text-sm text-gray-900 dark:text-gray-100">{children}</td>
  ),
  a: ({ href, children }: { href?: string; children?: React.ReactNode }) => {
    const workspacePrefixes = ['Chats/', 'Downloads/', 'skills/', 'Workflow/']
    const isWorkspacePath = href && workspacePrefixes.some(p => href.startsWith(p))
    const workspaceFilepath = href?.startsWith('#workspace/')
      ? decodeURIComponent(href.replace('#workspace/', ''))
      : isWorkspacePath ? href : null

    if (workspaceFilepath) {
      const handleClick = (e: React.MouseEvent) => {
        e.preventDefault()
        e.stopPropagation()
        const { setSelectedFile, setShowFileContent, setLoadingFileContent, highlightFile, expandFoldersForFile } = useWorkspaceStore.getState()
        const { setWorkspaceMinimized } = useAppStore.getState()
        const resolvedPath = workspaceFilepath
        const fileName = resolvedPath.split('/').pop() || resolvedPath
        setWorkspaceMinimized(false)
        expandFoldersForFile(resolvedPath)
        highlightFile(resolvedPath)
        setSelectedFile({ name: fileName, path: resolvedPath })
        setLoadingFileContent(true)
        setShowFileContent(true)
      }
      return (
        <a href={href} onClick={handleClick} style={{ cursor: 'pointer' }}
          className="text-purple-600 dark:text-purple-400 hover:text-purple-800 dark:hover:text-purple-300 underline cursor-pointer break-words overflow-wrap-anywhere font-medium transition-colors"
          title={`Open ${workspaceFilepath} in workspace`}>
          {children}
        </a>
      )
    }
    return (
      <a href={href} target="_blank" rel="noopener noreferrer"
        className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline break-words overflow-wrap-anywhere">
        {children}
      </a>
    )
  },
  img: ({ src, alt }: { src?: string; alt?: string }) => {
    const workspacePrefixes = ['Chats/', 'Downloads/', 'skills/', 'Workflow/']
    const isWorkspacePath = !!src && workspacePrefixes.some(p => src.startsWith(p))
    const resolvedSrc = isWorkspacePath
      ? `${getApiBaseUrl()}/api/public/file?path=${btoa(src!)}`
      : src
    console.log(`[IMAGE_RENDER] src="${src}" isWorkspace=${isWorkspacePath} resolvedSrc="${resolvedSrc}"`)
    return <img src={resolvedSrc} alt={alt} className="max-w-full h-auto rounded-lg shadow-md my-4 border border-gray-200 dark:border-gray-700" />
  },
})

// Isolated event display component that can re-render without affecting input
export const EventDisplay = React.memo<EventDisplayProps>(({ onFeedbackSubmitted, onSendMessage, compact = false, events: propEvents, executionTree, sessionId, tabId }) => {
  // Store subscriptions (only for finalResponse and isCompleted - not events)
  const {
    finalResponse,
    isCompleted,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    isApprovingWorkflow: _isApproving
  } = useChatStore(useShallow(state => ({
    finalResponse: state.finalResponse,
    isCompleted: state.isCompleted,
    isApprovingWorkflow: state.isApprovingWorkflow
  })))

  // Subscribe to streaming text for current session
  const currentStreamingText = useChatStore(state =>
    sessionId ? state.streamingText[sessionId] || '' : ''
  )
  const currentStreamingStatus = useChatStore(state =>
    sessionId ? state.streamingStatus[sessionId] || '' : ''
  )
  const completedStreamingText = useChatStore(state =>
    sessionId ? state.completedStreamingText[sessionId] || '' : ''
  )
  const eventViewMode = useChatStore(state => {
    const targetTabId = tabId || state.activeTabId
    return normalizeEventViewMode(targetTabId ? state.chatTabs[targetTabId]?.viewMode : state.eventViewModePreference)
  })
  // CRITICAL: Always use prop events - never fall back to global events to prevent cross-tab mixing
  // Events should always be passed from ChatArea (which uses tab-specific events)
  const events = React.useMemo(() => {
    if (!propEvents) {
      console.warn(`[EventDisplay] No events prop provided - this should not happen. Events should be passed from ChatArea.`)
    }
    return propEvents || []
  }, [propEvents])

  useRenderLogger('EventDisplay', {
    events: events.length,
    hasStreamingText: !!currentStreamingText,
    streamingTextLen: currentStreamingText.length,
    finalResponse: !!finalResponse,
    isCompleted,
    sessionId,
  })

  // Check if events already contain a completion event (to avoid showing duplicate "Thinking" text)
  const hasCompletionEvent = React.useMemo(
    () => events.some(e => e.type === 'unified_completion' || e.type === 'llm_generation_end'),
    [events]
  )

  // Memoize markdown components to avoid re-creating on every render
  const markdownComponents = React.useMemo(() => getMarkdownComponents(compact), [compact])
  const normalizedFinalResponse = React.useMemo(() => normalizeMarkdownContent(finalResponse || ''), [finalResponse])
  const liveStreamingActivity = currentStreamingText || currentStreamingStatus
  const liveStreamingPreview = React.useMemo(
    () => formatLiveStreamingPreview(currentStreamingStatus || currentStreamingText),
    [currentStreamingStatus, currentStreamingText]
  )
  const showCompletedThinking =
    eventViewMode !== 'tree' &&
    completedStreamingText.trim() !== '' &&
    !currentStreamingText &&
    completedStreamingText.trim() !== finalResponse?.trim() &&
    !hasCompletionEvent
  const streamingActivityCard = eventViewMode !== 'tree' && liveStreamingActivity ? (
    <Card className="border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-900/20 shadow-sm min-w-0" data-testid="live-streaming-activity">
      <CardContent className={`${compact ? 'px-2 py-1.5' : 'px-3 py-2'} min-w-0`}>
        <div className="flex min-w-0 items-center gap-2">
          <div className="h-1.5 w-1.5 shrink-0 rounded-full bg-blue-500 animate-pulse" />
          <span className={`${compact ? 'text-[9px]' : 'text-[10px]'} text-blue-600 dark:text-blue-400 font-medium`}>
            Generating...
          </span>
          {liveStreamingPreview && (
            <span className={`${compact ? 'text-[9px]' : 'text-[10px]'} min-w-0 truncate text-blue-500 dark:text-blue-400 opacity-80`}>
              {liveStreamingPreview}
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  ) : null

  // Handle workflow approval
  const handleApproveWorkflow = React.useCallback(async (requestId: string) => {
    // Submit "Approve" response to unblock the orchestrator
    // Let errors propagate so callers can handle failure
    await agentApi.submitHumanFeedback(requestId, "Approve")
    console.log('Workflow approved:', requestId)
  }, [])

  // Handle feedback submission
  const handleSubmitFeedback = React.useCallback(async (requestId: string, feedback: string) => {
    // Submit feedback response to unblock the orchestrator
    // Let errors propagate so callers can handle failure (e.g. reset UI state)
    await agentApi.submitHumanFeedback(requestId, feedback)
    console.log('Feedback submitted:', requestId, feedback)
  }, [])

  return (
    <div className="space-y-4 min-w-0" data-testid="event-display-container">
      {/* Transient model/tool activity stays visually outside the hierarchy. */}
      {streamingActivityCard}

      {/* Event Display */}
      {events.length > 0 && (
        <div className="space-y-4 min-w-0" data-testid="event-list-container">
          <div className="flex items-center justify-between min-w-0">
            {events.some(event => event.type === 'conversation_end' && event.id?.startsWith('final-result-')) && (
              <div className={`flex items-center gap-2 ${compact ? 'text-[10px]' : 'text-xs'} text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 ${compact ? 'px-1.5 py-0.5' : 'px-2 py-1'} rounded-md flex-shrink-0`}>
                <span>💾</span>
                <span>Final Result preserved in history</span>
              </div>
            )}
          </div>
          <div className="min-w-0" data-testid="event-list-wrapper" data-event-count={events.length}>
            <EventList
              events={events}
              executionTree={executionTree}
              onApproveWorkflow={handleApproveWorkflow}
              onSubmitFeedback={handleSubmitFeedback}
              onFeedbackSubmitted={onFeedbackSubmitted}
              onSendMessage={onSendMessage}
              isApproving={false}
              compact={compact}
              tabId={tabId}
            />
          </div>
        </div>
      )}


      {/* Completed Streaming Text - preserved intermediate output from generation */}
      {/* Hidden in tree mode; active streaming remains visible above the hierarchy. */}
      {showCompletedThinking && (
        <details className="min-w-0 group">
          <summary className={`${compact ? 'text-[9px]' : 'text-[10px]'} text-gray-400 dark:text-gray-500 cursor-pointer hover:text-gray-600 dark:hover:text-gray-300 select-none`}>
            Thinking
          </summary>
          <div className="mt-1 border-l-2 border-gray-200 dark:border-gray-700 pl-2">
            <div className={`prose prose-xs max-w-none dark:prose-invert min-w-0 ${compact ? 'text-[9px]' : 'text-[10px]'} opacity-75`}>
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                {completedStreamingText}
              </ReactMarkdown>
            </div>
          </div>
        </details>
      )}
      {/* Final Response Display */}
      {finalResponse && (
        <div className="space-y-4 min-w-0">
          <div className="flex items-center gap-2 min-w-0">
            <h3 className={`${compact ? 'text-base' : 'text-xl'} font-bold text-green-700 dark:text-green-400 flex-shrink-0`}>
              ✅ Final Response
            </h3>
            <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-500 flex-shrink-0`}>
              {isCompleted && 'Agent completed successfully'}
            </div>
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-400 ml-auto flex-shrink-0`}>
              Length: {finalResponse.length} chars
            </div>
          </div>
          <Card className="border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-900/20 shadow-lg min-w-0">
            <CardContent className={`${compact ? 'p-3' : 'p-6'} min-w-0`}>
              <div className={`prose ${compact ? 'prose-xs' : 'prose-sm'} max-w-none dark:prose-invert min-w-0`}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                  {normalizedFinalResponse}
                </ReactMarkdown>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
})

EventDisplay.displayName = 'EventDisplay'

EventDisplay.whyDidYouRender = true
