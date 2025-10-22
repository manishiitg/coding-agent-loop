import React, { useState } from 'react'

interface BlockingHumanFeedbackEvent {
  question?: string
  allow_feedback?: boolean
  context?: string
  session_id?: string
  workflow_id?: string
  request_id?: string
}

interface BlockingHumanFeedbackDisplayProps {
  event: {
    type: string
    data: BlockingHumanFeedbackEvent
    timestamp: string
  }
  onApprove: (requestId: string, eventData?: BlockingHumanFeedbackEvent & { feedback?: string }) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  isApproving?: boolean  // Loading state
}

export const BlockingHumanFeedbackDisplay: React.FC<BlockingHumanFeedbackDisplayProps> = ({
  event,
  onApprove,
  onSubmitFeedback,
  isApproving = false
}) => {
  const [feedback, setFeedback] = useState<string>('')
  const [isSubmittingFeedback, setIsSubmittingFeedback] = useState(false)

  // Use backend-provided content directly
  const question = event.data.question || 'Do you want to continue?'
  const context = event.data.context || ''

  const handleSubmitFeedback = async () => {
    if (event.data.request_id && feedback.trim() && onSubmitFeedback) {
      setIsSubmittingFeedback(true)
      try {
        await onSubmitFeedback(event.data.request_id, feedback.trim())
        setFeedback('') // Clear feedback after submission
      } catch (error) {
        console.error('Failed to submit feedback:', error)
      } finally {
        setIsSubmittingFeedback(false)
      }
    }
  }

  const handleApprove = async () => {
    if (event.data.request_id) {
      // Submit "Approve" as feedback
      if (onSubmitFeedback) {
        await onSubmitFeedback(event.data.request_id, "Approve")
      }
      // Then proceed with approval
      await onApprove(event.data.request_id, { 
        ...event.data, 
        feedback: "Approve"
      })
    }
  }

  return (
    <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md p-4 my-3">
      <div className="flex items-start gap-3">
        <div className="flex-shrink-0 w-8 h-8 bg-yellow-100 dark:bg-yellow-800 rounded-full flex items-center justify-center">
          <svg className="w-5 h-5 text-yellow-600 dark:text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L3.732 16.5c-.77.833.192 2.5 1.732 2.5z" />
          </svg>
        </div>
        <div className="flex-1">
          <h3 className="text-sm font-semibold text-yellow-900 dark:text-yellow-100 mb-2">
            Human Feedback Required
          </h3>
          
          <p className="text-xs text-yellow-700 dark:text-yellow-300 mb-3">
            {question}
          </p>

          {/* Context Information */}
          {context && (
            <div className="mb-4 p-3 bg-gray-50 dark:bg-gray-800 rounded border">
              <h4 className="text-xs font-medium text-gray-900 dark:text-gray-100 mb-2">
                Context:
              </h4>
              <div className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                {context}
              </div>
            </div>
          )}
          
          {/* Feedback Input */}
          <div className="mb-4">
            <label htmlFor="feedback-input" className="block text-xs font-medium text-yellow-900 dark:text-yellow-100 mb-1">
              Your feedback:
            </label>
            <textarea
              id="feedback-input"
              value={feedback}
              onChange={(e) => setFeedback(e.target.value)}
              placeholder="Type your feedback here... (e.g., 'Approve', 'Looks good', 'Please fix the validation issue', etc.)"
              className="w-full px-3 py-2 text-xs border border-yellow-200 dark:border-yellow-700 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:ring-2 focus:ring-yellow-500 focus:border-yellow-500 resize-none"
              rows={4}
              disabled={isApproving || isSubmittingFeedback}
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Type "Approve" or positive feedback to continue, or describe any issues to stop execution.
            </p>
          </div>

          {/* Action Buttons */}
          <div className="flex justify-end gap-2">
            <button
              onClick={handleApprove}
              disabled={isApproving || isSubmittingFeedback}
              className="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:bg-green-400 text-white text-xs font-medium rounded transition-colors"
            >
              {isApproving ? '⏳ Processing...' : '✅ Approve & Continue'}
            </button>
            {feedback.trim() && (
              <button
                onClick={handleSubmitFeedback}
                disabled={isSubmittingFeedback || isApproving || !feedback.trim()}
                className="px-4 py-2 bg-yellow-600 hover:bg-yellow-700 disabled:bg-yellow-400 text-white text-xs font-medium rounded transition-colors"
              >
                {isSubmittingFeedback ? '⏳ Submitting...' : '📝 Submit Feedback'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
