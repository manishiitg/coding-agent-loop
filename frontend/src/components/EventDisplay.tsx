import React from 'react'
import { EventList } from './events'
import { Card, CardContent } from './ui/Card'
import ReactMarkdown from 'react-markdown'
import { useChatStore } from '../stores'
import { agentApi } from '../services/api'
import type { PollingEvent } from '../services/api-types'

interface EventDisplayProps {
  onFeedbackSubmitted?: () => void
  compact?: boolean
  events?: PollingEvent[]  // Required: events should always be passed from ChatArea (tab-specific)
}

// Isolated event display component that can re-render without affecting input
export const EventDisplay = React.memo<EventDisplayProps>(({ onFeedbackSubmitted, compact = false, events: propEvents }) => {
  // Store subscriptions (only for finalResponse and isCompleted - not events)
  const {
    finalResponse,
    isCompleted,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    isApprovingWorkflow: _isApproving
  } = useChatStore()
  
  // CRITICAL: Always use prop events - never fall back to global events to prevent cross-tab mixing
  // Events should always be passed from ChatArea (which uses tab-specific events)
  const events = React.useMemo(() => {
    if (!propEvents) {
      console.warn(`[EventDisplay] No events prop provided - this should not happen. Events should be passed from ChatArea.`)
    }
    return propEvents || []
  }, [propEvents])

  // Handle workflow approval
  const handleApproveWorkflow = React.useCallback(async (requestId: string) => {
    try {
      // Submit "Approve" response to unblock the orchestrator
      await agentApi.submitHumanFeedback(requestId, "Approve")
      console.log('Workflow approved:', requestId)
    } catch (error) {
      console.error('Failed to approve workflow:', error)
    }
  }, [])

  // Handle feedback submission
  const handleSubmitFeedback = React.useCallback(async (requestId: string, feedback: string) => {
    try {
      // Submit feedback response to unblock the orchestrator
      await agentApi.submitHumanFeedback(requestId, feedback)
      console.log('Feedback submitted:', requestId, feedback)
    } catch (error) {
      console.error('Failed to submit feedback:', error)
    }
  }, [])

  // Debug: Log events received by EventDisplay
  React.useEffect(() => {
    console.log(`[EventDisplay] Received ${events.length} events:`, events.map(e => ({ id: e.id, type: e.type, timestamp: e.timestamp })))
  }, [events])

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
          <div className="min-w-0" data-testid="event-list-wrapper" data-event-count={events.length}>
            <EventList 
              events={events} 
              onApproveWorkflow={handleApproveWorkflow}
              onSubmitFeedback={handleSubmitFeedback}
              onFeedbackSubmitted={onFeedbackSubmitted}
              isApproving={false}
              compact={compact}
            />
          </div>
        </div>
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
                <ReactMarkdown 
                  components={{
                    p: ({ children }) => <p className={`${compact ? 'mb-2 last:mb-0 text-xs' : 'mb-3 last:mb-0'} text-gray-800 dark:text-gray-200 leading-relaxed`}>{children}</p>,
                    h1: ({ children }) => <h1 className={`${compact ? 'text-lg' : 'text-2xl'} font-bold ${compact ? 'mb-2' : 'mb-4'} text-gray-900 dark:text-gray-100`}>{children}</h1>,
                    h2: ({ children }) => <h2 className={`${compact ? 'text-base' : 'text-xl'} font-semibold ${compact ? 'mb-2' : 'mb-3'} text-gray-900 dark:text-gray-100`}>{children}</h2>,
                    h3: ({ children }) => <h3 className={`${compact ? 'text-sm' : 'text-lg'} font-semibold ${compact ? 'mb-1' : 'mb-2'} text-gray-900 dark:text-gray-100`}>{children}</h3>,
                    ul: ({ children }) => <ul className={`list-disc list-inside ${compact ? 'mb-2 space-y-0.5' : 'mb-3 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ul>,
                    ol: ({ children }) => <ol className={`list-decimal list-inside ${compact ? 'mb-2 space-y-0.5' : 'mb-3 space-y-1'} text-gray-800 dark:text-gray-200`}>{children}</ol>,
                    li: ({ children }) => <li className={`${compact ? 'text-xs' : ''} text-gray-800 dark:text-gray-200`}>{children}</li>,
                    code: ({ children }) => (
                      <code className={`bg-gray-100 dark:bg-gray-800 ${compact ? 'px-1 py-0.5' : 'px-2 py-1'} rounded ${compact ? 'text-[10px]' : 'text-sm'} font-mono text-gray-800 dark:text-gray-200`}>
                        {children}
                      </code>
                    ),
                    pre: ({ children }) => (
                      <pre className={`bg-gray-100 dark:bg-gray-800 ${compact ? 'p-2' : 'p-3'} rounded ${compact ? 'text-[10px]' : 'text-sm'} font-mono overflow-x-auto text-gray-800 dark:text-gray-200`}>
                        {children}
                      </pre>
                    ),
                    blockquote: ({ children }) => (
                      <blockquote className={`border-l-4 border-green-300 pl-4 italic text-gray-700 dark:text-gray-300 ${compact ? 'my-2' : 'my-3'}`}>
                        {children}
                      </blockquote>
                    ),
                    strong: ({ children }) => <strong className="font-semibold text-gray-900 dark:text-gray-100">{children}</strong>,
                    em: ({ children }) => <em className="italic text-gray-800 dark:text-gray-200">{children}</em>,
                  }}
                >
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
