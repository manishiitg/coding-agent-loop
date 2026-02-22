import React from 'react'
import type { ToolCallEndEvent } from '../../../generated/events'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'
import { WorkspaceToolCallEndDisplay, CodeExecutionToolCallEndDisplay, ToolSearchToolCallEndDisplay } from './ToolCallSpecialRender'
import { CircularProgress, type ContextOnlyTokenUsage } from '../../ui/CircularProgress'
import { TooltipProvider } from '../../ui/tooltip'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface ToolCallEndEventProps {
  event: ToolCallEndEvent
}

export const ToolCallEndEventDisplay: React.FC<ToolCallEndEventProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable()
  
  // Check if this is a workspace tool
  const isWorkspaceTool = (toolName: string): boolean => {
    const workspaceToolNames = [
      'read_workspace_file',
      'update_workspace_file',
      'diff_patch_workspace_file',
      'list_workspace_files',
      'delete_workspace_file',
      // Add more as we implement their UI
    ]
    const isWorkspace = workspaceToolNames.includes(toolName)
    return isWorkspace
  }

  // Check if this is a code execution tool
  const isCodeExecutionTool = (toolName: string): boolean => {
    return toolName === 'discover_code_structure' || toolName === 'discover_code_files' || toolName === 'write_code' || toolName === 'get_api_spec' || toolName === 'execute_shell_command'
  }

  // Check if this is a tool search tool
  const isToolSearchTool = (toolName: string): boolean => {
    return toolName === 'search_tools' || toolName === 'add_tool'
  }

  // If it's a workspace tool, use the specialized component
  if (event.tool_name && isWorkspaceTool(event.tool_name)) {
    const specializedDisplay = <WorkspaceToolCallEndDisplay event={event} />
    // If the specialized renderer returns null, fall back to default
    if (specializedDisplay) {
      return specializedDisplay
    }
  }

  // If it's a code execution tool, use the specialized component
  if (event.tool_name && isCodeExecutionTool(event.tool_name)) {
    const specializedDisplay = <CodeExecutionToolCallEndDisplay event={event} />
    // If the specialized renderer returns null, fall back to default
    if (specializedDisplay) {
      return specializedDisplay
    }
  }

  // If it's a tool search tool, use the specialized component
  if (event.tool_name && isToolSearchTool(event.tool_name)) {
    const specializedDisplay = <ToolSearchToolCallEndDisplay event={event} />
    // If the specialized renderer returns null, fall back to default
    if (specializedDisplay) {
      return specializedDisplay
    }
  }

  // Function to parse and extract content from JSON results
  const parseResultContent = (result: string): { 
    isJson: boolean; 
    textContent: string; 
    formattedJson?: string;
    hasTextField: boolean;
  } => {
    try {
      const parsed = JSON.parse(result)
      
      // Check if it's a structured response with text field
      if (parsed && typeof parsed === 'object' && parsed.text) {
        return {
          isJson: true,
          textContent: parsed.text,
          formattedJson: JSON.stringify(parsed, null, 2),
          hasTextField: true
        }
      }
      
      // If it's JSON but doesn't have a text field, return formatted JSON
      return {
        isJson: true,
        textContent: result,
        formattedJson: JSON.stringify(parsed, null, 2),
        hasTextField: false
      }
    } catch {
      // Not valid JSON, return as plain text
      return {
        isJson: false,
        textContent: result,
        hasTextField: false
      }
    }
  }

  // Note: event.duration is in nanoseconds from Go time.Duration
  const formatDuration = (durationNs: number) => {
    if (!durationNs || durationNs <= 0) {
      return '0ms'
    }

    // Convert nanoseconds to milliseconds
    const durationMs = durationNs / 1000000

    if (durationMs < 1) {
      // Less than 1ms, show in microseconds
      const durationUs = durationNs / 1000
      return `${Math.round(durationUs)}μs`
    } else if (durationMs < 1000) {
      // Less than 1ms, show in milliseconds
      return `${Math.round(durationMs)}ms`
    } else if (durationMs < 60000) {
      // Less than 1 minute, show in seconds
      return `${(durationMs / 1000).toFixed(1)}s`
    } else {
      // 1 minute or more, show in minutes
      return `${(durationMs / 60000).toFixed(1)}m`
    }
  }

  // Parse the result content to extract text
  const resultInfo = event.result ? parseResultContent(event.result) : null

  // Extract context usage information
  const contextUsagePercent = event.context_usage_percent
  const modelContextWindow = event.model_context_window
  const contextWindowUsage = event.context_window_usage
  const modelId = event.model_id

  // Create a minimal token usage object for the tooltip (only context info available)
  const tokenUsageForTooltip: ContextOnlyTokenUsage | undefined =
    contextUsagePercent !== undefined && contextUsagePercent > 0 ? {
      context_usage_percent: contextUsagePercent,
      model_context_window: modelContextWindow,
      context_window_usage: contextWindowUsage,
      model_id: modelId,
    } : undefined

  // Determine theme color based on tool name (retrieval vs action)
  const toolName = event.tool_name?.toLowerCase() || ''
  const isRetrieval = toolName.includes('search') || 
                      toolName.includes('list') || 
                      toolName.includes('read') || 
                      toolName.includes('get') ||
                      toolName.includes('fetch') ||
                      toolName.includes('find')

  const theme = isRetrieval ? 'blue' : 'green'
  
  const bgColor = theme === 'blue' ? 'bg-blue-50 dark:bg-blue-900/20' : 'bg-green-50 dark:bg-green-900/20'
  const borderColor = theme === 'blue' ? 'border-blue-200 dark:border-blue-800' : 'border-green-200 dark:border-green-800'
  const textColor = theme === 'blue' ? 'text-blue-700 dark:text-blue-300' : 'text-green-700 dark:text-green-300'
  const textSecondaryColor = theme === 'blue' ? 'text-blue-600 dark:text-blue-400' : 'text-green-600 dark:text-green-400'
  const hoverBgColor = theme === 'blue' ? 'hover:bg-blue-200 dark:hover:bg-blue-800' : 'hover:bg-green-200 dark:hover:bg-green-800'

  // Single-line layout following design guidelines
  return (
    <div className={`${bgColor} border ${borderColor} rounded p-2`}>
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className={`text-sm font-medium ${textColor} flex items-center gap-2`}>
              Tool Call End{' '}
              <span className={`text-xs font-normal ${textSecondaryColor}`}>
                {event.turn && `• Turn: ${event.turn}`}
                {event.tool_name && ` • Tool: ${event.tool_name}`}
                {event.server_name && ` • Server: ${event.server_name}`}
                {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
              </span>
              {/* Context completion indicator */}
              {contextUsagePercent !== undefined && contextUsagePercent > 0 && (
                <TooltipProvider>
                  <CircularProgress
                    percentage={contextUsagePercent}
                    size={18}
                    strokeWidth={3}
                    tokenUsage={tokenUsageForTooltip}
                  />
                </TooltipProvider>
              )}
            </div>
          </div>
        </div>

        {/* Right side: Time and Toggle */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${textSecondaryColor}`}>
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
          {resultInfo && (
            <button
              onClick={toggle}
              className={`p-0.5 ${hoverBgColor} rounded ${textColor} transition-colors`}
              title={isExpanded ? "Collapse output (Alt+Click for all)" : "Expand output (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          )}
        </div>
      </div>

      {/* Extract Content visibility controlled by isExpanded */}
      {resultInfo && isExpanded && (
        <div className="bg-white dark:bg-gray-800 rounded-md mt-2">
          <ConversationMarkdownRenderer content={resultInfo.textContent} />
        </div>
      )}
    </div>
  )
}