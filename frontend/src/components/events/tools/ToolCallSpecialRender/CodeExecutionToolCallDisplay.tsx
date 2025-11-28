import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/events'

interface CodeExecutionToolCallDisplayProps {
  event: ToolCallStartEvent
}

export const CodeExecutionToolCallDisplay: React.FC<CodeExecutionToolCallDisplayProps> = ({ event }) => {
  const toolName = event.tool_name || ''

  // Handle discover_code_structure tool (no parameters)
  if (toolName === 'discover_code_structure') {
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
                🔍 Discover Code Structure{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-blue-600 dark:text-blue-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        <div className="mt-2">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
            <div className="text-xs text-gray-500 dark:text-gray-400">
              Discovering available servers, tools, and code structure...
            </div>
          </div>
        </div>
      </div>
    )
  }

  // For other tools, parse arguments
  if (!event.tool_params?.arguments) {
    return null
  }

  let parsedArgs: Record<string, unknown> = {}
  try {
    parsedArgs = JSON.parse(event.tool_params.arguments)
  } catch {
    return null
  }

  // Handle discover_code_files tool
  if (toolName === 'discover_code_files') {
    const serverName = (parsedArgs.server_name as string) || null
    const toolNameParam = (parsedArgs.tool_name as string) || null

    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
                🔍 Discover Code Files{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-blue-600 dark:text-blue-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        <div className="mt-2 space-y-2">
          {serverName && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">📦 Server:</div>
              <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                {serverName}
              </div>
            </div>
          )}
          {toolNameParam && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">🔧 Tool:</div>
              <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                {toolNameParam}
              </div>
            </div>
          )}
          {(serverName || toolNameParam) && (
            <div className="text-xs text-gray-500 dark:text-gray-400 px-2">
              Fetching Go source code...
            </div>
          )}
        </div>
      </div>
    )
  }

  // Handle write_code tool
  if (toolName === 'write_code') {
    const filename = (parsedArgs.filename as string) || null
    const code = (parsedArgs.code as string) || ''
    const lineCount = code.split('\n').length

    return (
      <div className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-purple-700 dark:text-purple-300">
                ✍️ Write Code{' '}
                <span className="text-xs font-normal text-purple-600 dark:text-purple-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.server_name && ` • Server: ${event.server_name}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className="text-xs text-purple-600 dark:text-purple-400 flex-shrink-0">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        <div className="mt-2 space-y-2">
          {filename && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-purple-700 dark:text-purple-300 mb-1">📄 Filename:</div>
              <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                {filename}
              </div>
            </div>
          )}

          {code && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="flex items-center justify-between mb-1">
                <div className="text-xs font-medium text-purple-700 dark:text-purple-300">
                  💻 Go Code Preview
                </div>
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  {lineCount} line{lineCount !== 1 ? 's' : ''} • {code.length} chars
                </div>
              </div>
              <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto bg-gray-50 dark:bg-gray-900 p-2 rounded max-h-48 overflow-y-auto">
                {code}
              </pre>
            </div>
          )}
        </div>
      </div>
    )
  }

  return null
}

