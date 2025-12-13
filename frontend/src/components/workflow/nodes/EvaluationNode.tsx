import { memo, useState } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Brain, Info, X } from 'lucide-react'

export interface EvaluationNodeData extends Record<string, unknown> {
  id: string
  parentStepId: string
  parentStepTitle: string
  evaluationQuestion?: string
  status: 'pending' | 'running' | 'evaluated_true' | 'evaluated_false'
  llmProvider?: string
  llmModel?: string
}

interface EvaluationNodeProps {
  data: EvaluationNodeData
  selected?: boolean
}

const statusStyles = {
  pending: {
    border: 'border-gray-300 dark:border-gray-600',
    bg: 'bg-gray-50 dark:bg-gray-800/50',
    icon: 'text-gray-400'
  },
  running: {
    border: 'border-purple-400 dark:border-purple-500',
    bg: 'bg-purple-50 dark:bg-purple-900/30',
    icon: 'text-purple-500 animate-pulse'
  },
  evaluated_true: {
    border: 'border-green-400 dark:border-green-500',
    bg: 'bg-green-50 dark:bg-green-900/30',
    icon: 'text-green-500'
  },
  evaluated_false: {
    border: 'border-red-400 dark:border-red-500',
    bg: 'bg-red-50 dark:bg-red-900/30',
    icon: 'text-red-500'
  }
}

export const EvaluationNode = memo(({ data, selected }: EvaluationNodeProps) => {
  const { status } = data
  const styles = statusStyles[status]
  const [isExpanded, setIsExpanded] = useState(false)
  const showExpandIcon = data.llmModel && data.llmModel.length > 20

  return (
    <div className={`
      relative ${isExpanded ? 'w-[200px]' : 'w-[160px]'} rounded-lg border-2 ${styles.border} ${styles.bg}
      shadow-md transition-all
      ${selected ? 'ring-2 ring-purple-500/40' : ''}
    `}>
      {/* Input Handle */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-2.5 !h-2.5 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />

      {/* Content */}
      <div className="px-2.5 py-2">
        <div className="flex items-start gap-2">
          <Brain className={`w-4 h-4 ${styles.icon} flex-shrink-0 mt-0.5`} />
          <div className="flex-1 min-w-0">
            <div className="text-[10px] font-semibold text-gray-700 dark:text-gray-200 truncate">
              Evaluate
            </div>
            {data.evaluationQuestion && (
              <div className="text-[8px] text-gray-600 dark:text-gray-400 mt-0.5 line-clamp-2">
                {data.evaluationQuestion}
              </div>
            )}
            {data.llmProvider && data.llmModel ? (
              <>
                <div className="text-[8px] text-purple-600 dark:text-purple-400 font-medium truncate mt-0.5">
                  {data.llmProvider}
                </div>
                <div className="text-[8px] text-purple-500 dark:text-purple-300">
                  {isExpanded ? data.llmModel : (data.llmModel.length > 20 ? `${data.llmModel.slice(0, 20)}...` : data.llmModel)}
                </div>
              </>
            ) : (
              <div className="text-[8px] text-gray-500 dark:text-gray-400 mt-0.5">
                LLM evaluation
              </div>
            )}
          </div>
          {showExpandIcon && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                setIsExpanded(!isExpanded)
              }}
              className="flex-shrink-0 p-0.5 hover:bg-purple-100 dark:hover:bg-purple-900/50 rounded transition-colors"
              title={isExpanded ? 'Hide full model name' : 'Show full model name'}
            >
              {isExpanded ? (
                <X className="w-3 h-3 text-purple-600 dark:text-purple-400" />
              ) : (
                <Info className="w-3 h-3 text-purple-600 dark:text-purple-400" />
              )}
            </button>
          )}
        </div>
      </div>

      {/* Output Handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-2.5 !h-2.5 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

EvaluationNode.displayName = 'EvaluationNode'
export default EvaluationNode

