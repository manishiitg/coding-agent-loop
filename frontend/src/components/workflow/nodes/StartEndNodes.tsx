import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Play, Flag } from 'lucide-react'
import type { WorkflowNodeData } from '../hooks/usePlanToFlow'

interface StartEndNodeProps {
  data: WorkflowNodeData
  selected?: boolean
}

export const StartNode = memo(({ data }: StartEndNodeProps) => {
  // The orphan-section header reuses this node type; render it as a distinct
  // amber "Orphan Steps" label instead of a green Start pill (which was the bug
  // that made the orphan section look like a second Start node).
  const isOrphanLabel = (data as { id?: string })?.id === 'orphan-label'
  if (isOrphanLabel) {
    const labelText = typeof (data as { title?: string })?.title === 'string'
      ? (data as { title?: string }).title
      : 'Orphan Steps'
    return (
      <div className="flex items-center justify-center px-3 h-10 rounded-lg bg-amber-100 dark:bg-amber-900/40 border-2 border-dashed border-amber-400 dark:border-amber-500 shadow-sm">
        <span className="text-xs font-semibold uppercase tracking-wide text-amber-700 dark:text-amber-300 whitespace-nowrap">
          {labelText}
        </span>
      </div>
    )
  }
  return (
    <div className="flex items-center justify-center w-24 h-10 rounded-full bg-green-100 dark:bg-green-900/40 border-2 border-green-400 dark:border-green-500 shadow-sm">
      <div className="flex items-center gap-1.5">
        <Play className="w-4 h-4 text-green-600 dark:text-green-400" />
        <span className="text-sm font-medium text-green-700 dark:text-green-300">Start</span>
      </div>

      {/* Output handle */}
      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-3 !h-3 !bg-green-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

StartNode.displayName = 'StartNode'

export const EndNode = memo(({ data }: StartEndNodeProps) => {
  const isCompleted = data.status === 'completed'
  
  return (
    <div className={`
      flex items-center justify-center w-24 h-10 rounded-full shadow-sm border-2
      ${isCompleted 
        ? 'bg-green-100 dark:bg-green-900/40 border-green-400 dark:border-green-500' 
        : 'bg-gray-100 dark:bg-muted border-gray-300 dark:border-border'}
    `}>
      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Top}
        className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />
      
      <div className="flex items-center gap-1.5">
        <Flag className={`w-4 h-4 ${isCompleted ? 'text-green-600 dark:text-green-400' : 'text-gray-500 dark:text-muted-foreground'}`} />
        <span className={`text-sm font-medium ${isCompleted ? 'text-green-700 dark:text-green-300' : 'text-gray-600 dark:text-muted-foreground'}`}>
          End
        </span>
      </div>

      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

EndNode.displayName = 'EndNode'
