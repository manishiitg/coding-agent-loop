import React from 'react'
import { Loader2 } from 'lucide-react'
import type { ContextSummarizationStartedEvent } from '../../../generated/events'

interface ContextSummarizationStartedEventDisplayProps {
  event: ContextSummarizationStartedEvent
  compact?: boolean
}

export const ContextSummarizationStartedEventDisplay: React.FC<ContextSummarizationStartedEventDisplayProps> = ({
  event,
  compact = false
}) => {
  return (
    <div className="p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
      <div className="text-xs text-blue-700 dark:text-blue-300 flex items-center gap-2">
        <Loader2 className="w-3 h-3 text-blue-600 animate-spin" />
        <span className="font-medium">Context Summarization Started</span>
      </div>
    </div>
  )
}

