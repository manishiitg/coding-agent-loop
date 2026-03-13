import React from 'react'
import type { ToolCallStartEvent } from '../../../generated/event-types'
import { WorkspaceToolCallDisplay, CodeExecutionToolCallDisplay, ToolSearchToolCallDisplay, DelegationToolCallDisplay, SubAgentToolCallDisplay } from './ToolCallSpecialRender'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'
import { getLogicalToolName } from '../../../utils/event-helpers'

interface ToolCallStartEventProps {
  event: ToolCallStartEvent
}

export const ToolCallStartEventDisplay: React.FC<ToolCallStartEventProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)
  
  const logicalToolName = event.tool_name ? getLogicalToolName(event.tool_name) : ''

  // Check if this is a workspace tool
  const isWorkspaceTool = (name: string): boolean => {
    const workspaceToolNames = [
      'update_workspace_file',
      'read_workspace_file',
      'list_workspace_files',
      'diff_patch_workspace_file',
      'delete_workspace_file',
    ]
    return workspaceToolNames.includes(name)
  }

  // Check if this is a human tool (feedback or questions)
  const isHumanTool = (name: string): boolean => {
    return name === 'human_feedback' || name === 'human_questions'
  }

  // Check if this is a code execution tool
  const isCodeExecutionTool = (name: string): boolean => {
    return name === 'discover_code_structure' || name === 'discover_code_files' || name === 'write_code' || name === 'get_api_spec' || name === 'execute_shell_command'
  }

  // Check if this is a tool search tool
  const isToolSearchTool = (name: string): boolean => {
    return name === 'search_tools' || name === 'add_tool'
  }

  // Check if this is a delegation tool
  const isDelegationTool = (name: string): boolean => {
    return name === 'create_delegation_plan' || name === 'confirm_plan_execution'
  }

  // If it's a workspace tool, use the specialized component
  if (isWorkspaceTool(logicalToolName)) {
    return <WorkspaceToolCallDisplay event={event} />
  }

  // Human tools: don't render here — blocking_human_feedback / blocking_human_questions
  // events (emitted inside the tool handlers) render the interactive UI via their
  // dedicated display components, so rendering here would show the question twice.
  if (isHumanTool(logicalToolName)) {
    return null
  }

  // If it's a code execution tool, use the specialized component
  if (isCodeExecutionTool(logicalToolName)) {
    return <CodeExecutionToolCallDisplay event={event} />
  }

  // If it's a tool search tool, use the specialized component
  if (isToolSearchTool(logicalToolName)) {
    return <ToolSearchToolCallDisplay event={event} />
  }

  // If it's a delegation tool, use the specialized component
  if (isDelegationTool(logicalToolName)) {
    return <DelegationToolCallDisplay event={event} />
  }

  // If it's a sub-agent tool, use the specialized component
  if (logicalToolName === 'call_sub_agent' || logicalToolName === 'call_generic_agent') {
    return <SubAgentToolCallDisplay event={event} />
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