import React from 'react'
import type { ToolCallEndEvent } from '../../../generated/events'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'
import { WorkspaceToolCallEndDisplay } from './ToolCallSpecialRender'

interface ToolCallEndEventProps {
  event: ToolCallEndEvent
}

export const ToolCallEndEventDisplay: React.FC<ToolCallEndEventProps> = ({ event }) => {
  
  // Check if this is a workspace tool
  const isWorkspaceTool = (toolName: string): boolean => {
    const workspaceToolNames = [
      'read_workspace_file',
      'update_workspace_file',
      'diff_patch_workspace_file',
      'list_workspace_files',
      // Add more as we implement their UI
    ]
    const isWorkspace = workspaceToolNames.includes(toolName)
    return isWorkspace
  }

  // If it's a workspace tool, use the specialized component
  if (event.tool_name && isWorkspaceTool(event.tool_name)) {
    const specializedDisplay = <WorkspaceToolCallEndDisplay event={event} />
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

  // Single-line layout following design guidelines
  return (
    <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-green-700 dark:text-green-300">
              Tool Call End{' '}
              <span className="text-xs font-normal text-green-600 dark:text-green-400">
                {event.turn && `• Turn: ${event.turn}`}
                {event.tool_name && ` • Tool: ${event.tool_name}`}
                {event.server_name && ` • Server: ${event.server_name}`}
                {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-green-600 dark:text-green-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>

      {/* Extract Content always visible below */}
      {resultInfo && (
        <div className="bg-white dark:bg-gray-800 rounded-md mt-2">
          <ConversationMarkdownRenderer content={resultInfo.textContent} />
        </div>
      )}
    </div>
  )
}