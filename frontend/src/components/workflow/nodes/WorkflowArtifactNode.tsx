import { memo } from 'react'
import { Handle, Position } from '@xyflow/react'
import { BarChart3, CheckCircle2, CircleDashed, FileText } from 'lucide-react'
import type { WorkflowArtifactNodeData } from '../hooks/usePlanToFlow'

interface WorkflowArtifactNodeProps {
  data: WorkflowArtifactNodeData
  selected?: boolean
}

const styles = {
  evaluation: {
    border: 'border-sky-300 dark:border-sky-500/50',
    surface: 'bg-white dark:bg-slate-900/95',
    iconBg: 'bg-sky-100 dark:bg-sky-500/15',
    accent: 'text-sky-700 dark:text-sky-300',
    badge: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
    shadow: 'shadow-sky-500/10',
    icon: BarChart3
  },
  output: {
    border: 'border-amber-300 dark:border-amber-500/50',
    surface: 'bg-white dark:bg-slate-900/95',
    iconBg: 'bg-amber-100 dark:bg-amber-500/15',
    accent: 'text-amber-700 dark:text-amber-300',
    badge: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    shadow: 'shadow-amber-500/10',
    icon: FileText
  }
} as const

export const WorkflowArtifactNode = memo(({ data, selected }: WorkflowArtifactNodeProps) => {
  const variant = styles[data.kind]
  const Icon = variant.icon

  return (
    <div
      className={`relative w-[220px] rounded-xl border-2 ${variant.border} ${variant.surface} shadow-lg ${variant.shadow} transition-all ${
        selected ? 'ring-2 ring-primary/30' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!w-2.5 !h-2.5 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />

      <div className={`h-1.5 w-full rounded-t-[10px] ${variant.iconBg}`} />

      <div className="px-4 py-3">
        <div className="flex items-start gap-3">
          <div className={`mt-0.5 rounded-md p-2 ${variant.iconBg}`}>
            <Icon className={`w-4 h-4 ${variant.accent}`} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <div className="text-sm font-semibold text-foreground truncate">{data.title}</div>
              {data.configured ? (
                <CheckCircle2 className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0" />
              ) : (
                <CircleDashed className="w-4 h-4 text-muted-foreground flex-shrink-0" />
              )}
            </div>
            {data.description && (
              <div className="mt-1 text-xs text-muted-foreground line-clamp-2">{data.description}</div>
            )}
            <div className={`mt-2 inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium ${variant.badge}`}>
              {data.configured ? 'Configured' : 'Not configured'}
            </div>
            {data.detail && (
              <div className="mt-1 text-[11px] text-muted-foreground line-clamp-2">{data.detail}</div>
            )}
          </div>
        </div>
      </div>

      <Handle
        type="source"
        position={Position.Right}
        className="!w-2.5 !h-2.5 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

WorkflowArtifactNode.displayName = 'WorkflowArtifactNode'

export default WorkflowArtifactNode
