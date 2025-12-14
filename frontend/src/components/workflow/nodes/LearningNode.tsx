import { memo, useState } from 'react'
import { Handle, Position } from '@xyflow/react'
import { BookOpen, Sparkles, Info, X } from 'lucide-react'

export interface LearningNodeData extends Record<string, unknown> {
  id: string
  parentStepId: string
  parentStepTitle: string
  status: 'pending' | 'running' | 'completed' | 'skipped'
  llmProvider?: string
  llmModel?: string
}

interface LearningNodeProps {
  data: LearningNodeData
  selected?: boolean
}

const statusStyles = {
  pending: {
    border: 'border-gray-300 dark:border-gray-600',
    bg: 'bg-gray-50 dark:bg-gray-800/50',
    icon: 'text-gray-400'
  },
  running: {
    border: 'border-amber-400 dark:border-amber-500',
    bg: 'bg-amber-50 dark:bg-amber-900/30',
    icon: 'text-amber-500 animate-pulse'
  },
  completed: {
    border: 'border-amber-400 dark:border-amber-500',
    bg: 'bg-amber-50 dark:bg-amber-900/30',
    icon: 'text-amber-500'
  },
  skipped: {
    border: 'border-gray-300 dark:border-gray-600',
    bg: 'bg-gray-50 dark:bg-gray-800/50',
    icon: 'text-gray-400'
  }
}

export const LearningNode = memo(({ data, selected }: LearningNodeProps) => {
  const { status } = data
  const styles = statusStyles[status]
  const [isExpanded, setIsExpanded] = useState(false)
  const showExpandIcon = data.llmModel && data.llmModel.length > 20

  return (
    <div className={`
      relative ${isExpanded ? 'w-[200px]' : 'w-[160px]'} rounded-lg border-2 ${styles.border} ${styles.bg}
      shadow-md transition-all
      ${selected ? 'ring-2 ring-amber-500/40' : ''}
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
          <BookOpen className={`w-4 h-4 ${styles.icon} flex-shrink-0 mt-0.5`} />
          <div className="flex-1 min-w-0">
            <div className="text-[10px] font-semibold text-gray-700 dark:text-gray-200 truncate">
              Learn
            </div>
            {data.llmProvider && data.llmModel ? (
              <>
                <div className="text-[8px] text-amber-600 dark:text-amber-400 font-medium truncate mt-0.5">
                  {data.llmProvider}
                </div>
                <div className="text-[8px] text-amber-500 dark:text-amber-300">
                  {isExpanded ? data.llmModel : (data.llmModel.length > 20 ? `${data.llmModel.slice(0, 20)}...` : data.llmModel)}
                </div>
              </>
            ) : (
              <div className="text-[8px] text-gray-500 dark:text-gray-400 mt-0.5">
                Capture insights
              </div>
            )}
          </div>
          {showExpandIcon && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                setIsExpanded(!isExpanded)
              }}
              className="flex-shrink-0 p-0.5 hover:bg-amber-100 dark:hover:bg-amber-900/50 rounded transition-colors"
              title={isExpanded ? 'Hide full model name' : 'Show full model name'}
            >
              {isExpanded ? (
                <X className="w-3 h-3 text-amber-600 dark:text-amber-400" />
              ) : (
                <Info className="w-3 h-3 text-amber-600 dark:text-amber-400" />
              )}
            </button>
          )}
        </div>
      </div>

      {/* Sparkle indicator */}
      <div className="absolute -top-1.5 -right-1.5 z-10">
        <div className="w-4 h-4 rounded-full bg-amber-500 flex items-center justify-center shadow-sm">
          <Sparkles className="w-2.5 h-2.5 text-white" />
        </div>
      </div>

      {/* Output Handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-2.5 !h-2.5 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />

      {/* Prerequisite source handles (bottom, for edges going back to previous steps when prerequisite is detected during execution) */}
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-left"
        style={{ left: '25%' }}
        className="!w-2 !h-2 !bg-orange-500 !border-2 !border-white dark:!border-gray-800"
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-middle"
        style={{ left: '50%' }}
        className="!w-2 !h-2 !bg-orange-500 !border-2 !border-white dark:!border-gray-800"
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="prereq-right"
        style={{ left: '75%' }}
        className="!w-2 !h-2 !bg-orange-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

LearningNode.displayName = 'LearningNode'
export default LearningNode

