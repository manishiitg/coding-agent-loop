import React from 'react'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'

interface ChatHeaderProps {
  chatSessionTitle: string
  chatSessionId: string
  sessionState: 'active' | 'completed' | 'loading' | 'error' | 'not-found'
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
  chatSessionTitle,
  chatSessionId,
  sessionState
}) => {
  const { selectedModeCategory } = useModeStore()
  const { getActivePreset } = usePresetApplication()

  // Get active preset for current mode
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')

  // Check if header has any content to display
  const hasTitle = chatSessionTitle && !(selectedModeCategory === 'workflow' && activePreset)
  const hasStatus = chatSessionId

  // Don't render if there's no content to show
  if (!hasTitle && !hasStatus) {
    return null
  }

  return (
    <div className="border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
      {/* Session Info Bar - Tier 2 only (Tier 1 moved to ModePresetBar at App level) */}
      <div className="px-4 py-1.5 bg-white dark:bg-gray-900">
            <div className="flex items-center gap-3">
              {/* Session Title - Hide in workflow mode when preset is active to avoid duplication */}
              {hasTitle && (
                <h2 className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">
                  {chatSessionTitle}
                </h2>
              )}

              {/* Session Status */}
              {hasStatus && (
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {sessionState === 'active' ? 'Live' :
                   sessionState === 'completed' ? 'Historical' :
                   sessionState === 'loading' ? 'Loading...' :
                   sessionState === 'error' ? 'Error' :
                   'Not Found'}
                </span>
              )}
            </div>
          </div>
    </div>
  )
}
