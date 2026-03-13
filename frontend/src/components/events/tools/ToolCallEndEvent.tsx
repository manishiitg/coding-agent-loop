import React from 'react'
import type { ToolCallEndEvent } from '../../../generated/events'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import { CsvRenderer } from '../../ui/CsvRenderer'
import { WorkspaceToolCallEndDisplay, CodeExecutionToolCallEndDisplay, ToolSearchToolCallEndDisplay } from './ToolCallSpecialRender'
import { ImageGenToolCallEndDisplay } from './ToolCallSpecialRender/ImageGenToolCallEndDisplay'
import { CircularProgress, type ContextOnlyTokenUsage } from '../../ui/CircularProgress'
import { TooltipProvider } from '../../ui/tooltip'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'
import { normalizeMCPToolName } from '../../../utils/customToolNames'

type OutputFormat = 'markdown' | 'json' | 'csv' | null

function detectOutputFormat(text: string): OutputFormat {
  if (!text || text.length < 10) return null
  const trimmed = text.trim()
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try { JSON.parse(trimmed); return 'json' } catch { /* not json */ }
  }
  const lines = trimmed.split('\n').filter(l => l.trim())
  if (lines.length >= 2) {
    const cols = lines[0].split(',').length
    if (cols >= 2 && lines.slice(0, 5).every(l => l.split(',').length === cols)) return 'csv'
  }
  if (text.length >= 30 && (
    /^#{1,6}\s/m.test(text) || /\*\*.+?\*\*/s.test(text) ||
    /^\s*[-*+]\s\S/m.test(text) || /^\|.+\|/m.test(text) ||
    /^>\s/m.test(text) || /```[\s\S]*```/s.test(text)
  )) return 'markdown'
  return null
}

interface ToolCallEndEventProps {
  event: ToolCallEndEvent
}

export const ToolCallEndEventDisplay: React.FC<ToolCallEndEventProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)
  const [isRawMode, setIsRawMode] = React.useState(false)

  const normalizedToolName = event.tool_name ? normalizeMCPToolName(event.tool_name) : event.tool_name

  // Check if this is a workspace tool
  const isWorkspaceTool = (name: string): boolean => {
    const workspaceToolNames = [
      'read_workspace_file',
      'update_workspace_file',
      'diff_patch_workspace_file',
      'list_workspace_files',
      'delete_workspace_file',
    ]
    return workspaceToolNames.includes(name)
  }

  // Check if this is a code execution tool
  const isCodeExecutionTool = (name: string): boolean => {
    return name === 'discover_code_structure' || name === 'discover_code_files' || name === 'write_code' || name === 'get_api_spec' || name === 'execute_shell_command'
  }

  // Check if this is a tool search tool
  const isToolSearchTool = (name: string): boolean => {
    return name === 'search_tools' || name === 'add_tool'
  }

  // Check if this is an image generation/editing tool
  const isImageGenTool = (name: string): boolean => {
    return name === 'workspace_image_gen' || name === 'workspace_image_edit'
  }

  // If it's a workspace tool, use the specialized component
  if (normalizedToolName && isWorkspaceTool(normalizedToolName)) {
    const specializedDisplay = <WorkspaceToolCallEndDisplay event={event} />
    if (specializedDisplay) return specializedDisplay
  }

  // If it's a code execution tool, use the specialized component
  if (normalizedToolName && isCodeExecutionTool(normalizedToolName)) {
    const specializedDisplay = <CodeExecutionToolCallEndDisplay event={{ ...event, tool_name: normalizedToolName }} />
    if (specializedDisplay) return specializedDisplay
  }

  // If it's a tool search tool, use the specialized component
  if (normalizedToolName && isToolSearchTool(normalizedToolName)) {
    const specializedDisplay = <ToolSearchToolCallEndDisplay event={event} />
    if (specializedDisplay) return specializedDisplay
  }

  // If it's the image generation tool, use the specialized component
  if (normalizedToolName && isImageGenTool(normalizedToolName)) {
    return <ImageGenToolCallEndDisplay event={event} />
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

      // Shell-like result: prefer stdout (may itself be JSON/CSV/markdown)
      if (parsed && typeof parsed === 'object' && typeof parsed.stdout === 'string') {
        return {
          isJson: true,
          textContent: parsed.stdout,
          formattedJson: JSON.stringify(parsed, null, 2),
          hasTextField: true
        }
      }

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
  const detectedFormat = resultInfo ? detectOutputFormat(resultInfo.textContent) : null
  const isFormatted = !!detectedFormat && !isRawMode

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

      {/* Output panel */}
      {resultInfo && isExpanded && (
        <div className={`border rounded-md mt-2 p-3 ${isFormatted ? 'bg-white dark:bg-gray-900 border-gray-200 dark:border-gray-700' : 'bg-gray-900 dark:bg-gray-950 border-gray-700'}`}>
          <div className="flex items-center justify-between mb-2">
            <div className={`text-xs font-medium ${isFormatted ? 'text-gray-600 dark:text-gray-400' : 'text-gray-400'}`}>
              Output
            </div>
            <div className="flex items-center gap-2">
              <div className={`text-xs ${isFormatted ? 'text-gray-400 dark:text-gray-500' : 'text-gray-500'}`}>
                {resultInfo.textContent.split('\n').length} line{resultInfo.textContent.split('\n').length !== 1 ? 's' : ''} • {resultInfo.textContent.length} chars
              </div>
              {detectedFormat && (
                <button
                  onClick={() => setIsRawMode(r => !r)}
                  className={`text-[10px] font-bold px-1.5 py-0.5 rounded border transition-colors ${
                    isFormatted
                      ? 'bg-blue-600 border-blue-500 text-white hover:bg-blue-700'
                      : 'bg-gray-600 border-gray-500 text-white hover:bg-gray-500'
                  }`}
                  title={isFormatted ? 'Show raw text' : `Render as ${detectedFormat}`}
                >
                  {isFormatted ? detectedFormat.toUpperCase() : 'RAW'}
                </button>
              )}
            </div>
          </div>
          {isFormatted && detectedFormat === 'markdown' && (
            <div className="max-h-96 overflow-y-auto overflow-x-auto">
              <MarkdownRenderer content={resultInfo.textContent} />
            </div>
          )}
          {isFormatted && detectedFormat === 'json' && (
            <div className="max-h-96 overflow-y-auto overflow-x-auto">
              <MarkdownRenderer content={'```json\n' + JSON.stringify(JSON.parse(resultInfo.textContent.trim()), null, 2) + '\n```'} />
            </div>
          )}
          {isFormatted && detectedFormat === 'csv' && (
            <div className="max-h-96 overflow-y-auto overflow-x-auto">
              <CsvRenderer content={resultInfo.textContent} />
            </div>
          )}
          {!isFormatted && (
            <pre className="text-xs font-mono whitespace-pre-wrap overflow-x-auto max-h-96 overflow-y-auto text-green-300">
              {resultInfo.textContent}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}
