import React, { useState } from 'react'
import { getSubmittedQuestions, setSubmittedQuestions } from '../../utils/notificationDedup'

export interface BlockingHumanQuestionsQuestion {
  id: string
  question: string
}

export interface BlockingHumanQuestionsEvent {
  request_id?: string
  questions?: BlockingHumanQuestionsQuestion[]
  session_id?: string
}

interface BlockingHumanQuestionsDisplayProps {
  event: {
    type: string
    data: BlockingHumanQuestionsEvent
    timestamp: string
  }
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
}

export const BlockingHumanQuestionsDisplay: React.FC<BlockingHumanQuestionsDisplayProps> = ({
  event,
  onSubmitFeedback,
  onFeedbackSubmitted
}) => {
  const questions = event.data.questions || []
  const cachedData = event.data.request_id ? getSubmittedQuestions(event.data.request_id) : undefined
  const [answers, setAnswers] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {}
    for (const q of questions) {
      initial[q.id] = ''
    }
    return initial
  })
  const [generalFeedback, setGeneralFeedback] = useState('')
  const [showGeneralFeedback, setShowGeneralFeedback] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [hasSubmitted, setHasSubmitted] = useState(!!cachedData)
  const [submittedData, setSubmittedData] = useState<{ answers: Record<string, string>; general_feedback: string } | null>(cachedData || null)

  const handleAnswerChange = (questionId: string, value: string) => {
    setAnswers(prev => ({ ...prev, [questionId]: value }))
  }

  const handleSubmit = async () => {
    if (!event.data.request_id || !onSubmitFeedback) return

    setIsSubmitting(true)
    try {
      const responseData = {
        answers,
        general_feedback: generalFeedback.trim()
      }
      const jsonString = JSON.stringify(responseData)
      await onSubmitFeedback(event.data.request_id, jsonString)
      if (event.data.request_id) setSubmittedQuestions(event.data.request_id, responseData)
      setSubmittedData(responseData)
      setHasSubmitted(true)
      if (onFeedbackSubmitted) {
        setTimeout(() => onFeedbackSubmitted(), 100)
      }
    } catch (error) {
      console.error('Failed to submit answers:', error)
    } finally {
      setIsSubmitting(false)
    }
  }

  // Submitted state — compact green confirmation
  if (hasSubmitted && submittedData) {
    const answeredCount = Object.values(submittedData.answers).filter(a => a.trim()).length
    return (
      <div className="bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800 rounded-md px-3 py-2 my-2">
        <div className="flex items-center gap-2">
          <svg className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
          <span className="text-xs font-medium text-green-800 dark:text-green-200">
            Answered {answeredCount}/{questions.length} questions
          </span>
          <span className="text-[10px] text-green-600 dark:text-green-400 italic ml-auto">
            Processing...
          </span>
        </div>
        <details className="mt-2 group">
          <summary className="text-[10px] text-green-600 dark:text-green-400 cursor-pointer font-medium flex items-center gap-1">
            <span className="group-open:hidden">+ Show answers</span>
            <span className="hidden group-open:inline">− Hide answers</span>
          </summary>
          <div className="mt-1.5 space-y-1.5">
            {questions.map((q, i) => {
              const answer = submittedData.answers[q.id]
              return (
                <div key={q.id} className="text-xs">
                  <span className="font-medium text-green-700 dark:text-green-300">{i + 1}. {q.question}</span>
                  <div className="text-green-600 dark:text-green-400 ml-3 mt-0.5">
                    {answer?.trim() ? answer : <span className="italic text-green-500 dark:text-green-500">Skipped</span>}
                  </div>
                </div>
              )
            })}
            {submittedData.general_feedback && (
              <div className="text-xs border-t border-green-200 dark:border-green-800 pt-1.5 mt-1.5">
                <span className="font-medium text-green-700 dark:text-green-300">General feedback:</span>
                <div className="text-green-600 dark:text-green-400 ml-3 mt-0.5">{submittedData.general_feedback}</div>
              </div>
            )}
          </div>
        </details>
      </div>
    )
  }

  // Waiting state — indigo card with question fields
  return (
    <div className="bg-indigo-50 dark:bg-indigo-950/30 border border-indigo-200 dark:border-indigo-800/60 rounded-md px-3 py-2.5 my-2">
      <div className="flex items-center gap-2 mb-3">
        <div className="w-1.5 h-1.5 bg-indigo-500 rounded-full animate-pulse" />
        <span className="text-xs font-medium text-indigo-700 dark:text-indigo-300">
          Please answer the following questions
        </span>
      </div>

      <div className="space-y-3">
        {questions.map((q, i) => (
          <div key={q.id}>
            <label className="block text-xs font-medium text-indigo-700 dark:text-indigo-300 mb-1">
              {i + 1}. {q.question}
            </label>
            <textarea
              value={answers[q.id] || ''}
              onChange={(e) => handleAnswerChange(q.id, e.target.value)}
              placeholder="Type your answer..."
              className="w-full px-2.5 py-1.5 text-xs border border-gray-200 dark:border-gray-700 rounded bg-white dark:bg-gray-800/80 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:ring-1 focus:ring-indigo-500 focus:border-indigo-500 resize-none"
              rows={2}
              disabled={isSubmitting}
            />
          </div>
        ))}
      </div>

      {/* Collapsible General Feedback */}
      <div className="mt-3 border-t border-indigo-200 dark:border-indigo-800/60 pt-2">
        <button
          onClick={() => setShowGeneralFeedback(!showGeneralFeedback)}
          className="text-[10px] text-indigo-500 dark:text-indigo-400 font-medium flex items-center gap-1 hover:text-indigo-700 dark:hover:text-indigo-300 transition-colors"
        >
          <span>{showGeneralFeedback ? '−' : '+'}</span>
          <span>General feedback (optional)</span>
        </button>
        {showGeneralFeedback && (
          <textarea
            value={generalFeedback}
            onChange={(e) => setGeneralFeedback(e.target.value)}
            placeholder="Any additional feedback or context..."
            className="w-full mt-1.5 px-2.5 py-1.5 text-xs border border-gray-200 dark:border-gray-700 rounded bg-white dark:bg-gray-800/80 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:ring-1 focus:ring-indigo-500 focus:border-indigo-500 resize-none"
            rows={3}
            disabled={isSubmitting}
          />
        )}
      </div>

      {/* Submit Button */}
      <div className="flex justify-end mt-2">
        <button
          onClick={handleSubmit}
          disabled={isSubmitting}
          className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-700 dark:bg-indigo-700 dark:hover:bg-indigo-600 text-white text-xs font-medium rounded transition-colors disabled:opacity-50"
        >
          {isSubmitting ? 'Submitting...' : 'Submit Answers'}
        </button>
      </div>
    </div>
  )
}
