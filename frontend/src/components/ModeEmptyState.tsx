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
    <div className="flex flex-col items-center justify-center h-full p-8 text-center">
      {/* Icon */}
      <div className="mb-6">
        {modeInfo.icon}
      </div>

      {/* Title */}
      <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-3">
        {modeInfo.title}
      </h3>

      {/* Description */}
      <p className="text-gray-600 dark:text-gray-400 mb-8 max-w-md">
        {modeInfo.description}
      </p>

      {/* Examples */}
      {modeInfo.examples.length > 0 && (
        <div className="mb-8 w-full max-w-lg">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">
            Example Queries:
          </h4>
          <div className="grid grid-cols-1 gap-2">
            {modeInfo.examples.map((example, index) => (
              <div
                key={index}
                className="text-sm text-gray-500 dark:text-gray-400 italic bg-gray-50 dark:bg-gray-800 p-3 rounded-lg border border-gray-200 dark:border-gray-700"
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
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">
            Tips for Success:
          </h4>
          <div className="space-y-2">
            {modeInfo.tips.map((tip, index) => (
              <div key={index} className="flex items-start text-sm text-gray-600 dark:text-gray-400">
                <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-3 mt-2 flex-shrink-0" />
                {tip}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Action Hint */}
      {modeCategory && (
        <div className="mt-8 flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <ArrowRight className="w-4 h-4" />
          <span>
            {modeCategory === 'chat' 
              ? 'Type your message below to get started'
              : 'Select a preset from the sidebar to begin'
            }
          </span>
        </div>
      )}
    </div>
  )
}
