import React, { useState } from 'react'
import { CheckCircle2, ChevronDown, ChevronUp, Info } from 'lucide-react'
import type { ContextEditingCompletedEvent, ToolResponseEvaluation } from '../../../generated/events'

interface ContextEditingCompletedEventDisplayProps {
  event: ContextEditingCompletedEvent
  compact?: boolean
}

export const ContextEditingCompletedEventDisplay: React.FC<ContextEditingCompletedEventDisplayProps> = ({
  event
}) => {
  const [isExpanded, setIsExpanded] = useState(false)
  
  const compactedCount = event.compacted_count ?? 0
  const totalTokensSaved = event.total_tokens_saved ?? 0
  const toolResponseCount = event.tool_response_count ?? 0
  const alreadyCompactedCount = event.already_compacted_count ?? 0
  const tokenThreshold = event.token_threshold ?? 0
  const turnThreshold = event.turn_threshold ?? 0
  const currentTurn = event.current_turn ?? 0
  const evaluations = event.evaluations ?? []
  const totalMessages = event.total_messages ?? 0

  const hasEvaluations = evaluations.length > 0
  const hasDetails = toolResponseCount > 0 || alreadyCompactedCount > 0 || hasEvaluations

  return (
    <div className="p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
      <div className="text-xs text-blue-700 dark:text-blue-300 space-y-1.5">
        {/* Header */}
        <div className="flex items-center gap-2">
          <CheckCircle2 className="w-3 h-3 text-blue-600" />
          <span className="font-medium">Context Editing Completed</span>
          {hasDetails && (
            <button
              onClick={() => setIsExpanded(!isExpanded)}
              className="ml-auto flex items-center gap-1 text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300"
            >
              {isExpanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
              <span className="text-xs">{isExpanded ? 'Hide' : 'Details'}</span>
            </button>
          )}
        </div>

        {/* Stats - Compact single line */}
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
          {toolResponseCount > 0 && (
            <span>
              <span className="font-semibold">Tool Responses:</span> {toolResponseCount}
            </span>
          )}
          {compactedCount > 0 && (
            <span>
              <span className="font-semibold">Compacted:</span> {compactedCount}
            </span>
          )}
          {alreadyCompactedCount > 0 && (
            <span>
              <span className="font-semibold">Already Compacted:</span> {alreadyCompactedCount}
            </span>
          )}
          {totalTokensSaved > 0 && (
            <span>
              <span className="font-semibold">Tokens Saved:</span> {totalTokensSaved.toLocaleString()}
            </span>
          )}
          {tokenThreshold > 0 && (
            <span>
              <span className="font-semibold">Token Threshold:</span> {tokenThreshold}
            </span>
          )}
          {turnThreshold > 0 && (
            <span>
              <span className="font-semibold">Turn Threshold:</span> {turnThreshold}
            </span>
          )}
          {currentTurn > 0 && (
            <span>
              <span className="font-semibold">Current Turn:</span> {currentTurn}
            </span>
          )}
        </div>

        {/* Expanded Details */}
        {isExpanded && hasEvaluations && (
          <div className="mt-2 pt-2 border-t border-blue-200 dark:border-blue-700 space-y-2">
            <div className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
              <Info className="w-3 h-3" />
              <span className="font-semibold">Tool Response Evaluations ({evaluations.length})</span>
            </div>
            <div className="space-y-1.5 max-h-64 overflow-y-auto">
              {evaluations.map((evaluation: ToolResponseEvaluation, idx: number) => (
                <div
                  key={idx}
                  className={`p-1.5 rounded text-xs ${
                    evaluation.was_compacted
                      ? 'bg-green-100 dark:bg-green-900/30 border border-green-300 dark:border-green-700'
                      : 'bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-700'
                  }`}
                >
                  <div className="font-semibold">{evaluation.tool_name || 'Unknown Tool'}</div>
                  <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 mt-1 text-xs">
                    <div>
                      <span className="text-gray-600 dark:text-gray-400">Tokens:</span>{' '}
                      <span className="font-medium">{evaluation.token_count ?? 0}</span>
                      {evaluation.meets_token_threshold !== undefined && (
                        <span className={evaluation.meets_token_threshold ? ' text-green-600' : ' text-red-600'}>
                          {' '}({evaluation.meets_token_threshold ? '✓' : '✗'})
                        </span>
                      )}
                    </div>
                    <div>
                      <span className="text-gray-600 dark:text-gray-400">Turn Age:</span>{' '}
                      <span className="font-medium">{evaluation.turn_age ?? 0}</span>
                      {evaluation.meets_turn_threshold !== undefined && (
                        <span className={evaluation.meets_turn_threshold ? ' text-green-600' : ' text-red-600'}>
                          {' '}({evaluation.meets_turn_threshold ? '✓' : '✗'})
                        </span>
                      )}
                    </div>
                    {evaluation.tokens_saved !== undefined && evaluation.tokens_saved > 0 && (
                      <div className="col-span-2">
                        <span className="text-gray-600 dark:text-gray-400">Tokens Saved:</span>{' '}
                        <span className="font-medium text-green-600">{evaluation.tokens_saved.toLocaleString()}</span>
                      </div>
                    )}
                    {evaluation.skip_reason && (
                      <div className="col-span-2 text-orange-600 dark:text-orange-400">
                        <span className="font-medium">Skip Reason:</span> {evaluation.skip_reason}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Summary when no evaluations but has counts */}
        {isExpanded && !hasEvaluations && (toolResponseCount > 0 || alreadyCompactedCount > 0) && (
          <div className="mt-2 pt-2 border-t border-blue-200 dark:border-blue-700">
            <div className="text-xs">
              <div>Total Messages: {totalMessages}</div>
              <div>Tool Responses Found: {toolResponseCount}</div>
              {alreadyCompactedCount > 0 && <div>Already Compacted: {alreadyCompactedCount}</div>}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

