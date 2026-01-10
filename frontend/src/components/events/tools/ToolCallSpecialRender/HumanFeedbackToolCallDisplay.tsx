import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/events'
import { Button } from '../../../ui/Button'
import { Textarea } from '../../../ui/Textarea'
import { Card } from '../../../ui/Card'
import { MarkdownRenderer } from '../../../ui/MarkdownRenderer'

interface HumanFeedbackToolCallDisplayProps {
  event: ToolCallStartEvent
}

export const HumanFeedbackToolCallDisplay: React.FC<HumanFeedbackToolCallDisplayProps> = ({ event }) => {
  const [response, setResponse] = React.useState('')
  const [isSubmitting, setIsSubmitting] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [isSubmitted, setIsSubmitted] = React.useState(false)
  const [notificationPermission, setNotificationPermission] = React.useState<NotificationPermission>('default')

  // Extract parameters from tool arguments
  const getToolParams = () => {
    try {
      if (event.tool_params?.arguments) {
        const args = JSON.parse(event.tool_params.arguments)
        return {
          unique_id: args.unique_id || '',
          message_for_user: args.message_for_user || '',
          session_id: args.session_id
        }
      }
    } catch (err) {
      console.error('Failed to parse tool arguments:', err)
    }
    return {
      unique_id: '',
      message_for_user: 'Please provide your feedback',
      session_id: undefined
    }
  }

  const toolParams = getToolParams()

  // Request notification permission on component mount
  React.useEffect(() => {
    if ('Notification' in window) {
      setNotificationPermission(Notification.permission)
      
      if (Notification.permission === 'default') {
        Notification.requestPermission().then((permission) => {
          setNotificationPermission(permission)
        }).catch((error) => {
          console.error('[HUMAN_FEEDBACK] Permission request failed:', error)
        })
      }
    }
  }, [])

  // Show browser notification when component mounts (feedback request)
  React.useEffect(() => {
    if ('Notification' in window && Notification.permission === 'granted') {
      try {
        const notification = new Notification('Human Feedback Required', {
          body: toolParams.message_for_user,
          icon: '/favicon.ico',
          tag: `human-feedback-${toolParams.unique_id}`,
          requireInteraction: true,
          silent: false
        })

        notification.onclick = () => {
          window.focus()
          notification.close()
        }

        notification.onshow = () => {
          // Notification shown successfully
        }

        notification.onerror = (error) => {
          console.error('[HUMAN_FEEDBACK] Notification error:', error)
        }

        notification.onclose = () => {
          // Notification closed
        }

        // Auto-close notification after 30 seconds
        setTimeout(() => {
          notification.close()
        }, 30000)

        return () => {
          notification.close()
        }
      } catch (error) {
        console.error('[HUMAN_FEEDBACK] Failed to create notification:', error)
      }
    }
  }, [toolParams.message_for_user, toolParams.unique_id])

  const handleSubmit = async () => {
    if (!response.trim()) {
      setError('Please provide a response')
      return
    }

    setIsSubmitting(true)
    setError(null)

    try {
      // Import the API function dynamically to avoid circular imports
      const { agentApi } = await import('../../../../services/api')
      await agentApi.submitHumanFeedback(toolParams.unique_id, response.trim())
      setIsSubmitted(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to submit feedback')
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!isSubmitting && !isSubmitted && response.trim()) {
        handleSubmit()
      }
    }
  }

  return (
    <div className="bg-orange-50 dark:bg-orange-900/20 border border-orange-200 dark:border-orange-800 rounded p-2">
      <div className="flex items-center justify-between gap-3 mb-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="w-6 h-6 bg-orange-500 rounded-full flex items-center justify-center">
            <span className="text-white text-xs font-bold">👤</span>
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-orange-700 dark:text-orange-300">
              Human Feedback Required{' '}
              <span className="text-xs font-normal text-orange-600 dark:text-orange-400">
                {event.turn && `• Turn: ${event.turn}`}
                {event.tool_name && ` • Tool: ${event.tool_name}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-orange-600 dark:text-orange-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>

      {/* Human Feedback UI */}
      <Card className="p-3 mb-2 border border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-950">
        <div className="space-y-3">
          {/* Message from LLM */}
          <div className="bg-white dark:bg-gray-800 p-3 rounded border border-gray-200 dark:border-gray-700">
            <MarkdownRenderer 
              content={toolParams.message_for_user}
              className="text-gray-700 dark:text-gray-300"
            />
          </div>

          {/* Response input */}
          {!isSubmitted ? (
            <div className="space-y-2">
              <label htmlFor="feedback-response" className="block text-xs font-medium text-gray-700 dark:text-gray-300">
                Your Response:
              </label>
              <Textarea
                id="feedback-response"
                value={response}
                onChange={(e) => setResponse(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Please provide your feedback here... (Enter to submit, Shift+Enter for newline)"
                className="min-h-[100px] resize-y text-sm border-gray-200 dark:border-gray-700 focus:border-blue-500 dark:focus:border-blue-400"
                disabled={isSubmitting}
              />
            </div>
          ) : (
            <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-3">
              <div className="flex items-center gap-2 mb-2">
                <span className="text-green-600 dark:text-green-400 text-sm">✓</span>
                <span className="text-sm font-medium text-green-700 dark:text-green-300">
                  Feedback Submitted Successfully
                </span>
              </div>
              <p className="text-xs text-green-600 dark:text-green-400">
                Your response has been sent to the agent.
              </p>
            </div>
          )}

          {/* Error display */}
          {error && (
            <div className="p-2 bg-red-100 dark:bg-red-900 border border-red-300 dark:border-red-700 rounded">
              <p className="text-red-700 dark:text-red-300 text-xs">{error}</p>
            </div>
          )}

          {/* Action buttons */}
          {!isSubmitted && (
            <div className="flex gap-2 justify-between items-center">
              <div className="text-xs text-gray-500 dark:text-gray-400">
                💡 Tip: Press <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-700 rounded text-xs font-mono">Enter</kbd> to submit
              </div>
              <Button
                onClick={handleSubmit}
                disabled={isSubmitting || !response.trim()}
                className="bg-blue-600 hover:bg-blue-700 text-white text-xs px-3 py-1 h-7 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isSubmitting ? 'Submitting...' : 'Submit Feedback'}
              </Button>
            </div>
          )}

          {/* Request ID and Notification Status */}
          <div className="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
            <span>Request ID: {toolParams.unique_id}</span>
            <div className="flex items-center gap-2">
              {notificationPermission === 'granted' && (
                <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
                  <span>🔔</span>
                  <span>Notifications enabled</span>
                </span>
              )}
              {notificationPermission === 'denied' && (
                <span className="flex items-center gap-1 text-red-600 dark:text-red-400">
                  <span>🔕</span>
                  <span>Notifications blocked</span>
                </span>
              )}
              {notificationPermission === 'default' && (
                <button
                  onClick={() => {
                    if ('Notification' in window) {
                      Notification.requestPermission().then((permission) => {
                        setNotificationPermission(permission)
                      })
                    }
                  }}
                  className="flex items-center gap-1 text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300"
                >
                  <span>🔔</span>
                  <span>Enable notifications</span>
                </button>
              )}
            </div>
          </div>
        </div>
      </Card>
    </div>
  )
}
