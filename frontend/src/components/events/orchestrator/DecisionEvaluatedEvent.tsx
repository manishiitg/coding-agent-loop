import React from 'react'

interface DecisionResponse {
  result?: boolean
  reasoning?: string
}

interface DecisionEvaluatedEventData {
  step_id?: string
  step_index?: number
  step_title?: string
  step_path?: string
  decision_question?: string
  decision_response?: DecisionResponse
  run_folder?: string
  workspace_path?: string
}

interface DecisionEvaluatedEventDisplayProps {
  event: DecisionEvaluatedEventData
  compact?: boolean
}

export const DecisionEvaluatedEventDisplay: React.FC<DecisionEvaluatedEventDisplayProps> = ({ 
  event, 
  compact = false 
}) => {
  const result = event.decision_response?.result
  const reasoning = event.decision_response?.reasoning

  return (
    <div className={`bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} text-purple-700 dark:text-purple-300`}>
        <div className="font-medium flex items-center gap-2">
          <span>🎯 Decision Evaluated</span>
          {result !== undefined && (
            <span className={`px-2 py-0.5 rounded text-xs font-semibold ${
              result 
                ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' 
                : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
            }`}>
              {result ? 'TRUE' : 'FALSE'}
            </span>
          )}
        </div>
        
        {event.step_title && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-purple-600 dark:text-purple-400 mt-1`}>
            Step: {event.step_title}
          </div>
        )}
        
        {event.step_id && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-purple-500 dark:text-purple-500 mt-1`}>
            ID: {event.step_id}
          </div>
        )}

        {event.decision_question && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-purple-600 dark:text-purple-400 mt-2`}>
            <div className="font-medium">Question:</div>
            <div className="mt-0.5">{event.decision_question}</div>
          </div>
        )}

        {reasoning && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-purple-600 dark:text-purple-400 mt-2`}>
            <div className="font-medium">Reasoning:</div>
            <div className="mt-0.5 whitespace-pre-wrap">{reasoning}</div>
          </div>
        )}
      </div>
    </div>
  )
}

