import React, { useState } from 'react'
import type { ToolCallEndEvent } from '../../../../generated/events'
import { ToolMarkdownRenderer } from '../../../ui/MarkdownRenderer'

interface WorkspaceToolCallEndDisplayProps {
  event: ToolCallEndEvent
}

// Simple markdown detection function (same as WorkspaceToolCallDisplay)
const isMarkdownContent = (content: string): boolean => {
  if (!content || content.length < 10) return false
  
  const markdownPatterns = [
    /^#{1,6}\s+/m,           // Headers
    /^\*\s+/m,               // Bullet lists
    /^\d+\.\s+/m,            // Numbered lists
    /^\s*[-*+]\s+/m,         // Alternative bullet lists
    /```[\s\S]*?```/m,       // Code blocks
    /`[^`]+`/m,              // Inline code
    /\[([^\]]+)\]\(([^)]+)\)/m, // Links
    /\*\*[^*]+\*\*/m,        // Bold
    /\*[^*]+\*/m,            // Italic
    /^>\s+/m,                // Blockquotes
    /^\|.*\|$/m,             // Tables
  ]
  
  const matches = markdownPatterns.filter(pattern => pattern.test(content)).length
  return matches >= 2
}

// Format duration from nanoseconds
const formatDuration = (durationNs: number) => {
  if (!durationNs || durationNs <= 0) return '0ms'
  
  const durationMs = durationNs / 1000000
  
  if (durationMs < 1) {
    const durationUs = durationNs / 1000
    return `${Math.round(durationUs)}μs`
  } else if (durationMs < 1000) {
    return `${Math.round(durationMs)}ms`
  } else if (durationMs < 60000) {
    return `${(durationMs / 1000).toFixed(1)}s`
  } else {
    return `${(durationMs / 60000).toFixed(1)}m`
  }
}

export const WorkspaceToolCallEndDisplay: React.FC<WorkspaceToolCallEndDisplayProps> = ({ event }) => {
  const [showContent, setShowContent] = useState(true) // Always show content by default
  
  if (!event.result) {
    return null
  }

  let parsedResult: Record<string, unknown> = {}
  let isJsonResult = false
  
  try {
    parsedResult = JSON.parse(event.result)
    isJsonResult = true
  } catch {
    // For non-JSON results, create a simple object with the result as content
    parsedResult = { content: event.result }
    isJsonResult = false
  }

  const toolName = event.tool_name || ''
  
  // Handle list_workspace_files tool response
  if (toolName === 'list_workspace_files') {
    // The response is an array of files/folders
    const files = Array.isArray(parsedResult) ? parsedResult : []
    
    const renderFileTree = (items: unknown[], depth = 0): React.ReactElement[] => {
      return items.map((item: unknown, index: number) => {
        const fileItem = item as Record<string, unknown>
        const isFolder = fileItem.is_directory || fileItem.children
        const icon = isFolder ? '📁' : '📄'
        const indent = depth * 16
        const name = (fileItem.name || fileItem.filepath) as string
        const size = fileItem.size as number | undefined
        const children = fileItem.children as unknown[] | undefined
        
        return (
          <div key={`${name}-${index}`}>
            <div 
              className="flex items-center gap-2 py-1 px-2 hover:bg-gray-100 dark:hover:bg-gray-700 rounded text-xs"
              style={{ paddingLeft: `${indent + 8}px` }}
            >
              <span>{icon}</span>
              <span className="font-mono text-gray-800 dark:text-gray-200">
                {name}
              </span>
              {size && !isFolder && (
                <span className="text-gray-500 dark:text-gray-400 text-xs ml-auto">
                  {(size / 1024).toFixed(1)} KB
                </span>
              )}
            </div>
            {children && children.length > 0 && (
              <div>{renderFileTree(children, depth + 1)}</div>
            )}
          </div>
        )
      })
    }
    
    return (
      <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-green-700 dark:text-green-300">
                📂 Files Listed Successfully{' '}
                <span className="text-xs font-normal text-green-600 dark:text-green-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.tool_name && ` • Tool: ${event.tool_name}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-green-600 dark:text-green-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        {/* File tree */}
        {files.length > 0 && (
          <div className="mt-2">
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-green-700 dark:text-green-300 mb-2">
                📋 Found {files.length} {files.length === 1 ? 'item' : 'items'}
              </div>
              <div className="max-h-96 overflow-y-auto">
                {renderFileTree(files)}
              </div>
            </div>
          </div>
        )}
      </div>
    )
  }

  // Handle read_workspace_file tool response
  if (toolName === 'read_workspace_file') {
    const content = (parsedResult.content as string) || ''
    const filepath = (parsedResult.filepath as string) || ''
    const folder = (parsedResult.folder as string) || ''
    const lastModified = (parsedResult.last_modified as string) || ''
    
    
    // If it's not a JSON result, the content is just the result message
    const resultMessage = isJsonResult ? 'File Read Successfully' : content
    
    return (
      <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-green-700 dark:text-green-300">
                📖 {resultMessage}{' '}
                <span className="text-xs font-normal text-green-600 dark:text-green-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.tool_name && ` • Tool: ${event.tool_name}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-green-600 dark:text-green-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        {/* File metadata */}
        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="grid grid-cols-1 gap-2 text-xs">
              {filepath && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">📁 File: </span>
                  <span className="font-mono text-gray-800 dark:text-gray-200">{filepath}</span>
                </div>
              )}
              {folder && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">📂 Folder: </span>
                  <span className="font-mono text-gray-800 dark:text-gray-200">{folder}</span>
                </div>
              )}
              {lastModified && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">🕒 Modified: </span>
                  <span className="text-gray-800 dark:text-gray-200">{new Date(lastModified).toLocaleString()}</span>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* File content */}
        {content && (
          <div className="mt-2">
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs font-medium text-green-700 dark:text-green-300">
                  📄 Content {isMarkdownContent(content) && <span className="text-blue-600 dark:text-blue-400">(Markdown)</span>}
                </div>
                <button
                  onClick={() => setShowContent(!showContent)}
                  className="text-xs text-green-600 dark:text-green-400 hover:text-green-800 dark:hover:text-green-200 transition-colors"
                >
                  {showContent ? 'Hide' : 'Show'}
                </button>
              </div>
              
              {showContent && (
                <div className="text-sm text-gray-800 dark:text-gray-200 mt-2">
                  {isMarkdownContent(content) ? (
                    <ToolMarkdownRenderer content={content} maxHeight="400px" />
                  ) : (
                    <pre className="whitespace-pre-wrap font-mono bg-gray-50 dark:bg-gray-900 p-2 rounded border max-h-96 overflow-y-auto text-xs">
                      {content}
                    </pre>
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    )
  }

  // Handle update_workspace_file tool response
  if (toolName === 'update_workspace_file' || toolName === 'diff_patch_workspace_file') {
    const filepath = (parsedResult.filepath as string) || ''
    const folder = (parsedResult.folder as string) || ''
    const lastModified = (parsedResult.last_modified as string) || ''
    const applied = (parsedResult.applied as boolean) || false
    
    const icon = toolName === 'diff_patch_workspace_file' ? '🔧' : '📝'
    const action = toolName === 'diff_patch_workspace_file' ? 'Patched' : 'Updated'
    
    return (
      <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-green-700 dark:text-green-300">
                {icon} File {action} Successfully{' '}
                <span className="text-xs font-normal text-green-600 dark:text-green-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.tool_name && ` • Tool: ${event.tool_name}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-green-600 dark:text-green-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        {/* File metadata */}
        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="grid grid-cols-1 gap-2 text-xs">
              {filepath && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">📁 File: </span>
                  <span className="font-mono text-gray-800 dark:text-gray-200">{filepath}</span>
                </div>
              )}
              {folder && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">📂 Folder: </span>
                  <span className="font-mono text-gray-800 dark:text-gray-200">{folder}</span>
                </div>
              )}
              {lastModified && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">🕒 Modified: </span>
                  <span className="text-gray-800 dark:text-gray-200">{new Date(lastModified).toLocaleString()}</span>
                </div>
              )}
              {applied && (
                <div>
                  <span className="font-medium text-green-700 dark:text-green-300">✅ Status: </span>
                  <span className="text-gray-800 dark:text-gray-200">Changes applied successfully</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // For other workspace tools, return null to use default renderer
  return null
}
