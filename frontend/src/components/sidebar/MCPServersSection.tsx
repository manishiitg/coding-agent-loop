import { Server, Loader2, AlertCircle, Settings } from 'lucide-react'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import MCPConfigPopup from '../MCPConfigPopup'
import MCPToolApiTester from '../MCPToolApiTester'
import { OAuthStatusBadge } from '../OAuthStatusBadge'
import { useMCPStore } from '../../stores'

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

// Sanitize tool descriptions by escaping XML-like tags that aren't standard HTML
// This prevents ReactMarkdown from breaking on custom tags like <example>, <preserve>, etc.
const sanitizeDescription = (description: string | undefined): string => {
  if (!description) return '';
  // Escape custom XML-like tags by converting < to &lt; for non-standard HTML tags
  // Keep standard markdown-compatible HTML tags
  const standardTags = ['a', 'b', 'i', 'u', 'strong', 'em', 'code', 'pre', 'br', 'hr', 'p', 'div', 'span', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'img', 'sup', 'sub'];
  return description.replace(/<\/?([a-zA-Z][a-zA-Z0-9_-]*)[^>]*>/g, (match, tagName) => {
    if (standardTags.includes(tagName.toLowerCase())) {
      return match;
    }
    // Escape the tag
    return match.replace(/</g, '&lt;').replace(/>/g, '&gt;');
  });
};

export default function MCPServersSection() {
  
  // Store subscriptions
  const {
    toolList,
    enabledServers,
    setEnabledServers,
    isLoadingTools,
    toolsError,
    showMCPDetails,
    setShowMCPDetails,
    expandedServers,
    setExpandedServers,
    selectedTool,
    setSelectedTool,
    toolDetails,
    loadingToolDetails,
    showConfigEditor,
    setShowConfigEditor,
    showApiTester,
    setShowApiTester,
    getServerGroups,
    loadToolDetails,
    refreshTools
  } = useMCPStore()

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 text-gray-600 dark:text-gray-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">MCP Servers</span>
        </div>
        <span className="px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded-full">
          {toolList.length}
        </span>
      </div>


      {isLoadingTools && (
        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <Loader2 className="w-4 h-4 animate-spin" />
          <span>Loading servers...</span>
        </div>
      )}

      {toolsError && (
        <div className="flex items-center gap-2 text-sm text-red-500 dark:text-red-400">
          <AlertCircle className="w-4 h-4" />
          <span>Error: {toolsError}</span>
        </div>
      )}

      {!isLoadingTools && !toolsError && toolList.length > 0 && (
        <div className="space-y-2">
          <button
            onClick={() => setShowMCPDetails(!showMCPDetails)}
            className="w-full p-2 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors text-left"
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {new Set(toolList.map(tool => tool.server).filter(Boolean)).size} Servers
                </span>
              </div>
              <span className="text-xs text-gray-500">
                {showMCPDetails ? '▼' : '▶'}
              </span>
            </div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              {toolList.reduce((total, tool) => total + (tool.toolsEnabled || 0), 0)} tools available
            </div>
          </button>

      {/* MCP Server Details Modal Popup */}
      {showMCPDetails && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-6xl h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                MCP Server Details
              </h3>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => {
                    setShowMCPDetails(false)
                    setShowConfigEditor(true)
                  }}
                  className="px-3 py-1.5 text-sm font-medium text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50 rounded-md transition-colors flex items-center gap-2"
                >
                  <Settings className="w-4 h-4" />
                  Configure MCP Server
                </button>
                <button 
                  onClick={() => setShowMCPDetails(false)}
                  className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                >
                  ✕
                </button>
              </div>
            </div>
            
            {/* Server Groups with Individual Controls */}
            {Object.entries(getServerGroups()).map(([serverName, tools]) => (
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
                    {/* OAuth Status Badge - auto-detects if server requires OAuth */}
                    <OAuthStatusBadge
                      serverName={serverName}
                      requiresOAuth={(tools[0] as any).requires_oauth}
                      onAuthChange={(valid) => {
                        if (valid) {
                          // Refresh tools after successful OAuth authentication
                          refreshTools();
                        }
                      }}
                    />
                  </div>
                  
                  <div className="flex items-center gap-2">
                    {/* Show/Hide Tools Button */}
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
                        <span>
                          {expandedServers.has(serverName) ? 'Hide' : 'Show'}
                        </span>
                      </button>
                    )}
                    
                    {/* Toggle Enable/Disable */}
                    <button
                      onClick={() => {
                        const isCurrentlyEnabled = enabledServers.includes(serverName)
                        if (isCurrentlyEnabled) {
                          setEnabledServers(enabledServers.filter(s => s !== serverName))
                        } else {
                          setEnabledServers([...enabledServers, serverName])
                        }
                      }}
                      className={`w-12 h-6 rounded-full transition-all duration-200 ${
                        enabledServers.includes(serverName) 
                          ? 'bg-green-500' 
                          : 'bg-gray-300 dark:bg-gray-600'
                      }`}
                    >
                      <div className={`w-4 h-4 bg-white rounded-full transition-transform ${
                        enabledServers.includes(serverName) ? 'translate-x-6' : 'translate-x-1'
                      }`}></div>
                    </button>
                  </div>
                </div>
                
                {/* Expanded Tools Section */}
                {expandedServers.has(serverName) && tools[0].function_names && tools[0].function_names.length > 0 && (
                  <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
                    <div className="grid grid-cols-1 gap-1">
                      {tools[0].function_names.map((toolName: string, index: number) => {
                        // Find the detailed tool information from the main API response or cached data
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
                                  
                                  // Only fetch detailed tool information if not already available in main response
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
                                {/* Test API button */}
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
                            
                            {/* Tool Details Popup */}
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
            ))}
          </div>
        </div>
      )}
        </div>
      )}

      {/* MCP Config Popup Modal */}
      {showConfigEditor && (
        <MCPConfigPopup
          onConfigChange={() => {
            // Refresh tools after config change
            refreshTools();
          }}
          onClose={() => setShowConfigEditor(false)}
        />
      )}

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
    </div>
  )
}
