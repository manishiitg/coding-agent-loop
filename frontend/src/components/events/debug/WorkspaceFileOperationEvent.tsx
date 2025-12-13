import React from 'react'
import type { WorkspaceFileOperationEvent } from '../../../generated/events'
import { FileText, Folder, Server } from 'lucide-react'

interface WorkspaceFileOperationEventDisplayProps {
  event: WorkspaceFileOperationEvent
  compact?: boolean
}

export const WorkspaceFileOperationEventDisplay: React.FC<WorkspaceFileOperationEventDisplayProps> = ({ 
  event,
  compact = false 
}) => {
  const getOperationIcon = (operation?: string) => {
    switch (operation?.toLowerCase()) {
      case 'read':
        return '📖'
      case 'write':
        return '✏️'
      case 'create':
        return '➕'
      case 'delete':
        return '🗑️'
      case 'list':
        return '📋'
      case 'exists':
        return '🔍'
      default:
        return '📁'
    }
  }

  const getOperationColor = (operation?: string) => {
    switch (operation?.toLowerCase()) {
      case 'read':
        return 'text-blue-600 dark:text-blue-400'
      case 'write':
        return 'text-green-600 dark:text-green-400'
      case 'create':
        return 'text-purple-600 dark:text-purple-400'
      case 'delete':
        return 'text-red-600 dark:text-red-400'
      case 'list':
        return 'text-yellow-600 dark:text-yellow-400'
      case 'exists':
        return 'text-gray-600 dark:text-gray-400'
      default:
        return 'text-gray-600 dark:text-gray-400'
    }
  }

  const operation = event.operation || 'unknown'

  return (
    <div className={`bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded ${compact ? 'p-2' : 'p-3'}`}>
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className={`text-lg flex-shrink-0 ${getOperationColor(operation)}`}>
            {getOperationIcon(operation)}
          </div>
          <div className="min-w-0 flex-1">
            <div className={`${compact ? 'text-xs' : 'text-sm'} font-medium text-gray-700 dark:text-gray-300`}>
              <span className={getOperationColor(operation)}>
                {operation.toUpperCase()}
              </span>
              {' '}
              <span className="text-gray-600 dark:text-gray-400">
                Workspace File Operation
              </span>
            </div>
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-600 dark:text-gray-400 mt-1 flex items-center gap-2 flex-wrap`}>
              {event.filepath && (
                <span className="flex items-center gap-1">
                  <FileText className="w-3 h-3" />
                  <span className="truncate max-w-xs">{event.filepath}</span>
                </span>
              )}
              {event.folder && !event.filepath && (
                <span className="flex items-center gap-1">
                  <Folder className="w-3 h-3" />
                  <span className="truncate max-w-xs">{event.folder}</span>
                </span>
              )}
              {event.server_name && (
                <span className="flex items-center gap-1">
                  <Server className="w-3 h-3" />
                  {event.server_name}
                </span>
              )}
              {event.turn !== undefined && (
                <span>• Turn: {event.turn}</span>
              )}
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-600 dark:text-gray-400 flex-shrink-0`}>
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>
    </div>
  )
}

