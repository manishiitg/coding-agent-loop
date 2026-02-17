import React, { useState } from 'react'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { playNotificationSound } from '../../utils/sound'
import {
  hasBeenNotified,
  markNotified,
  getSubmittedFeedback,
  setSubmittedFeedback as persistSubmittedFeedback,
} from '../../utils/notificationDedup'

export interface BlockingHumanFeedbackEvent {
  question?: string
  allow_feedback?: boolean
  context?: string
  session_id?: string
  workflow_id?: string
  request_id?: string
  yes_no_only?: boolean
  yes_label?: string
  no_label?: string
  options?: string[] // Array of option labels for multiple choice
}

interface BlockingHumanFeedbackDisplayProps {
  event: {
    type: string
    data: BlockingHumanFeedbackEvent
    timestamp: string
  }
  onApprove: (requestId: string, eventData?: BlockingHumanFeedbackEvent & { feedback?: string }) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean  // Loading state
}

export const BlockingHumanFeedbackDisplay: React.FC<BlockingHumanFeedbackDisplayProps> = ({
  event,
  onApprove,
  onSubmitFeedback,
  onFeedbackSubmitted,
  isApproving = false
}) => {
  const cachedValue = event.data.request_id ? getSubmittedFeedback(event.data.request_id) : undefined
  const [feedback, setFeedback] = useState<string>('')
  const [isSubmittingFeedback, setIsSubmittingFeedback] = useState(false)
  const [hasSubmitted, setHasSubmitted] = useState(!!cachedValue)
  const [submittedFeedback, setSubmittedFeedback] = useState<string>(cachedValue || '')
  const [notificationPermission, setNotificationPermission] = useState<NotificationPermission>('default')

  // Use backend-provided content directly
  const question = event.data.question || 'Do you want to continue?'
  const context = event.data.context || ''
  const yesNoOnly = event.data.yes_no_only || false
  const options = event.data.options || []
  const hasMultipleOptions = options.length > 0
  const yesLabel = event.data.yes_label || 'Approve'
  const noLabel = event.data.no_label || ''

  // Request notification permission on component mount
  React.useEffect(() => {
    if ('Notification' in window) {
      setNotificationPermission(Notification.permission)

      if (Notification.permission === 'default') {
        Notification.requestPermission().then((permission) => {
          setNotificationPermission(permission)
        }).catch((error) => {
          console.error('[BLOCKING_FEEDBACK] Permission request failed:', error)
        })
      }
    }

    // Set Dock badge if running in Electron
    if (!hasSubmitted && (window as any).electronAPI) {
      (window as any).electronAPI.setDockBadge('1')
    }

    return () => {
      // Clear dock badge on unmount
      if ((window as any).electronAPI) {
        (window as any).electronAPI.setDockBadge('')
      }
    }
  }, [])

  // Clear dock badge when submitted
  React.useEffect(() => {
    if (hasSubmitted && (window as any).electronAPI) {
      (window as any).electronAPI.setDockBadge('')
    }
  }, [hasSubmitted])

  // Show browser notification when component mounts (if not already submitted or notified)
  React.useEffect(() => {
    const requestId = event.data.request_id || ''
    const enabled = localStorage.getItem('mcp_notifications_enabled') !== 'false'
    // Skip if already submitted, already notified (e.g. page refresh), or notifications disabled
    if (!enabled || hasSubmitted || (requestId && hasBeenNotified(requestId))) return
    if (!('Notification' in window) || Notification.permission !== 'granted') return

    try {
      playNotificationSound()
      if (requestId) markNotified(requestId)

      const notification = new Notification('Action Required', {
        body: question,
        icon: '/favicon.ico',
        tag: `blocking-feedback-${requestId || Date.now()}`,
        requireInteraction: true,
        silent: false
      })

      notification.onclick = () => {
        window.focus()
        notification.close()
      }

      notification.onerror = (error) => {
        console.error('[BLOCKING_FEEDBACK] Notification error:', error)
      }

      // Auto-close notification after 30 seconds
      setTimeout(() => {
        notification.close()
      }, 30000)

      return () => {
        notification.close()
      }
    } catch (error) {
      console.error('[BLOCKING_FEEDBACK] Failed to create notification:', error)
    }
  }, [question, event.data.request_id, hasSubmitted])

  const triggerScrollCallback = () => {
    if (onFeedbackSubmitted) {
      setTimeout(() => onFeedbackSubmitted(), 100)
    }
  }

  const handleSubmitFeedback = async () => {
    if (event.data.request_id && feedback.trim() && onSubmitFeedback) {
      setIsSubmittingFeedback(true)
      try {
        await onSubmitFeedback(event.data.request_id, feedback.trim())
        if (event.data.request_id) persistSubmittedFeedback(event.data.request_id, feedback.trim())
        setSubmittedFeedback(feedback.trim())
        setHasSubmitted(true)
        setFeedback('')
        triggerScrollCallback()
      } catch (error) {
        console.error('Failed to submit feedback:', error)
      } finally {
        setIsSubmittingFeedback(false)
      }
    }
  }

  const handleApprove = async () => {
    if (event.data.request_id) {
      setIsSubmittingFeedback(true)
      try {
        if (onSubmitFeedback) {
          await onSubmitFeedback(event.data.request_id, "Approve")
        } else {
          onApprove(event.data.request_id, { ...event.data, feedback: "Approve" })
        }
        if (event.data.request_id) persistSubmittedFeedback(event.data.request_id, yesLabel || "Approved")
        setSubmittedFeedback(yesLabel || "Approved")
        setHasSubmitted(true)
        triggerScrollCallback()
      } catch (error) {
        console.error('Failed to approve:', error)
      } finally {
        setIsSubmittingFeedback(false)
      }
    }
  }

  const handleReject = async () => {
    if (event.data.request_id) {
      setIsSubmittingFeedback(true)
      try {
        if (onSubmitFeedback) {
          await onSubmitFeedback(event.data.request_id, "Reject")
        } else {
          onApprove(event.data.request_id, { ...event.data, feedback: "Reject" })
        }
        if (event.data.request_id) persistSubmittedFeedback(event.data.request_id, "Reject")
        setSubmittedFeedback("Reject")
        setHasSubmitted(true)
        triggerScrollCallback()
      } catch (error) {
        console.error('Failed to reject:', error)
      } finally {
        setIsSubmittingFeedback(false)
      }
    }
  }

  const handleOption = async (index: number) => {
    if (event.data.request_id && onSubmitFeedback) {
      setIsSubmittingFeedback(true)
      try {
        // Send the actual option label text so the LLM clearly understands the user's choice
        const optionLabel = options[index] || `option${index}`
        await onSubmitFeedback(event.data.request_id, optionLabel)
        if (event.data.request_id) persistSubmittedFeedback(event.data.request_id, optionLabel)
        setSubmittedFeedback(optionLabel)
        setHasSubmitted(true)
        triggerScrollCallback()
      } catch (error) {
        console.error(`Failed to select option ${index}:`, error)
      } finally {
        setIsSubmittingFeedback(false)
      }
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!isSubmittingFeedback && !hasSubmitted && feedback.trim()) {
        handleSubmitFeedback()
      }
    }
  }

  // Submitted state — show question + answer
  if (hasSubmitted) {
    return (
      <div className="bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800 rounded-md px-3 py-2 my-2">
        {/* Question */}
        <div className="text-xs text-green-700 dark:text-green-300 mb-1.5">
          <MarkdownRenderer content={question} className="text-xs" />
        </div>
        {/* Answer */}
        <div className="flex items-center gap-2">
          <svg className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
          <span className="text-xs font-medium text-green-800 dark:text-green-200">
            {submittedFeedback}
          </span>
        </div>
        {context && (
          <details className="mt-2 group">
            <summary className="text-[10px] text-green-600 dark:text-green-400 cursor-pointer font-medium flex items-center gap-1">
              <span className="group-open:hidden">+ Show details</span>
              <span className="hidden group-open:inline">− Hide details</span>
            </summary>
            <div className="mt-1.5 p-3 bg-white dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded max-h-[500px] overflow-y-auto">
              <MarkdownRenderer content={context} className="text-xs" />
            </div>
          </details>
        )}
      </div>
    )
  }

  // Waiting state
  return (
    <div className="bg-indigo-50 dark:bg-indigo-950/30 border border-indigo-200 dark:border-indigo-800/60 rounded-md px-3 py-2.5 my-2">
      {/* Question text */}
      <div className="text-xs text-indigo-700 dark:text-indigo-300">
        <MarkdownRenderer content={question} className="text-xs" />
      </div>

      {/* Action Buttons */}
      <div className="flex flex-wrap items-center gap-2 mt-2">
          {hasMultipleOptions ? (
            // Multiple-choice mode
            options.map((optionLabel, index) => {
              const colorClasses = [
                'bg-blue-600 hover:bg-blue-700 dark:bg-blue-700 dark:hover:bg-blue-600 disabled:opacity-50',
                'bg-indigo-600 hover:bg-indigo-700 dark:bg-indigo-700 dark:hover:bg-indigo-600 disabled:opacity-50',
                'bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-600 disabled:opacity-50',
                'bg-purple-600 hover:bg-purple-700 dark:bg-purple-700 dark:hover:bg-purple-600 disabled:opacity-50',
              ]
              const colorClass = colorClasses[index % colorClasses.length]

              return (
                <button
                  key={index}
                  onClick={() => handleOption(index)}
                  disabled={isApproving || isSubmittingFeedback}
                  className={`px-3 py-1.5 ${colorClass} text-white text-xs font-medium rounded transition-colors`}
                >
                  {isSubmittingFeedback ? 'Processing...' : optionLabel}
                </button>
              )
            })
          ) : yesNoOnly ? (
            // Yes/No mode — hide reject button when noLabel is empty
            <>
              {noLabel && (
                <button
                  onClick={handleReject}
                  disabled={isApproving || isSubmittingFeedback}
                  className="px-3 py-1.5 bg-gray-200 hover:bg-gray-300 dark:bg-gray-700 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-200 text-xs font-medium rounded transition-colors disabled:opacity-50"
                >
                  {isSubmittingFeedback ? 'Processing...' : noLabel}
                </button>
              )}
              <button
                onClick={handleApprove}
                disabled={isApproving || isSubmittingFeedback}
                className="px-3 py-1.5 bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-600 text-white text-xs font-medium rounded transition-colors disabled:opacity-50"
              >
                {isApproving || isSubmittingFeedback ? 'Processing...' : yesLabel}
              </button>
            </>
          ) : null}
      </div>

      {/* Feedback textarea + buttons — below the main row, only in normal mode */}
      {!yesNoOnly && !hasMultipleOptions && (
        <div className="mt-2">
          <textarea
            id="feedback-input"
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type feedback here if changes are needed... (Enter to submit)"
            className="w-full px-2.5 py-1.5 text-xs border border-gray-200 dark:border-gray-700 rounded bg-white dark:bg-gray-800/80 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:ring-1 focus:ring-indigo-500 focus:border-indigo-500 resize-none"
            rows={5}
            disabled={isApproving || isSubmittingFeedback}
          />
          <div className="flex justify-end gap-2 mt-1.5">
            {!feedback.trim() && (
              <button
                onClick={handleApprove}
                disabled={isApproving || isSubmittingFeedback}
                className="px-3 py-1.5 bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-600 text-white text-xs font-medium rounded transition-colors disabled:opacity-50"
              >
                {isApproving ? 'Processing...' : yesLabel || 'Approve & Continue'}
              </button>
            )}
            {feedback.trim() && (
              <button
                onClick={handleSubmitFeedback}
                disabled={isSubmittingFeedback || isApproving || !feedback.trim()}
                className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-700 dark:bg-indigo-700 dark:hover:bg-indigo-600 text-white text-xs font-medium rounded transition-colors disabled:opacity-50"
              >
                {isSubmittingFeedback ? 'Submitting...' : 'Submit Feedback'}
              </button>
            )}
          </div>
        </div>
      )}

      {/* Context / Plan content — collapsible toggle */}
      {context && (
        <details className="mt-2 group">
          <summary className="text-[10px] text-indigo-600 dark:text-indigo-400 cursor-pointer font-medium flex items-center gap-1">
            <span className="group-open:hidden">+ Show details</span>
            <span className="hidden group-open:inline">− Hide details</span>
          </summary>
          <div className="mt-1.5 p-3 bg-white dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded max-h-[500px] overflow-y-auto">
            <MarkdownRenderer content={context} className="text-xs" />
          </div>
        </details>
      )}
    </div>
  )
}
