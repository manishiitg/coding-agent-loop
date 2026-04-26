import React from 'react'
import { ArrowRight, Brain, MessageSquareText, UsersRound, Workflow } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'
import { getModeInfo } from '../constants/modeInfo'

interface ModeEmptyStateProps {
  modeCategory: ModeCategory | null
}

export const ModeEmptyState: React.FC<ModeEmptyStateProps> = ({ modeCategory }) => {
  const modeInfo = getModeInfo(modeCategory)
  const isMultiAgent = modeCategory === 'multi-agent'

  return (
    <div className="flex h-full flex-col items-center justify-center p-5 text-center sm:p-8">
      {/* Icon */}
      <div className="mb-6">
        {isMultiAgent ? <MultiAgentEmptyAnimation /> : modeInfo.icon}
      </div>

      {/* Title */}
      <h3 className="mb-3 text-2xl font-bold text-foreground">
        {modeInfo.title}
      </h3>

      {/* Description */}
      <p className="mb-8 max-w-md text-muted-foreground">
        {modeInfo.description}
      </p>

      {/* Examples */}
      {modeInfo.examples.length > 0 && (
        <div className="mb-8 w-full max-w-lg">
          <h4 className="mb-4 text-sm font-semibold text-foreground">
            Example Queries:
          </h4>
          <div className="grid grid-cols-1 gap-2">
            {modeInfo.examples.map((example, index) => (
              <div
                key={index}
                className="rounded-lg border border-border bg-card p-3 text-sm italic text-muted-foreground"
              >
                "{example}"
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Tips */}
      {modeInfo.tips.length > 0 && (
        <div className="w-full max-w-lg">
          <h4 className="mb-4 text-sm font-semibold text-foreground">
            Tips for Success:
          </h4>
          <div className="space-y-2">
            {modeInfo.tips.map((tip, index) => (
              <div key={index} className="flex items-start text-sm text-muted-foreground">
                <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-3 mt-2 flex-shrink-0" />
                {tip}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Action Hint */}
      {modeCategory && (
        <div className="mt-8 flex items-center gap-2 text-sm text-muted-foreground">
          <ArrowRight className="w-4 h-4" />
          <span>
            {modeCategory === 'multi-agent'
              ? 'Type your message below to get started'
              : 'Get started with your workflow'
            }
          </span>
        </div>
      )}
    </div>
  )
}

const MultiAgentEmptyAnimation: React.FC = () => {
  return (
    <div className="w-[min(34rem,88vw)]" aria-hidden="true">
      <div className="grid grid-cols-[1fr_auto_1fr_auto_1fr_auto_1fr] items-center gap-2 sm:gap-3">
        <HubNode icon={<MessageSquareText className="h-5 w-5" />} label="Text" tone="neutral" />
        <PipelineArrow />
        <HubNode icon={<Brain className="h-5 w-5" />} label="Memory" tone="cyan" />
        <PipelineArrow />
        <HubNode icon={<UsersRound className="h-5 w-5" />} label="Employees" tone="emerald" />
        <PipelineArrow />
        <HubNode icon={<Workflow className="h-5 w-5" />} label="Workflows" tone="indigo" />
      </div>
    </div>
  )
}

const toneClasses = {
  neutral: 'border-border bg-card text-primary',
  cyan: 'border-cyan-300/70 bg-cyan-500/10 text-cyan-700 dark:border-cyan-400/30 dark:bg-cyan-400/10 dark:text-cyan-200',
  emerald: 'border-emerald-300/70 bg-emerald-500/10 text-emerald-700 dark:border-emerald-400/30 dark:bg-emerald-400/10 dark:text-emerald-200',
  indigo: 'border-indigo-300/70 bg-indigo-500/10 text-indigo-700 dark:border-indigo-400/30 dark:bg-indigo-400/10 dark:text-indigo-200',
}

const HubNode: React.FC<{
  icon: React.ReactNode
  label: string
  tone: keyof typeof toneClasses
}> = ({ icon, label, tone }) => (
  <div className={`flex h-14 min-w-0 flex-col items-center justify-center gap-1 rounded-xl border px-2 shadow-sm ${toneClasses[tone]}`}>
    {icon}
    <span className="max-w-full truncate text-[10px] font-medium text-muted-foreground sm:text-[11px]">{label}</span>
  </div>
)

const PipelineArrow: React.FC = () => (
  <div className="flex items-center text-muted-foreground/70">
    <div className="h-px w-3 bg-border sm:w-5" />
    <ArrowRight className="h-3.5 w-3.5" />
  </div>
)
