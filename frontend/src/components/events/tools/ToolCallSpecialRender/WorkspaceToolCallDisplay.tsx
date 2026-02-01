import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/event-types'
import { ToolMarkdownRenderer } from '../../../ui/MarkdownRenderer'
import { DiffRenderer } from './DiffRenderer'
import { useExpandable } from '../../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface WorkspaceToolCallDisplayProps {
  event: ToolCallStartEvent
}

// Simple markdown detection function
const isMarkdownContent = (content: string): boolean => {
  if (!content || content.length < 10) return false
  
  // Check for common markdown patterns
  const markdownPatterns = [
    /^#{1,6}\s+/m,           // Headers (# ## ###)
    /^\*\s+/m,               // Bullet lists (* item)
    /^\d+\.\s+/m,            // Numbered lists (1. item)
    /^\s*[-*+]\s+/m,         // Alternative bullet lists (- item)
    /```[\s\S]*?```/m,       // Code blocks
    /`[^`]+`/m,              // Inline code
    /\[([^\]]+)\]\(([^)]+)\)/m, // Links [text](url)
    /\*\*[^*]+\*\*/m,        // Bold **text**
    /\*[^*]+\*/m,            // Italic *text*
    /^>\s+/m,                // Blockquotes (> text)
    /^\|.*\|$/m,             // Tables (| col | col |)
  ]
  
  // Count how many markdown patterns match
  const matches = markdownPatterns.filter(pattern => pattern.test(content)).length
  
  // If 2 or more patterns match, consider it markdown
  return matches >= 2
}

export const WorkspaceToolCallDisplay: React.FC<WorkspaceToolCallDisplayProps> = ({ event }) => {
  const { isExpanded: showContent, toggle } = useExpandable()
  
  if (!event.tool_params?.arguments) {
    return null
  }

  let parsedArgs: Record<string, unknown> = {}
  try {
    parsedArgs = JSON.parse(event.tool_params.arguments)
  } catch {
    return null
  }

  const toolName = event.tool_name || ''

  const parallelBadge = event.is_parallel ? (
    <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300 border border-purple-200 dark:border-purple-700 ml-1.5">
      PARALLEL
    </span>
  ) : null

  const renderHeaderRight = () => (
    <div className="flex items-center gap-2 flex-shrink-0">
      {event.timestamp && (
        <div className="text-xs text-blue-600 dark:text-blue-400">
          {new Date(event.timestamp).toLocaleTimeString()}
        </div>
      )}
      <button
        onClick={toggle}
        className="p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800 rounded text-blue-700 dark:text-blue-300 transition-colors"
        title={showContent ? "Collapse arguments (Alt+Click for all)" : "Expand arguments (Alt+Click for all)"}
      >
        {showContent ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
      </button>
    </div>
  )
  
  // Handle read_workspace_file tool
  if (toolName === 'read_workspace_file') {
    const filepath = (parsedArgs.filepath as string) || ''
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                📖 Read Workspace File{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {renderHeaderRight()}
        </div>

        {showContent && filepath && (
          <div className="mt-2">
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📁 File Path:</div>
              <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                {filepath}
              </div>
            </div>
          </div>
        )}
      </div>
    )
  }

  // Handle list_workspace_files tool
  if (toolName === 'list_workspace_files') {
    const folder = (parsedArgs.folder as string) || ''
    const maxDepth = (parsedArgs.max_depth as number) || 3
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                📂 List Workspace Files{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {renderHeaderRight()}
        </div>

        {showContent && (
          <div className="mt-2">
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="grid grid-cols-1 gap-2">
                {folder && (
                  <div>
                    <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📁 Folder:</div>
                    <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                      {folder}
                    </div>
                  </div>
                )}
                <div>
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📏 Max Depth:</div>
                  <div className="text-sm text-gray-800 dark:text-gray-200">
                    {maxDepth} levels
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    )
  }

  // Handle diff_patch_workspace_file tool
  if (toolName === 'diff_patch_workspace_file') {
    const filepath = (parsedArgs.filepath as string) || ''
    const diff = (parsedArgs.diff as string) || ''
    const commitMessage = (parsedArgs.commit_message as string) || ''
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                🔧 Patch Workspace File{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {renderHeaderRight()}
        </div>

        {showContent && (
          <>
            {/* File path */}
            {filepath && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📁 File Path:</div>
                  <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                    {filepath}
                  </div>
                </div>
              </div>
            )}

            {/* Commit message */}
            {commitMessage && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">💬 Commit Message:</div>
                  <div className="text-sm text-gray-800 dark:text-gray-200">
                    {commitMessage}
                  </div>
                </div>
              </div>
            )}

            {/* Diff content */}
            {diff && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="flex items-center justify-between mb-1">
                    <div className="text-xs font-medium text-blue-700 dark:text-blue-300">
                      📝 Unified Diff Format
                    </div>
                  </div>
                  
                  <div className="mt-2">
                    <DiffRenderer diff={diff} maxHeight="400px" />
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    )
  }

  // Handle update_workspace_file tool
  if (toolName === 'update_workspace_file') {
    const filepath = (parsedArgs.filepath as string) || ''
    const content = (parsedArgs.content as string) || ''
    const commitMessage = (parsedArgs.commit_message as string) || ''
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          {/* Left side: Icon and main content */}
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                📝 Update Workspace File{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {/* Right side: Time */}
          {renderHeaderRight()}
        </div>

        {showContent && (
          <>
            {/* File path */}
            {filepath && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📁 File Path:</div>
                  <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                    {filepath}
                  </div>
                </div>
              </div>
            )}

            {/* Commit message */}
            {commitMessage && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">💬 Commit Message:</div>
                  <div className="text-sm text-gray-800 dark:text-gray-200">
                    {commitMessage}
                  </div>
                </div>
              </div>
            )}

            {/* Content preview */}
            {content && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300">
                  📄 Content {isMarkdownContent(content) && <span className="text-blue-600 dark:text-blue-400">(Markdown)</span>}
                </div>
                <button
                  onClick={toggle}
                  className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 transition-colors"
                >
                  {showContent ? 'Hide' : 'Show'}
                </button>
              </div>
                  
                  <div className="text-xs text-gray-800 dark:text-gray-200 mt-2">
                    {isMarkdownContent(content) ? (
                      <ToolMarkdownRenderer content={content} maxHeight="400px" />
                    ) : (
                      <pre className="text-xs whitespace-pre-wrap font-mono bg-gray-50 dark:bg-gray-900 p-2 rounded border max-h-96 overflow-y-auto">
                        {content}
                      </pre>
                    )}
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    )
  }

  // Handle delete_workspace_file tool
  if (toolName === 'delete_workspace_file') {
    const filepath = (parsedArgs.filepath as string) || ''
    const commitMessage = (parsedArgs.commit_message as string) || ''
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          {/* Left side: Icon and main content */}
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                🗑️ Delete Workspace File{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {/* Right side: Time */}
          {renderHeaderRight()}
        </div>

        {showContent && (
          <>
            {/* File path */}
            {filepath && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📁 File Path:</div>
                  <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                    {filepath}
                  </div>
                </div>
              </div>
            )}

            {/* Commit message */}
            {commitMessage && (
              <div className="mt-2">
                <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                  <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">💬 Commit Message:</div>
                  <div className="text-sm text-gray-800 dark:text-gray-200">
                    {commitMessage}
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    )
  }

  // Handle other workspace tools (fallback to JSON for now)
  return (
    <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
              🔧 Workspace Tool: {toolName}{' '}
              <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                {event.turn && `• Turn: ${event.turn}`}
                {event.server_name && ` • Server: ${event.server_name}`}
              </span>
            </div>
          </div>
        </div>

        {renderHeaderRight()}
      </div>

      {/* Show JSON for other workspace tools */}
      {showContent && (
        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">Arguments:</div>
            <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto">
              {JSON.stringify(parsedArgs, null, 2)}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
