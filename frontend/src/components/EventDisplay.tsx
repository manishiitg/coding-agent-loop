import React from 'react'
import { useShallow } from 'zustand/react/shallow'
import { EventList } from './events'
import { BackgroundAgentsStatusBar } from './events/BackgroundAgentsStatusBar'
import { Card, CardContent } from './ui/Card'
import ReactMarkdown from 'react-markdown'
import { useChatStore } from '../stores'
import { agentApi } from '../services/api'
import type { PollingEvent } from '../services/api-types'
import { useRenderLogger } from '../utils/renderLogger'

interface EventDisplayProps {
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  compact?: boolean
  flatHierarchy?: boolean
  events?: PollingEvent[]  // Required: events should always be passed from ChatArea (tab-specific)
  sessionId?: string | null  // Session ID for streaming text lookup
  tabId?: string  // Specific tab ID for independent tool-call toggling in multi-chat
}

// Shared markdown components factory for consistent rendering
const getMarkdownComponents = (compact: boolean) => ({
  p: ({ children }: { children?: React.ReactNode }) => <p className={`${compact ? 'mb-2 last:mb-0 text-xs' : 'mb-2.5 last:mb-0 text-xs'} text-gray-800 dark:text-gray-200 leading-relaxed`}>{children}</p>,
  h1: ({ children }: { children?: React.ReactNode }) => <h1 className={`${compact ? 'text-lg' : 'text-xl'} font-bold ${compact ? 'mb-2' : 'mb-3'} text-gray-900 dark:text-gray-100`}>{children}</h1>,
  h2: ({ children }: { children?: React.ReactNode }) => <h2 className={`${compact ? 'text-base' : 'text-lg'} font-semibold ${compact ? 'mb-2' : 'mb-2.5'} text-gray-900 dark:text-gray-100`}>{children}</h2>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className={`${compact ? 'text-sm' : 'text-base'} font-semibold ${compact ? 'mb-1' : 'mb-2'} text-gray-900 dark:text-gray-100`}>{children}</h3>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className={`list-disc ${compact ? 'pl-4 mb-2 space-y-0.5' : 'pl-5 mb-2.5 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className={`list-decimal ${compact ? 'pl-4 mb-2 space-y-0.5' : 'pl-5 mb-2.5 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className={`${compact ? 'text-xs' : 'text-xs'} text-gray-800 dark:text-gray-200 leading-relaxed`}>{children}</li>,
  code: ({ children }: { children?: React.ReactNode }) => (
    <code className={`bg-gray-100 dark:bg-gray-800 ${compact ? 'px-1 py-0.5' : 'px-1.5 py-0.5'} rounded ${compact ? 'text-[10px]' : 'text-xs'} font-mono text-gray-800 dark:text-gray-200`}>
      {children}
    </code>
  ),
  pre: ({ children }: { children?: React.ReactNode }) => (
    <pre className={`bg-gray-100 dark:bg-gray-800 ${compact ? 'p-2 my-2' : 'p-3 my-2.5'} rounded ${compact ? 'text-[10px]' : 'text-xs'} font-mono overflow-x-auto text-gray-800 dark:text-gray-200`}>
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
})

// Isolated event display component that can re-render without affecting input
export const EventDisplay = React.memo<EventDisplayProps>(({ onFeedbackSubmitted, onSendMessage, compact = false, flatHierarchy = false, events: propEvents, sessionId, tabId }) => {
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

  // Memoize markdown components to avoid re-creating on every render
  const markdownComponents = React.useMemo(() => getMarkdownComponents(compact), [compact])

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
          <BackgroundAgentsStatusBar events={events} />
          <div className="min-w-0" data-testid="event-list-wrapper" data-event-count={events.length}>
            <EventList
              events={events}
              onApproveWorkflow={handleApproveWorkflow}
              onSubmitFeedback={handleSubmitFeedback}
              onFeedbackSubmitted={onFeedbackSubmitted}
              onSendMessage={onSendMessage}
              isApproving={false}
              compact={compact}
              flatHierarchy={flatHierarchy}
              tabId={tabId}
            />
          </div>
        </div>
      )}

      {/* Streaming Text Display - shows LLM output as it generates */}
      {(currentStreamingText || currentStreamingStatus) && (
        <Card className="border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-900/20 shadow-sm min-w-0">
          <CardContent className={`${compact ? 'p-2' : 'p-3'} min-w-0`}>
            <div className="flex items-center gap-1.5 mb-1">
              <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse" />
              <span className={`${compact ? 'text-[9px]' : 'text-[10px]'} text-blue-600 dark:text-blue-400 font-medium`}>
                Generating...
              </span>
            </div>
            {currentStreamingText && (
              <div className={`prose prose-xs max-w-none dark:prose-invert min-w-0 ${compact ? 'text-[10px]' : 'text-xs'}`}>
                <ReactMarkdown components={markdownComponents}>
                  {currentStreamingText}
                </ReactMarkdown>
                <span className="inline-block w-1.5 h-3 bg-blue-500 animate-pulse ml-0.5" />
              </div>
            )}
            {currentStreamingStatus && (
              <div className={`${compact ? 'text-[9px]' : 'text-[10px]'} text-blue-500 dark:text-blue-400 italic mt-1 opacity-75`}>
                {currentStreamingStatus}
              </div>
            )}
          </CardContent>
        </Card>
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
                <ReactMarkdown components={markdownComponents}>
                  {finalResponse}
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
