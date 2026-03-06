import { Server, Settings } from 'lucide-react'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { OAuthStatusBadge } from './OAuthStatusBadge'
import MCPToolApiTester from './MCPToolApiTester'
import { useMCPStore } from '../stores'

// Tool detail type for cached data
type ToolDetail = {
  name: string;
  description: string;
  server: string;
  parameters?: Record<string, {
    description?: string;
    type?: string;
  }>;
  required?: string[];
};

// Sanitize tool descriptions
const sanitizeDescription = (description: string | undefined): string => {
  if (!description) return '';
  const standardTags = ['a', 'b', 'i', 'u', 'strong', 'em', 'code', 'pre', 'br', 'hr', 'p', 'div', 'span', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'img', 'sup', 'sub'];
  return description.replace(/<\/?([a-zA-Z][a-zA-Z0-9_-]*)[^>]*>/g, (match, tagName) => {
    if (standardTags.includes(tagName.toLowerCase())) {
      return match;
    }
    return match.replace(/</g, '&lt;').replace(/>/g, '&gt;');
  });
};

interface MCPDetailsModalProps {
  onClose: () => void
  onOpenConfigEditor: () => void
}

export default function MCPDetailsModal({ onClose, onOpenConfigEditor }: MCPDetailsModalProps) {
  const {
    toolList,
    expandedServers,
    setExpandedServers,
    selectedTool,
    setSelectedTool,
    toolDetails,
    loadingToolDetails,
    showApiTester,
    setShowApiTester,
    getServerGroups,
    loadToolDetails,
    refreshTools
  } = useMCPStore()

  return (
    <>
      <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-6xl h-[90vh] overflow-y-auto">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              MCP Server Details
            </h3>
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  onClose()
                  onOpenConfigEditor()
                }}
                className="px-3 py-1.5 text-sm font-medium text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50 rounded-md transition-colors flex items-center gap-2"
              >
                <Settings className="w-4 h-4" />
                Configure MCP Server
              </button>
              <button
                onClick={onClose}
                className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                ✕
              </button>
            </div>
          </div>

          {toolList.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
              <Server className="w-12 h-12 mb-4 opacity-50" />
              <p className="text-lg font-medium mb-2">No MCP servers configured</p>
              <p className="text-sm text-center mb-4">
                Add MCP servers to extend your agent's capabilities
              </p>
              <button
                onClick={() => {
                  onClose()
                  onOpenConfigEditor()
                }}
                className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md transition-colors flex items-center gap-2"
              >
                <Settings className="w-4 h-4" />
                Configure MCP Server
              </button>
            </div>
          ) : (
            Object.entries(getServerGroups()).map(([serverName, tools]) => (
              <div key={serverName} className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg p-3 mb-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className="w-3 h-3 rounded-full bg-gradient-to-r from-blue-500 to-purple-500"></div>
                    <h4 className="text-sm font-semibold">{serverName}</h4>
                    <span className="text-xs text-gray-500 bg-gray-200 dark:bg-gray-700 px-2 py-1 rounded-full">
                      {tools[0].function_names ? tools[0].function_names.length : 0} tools
                    </span>
                    <span className={`w-2 h-2 rounded-full ${
                      tools[0].status === 'ok' ? 'bg-green-500' : 'bg-red-500'
                    }`}></span>
                    <OAuthStatusBadge
                      serverName={serverName}
                      // eslint-disable-next-line @typescript-eslint/no-explicit-any
                      requiresOAuth={(tools[0] as any).requires_oauth}
                      onAuthChange={(valid) => {
                        if (valid) refreshTools();
                      }}
                    />
                  </div>

                  <div className="flex items-center gap-2">
                    {tools[0].function_names && tools[0].function_names.length > 0 && (
                      <button
                        onClick={() => {
                          const isCurrentlyExpanded = expandedServers.has(serverName)
                          if (isCurrentlyExpanded) {
                            const newSet = new Set(expandedServers)
                            newSet.delete(serverName)
                            setExpandedServers(newSet)
                          } else {
                            setExpandedServers(new Set([...expandedServers, serverName]))
                          }
                        }}
                        className="flex items-center gap-1 px-2 py-1 text-xs font-medium text-gray-600 dark:text-gray-400 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded transition-colors"
                      >
                        <span className="text-xs">
                          {expandedServers.has(serverName) ? '▼' : '▶'}
                        </span>
                        <span>Tools</span>
                      </button>
                    )}
                  </div>
                </div>

                {expandedServers.has(serverName) && tools[0].function_names && tools[0].function_names.length > 0 && (
                  <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
                    <div className="grid grid-cols-1 gap-1">
                      {tools[0].function_names.map((toolName: string, index: number) => {
                        const toolDetail = tools[0].tools?.find((t: ToolDetail) => t.name === toolName) ||
                                         toolDetails[serverName]?.tools?.find((t: ToolDetail) => t.name === toolName)
                        const isSelected = selectedTool?.serverName === serverName && selectedTool?.toolName === toolName

                        return (
                          <div key={index} className="space-y-1">
                            <div
                              className={`flex items-center justify-between p-2 rounded-md border cursor-pointer transition-colors ${
                                isSelected
                                  ? 'bg-blue-50 dark:bg-blue-900/30 border-blue-200 dark:border-blue-700'
                                  : 'bg-gray-50 dark:bg-gray-800/50 border-gray-100 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700'
                              }`}
                              onClick={async () => {
                                if (isSelected) {
                                  setSelectedTool(null)
                                } else {
                                  setSelectedTool({serverName, toolName})
                                  if (!tools[0].tools && !toolDetails[serverName]) {
                                    await loadToolDetails(serverName)
                                  }
                                }
                              }}
                            >
                              <div className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-blue-500"></span>
                                <span className="text-xs font-mono text-gray-700 dark:text-gray-300">
                                  {toolName}
                                </span>
                                {loadingToolDetails.has(serverName) && !tools[0].tools ? (
                                  <span className="text-xs text-gray-500 dark:text-gray-400 flex items-center gap-1">
                                    <div className="w-3 h-3 border border-gray-300 border-t-blue-500 rounded-full animate-spin"></div>
                                    Loading details...
                                  </span>
                                ) : toolDetail?.description ? (
                                  <span className="text-xs text-gray-500 dark:text-gray-400 truncate max-w-[200px]">
                                    {toolDetail.description.replace(/<[^>]*>/g, '').substring(0, 50)}...
                                  </span>
                                ) : null}
                              </div>
                              <div className="flex items-center gap-2">
                                <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">
                                  tool
                                </span>
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    setShowApiTester({ serverName, toolName, toolDetail })
                                  }}
                                  className="px-2 py-1 text-xs font-medium text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/30 hover:bg-green-100 dark:hover:bg-green-900/50 rounded transition-colors"
                                >
                                  Test API
                                </button>
                                <span className="text-xs text-gray-400">
                                  {isSelected ? '▼' : '▶'}
                                </span>
                              </div>
                            </div>

                            {isSelected && toolDetail && (
                              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-md p-3 mt-2">
                                <div className="space-y-2">
                                  <div className="flex items-center justify-between">
                                    <h5 className="text-sm font-semibold text-blue-900 dark:text-blue-100">
                                      {toolDetail.name}
                                    </h5>
                                    <span className="px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">
                                      {toolDetail.server}
                                    </span>
                                  </div>
                                  <div className="text-sm text-blue-800 dark:text-blue-200 max-h-[300px] overflow-y-auto">
                                    <MarkdownRenderer
                                      content={sanitizeDescription(toolDetail.description)}
                                      className="text-sm"
                                    />
                                  </div>
                                  {toolDetail.parameters && (
                                    <div className="mt-2">
                                      <h6 className="text-xs font-semibold text-blue-900 dark:text-blue-100 mb-1">
                                        Parameters:
                                      </h6>
                                      <div className="space-y-2">
                                        {Object.entries(toolDetail.parameters).map(([paramName, paramInfo]) => (
                                          <div key={paramName} className="bg-blue-100 dark:bg-blue-800 p-2 rounded border">
                                            <div className="flex items-center justify-between mb-1">
                                              <span className="text-xs font-semibold text-blue-900 dark:text-blue-100">
                                                {paramName}
                                                {toolDetail.required?.includes(paramName) && (
                                                  <span className="text-red-500 ml-1">*</span>
                                                )}
                                              </span>
                                              <span className="text-xs text-blue-700 dark:text-blue-300 bg-blue-200 dark:bg-blue-700 px-2 py-1 rounded">
                                                {paramInfo.type || 'unknown'}
                                              </span>
                                            </div>
                                            {paramInfo.description && (
                                              <div className="text-xs text-blue-700 dark:text-blue-300">
                                                <MarkdownRenderer
                                                  content={sanitizeDescription(paramInfo.description)}
                                                  className="text-xs"
                                                />
                                              </div>
                                            )}
                                          </div>
                                        ))}
                                      </div>
                                    </div>
                                  )}
                                </div>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      </div>

      {/* MCP Tool API Tester Modal */}
      {showApiTester && (
        <MCPToolApiTester
          isOpen={!!showApiTester}
          onClose={() => setShowApiTester(null)}
          serverName={showApiTester.serverName}
          toolName={showApiTester.toolName}
          toolDetail={showApiTester.toolDetail}
        />
      )}
    </>
  )
}
