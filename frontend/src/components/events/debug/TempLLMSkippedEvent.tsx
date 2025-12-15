import React from 'react'

interface TempLLMSkippedEventData {
  timestamp?: string
  hierarchy_level?: number
  component?: string
  metadata?: {
    orchestrator_agent_name?: string
    orchestrator_phase?: string
    orchestrator_step?: number
  }
  step_id?: string
  step_index?: number
  step_title?: string
  step_path?: string
  is_branch_step?: boolean
  reason?: string
  temp_llm_provider?: string
  temp_llm_model?: string
  learnings_path?: string
  run_folder?: string
  workspace_path?: string
}

interface TempLLMSkippedEventDisplayProps {
  event: TempLLMSkippedEventData
  compact?: boolean
}

export const TempLLMSkippedEventDisplay: React.FC<TempLLMSkippedEventDisplayProps> = ({
  event,
  compact = false
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return ''
    return new Date(timestamp).toLocaleTimeString()
  }

  const getReasonDisplay = (reason?: string) => {
    if (!reason) return 'Unknown reason'
    // Format reason for display (e.g., "learnings_folder_empty" -> "Learnings folder empty")
    return reason
      .split('_')
      .map(word => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ')
  }

  const stepLabel = event.step_title || event.step_id || `Step ${(event.step_index ?? 0) + 1}`

  return (
    <div className={`${compact ? 'p-2' : 'p-3'} bg-slate-50 dark:bg-slate-800/40 border border-slate-200 dark:border-slate-700 rounded-md`}>
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3 mb-2">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <span className="text-lg flex-shrink-0">⏭️</span>
          <div className="min-w-0 flex-1">
            <div className={`${compact ? 'text-xs' : 'text-sm'} font-medium text-slate-700 dark:text-slate-300`}>
              Temp LLM Skipped{' '}
              <span className={`${compact ? 'text-[10px]' : 'text-xs'} font-normal text-slate-600 dark:text-slate-400`}>
                • {stepLabel}
                {event.step_path && ` • ${event.step_path}`}
                {event.metadata?.orchestrator_phase && ` • ${event.metadata.orchestrator_phase}`}
              </span>
            </div>
          </div>
        </div>
        
        {/* Right side: Time */}
        {event.timestamp && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-slate-600 dark:text-slate-400 flex-shrink-0`}>
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Reason */}
      {event.reason && (
        <div className={`${compact ? 'mb-1.5' : 'mb-2'}`}>
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} font-medium text-slate-700 dark:text-slate-300 mb-1`}>
            Reason:
          </div>
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-slate-600 dark:text-slate-400 bg-slate-100 dark:bg-slate-700/50 rounded px-2 py-1`}>
            {getReasonDisplay(event.reason)}
          </div>
        </div>
      )}

      {/* Temp LLM Details */}
      {(event.temp_llm_provider || event.temp_llm_model) && (
        <div className={`${compact ? 'mb-1.5' : 'mb-2'}`}>
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} font-medium text-slate-700 dark:text-slate-300 mb-1`}>
            Skipped Temp LLM:
          </div>
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-slate-600 dark:text-slate-400`}>
            {event.temp_llm_provider && <span>Provider: <span className="font-medium">{event.temp_llm_provider}</span></span>}
            {event.temp_llm_provider && event.temp_llm_model && <span className="mx-1">•</span>}
            {event.temp_llm_model && <span>Model: <span className="font-medium">{event.temp_llm_model}</span></span>}
          </div>
        </div>
      )}

      {/* Paths */}
      {(event.learnings_path || event.workspace_path) && (
        <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-slate-600 dark:text-slate-400 space-y-0.5`}>
          {event.learnings_path && (
            <div>
              <span className="font-medium">Learnings:</span> <span className="font-mono text-[10px]">{event.learnings_path}</span>
            </div>
          )}
          {event.workspace_path && (
            <div>
              <span className="font-medium">Workspace:</span> <span className="font-mono text-[10px]">{event.workspace_path}</span>
            </div>
          )}
          {event.run_folder && (
            <div>
              <span className="font-medium">Run:</span> <span className="font-mono text-[10px]">{event.run_folder}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

