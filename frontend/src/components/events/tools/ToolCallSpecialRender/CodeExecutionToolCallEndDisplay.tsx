import React from 'react'
import type { ToolCallEndEvent } from '../../../../generated/events'

interface CodeExecutionToolCallEndDisplayProps {
  event: ToolCallEndEvent
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

export const CodeExecutionToolCallEndDisplay: React.FC<CodeExecutionToolCallEndDisplayProps> = ({ event }) => {

  if (!event.result) {
    return null
  }

  let parsedResult: Record<string, unknown> = {}
  let resultText = event.result

  try {
    parsedResult = JSON.parse(event.result)
    // Try to extract text content if it's a structured response
    if (parsedResult.text) {
      resultText = parsedResult.text as string
    } else if (parsedResult.content) {
      resultText = parsedResult.content as string
    } else {
      resultText = JSON.stringify(parsedResult, null, 2)
    }
  } catch {
    // Not JSON, use as-is
    resultText = event.result
  }

  const toolName = event.tool_name || ''

  // Handle discover_code_structure tool response
  if (toolName === 'discover_code_structure') {
    // This tool always returns JSON with server/tool structure
    let serverListData: unknown = null

    try {
      const parsed = JSON.parse(event.result)
      if (parsed.servers || parsed.custom_tools || parsed.virtual_tools) {
        serverListData = parsed
      }
    } catch {
      // If it's not valid JSON, show error
      return (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
          <div className="text-sm font-medium text-red-700 dark:text-red-300">
            ❌ Discovery Error{' '}
            <span className="text-xs font-normal text-red-600 dark:text-red-400">
              {event.turn && `• Turn: ${event.turn}`}
              {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
            </span>
          </div>
          <div className="mt-2 text-xs text-red-600 dark:text-red-400">
            Invalid JSON response: {event.result}
          </div>
        </div>
      )
    }

    if (serverListData) {
      const data = serverListData as {
        servers?: Array<{ name: string; package: string; tools: string[] }>
        custom_tools?: { package: string; tools: string[] }
        virtual_tools?: { package: string; tools: string[] }
      }

      return (
        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3 min-w-0 flex-1">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
                  ✅ Code Structure Discovered{' '}
                  <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                    {event.turn && `• Turn: ${event.turn}`}
                    {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
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

          <div className="mt-2 space-y-3">
            {/* MCP Servers */}
            {data.servers && data.servers.length > 0 && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  📦 MCP Servers ({data.servers.length})
                </div>
                <div className="space-y-2 max-h-96 overflow-y-auto">
                  {data.servers.map((server, idx) => (
                    <div key={idx} className="border-l-2 border-blue-300 dark:border-blue-700 pl-2">
                      <div className="font-mono text-sm font-semibold text-gray-800 dark:text-gray-200">
                        {server.name}
                      </div>
                      <div className="text-xs text-gray-600 dark:text-gray-400 mt-1">
                        Package: <span className="font-mono">{server.package}</span>
                      </div>
                      {server.tools && server.tools.length > 0 && (
                        <div className="mt-1">
                          <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">
                            Tools ({server.tools.length}):
                          </div>
                          <div className="flex flex-wrap gap-1">
                            {server.tools.map((tool, toolIdx) => (
                              <span
                                key={toolIdx}
                                className="text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded"
                              >
                                {tool}
                              </span>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Custom Tools */}
            {data.custom_tools && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  🔧 Custom Tools
                </div>
                <div className="text-xs text-gray-600 dark:text-gray-400">
                  Package: <span className="font-mono">{data.custom_tools.package}</span>
                </div>
                {data.custom_tools.tools && data.custom_tools.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {data.custom_tools.tools.map((tool, idx) => (
                      <span
                        key={idx}
                        className="text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 px-2 py-0.5 rounded"
                      >
                        {tool}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* Virtual Tools */}
            {data.virtual_tools && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  ⚡ Virtual Tools
                </div>
                <div className="text-xs text-gray-600 dark:text-gray-400">
                  Package: <span className="font-mono">{data.virtual_tools.package}</span>
                </div>
                {data.virtual_tools.tools && data.virtual_tools.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {data.virtual_tools.tools.map((tool, idx) => (
                      <span
                        key={idx}
                        className="text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 px-2 py-0.5 rounded"
                      >
                        {tool}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )
    }
  }

  // Handle discover_code_files tool response
  if (toolName === 'discover_code_files') {
    // Check if result is JSON (server list) or Go code
    let isGoCode = false
    let isServerList = false
    let serverListData: unknown = null

    try {
      const parsed = JSON.parse(event.result)
      if (parsed.servers || parsed.custom_tools || parsed.virtual_tools) {
        // It's a server list JSON
        isServerList = true
        serverListData = parsed
      }
    } catch {
      // If it's not valid JSON, assume it's Go code
      isGoCode = true
    }

    if (isServerList && serverListData) {
      const data = serverListData as {
        servers?: Array<{ name: string; package: string; tools: string[] }>
        custom_tools?: { package: string; tools: string[] }
        virtual_tools?: { package: string; tools: string[] }
      }

      return (
        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3 min-w-0 flex-1">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
                  ✅ Discovery Complete{' '}
                  <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                    {event.turn && `• Turn: ${event.turn}`}
                    {event.tool_name && ` • Tool: ${event.tool_name}`}
                    {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
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

          <div className="mt-2 space-y-3">
            {/* MCP Servers */}
            {data.servers && data.servers.length > 0 && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  📦 MCP Servers ({data.servers.length})
                </div>
                <div className="space-y-2 max-h-96 overflow-y-auto">
                  {data.servers.map((server, idx) => (
                    <div key={idx} className="border-l-2 border-blue-300 dark:border-blue-700 pl-2">
                      <div className="font-mono text-sm font-semibold text-gray-800 dark:text-gray-200">
                        {server.name}
                      </div>
                      <div className="text-xs text-gray-600 dark:text-gray-400 mt-1">
                        Package: <span className="font-mono">{server.package}</span>
                      </div>
                      {server.tools && server.tools.length > 0 && (
                        <div className="mt-1">
                          <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">
                            Tools ({server.tools.length}):
                          </div>
                          <div className="flex flex-wrap gap-1">
                            {server.tools.map((tool, toolIdx) => (
                              <span
                                key={toolIdx}
                                className="text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded"
                              >
                                {tool}
                              </span>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Custom Tools */}
            {data.custom_tools && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  🔧 Custom Tools
                </div>
                <div className="text-xs text-gray-600 dark:text-gray-400">
                  Package: <span className="font-mono">{data.custom_tools.package}</span>
                </div>
                {data.custom_tools.tools && data.custom_tools.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {data.custom_tools.tools.map((tool, idx) => (
                      <span
                        key={idx}
                        className="text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 px-2 py-0.5 rounded"
                      >
                        {tool}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* Virtual Tools */}
            {data.virtual_tools && (
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2">
                  ⚡ Virtual Tools
                </div>
                <div className="text-xs text-gray-600 dark:text-gray-400">
                  Package: <span className="font-mono">{data.virtual_tools.package}</span>
                </div>
                {data.virtual_tools.tools && data.virtual_tools.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {data.virtual_tools.tools.map((tool, idx) => (
                      <span
                        key={idx}
                        className="text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 px-2 py-0.5 rounded"
                      >
                        {tool}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )
    }

    // It's Go code - display with syntax highlighting
    if (isGoCode || resultText.includes('package ') || resultText.includes('func ')) {
      const codeLines = resultText.split('\n')
      const lineCount = codeLines.length

      return (
        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
          <div className="flex items-center justify-between gap-2 py-0.5">
            <div className="flex items-center gap-2 min-w-0 flex-1">
              <div className="min-w-0 flex-1">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300">
                  ✅ Go Code Retrieved{' '}
                  <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                    {event.turn && `• Turn: ${event.turn}`}
                    {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
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
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="text-xs font-medium text-blue-700 dark:text-blue-300">
                  💻 Go Source Code
                </div>
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  {lineCount} line{lineCount !== 1 ? 's' : ''} • {resultText.length} chars
                </div>
              </div>
              <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto bg-gray-50 dark:bg-gray-900 p-3 rounded max-h-48 overflow-y-auto">
                {resultText}
              </pre>
            </div>
          </div>
        </div>
      )
    }
  }

  // Handle write_code tool response
  if (toolName === 'write_code') {
    // Check if result contains an error - use more precise pattern matching
    // Only check for error patterns at the start or in structured error messages
    const errorPatterns = [
      /^.*\*\*❌ EXECUTION ERROR\*\*/m,  // Execution error header
      /^.*BUILD ERROR.*$/m,               // Build error
      /^.*PLUGIN LOAD ERROR.*$/m,         // Plugin load error
      /^.*FUNCTION SIGNATURE ERROR.*$/m,  // Function signature error
      /^go run failed:/m,                 // Go run failure at start
      /^Error:.*go run failed/m,         // Error: go run failed
    ]
    
    // Check if result starts with error indicators or contains structured error messages
    const isError = errorPatterns.some(pattern => pattern.test(resultText)) ||
                    (resultText.trim().startsWith('Error:') && resultText.includes('go run')) ||
                    (resultText.includes('**❌ EXECUTION ERROR**'))

    const bgColor = isError 
      ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
      : 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
    
    const textColor = isError
      ? 'text-red-700 dark:text-red-300'
      : 'text-green-700 dark:text-green-300'
    
    const secondaryTextColor = isError
      ? 'text-red-600 dark:text-red-400'
      : 'text-green-600 dark:text-green-400'

    const statusIcon = isError ? '❌' : '✅'
    const statusText = isError ? 'Code Execution Failed' : 'Code Executed Successfully'

    return (
      <div className={`${bgColor} border rounded p-2`}>
        <div className="flex items-center justify-between gap-2 py-0.5">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className={`text-xs font-medium ${textColor}`}>
                {statusIcon} {statusText}{' '}
                <span className={`text-xs font-normal ${secondaryTextColor}`}>
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
              </div>
            </div>
          </div>

          {event.timestamp && (
            <div className={`text-xs ${secondaryTextColor} flex-shrink-0`}>
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
        </div>

        {/* Display output or error */}
        {resultText && resultText.trim() && (
          <div className="mt-2">
            <div className={`bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3`}>
              <div className="flex items-center justify-between mb-2">
                <div className={`text-xs font-medium ${isError ? 'text-red-700 dark:text-red-300' : 'text-green-700 dark:text-green-300'}`}>
                  {isError ? '🔨 Error Details' : '📝 Execution Output'}
                </div>
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  {resultText.length} chars
                </div>
              </div>
              <pre className={`text-xs font-mono whitespace-pre-wrap overflow-x-auto p-3 rounded max-h-96 overflow-y-auto ${
                isError 
                  ? 'text-red-800 dark:text-red-200 bg-red-50 dark:bg-red-900/30' 
                  : 'text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900'
              }`}>
                {resultText}
              </pre>
            </div>
          </div>
        )}
      </div>
    )
  }

  return null
}

