import React from 'react'
import type { ToolCallStartEvent } from '../../../generated/events'
import { WorkspaceToolCallDisplay, HumanFeedbackToolCallDisplay } from './ToolCallSpecialRender'

interface ToolCallStartEventProps {
  event: ToolCallStartEvent
}

export const ToolCallStartEventDisplay: React.FC<ToolCallStartEventProps> = ({ event }) => {
  
  // Check if this is a workspace tool
  const isWorkspaceTool = (toolName: string): boolean => {
    const workspaceToolNames = [
      'update_workspace_file',
      'read_workspace_file',
      'list_workspace_files',
      'diff_patch_workspace_file',
      // TODO: Add more tools as we implement their UI
      // 'get_workspace_file_nested',
      // 'regex_search_workspace_files',
      // 'semantic_search_workspace_files',
      // 'sync_workspace_to_github',
      // 'get_workspace_github_status',
      // 'delete_workspace_file',
      // 'move_workspace_file'
    ]
    const isWorkspace = workspaceToolNames.includes(toolName)
    return isWorkspace
  }

  // Check if this is a human feedback tool
  const isHumanFeedbackTool = (toolName: string): boolean => {
    return toolName === 'human_feedback'
  }

  // If it's a workspace tool, use the specialized component
  if (event.tool_name && isWorkspaceTool(event.tool_name)) {
    return <WorkspaceToolCallDisplay event={event} />
  }

  // If it's a human feedback tool, use the specialized component
  if (event.tool_name && isHumanFeedbackTool(event.tool_name)) {
    return <HumanFeedbackToolCallDisplay event={event} />
  }

  // Simple JSON formatting function for regular tools
  const formatArguments = (args: string): string => {
    try {
      const parsed = JSON.parse(args)
      return JSON.stringify(parsed, null, 2)
    } catch {
      return args
    }
  }

  // Single-line layout following design guidelines
  return (
    <div className="bg-orange-50 dark:bg-orange-900/20 border border-orange-200 dark:border-orange-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-orange-700 dark:text-orange-300">
              Tool Call Start{' '}
              <span className="text-xs font-normal text-orange-600 dark:text-orange-400">
                {event.turn && `• Turn: ${event.turn}`}
                {event.tool_name && ` • Tool: ${event.tool_name}`}
                {event.server_name && ` • Server: ${event.server_name}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-orange-600 dark:text-orange-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>

      {/* Tool arguments always visible below */}
      {event.tool_params?.arguments !== undefined && (
        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="text-xs font-medium text-orange-700 dark:text-orange-300 mb-1">Arguments:</div>
            <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto">
              {event.tool_params.arguments ? formatArguments(event.tool_params.arguments) : '(no arguments)'}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}