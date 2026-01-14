import React from 'react'
import { Layers, CheckCircle, XCircle, Clock, Folder } from 'lucide-react'
import { useWorkflowStore } from '../../stores/useWorkflowStore'

interface BatchProgressHeaderProps {
  compact?: boolean
  position?: 'chat' | 'canvas'  // 'canvas' for React Flow overlay, 'chat' for chat area
}

export const BatchProgressHeader: React.FC<BatchProgressHeaderProps> = ({
  compact = false,
  position = 'chat'
}) => {
  const batchProgress = useWorkflowStore(state => state.batchProgress)
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const variablesManifest = useWorkflowStore(state => state.variablesManifest)

  // Don't render if no active batch
  if (!batchProgress?.isActive) {
    return null
  }

  const {
    totalGroups,
    currentGroupIndex,
    currentGroupId,
    completedCount,
    failedCount,
    remainingCount
  } = batchProgress

  // Get display name from variables manifest if available
  const currentGroup = variablesManifest?.groups?.find(g => g.group_id === currentGroupId)
  const displayName = currentGroup?.display_name || currentGroupId

  // Calculate progress percentage
  const progressPercent = totalGroups > 0
    ? ((currentGroupIndex + 1) / totalGroups) * 100
    : 0

  // Canvas position: absolute positioned above the legend
  if (position === 'canvas') {
    return (
      <div className="absolute bottom-[140px] left-4 z-10 w-44">
        <div className="bg-background/95 dark:bg-gray-900/95 backdrop-blur-sm rounded border border-border shadow-md text-[10px]">
          {/* Header */}
          <div className="px-1.5 py-1 border-b border-border">
            <div className="flex items-center gap-1">
              <div className="w-1 h-1 bg-blue-500 rounded-full animate-pulse flex-shrink-0" />
              <Layers className="w-2.5 h-2.5 text-blue-600 dark:text-blue-400" />
              <span className="font-medium text-foreground">Batch</span>
              <span className="font-mono text-blue-600 dark:text-blue-400 ml-auto">
                {currentGroupIndex + 1}/{totalGroups}
              </span>
            </div>
          </div>

          {/* Content */}
          <div className="px-1.5 py-1 space-y-1">
            {/* Group name */}
            {displayName && (
              <div className="font-medium text-foreground truncate text-[9px]" title={displayName}>
                {displayName}
              </div>
            )}

            {/* Run folder */}
            {selectedRunFolder && (
              <div className="flex items-center gap-1 text-muted-foreground">
                <Folder className="w-2 h-2 flex-shrink-0" />
                <span className="truncate font-mono text-[9px]">{selectedRunFolder}</span>
              </div>
            )}

            {/* Progress bar */}
            <div className="h-0.5 bg-blue-100 dark:bg-blue-800/50 rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-500 dark:bg-blue-400 transition-all duration-300 ease-out"
                style={{ width: `${progressPercent}%` }}
              />
            </div>

            {/* Counts */}
            <div className="flex items-center gap-2 text-[9px]">
              <div className="flex items-center gap-0.5 text-green-600 dark:text-green-400" title="Completed">
                <CheckCircle className="w-2 h-2" />
                <span>{completedCount}</span>
              </div>
              {failedCount > 0 && (
                <div className="flex items-center gap-0.5 text-red-600 dark:text-red-400" title="Failed">
                  <XCircle className="w-2 h-2" />
                  <span>{failedCount}</span>
                </div>
              )}
              <div className="flex items-center gap-0.5 text-gray-500 dark:text-gray-400" title="Remaining">
                <Clock className="w-2 h-2" />
                <span>{remainingCount}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Compact mode for chat area
  if (compact) {
    return (
      <div className="sticky top-0 z-10 flex items-center justify-center py-2 bg-white dark:bg-gray-900">
        <div className="px-2 py-1.5 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg shadow-sm">
          <div className="flex items-center gap-2 text-xs">
            <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse flex-shrink-0" />
            <Layers className="w-3 h-3 text-blue-600 dark:text-blue-400" />
            <span className="text-blue-700 dark:text-blue-300 font-medium">
              Group {currentGroupIndex + 1}/{totalGroups}
            </span>
            {currentGroupId && (
              <span className="text-blue-600 dark:text-blue-400 truncate max-w-[100px] font-mono">
                {currentGroupId.toUpperCase()}
              </span>
            )}
            <span className="text-blue-500 dark:text-blue-400">|</span>
            <div className="flex items-center gap-1.5">
              <span className="text-green-600 dark:text-green-400 flex items-center gap-0.5">
                <CheckCircle className="w-3 h-3" />
                {completedCount}
              </span>
              {failedCount > 0 && (
                <span className="text-red-600 dark:text-red-400 flex items-center gap-0.5">
                  <XCircle className="w-3 h-3" />
                  {failedCount}
                </span>
              )}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Default chat mode
  return (
    <div className="sticky top-0 z-10 flex items-center justify-center py-3 bg-white dark:bg-gray-900">
      <div className="w-full max-w-xl px-4 py-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg shadow-sm">
        <div className="flex items-center justify-between gap-4">
          {/* Left: Status indicator and group info */}
          <div className="flex items-center gap-3 min-w-0">
            <div className="flex items-center gap-2 flex-shrink-0">
              <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" />
              <Layers className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            </div>
            <div className="flex items-center gap-2 text-sm min-w-0">
              <span className="text-blue-700 dark:text-blue-300 font-medium whitespace-nowrap">
                Group {currentGroupIndex + 1}/{totalGroups}
              </span>
              <span className="text-blue-500 dark:text-blue-400">|</span>
              <span className="text-blue-600 dark:text-blue-400 font-mono truncate">
                {currentGroupId?.toUpperCase() || 'Starting...'}
              </span>
            </div>
          </div>

          {/* Right: Counts */}
          <div className="flex items-center gap-3 text-xs flex-shrink-0">
            <div className="flex items-center gap-1 text-green-600 dark:text-green-400" title="Completed">
              <CheckCircle className="w-3.5 h-3.5" />
              <span>{completedCount}</span>
            </div>
            {failedCount > 0 && (
              <div className="flex items-center gap-1 text-red-600 dark:text-red-400" title="Failed">
                <XCircle className="w-3.5 h-3.5" />
                <span>{failedCount}</span>
              </div>
            )}
            <div className="flex items-center gap-1 text-gray-500 dark:text-gray-400" title="Remaining">
              <Clock className="w-3.5 h-3.5" />
              <span>{remainingCount}</span>
            </div>
          </div>
        </div>

        {/* Run folder */}
        {selectedRunFolder && (
          <div className="flex items-center gap-2 mt-2 text-xs text-blue-600 dark:text-blue-400">
            <Folder className="w-3 h-3 flex-shrink-0" />
            <span className="truncate font-mono">{selectedRunFolder}</span>
          </div>
        )}

        {/* Progress bar */}
        <div className="mt-2 h-1.5 bg-blue-100 dark:bg-blue-800/50 rounded-full overflow-hidden">
          <div
            className="h-full bg-blue-500 dark:bg-blue-400 transition-all duration-300 ease-out"
            style={{ width: `${progressPercent}%` }}
          />
        </div>
      </div>
    </div>
  )
}
