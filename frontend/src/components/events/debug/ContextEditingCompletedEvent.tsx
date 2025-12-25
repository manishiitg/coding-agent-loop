import React from 'react'
import { FileText, Zap, Search } from 'lucide-react'
import { OrchestratorContext } from '../common/OrchestratorContext'
import type { ContextEditingCompletedEvent } from '../../../generated/events'

interface ContextEditingCompletedEventDisplayProps {
  event: ContextEditingCompletedEvent
  compact?: boolean
}

export const ContextEditingCompletedEventDisplay: React.FC<ContextEditingCompletedEventDisplayProps> = ({
  event,
  compact = false
}) => {
  const totalMessages = event.total_messages ?? 0
  const toolResponseCount = event.tool_response_count ?? 0
  const compactedCount = event.compacted_count ?? 0
  const totalTokensSaved = event.total_tokens_saved ?? 0
  const tokenThreshold = event.token_threshold ?? 0
  const turnThreshold = event.turn_threshold ?? 0
  const currentTurn = event.current_turn ?? 0
  const alreadyCompactedCount = event.already_compacted_count ?? 0
  const evaluations = event.evaluations || []

  const hasCompaction = compactedCount > 0 || totalTokensSaved > 0
  const wasJustChecking = compactedCount === 0 && totalTokensSaved === 0

  // Don't display anything when no compaction was needed
  if (wasJustChecking) {
    return null
  }

  if (compact) {
    return (
      <div className={`p-2 border rounded-md ${
        hasCompaction 
          ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' 
          : 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-800'
      }`}>
        <div className={`text-xs flex items-center gap-2 ${
          hasCompaction 
            ? 'text-green-700 dark:text-green-300' 
            : 'text-blue-700 dark:text-blue-300'
        }`}>
          {hasCompaction ? (
            <>
              <Zap className="w-3 h-3 text-green-600" />
              <span className="font-medium">Context Editing: {compactedCount} Compacted</span>
              <span className="text-green-600 dark:text-green-400">
                • {totalTokensSaved.toLocaleString()} tokens saved
              </span>
            </>
          ) : (
            <>
              <Search className="w-3 h-3 text-blue-600" />
              <span className="font-medium">Context Editing: Checked (No Compaction Needed)</span>
            </>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className={`p-3 border rounded-lg ${
      hasCompaction 
        ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-700' 
        : 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-700'
    }`}>
      <div className={`text-xs space-y-3 ${
        hasCompaction 
          ? 'text-green-700 dark:text-green-300' 
          : 'text-blue-700 dark:text-blue-300'
      }`}>
        {/* Header */}
        <div className="flex items-center gap-2">
          {hasCompaction ? (
            <Zap className="w-4 h-4 text-green-600" />
          ) : (
            <Search className="w-4 h-4 text-blue-600" />
          )}
          <span className="font-medium">Context Editing Completed</span>
        </div>

        {/* Status Banner - Clear indication of compaction vs checking */}
        <div className={`p-2 rounded-md border ${
          hasCompaction
            ? 'bg-green-100 dark:bg-green-900/30 border-green-300 dark:border-green-700'
            : 'bg-blue-100 dark:bg-blue-900/30 border-blue-300 dark:border-blue-700'
        }`}>
          {hasCompaction ? (
            <div className="flex items-center gap-2">
              <Zap className="w-4 h-4 text-green-600" />
              <div>
                <div className="font-semibold text-green-800 dark:text-green-200">
                  ✓ Compaction Performed
                </div>
                <div className="text-green-700 dark:text-green-300 text-xs mt-0.5">
                  {compactedCount} tool response{compactedCount !== 1 ? 's' : ''} compacted, saving {totalTokensSaved.toLocaleString()} tokens
                </div>
              </div>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <Search className="w-4 h-4 text-blue-600" />
              <div>
                <div className="font-semibold text-blue-800 dark:text-blue-200">
                  ✓ Checked - No Compaction Needed
                </div>
                <div className="text-blue-700 dark:text-blue-300 text-xs mt-0.5">
                  Evaluated {toolResponseCount} tool response{toolResponseCount !== 1 ? 's' : ''}, but none met the compaction criteria
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Orchestrator Context */}
        {event.metadata && <OrchestratorContext metadata={event.metadata} />}

        {/* Summary Statistics */}
        <div className={`grid grid-cols-2 gap-2 bg-white dark:bg-gray-800 border rounded-md p-2 ${
          hasCompaction 
            ? 'border-green-200 dark:border-green-800' 
            : 'border-blue-200 dark:border-blue-800'
        }`}>
          <div>
            <span className="font-medium">Total Messages:</span>
            <span className="ml-2">{totalMessages}</span>
          </div>
          <div>
            <span className="font-medium">Tool Responses:</span>
            <span className="ml-2">{toolResponseCount}</span>
          </div>
          <div>
            <span className="font-medium">Compacted:</span>
            <span className={`ml-2 ${compactedCount > 0 ? 'text-green-600 dark:text-green-400 font-semibold' : ''}`}>
              {compactedCount}
            </span>
          </div>
          <div>
            <span className="font-medium">Already Compacted:</span>
            <span className="ml-2">{alreadyCompactedCount}</span>
          </div>
        </div>

        {/* Token Savings - Only show if compaction occurred */}
        {hasCompaction && (
          <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md p-2">
            <div className="flex items-center gap-2 mb-1">
              <Zap className="w-3 h-3 text-green-600" />
              <span className="font-medium text-green-700 dark:text-green-300">Token Savings</span>
            </div>
            <div className="text-green-600 dark:text-green-400 font-semibold">
              {totalTokensSaved.toLocaleString()} tokens saved
            </div>
          </div>
        )}

        {/* No Compaction Message - Show when just checking */}
        {wasJustChecking && toolResponseCount > 0 && (
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-2">
            <div className="flex items-center gap-2 mb-1">
              <Search className="w-3 h-3 text-blue-600" />
              <span className="font-medium text-blue-700 dark:text-blue-300">Evaluation Results</span>
            </div>
            <div className="text-blue-600 dark:text-blue-400 text-xs">
              All tool responses were below the token threshold ({tokenThreshold.toLocaleString()}) or did not meet the turn age requirement ({turnThreshold} turns)
            </div>
          </div>
        )}

        {/* Thresholds and Current Turn */}
        <div className="grid grid-cols-3 gap-2 text-xs">
          <div>
            <span className="font-medium">Token Threshold:</span>
            <span className="ml-2">{tokenThreshold.toLocaleString()}</span>
          </div>
          <div>
            <span className="font-medium">Turn Threshold:</span>
            <span className="ml-2">{turnThreshold}</span>
          </div>
          <div>
            <span className="font-medium">Current Turn:</span>
            <span className="ml-2">{currentTurn}</span>
          </div>
        </div>

        {/* Evaluations */}
        {evaluations.length > 0 && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <FileText className="w-3 h-3" />
              <span className="font-medium">Tool Response Evaluations ({evaluations.length})</span>
            </div>
            <div className="space-y-1 max-h-64 overflow-y-auto">
              {evaluations.map((evalItem, index) => (
                <div
                  key={index}
                  className={`p-2 rounded-md border text-xs ${
                    evalItem.was_compacted
                      ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
                      : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700'
                  }`}
                >
                  <div className="flex items-center justify-between mb-1">
                    <span className="font-medium">{evalItem.tool_name}</span>
                    {evalItem.was_compacted && (
                      <span className="text-green-600 dark:text-green-400 text-xs font-semibold">
                        ✓ Compacted
                      </span>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-1 text-xs text-gray-600 dark:text-gray-400">
                    <div>
                      <span>Tokens:</span>
                      <span className="ml-1">{(evalItem.token_count ?? 0).toLocaleString()}</span>
                    </div>
                    <div>
                      <span>Turn Age:</span>
                      <span className="ml-1">{evalItem.turn_age ?? 0}</span>
                    </div>
                    {evalItem.tokens_saved !== undefined && evalItem.tokens_saved > 0 && (
                      <div className="col-span-2 text-green-600 dark:text-green-400">
                        <span>Tokens Saved:</span>
                        <span className="ml-1 font-semibold">{evalItem.tokens_saved.toLocaleString()}</span>
                      </div>
                    )}
                    {evalItem.skip_reason && (
                      <div className="col-span-2 text-gray-500 dark:text-gray-500 italic">
                        {evalItem.skip_reason}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Optional metadata */}
        {event.timestamp && (
          <div className="text-xs text-gray-500 dark:text-gray-500">
            <span className="font-medium">Time:</span>
            <span className="ml-2">{new Date(event.timestamp).toLocaleString()}</span>
          </div>
        )}
      </div>
    </div>
  )
}

