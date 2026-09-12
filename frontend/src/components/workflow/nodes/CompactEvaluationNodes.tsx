import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { CheckCheck, ChevronRight } from 'lucide-react'
import type { EvaluationStepNodeData } from '../hooks/usePlanToFlow'
import { effectiveExecutionMode } from '../../../utils/stepConfigMatching'

export const CompactEvaluationNode = memo(({ data, selected }: NodeProps) => {
  const evaluation = data as EvaluationStepNodeData
  const mode = effectiveExecutionMode(evaluation.step, { evaluation: true })
  return <button type="button" aria-label={`Open evaluation: ${evaluation.title}`} title={`${evaluation.title}\n${evaluation.evaluationScopeLabel}`} className={`nodrag nopan flex h-[76px] w-[280px] items-center gap-2.5 rounded-lg border bg-card px-3 text-left text-card-foreground shadow-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary ${selected ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-primary/60'}`}>
    <CheckCheck className="h-4 w-4 shrink-0 text-teal-500" />
    <div className="min-w-0 flex-1">
      <div className="line-clamp-2 text-xs font-medium leading-4">{evaluation.title}</div>
      <div className="mt-1 text-[10px] text-muted-foreground">{mode === 'scripted' ? 'Scripted check' : 'Agent evaluation'}</div>
    </div>
    <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
  </button>
})
CompactEvaluationNode.displayName = 'CompactEvaluationNode'

export const EvaluationGroupNode = memo(({ data }: NodeProps) => <div className="h-[52px] border-b border-border pb-2 text-foreground">
  <Handle type="target" position={Position.Top} className="!h-1 !w-1 !border-0 !bg-muted-foreground" />
  <div title={String(data.title)} className="flex items-center gap-2 text-sm font-semibold"><CheckCheck className="h-4 w-4 shrink-0 text-teal-500" /><span className="truncate">Evaluations · {String(data.title)}</span></div>
  <p className="mt-1 text-xs text-muted-foreground">{String(data.detail)}</p>
</div>)
EvaluationGroupNode.displayName = 'EvaluationGroupNode'
