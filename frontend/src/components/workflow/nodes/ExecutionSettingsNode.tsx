import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Settings, Zap } from 'lucide-react'

export type ExecutionSettingsNodeData = Record<string, unknown>

interface ExecutionSettingsNodeProps {
  data?: ExecutionSettingsNodeData
  selected?: boolean
}

export const ExecutionSettingsNode = memo(({ selected }: ExecutionSettingsNodeProps) => {
  return (
    <div
      className={`
        min-w-[240px] max-w-[300px] rounded-lg border-2 shadow-sm
        bg-purple-50 dark:bg-purple-900/20 border-purple-300 dark:border-purple-700
        hover:border-purple-400 dark:hover:border-purple-600 transition-colors
        ${selected ? 'ring-2 ring-purple-500/50' : ''}
      `}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />

      <div className="flex items-center gap-2 px-3 py-2 bg-purple-100 dark:bg-purple-900/40 rounded-t-lg border-b border-purple-200 dark:border-purple-800">
        <Settings className="w-4 h-4 text-purple-600 dark:text-purple-400" />
        <span className="text-sm font-medium text-purple-700 dark:text-purple-300">
          Execution Settings
        </span>
      </div>

      <div className="px-3 py-3">
        <div
          className="flex items-center justify-center gap-1.5 rounded-md border border-border bg-background px-2 py-1.5 text-xs font-medium text-foreground shadow-sm"
          title="Manual runs use the selected iteration and clear outputs. Scheduled runs create a new iteration."
        >
          <Zap className="w-3 h-3" />
          Stateless
        </div>
        <p className="mt-2 text-xs text-muted-foreground text-center">
          Manual: selected iteration with cleanup. Scheduled: new iteration.
        </p>
      </div>

      <Handle
        type="source"
        position={Position.Right}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

ExecutionSettingsNode.displayName = 'ExecutionSettingsNode'
