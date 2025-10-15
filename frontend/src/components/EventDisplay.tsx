import React from 'react'
import { EventList } from './events'
import { Card, CardContent } from './ui/Card'
import ReactMarkdown from 'react-markdown'
import { useChatStore } from '../stores'

interface EventDisplayProps {
  // Callbacks only
  onDismissUserMessage?: () => void
  // onApproveWorkflow?: (requestId: string) => void
}

// Isolated event display component that can re-render without affecting input
export const EventDisplay = React.memo<EventDisplayProps>(({ 
  onDismissUserMessage
}) => {
  // Store subscriptions
  const {
    events,
    finalResponse,
    isCompleted,
    currentUserMessage,
    showUserMessage,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    isApprovingWorkflow: _isApproving
  } = useChatStore()

  // Debug: Log events received by EventDisplay
  React.useEffect(() => {
    // Events received
  }, [events])

  const [isExpanded, setIsExpanded] = React.useState(false)
  return (
    <div className="space-y-4 min-w-0">
      {/* User Message Display - Dismissible Sticky Header */}
      {currentUserMessage && showUserMessage && (
        <div className="sticky top-0 z-10 bg-white dark:bg-gray-900">
          <div className="bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-200 dark:border-indigo-800 rounded-md p-2 min-w-0 mx-2 my-1">
            <div className="flex items-center gap-2 min-w-0">
              <span className="text-sm font-bold text-indigo-700 dark:text-indigo-300 flex-shrink-0">👤</span>
              <span className="text-xs text-indigo-600 dark:text-indigo-400 bg-indigo-100 dark:bg-indigo-800 px-1.5 py-0.5 rounded-full">
                📌
              </span>
              <div className="flex-1 min-w-0">
                <div className={`text-xs text-indigo-800 dark:text-indigo-200 break-words ${
                  isExpanded 
                    ? 'whitespace-pre-wrap' 
                    : 'whitespace-nowrap truncate overflow-hidden'
                }`}>
                  {currentUserMessage}
                </div>
              </div>
              <div className="flex items-center gap-1 flex-shrink-0">
                {/* Expand/Collapse Message Button */}
                <button
                  onClick={() => setIsExpanded(!isExpanded)}
                  className="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors duration-200 p-1 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800"
                  title={isExpanded ? "Collapse message" : "Expand message"}
                >
                  <svg 
                    className={`w-3 h-3 transition-transform duration-200 ${isExpanded ? 'rotate-180' : ''}`} 
                    fill="none" 
                    stroke="currentColor" 
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </button>
                
                {/* Dismiss Message Button */}
                {onDismissUserMessage && (
                  <button
                    onClick={onDismissUserMessage}
                    className="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors duration-200 p-1 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800"
                    title="Dismiss message"
                  >
                    <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Event Display */}
      {events.length > 0 && (
        <div className="space-y-4 min-w-0">
          <div className="flex items-center justify-between min-w-0">
            {events.some(event => event.type === 'conversation_end' && event.id?.startsWith('final-result-')) && (
              <div className="flex items-center gap-2 text-xs text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 px-2 py-1 rounded-md flex-shrink-0">
                <span>💾</span>
                <span>Final Result preserved in history</span>
              </div>
            )}
          </div>
          <div className="min-w-0">
            <EventList 
              events={events} 
            />
          </div>
        </div>
      )}

      {/* Final Response Display */}
      {finalResponse && (
        <div className="space-y-4 min-w-0">
          <div className="flex items-center gap-2 min-w-0">
            <h3 className="text-xl font-bold text-green-700 dark:text-green-400 flex-shrink-0">
              ✅ Final Response
            </h3>
            <div className="text-sm text-gray-500 flex-shrink-0">
              {isCompleted && 'Agent completed successfully'}
            </div>
            <div className="text-xs text-gray-400 ml-auto flex-shrink-0">
              Length: {finalResponse.length} chars
            </div>
          </div>
          <Card className="border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-900/20 shadow-lg min-w-0">
            <CardContent className="p-6 min-w-0">
              <div className="prose prose-sm max-w-none dark:prose-invert min-w-0">
                <ReactMarkdown 
                  components={{
                    p: ({ children }) => <p className="mb-3 last:mb-0 text-gray-800 dark:text-gray-200 leading-relaxed">{children}</p>,
                    h1: ({ children }) => <h1 className="text-2xl font-bold mb-4 text-gray-900 dark:text-gray-100">{children}</h1>,
                    h2: ({ children }) => <h2 className="text-xl font-semibold mb-3 text-gray-900 dark:text-gray-100">{children}</h2>,
                    h3: ({ children }) => <h3 className="text-lg font-semibold mb-2 text-gray-900 dark:text-gray-100">{children}</h3>,
                    ul: ({ children }) => <ul className="list-disc list-inside mb-3 space-y-1 text-gray-800 dark:text-gray-200">{children}</ul>,
                    ol: ({ children }) => <ol className="list-decimal list-inside mb-3 space-y-1 text-gray-800 dark:text-gray-200">{children}</ol>,
                    li: ({ children }) => <li className="text-gray-800 dark:text-gray-200">{children}</li>,
                    code: ({ children }) => (
                      <code className="bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded text-sm font-mono text-gray-800 dark:text-gray-200">
                        {children}
                      </code>
                    ),
                    pre: ({ children }) => (
                      <pre className="bg-gray-100 dark:bg-gray-800 p-3 rounded text-sm font-mono overflow-x-auto text-gray-800 dark:text-gray-200">
                        {children}
                      </pre>
                    ),
                    blockquote: ({ children }) => (
                      <blockquote className="border-l-4 border-green-300 pl-4 italic text-gray-700 dark:text-gray-300 my-3">
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
