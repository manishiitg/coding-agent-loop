import React, { useCallback, useState } from 'react'
import { Copy, Check } from 'lucide-react'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'

interface UnifiedCompletionEvent {
  timestamp?: string
  trace_id?: string
  span_id?: string
  event_id?: string
  parent_id?: string
  is_end_event?: boolean
  correlation_id?: string
  agent_type?: string
  agent_mode?: string
  question?: string
  final_result?: string
  status?: string
  duration?: number
  turns?: number
  error?: string
  metadata?: Record<string, unknown>
}

interface UnifiedCompletionEventDisplayProps {
  event: UnifiedCompletionEvent
}

const codingAgentProviders = new Set(['claude-code', 'codex-cli', 'gemini-cli', 'cursor-cli'])

const metadataBool = (value: unknown): boolean => value === true || value === 'true'

const shouldUseTerminalFormatting = (event: UnifiedCompletionEvent): boolean => {
  if (metadataBool(event.metadata?.coding_agent_terminal_format)) {
    return true
  }
  const provider = typeof event.metadata?.provider === 'string' ? event.metadata.provider : ''
  return codingAgentProviders.has(provider)
}

const TerminalCompletionRenderer: React.FC<{ content: string }> = ({ content }) => (
  <div className="overflow-x-auto max-w-full">
    <pre className="m-0 min-w-max whitespace-pre pr-8 font-mono text-xs leading-5 text-gray-800 dark:text-gray-200">
      {content}
    </pre>
  </div>
)

export const UnifiedCompletionEventDisplay: React.FC<UnifiedCompletionEventDisplayProps> = ({ event }) => {

  // Note: event.duration is in nanoseconds from Go time.Duration
  const formatDuration = (durationNs: number) => {
    if (!durationNs || durationNs <= 0) {
      return '0ms'
    }

    // Convert nanoseconds to milliseconds
    const durationMs = durationNs / 1000000

    if (durationMs < 1) {
      // Less than 1ms, show in microseconds
      const durationUs = durationNs / 1000
      return `${Math.round(durationUs)}μs`
    } else if (durationMs < 1000) {
      return `${Math.round(durationMs)}ms`
    } else if (durationMs < 60000) {
      return `${(durationMs / 1000).toFixed(1)}s`
    } else {
      return `${(durationMs / 60000).toFixed(1)}m`
    }
  }

  // Copy handler
  const [copied, setCopied] = useState(false)
  const handleCopy = useCallback(() => {
    if (!event.final_result) return
    navigator.clipboard.writeText(event.final_result).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [event.final_result])

  const isError = event.status === 'error' || event.error

  // Render restored step errors and other completions with content as normal
  // assistant messages. Otherwise an event with status=error and final_result
  // collapses to a generic "Error" box and hides the useful restored summary.
  if (event.final_result) {
    const useTerminalFormatting = shouldUseTerminalFormatting(event)
    // Detect JSON
    let isJSON = false
    let parsedJSON: unknown = null
    if (!useTerminalFormatting) {
      try {
        parsedJSON = JSON.parse(event.final_result)
        isJSON = true
      } catch {
        // not JSON
      }
    }

    return (
      <div className="flex items-start gap-2">
        <div className="flex-1 min-w-0">
          <div className="relative group rounded-md border border-gray-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-800/70">
            <div className="absolute top-1.5 right-1.5 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
              <button
                onClick={handleCopy}
                className="p-1 rounded text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                title={useTerminalFormatting ? 'Copy text' : 'Copy markdown'}
              >
                {copied ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
              </button>
            </div>
            {useTerminalFormatting ? (
              <TerminalCompletionRenderer content={event.final_result} />
            ) : isJSON ? (
              <pre className="text-xs text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap">
                {JSON.stringify(parsedJSON, null, 2)}
              </pre>
            ) : (
              <ConversationMarkdownRenderer content={event.final_result} maxHeight="none" framed={false} />
            )}
          </div>
          <div className="flex items-center gap-2 mt-1 px-1 text-[10px] text-gray-400 dark:text-gray-500">
            {event.duration && <span>{formatDuration(event.duration)}</span>}
            {event.turns && <span>{event.turns} turns</span>}
            {event.timestamp && <span>{new Date(event.timestamp).toLocaleTimeString()}</span>}
          </div>
        </div>
      </div>
    )
  }

  // Error case without final_result: keep the original compact error display
  if (isError) {
    return (
      <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-medium text-red-700 dark:text-red-300">
            Error
            <span className="text-xs font-normal text-red-600 dark:text-red-400 ml-2">
              {event.duration && `${formatDuration(event.duration)}`}
              {event.turns && ` | ${event.turns} turns`}
            </span>
          </div>
          {event.timestamp && (
            <div className="text-xs flex-shrink-0 text-red-600 dark:text-red-400">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>
        {event.error && (
          <div className="mt-2 bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded-md p-2">
            <div className="text-sm text-red-900 dark:text-red-100 whitespace-pre-wrap break-words">
              {event.error}
            </div>
          </div>
        )}
      </div>
    )
  }

  // No final_result: minimal completion indicator
  return (
    <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="text-xs text-gray-500 dark:text-gray-400">
          Completed
          {event.duration && ` in ${formatDuration(event.duration)}`}
          {event.turns && ` (${event.turns} turns)`}
        </div>
        {event.timestamp && (
          <div className="text-xs text-gray-400 dark:text-gray-500">
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>
    </div>
  )
}
