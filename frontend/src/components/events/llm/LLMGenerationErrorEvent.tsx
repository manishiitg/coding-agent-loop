import React from 'react'
import { Hash, Zap, AlertCircle, Clock } from 'lucide-react'
import type { LLMGenerationErrorEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'

interface LLMGenerationErrorEventProps {
  event: LLMGenerationErrorEvent
  mode?: 'compact' | 'detailed'
}

function getErrorSummary(error: string): string {
  const trimmed = error.trim()
  if (!trimmed) return trimmed

  const codexPrefix = /^codex cli execution failed:\s*/i
  if (codexPrefix.test(trimmed)) {
    const remainder = trimmed.replace(codexPrefix, '').trim()
    if (remainder && !/^exit status \d+$/i.test(remainder)) return remainder
  }

  const retryCodexPrefix = /^retry:\s*codex cli execution failed:\s*/i
  if (retryCodexPrefix.test(trimmed)) {
    const remainder = trimmed.replace(retryCodexPrefix, '').trim()
    if (remainder && !/^exit status \d+$/i.test(remainder)) return remainder
  }

  const jsonStart = trimmed.indexOf('{')
  if (jsonStart >= 0) {
    try {
      const parsed = JSON.parse(trimmed.slice(jsonStart)) as {
        metadata?: { raw?: string }
        error?: string
        message?: string
      }
      if (parsed.metadata?.raw?.trim()) return parsed.metadata.raw.trim()
      if (parsed.error?.trim()) return parsed.error.trim()
      if (parsed.message?.trim()) return parsed.message.trim()
    } catch {
      // Fall through to regex-based extraction for non-JSON error strings.
    }
  }

  const rawMatch = trimmed.match(/"raw"\s*:\s*"((?:[^"\\]|\\.)*)"/)
  if (rawMatch?.[1]) {
    try {
      return JSON.parse(`"${rawMatch[1]}"`)
    } catch {
      return rawMatch[1]
    }
  }

  const msgMatch = trimmed.match(/"message"\s*:\s*"((?:[^"\\]|\\.)*)"/)
  if (msgMatch?.[1]) return msgMatch[1]

  return trimmed
}

export const LLMGenerationErrorEventDisplay: React.FC<LLMGenerationErrorEventProps> = ({ event, mode = 'compact' }) => {
  const errorDisplay = event.error ? getErrorSummary(event.error) : null

  if (mode === 'compact') {
    return (
      <div className="p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
        <div className="text-xs text-red-700 dark:text-red-300 flex items-center gap-2">
          <AlertCircle className="w-3 h-3 text-red-600" />
          <span className="font-medium">LLM Generation Error</span>
          {event.turn && <span className="text-red-600 dark:text-red-400">• Turn {event.turn}</span>}
          {event.model_id && <span className="text-red-600 dark:text-red-400">• {event.model_id}</span>}
          {event.duration && <span className="text-red-600 dark:text-red-400">• {formatDuration(event.duration)}</span>}
          {errorDisplay && <span className="text-red-600 dark:text-red-400">• {errorDisplay}</span>}
        </div>
      </div>
    )
  }

  return (
    <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
      <div className="text-xs text-red-700 dark:text-red-300 space-y-1">
        {/* Header */}
        <div className="flex items-center gap-2">
          <AlertCircle className="w-4 h-4 text-red-600" />
          <span className="font-medium">LLM Generation Error</span>
        </div>
        
        {/* Turn information */}
        {event.turn && (
          <div className="flex items-center gap-2">
            <Hash className="w-3 h-3 text-red-600" />
            <span>Turn: {event.turn}</span>
          </div>
        )}
        
        {/* Model information */}
        {event.model_id && (
          <div className="flex items-center gap-2">
            <Zap className="w-3 h-3 text-red-600" />
            <span>Model: {event.model_id}</span>
          </div>
        )}
        
        {/* Error message */}
        {errorDisplay && (
          <div className="bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded-md p-2">
            <div className="font-medium">Error:</div>
            <div className="mt-1 text-red-800 dark:text-red-200 whitespace-pre-wrap break-words">{errorDisplay}</div>
          </div>
        )}
        
        {/* Duration */}
        {event.duration && (
          <div className="flex items-center gap-2">
            <Clock className="w-3 h-3 text-red-600" />
            <span>Duration: {formatDuration(event.duration)}</span>
          </div>
        )}
        
        {/* Optional metadata fields */}
        {event.timestamp && <div><strong>Timestamp:</strong> {new Date(event.timestamp).toLocaleString()}</div>}
        {event.trace_id && <div><strong>Trace ID:</strong> <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded">{event.trace_id}</code></div>}
        {event.correlation_id && <div><strong>Correlation ID:</strong> <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded">{event.correlation_id}</code></div>}
      </div>
    </div>
  )
}
