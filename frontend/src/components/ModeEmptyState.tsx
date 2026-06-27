import React from 'react'
import { ArrowRight } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'
import { getModeInfo } from '../constants/modeInfo'

interface ModeEmptyStateProps {
  modeCategory: ModeCategory | null
}

export const ModeEmptyState: React.FC<ModeEmptyStateProps> = ({ modeCategory }) => {
  const modeInfo = getModeInfo(modeCategory)

  return (
    <div className="flex h-full flex-col items-center justify-center p-5 text-center sm:p-8">
      {/* Icon */}
      <div className="mb-6">
        {modeInfo.icon}
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
              : 'Get started with your automation'
            }
          </span>
        </div>
      )}
    </div>
  )
}

