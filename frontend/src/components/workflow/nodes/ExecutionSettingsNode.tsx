import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Settings, FolderX } from 'lucide-react'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

export type ExecutionSettingsNodeData = Record<string, unknown>

interface ExecutionSettingsNodeProps {
  data?: ExecutionSettingsNodeData
  selected?: boolean
}

export const ExecutionSettingsNode = memo(({ selected }: ExecutionSettingsNodeProps) => {
  const skipExecutionCleanup = useWorkflowStore(state => state.skipExecutionCleanup)
  const setSkipExecutionCleanup = useWorkflowStore(state => state.setSkipExecutionCleanup)

  return (
    <div
      className={`
        min-w-[240px] max-w-[300px] rounded-lg border-2 shadow-sm
        bg-purple-50 dark:bg-purple-900/20 border-purple-300 dark:border-purple-700
        hover:border-purple-400 dark:hover:border-purple-600 transition-colors
        ${selected ? 'ring-2 ring-purple-500/50' : ''}
      `}
    >
      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />

      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-purple-100 dark:bg-purple-900/40 rounded-t-lg border-b border-purple-200 dark:border-purple-800">
        <Settings className="w-4 h-4 text-purple-600 dark:text-purple-400" />
        <span className="text-sm font-medium text-purple-700 dark:text-purple-300">
          Execution Settings
        </span>
      </div>

      {/* Body */}
      <div className="px-3 py-3">
        {/* Skip Execution Cleanup Toggle */}
        <div>
          <button
            onClick={() => setSkipExecutionCleanup(!skipExecutionCleanup)}
            className={`
              w-full flex items-center gap-2 px-3 py-2 rounded-md transition-all text-sm
              ${skipExecutionCleanup
                ? 'bg-stone-200 dark:bg-stone-700/50 border border-stone-400 dark:border-stone-500'
                : 'bg-white dark:bg-gray-800 border border-purple-200 dark:border-purple-800 hover:bg-gray-50 dark:hover:bg-gray-700'
              }
            `}
            title={skipExecutionCleanup
              ? "Execution output folders will be preserved"
              : "Execution output folders will be deleted before running"
            }
          >
            <FolderX className={`w-4 h-4 ${skipExecutionCleanup ? 'text-stone-600 dark:text-stone-400' : 'text-gray-400 dark:text-gray-500'}`} />
            <div className="flex-1 text-left">
              <div className={`font-medium ${skipExecutionCleanup ? 'text-stone-700 dark:text-stone-300' : 'text-gray-700 dark:text-gray-300'}`}>
                Skip Cleanup
              </div>
              <div className="text-xs text-gray-500 dark:text-gray-400">
                {skipExecutionCleanup ? "Keep existing outputs" : "Delete outputs before run"}
              </div>
            </div>
            <div className={`
              w-8 h-5 rounded-full transition-colors relative
              ${skipExecutionCleanup ? 'bg-stone-500' : 'bg-gray-300 dark:bg-gray-600'}
            `}>
              <div className={`
                absolute top-0.5 w-4 h-4 rounded-full bg-white shadow-sm transition-transform
                ${skipExecutionCleanup ? 'translate-x-3.5' : 'translate-x-0.5'}
              `} />
            </div>
          </button>
        </div>
      </div>

      {/* Output handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

ExecutionSettingsNode.displayName = 'ExecutionSettingsNode'
