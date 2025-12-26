import React from 'react'
import { XCircle } from 'lucide-react'
import type { ContextEditingErrorEvent } from '../../../generated/events'

interface ContextEditingErrorEventDisplayProps {
  event: ContextEditingErrorEvent
  compact?: boolean
}

export const ContextEditingErrorEventDisplay: React.FC<ContextEditingErrorEventDisplayProps> = ({
  event,
  compact = false
}) => {
  if (compact) {
    return (
      <div className="p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
        <div className="text-xs text-red-700 dark:text-red-300 flex items-center gap-2">
          <XCircle className="w-3 h-3 text-red-600" />
          <span className="font-medium">Context Editing Error</span>
          {event.error && (
            <span className="text-red-600 dark:text-red-400">
              • {event.error.substring(0, 40)}...
            </span>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-700 rounded-md">
      <div className="text-xs text-red-700 dark:text-red-300 space-y-1.5">
        {/* Header */}
        <div className="flex items-center gap-2">
          <XCircle className="w-3 h-3 text-red-600" />
          <span className="font-medium">Context Editing Error</span>
        </div>

        {/* Error Message */}
        {event.error && (
          <div className="bg-white dark:bg-gray-800 border border-red-200 dark:border-red-800 rounded p-1.5">
            <pre className="text-xs whitespace-pre-wrap text-red-700 dark:text-red-300">
              {event.error}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}

