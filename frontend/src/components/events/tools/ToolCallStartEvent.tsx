import React from 'react'
import type { ToolCallStartEvent } from '../../../generated/event-types'
import { WorkspaceToolCallDisplay, CodeExecutionToolCallDisplay, ToolSearchToolCallDisplay, DelegationToolCallDisplay } from './ToolCallSpecialRender'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface ToolCallStartEventProps {
  event: ToolCallStartEvent
}

export const ToolCallStartEventDisplay: React.FC<ToolCallStartEventProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)
  
  // Check if this is a workspace tool
  const isWorkspaceTool = (toolName: string): boolean => {
    const workspaceToolNames = [
      'update_workspace_file',
      'read_workspace_file',
      'list_workspace_files',
      'diff_patch_workspace_file',
      'delete_workspace_file',
      // TODO: Add more tools as we implement their UI
      // 'get_workspace_file_nested',
      // 'regex_search_workspace_files',
      // 'semantic_search_workspace_files',
      // 'sync_workspace_to_github',
      // 'get_workspace_github_status',
      // 'move_workspace_file'
    ]
    const isWorkspace = workspaceToolNames.includes(toolName)
    return isWorkspace
  }

  // Check if this is a human tool (feedback or questions)
  const isHumanTool = (toolName: string): boolean => {
    return toolName === 'human_feedback' || toolName === 'human_questions'
  }

  // Check if this is a code execution tool
  const isCodeExecutionTool = (toolName: string): boolean => {
    return toolName === 'discover_code_structure' || toolName === 'discover_code_files' || toolName === 'write_code' || toolName === 'get_api_spec' || toolName === 'execute_shell_command'
  }

  // Check if this is a tool search tool
  const isToolSearchTool = (toolName: string): boolean => {
    return toolName === 'search_tools' || toolName === 'add_tool'
  }

  // Check if this is a delegation tool
  const isDelegationTool = (toolName: string): boolean => {
    return toolName === 'create_delegation_plan' || toolName === 'confirm_plan_execution'
  }

  // If it's a workspace tool, use the specialized component
  if (event.tool_name && isWorkspaceTool(event.tool_name)) {
    return <WorkspaceToolCallDisplay event={event} />
  }

  // Human tools: don't render here — blocking_human_feedback / blocking_human_questions
  // events (emitted inside the tool handlers) render the interactive UI via their
  // dedicated display components, so rendering here would show the question twice.
  if (event.tool_name && isHumanTool(event.tool_name)) {
    return null
  }

  // If it's a code execution tool, use the specialized component
  if (event.tool_name && isCodeExecutionTool(event.tool_name)) {
    return <CodeExecutionToolCallDisplay event={event} />
  }

  // If it's a tool search tool, use the specialized component
  if (event.tool_name && isToolSearchTool(event.tool_name)) {
    return <ToolSearchToolCallDisplay event={event} />
  }

  // If it's a delegation tool, use the specialized component
  if (event.tool_name && isDelegationTool(event.tool_name)) {
    return <DelegationToolCallDisplay event={event} />
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

  const hasArguments = event.tool_params?.arguments !== undefined

  // Single-line layout following design guidelines
  return (
    <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2 transition-colors duration-200">
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center gap-1.5">
              Tool Call Start{' '}
              {event.is_parallel && (
                <span className="ml-1.5 text-[10px] text-gray-500 dark:text-gray-400 font-normal opacity-75">
                  (parallel)
                </span>
              )}
              <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                {event.turn && `• Turn: ${event.turn}`}
                {event.tool_name && ` • Tool: ${event.tool_name}`}
                {event.server_name && ` • Server: ${event.server_name}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time and Toggle */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className="text-xs text-blue-600 dark:text-blue-400">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
          
          {hasArguments && (
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800 rounded text-blue-700 dark:text-blue-300 transition-colors"
              title={isExpanded ? "Collapse arguments (Alt+Click for all)" : "Expand arguments (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          )}
        </div>
      </div>

      {/* Tool arguments visibility controlled by isExpanded */}
      {hasArguments && isExpanded && (
        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">Arguments:</div>
            <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto">
              {event.tool_params?.arguments ? formatArguments(event.tool_params.arguments) : '(no arguments)'}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}