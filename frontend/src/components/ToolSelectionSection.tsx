import React, { useState, useCallback, useEffect } from 'react';
import { Checkbox } from './ui/checkbox';
import { Check, Loader2 } from 'lucide-react';
import { agentApi } from '../services/api';
import type { ToolDefinition } from '../stores/types';

interface ToolSelectionSectionProps {
  availableServers: string[];
  selectedServers: string[];
  selectedTools: string[]; // Array of "server:tool"
  onServerChange: (servers: string[]) => void;
  onToolChange: (tools: string[]) => void;
  stepId?: string; // Optional step ID for debugging
}

export const ToolSelectionSection: React.FC<ToolSelectionSectionProps> = ({
  availableServers,
  selectedServers,
  selectedTools,
  onServerChange,
  onToolChange,
  stepId,
}) => {
  
  const [expandedServers, setExpandedServers] = useState<Set<string>>(new Set());
  const [toolDetails, setToolDetails] = useState<Record<string, ToolDefinition[]>>({});
  const [loadingServers, setLoadingServers] = useState<Set<string>>(new Set());
  const [serverToolMode, setServerToolMode] = useState<Record<string, 'all' | 'specific'>>({});
  const [showDebug, setShowDebug] = useState(false);

  // Load tool details for a server
  const loadServerTools = useCallback(async (serverName: string, force = false) => {
    if (!force && toolDetails[serverName]) {
      return;
    }
    
    setLoadingServers(prev => new Set(prev).add(serverName));
    try {
      const response = await agentApi.getToolDetail(serverName);
      
      // Handle different response formats
      let serverTools: ToolDefinition[];
      if (Array.isArray(response)) {
        serverTools = response;
      } else if (response && typeof response === 'object' && 'tools' in response) {
        serverTools = (response as { tools: ToolDefinition[] }).tools || [];
      } else if (response && typeof response === 'object' && 'data' in response) {
        serverTools = (response as { data: ToolDefinition[] }).data || [];
      } else {
        console.warn(`[ToolSelection] Unexpected response format for ${serverName}:`, response);
        serverTools = [];
      }
      
      setToolDetails(prev => {
        const updated = {
          ...prev,
          [serverName]: serverTools
        };
        console.log('[ToolSelectionSection] ToolDetails updated for:', serverName, 'tools:', serverTools.length);
        return updated;
      });
    } catch (error) {
      console.error(`Failed to load tools for ${serverName}:`, error);
    } finally {
      setLoadingServers(prev => {
        const next = new Set(prev);
        next.delete(serverName);
        return next;
      });
    }
  }, [toolDetails]);

  // Sync server tool mode based on current selection
  // This runs whenever selectedTools or selectedServers change to keep mode in sync
  useEffect(() => {
    const newMode: Record<string, 'all' | 'specific'> = {};
    
    selectedServers.forEach(server => {
      // Check if server has the "all tools" marker
      const hasAllToolsMarker = selectedTools.includes(`${server}:*`);
      
      if (hasAllToolsMarker) {
        // Server is in "all tools" mode
        newMode[server] = 'all';
      } else {
        // Check if server has specific tools selected
        const serverTools = selectedTools.filter(t => 
          t.startsWith(`${server}:`) && !t.endsWith(':*')
        );
        // If specific tools are selected, use 'specific' mode; otherwise default to 'all'
        newMode[server] = serverTools.length > 0 ? 'specific' : 'all';
        
        // Load tool details if specific tools are selected
        if (serverTools.length > 0) {
          console.log(`[StepConfigDebug] Loading tools for ${server} (specific mode)`, {
            serverTools,
            selectedTools
          });
          loadServerTools(server);
        }
      }
    });
    
    // Only log if there's a "*" marker (indicates potential issue)
    if (selectedTools.some(t => t.includes(':*'))) {
      console.log('[StepConfigDebug] ⚠️ CRITICAL: Syncing modes - found "*" marker:', {
        selectedTools,
        selectedServers,
        newMode
      });
    }
    setServerToolMode(newMode);
    
    // Expand servers that have specific tools selected (not "all tools" mode)
    setExpandedServers(prev => {
      const newExpandedServers = new Set(prev);
      
      selectedServers.forEach(server => {
        const hasAllToolsMarker = selectedTools.includes(`${server}:*`);
        const serverTools = selectedTools.filter(t => 
          t.startsWith(`${server}:`) && !t.endsWith(':*')
        );
        
        // Only expand if we have specific tools (not in "all tools" mode)
        if (!hasAllToolsMarker && serverTools.length > 0) {
          console.log(`[StepConfigDebug] Auto-expanding ${server} (has ${serverTools.length} specific tools)`);
          newExpandedServers.add(server);
        }
      });
      
      // Keep previously expanded servers that are still selected
      prev.forEach(server => {
        if (selectedServers.includes(server) && !newExpandedServers.has(server)) {
          newExpandedServers.add(server);
        }
      });
      
      return newExpandedServers;
    });
  }, [selectedServers, selectedTools, loadServerTools]);

  // Auto-expand server when selected
  const expandServer = useCallback((serverName: string) => {
    setExpandedServers(prev => {
      const next = new Set(prev);
      next.add(serverName);
      return next;
    });
    loadServerTools(serverName);
  }, [loadServerTools]);

  // Handle server checkbox
  const handleServerToggle = useCallback((serverName: string) => {
    const isSelected = selectedServers.includes(serverName);
    
    if (isSelected) {
      // Remove server
      const newServers = selectedServers.filter(s => s !== serverName);
      onServerChange(newServers);
      
      // Remove all tools from this server (including "*" marker)
      const newTools = selectedTools.filter(t => !t.startsWith(`${serverName}:`));
      onToolChange(newTools);
      
      // Remove from server tool mode
      setServerToolMode(prev => {
        const next = { ...prev };
        delete next[serverName];
        return next;
      });
    } else {
      // Add server - check if we already have specific tools for this server
      // (this can happen if server was removed and re-added, or if config has specific tools)
      const existingServerTools = selectedTools.filter(t => 
        t.startsWith(`${serverName}:`) && !t.endsWith(':*')
      );
      const hasSpecificTools = existingServerTools.length > 0;
      
      onServerChange([...selectedServers, serverName]);
      
      if (hasSpecificTools) {
        // Keep existing specific tools, set mode to 'specific'
        console.log(`[StepConfigDebug] Server ${serverName} added with existing specific tools:`, existingServerTools);
        setServerToolMode(prev => ({
          ...prev,
          [serverName]: 'specific'
        }));
        // Don't add "*" marker - keep specific tools
      } else {
        // No specific tools - use default 'all' mode
        setServerToolMode(prev => ({
          ...prev,
          [serverName]: 'all'
        }));
        
        // Set "all tools" marker by default
        const newTools = [...selectedTools, `${serverName}:*`];
        onToolChange(newTools);
      }
      
      // Always expand when server is selected so user can choose tool mode
      expandServer(serverName);
    }
  }, [selectedServers, selectedTools, onServerChange, onToolChange, expandServer]);

  // Handle switching between "all tools" and "specific tools" for a server
  const handleServerToolModeChange = useCallback((serverName: string, mode: 'all' | 'specific') => {
    console.log('[StepConfigDebug] Mode change:', { serverName, mode });
    setServerToolMode(prev => {
      const newMode = { ...prev, [serverName]: mode };
      console.log('[StepConfigDebug] New mode state:', newMode);
      return newMode;
    });
    
    if (mode === 'all') {
      // Set special marker "server:*" to indicate "all tools" mode
      const newTools = selectedTools.filter(t => !t.startsWith(`${serverName}:`));
      newTools.push(`${serverName}:*`);
      console.log('[StepConfigDebug] Setting all tools mode:', newTools);
      onToolChange(newTools);
    } else {
      // Remove the special marker and switch to specific mode
      const newTools = selectedTools.filter(t => t !== `${serverName}:*`);
      console.log('[StepConfigDebug] Setting specific tools mode, loading tools for:', serverName);
      onToolChange(newTools);
      // Load tools for this server when switching to specific mode (force reload)
      loadServerTools(serverName, true).then(() => {
        console.log('[StepConfigDebug] Tools loaded for:', serverName);
      });
      // Expand the server when switching to specific mode so user can see tools
      expandServer(serverName);
    }
  }, [selectedTools, onToolChange, loadServerTools, expandServer]);

  // Handle tool checkbox
  const handleToolToggle = useCallback((serverName: string, toolName: string) => {
    const fullName = `${serverName}:${toolName}`;
    const isSelected = selectedTools.includes(fullName);
    
    if (isSelected) {
      onToolChange(selectedTools.filter(t => t !== fullName));
    } else {
      onToolChange([...selectedTools, fullName]);
    }
  }, [selectedTools, onToolChange]);

  // Handle "Select all tools" for a server
  const handleSelectAllServerTools = useCallback((serverName: string) => {
    const serverTools = toolDetails[serverName] || [];
    if (!Array.isArray(serverTools) || serverTools.length === 0) return;
    
    const serverToolNames = serverTools.map(t => `${serverName}:${t.name}`);
    
    const allSelected = serverToolNames.every(t => selectedTools.includes(t));
    
    if (allSelected) {
      // Deselect all
      const newTools = selectedTools.filter(t => !t.startsWith(`${serverName}:`));
      onToolChange(newTools);
    } else {
      // Select all
      const newTools = [...selectedTools];
      serverToolNames.forEach(t => {
        if (!newTools.includes(t)) {
          newTools.push(t);
        }
      });
      onToolChange(newTools);
    }
  }, [toolDetails, selectedTools, onToolChange]);

  // Check if all tools from a server are selected
  const areAllServerToolsSelected = useCallback((serverName: string) => {
    // Check if in "all tools" mode first
    if (selectedTools.includes(`${serverName}:*`)) {
      return true;
    }
    
    const serverTools = toolDetails[serverName] || [];
    if (!Array.isArray(serverTools) || serverTools.length === 0) return false;
    
    // Filter out "*" marker when counting specific tools
    const specificTools = selectedTools.filter(t => 
      t.startsWith(`${serverName}:`) && !t.endsWith(':*')
    );
    
    return specificTools.length > 0 && serverTools.every(t => selectedTools.includes(`${serverName}:${t.name}`));
  }, [toolDetails, selectedTools]);

  return (
    <div className="space-y-3">
      <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
        Tools Selection
      </label>

      <div className="text-xs text-gray-500 dark:text-gray-400 mb-2">
        Select servers and choose whether to use all tools or select specific tools for each server.
      </div>

      {/* Server and Tool List */}
      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-96 overflow-y-auto">
        {availableServers
          .sort((a, b) => {
            const aSelected = selectedServers.includes(a);
            const bSelected = selectedServers.includes(b);
            if (aSelected && !bSelected) return -1;
            if (!aSelected && bSelected) return 1;
            return a.localeCompare(b);
          })
          .map((serverName) => {
          const isExpanded = expandedServers.has(serverName);
          const isLoading = loadingServers.has(serverName);
          const isServerSelected = selectedServers.includes(serverName);
          const serverTools = toolDetails[serverName] || [];
          const allToolsSelected = areAllServerToolsSelected(serverName);
          
          // Calculate mode from selectedTools if not in serverToolMode (fallback for initial render)
          const hasAllToolsMarker = selectedTools.includes(`${serverName}:*`);
          const serverSpecificTools = selectedTools.filter(t => 
            t.startsWith(`${serverName}:`) && !t.endsWith(':*')
          );
          const calculatedMode = hasAllToolsMarker ? 'all' : (serverSpecificTools.length > 0 ? 'specific' : 'all');
          const toolMode = serverToolMode[serverName] || calculatedMode;
          const isServerToolsArray = Array.isArray(serverTools);
          
          // Debug logging (only when there's a mismatch or issue)
          if (isServerSelected && (toolMode !== calculatedMode || hasAllToolsMarker !== (calculatedMode === 'all'))) {
            console.log(`[StepConfigDebug] Rendering server: ${serverName} (MISMATCH DETECTED)`, {
              isExpanded,
              isServerSelected,
              toolMode,
              calculatedMode,
              serverToolMode: serverToolMode[serverName],
              hasAllToolsMarker,
              serverSpecificTools,
              selectedTools,
              isLoading,
              serverToolsLength: serverTools.length,
              toolDetailsKeys: Object.keys(toolDetails)
            });
          }

          return (
            <div key={serverName} className="border-b border-gray-200 dark:border-gray-700 last:border-b-0">
              {/* Server Row */}
              <div className="flex items-center p-3 hover:bg-gray-100 dark:hover:bg-gray-700">
                <Checkbox
                  id={`server-${serverName}`}
                  checked={isServerSelected}
                  onCheckedChange={() => handleServerToggle(serverName)}
                />
                
                <label
                  htmlFor={`server-${serverName}`}
                  className="ml-2 text-sm font-medium text-gray-900 dark:text-gray-100 cursor-pointer flex-1"
                  onClick={(e) => {
                    // Only expand if server is selected and not already expanded
                    if (isServerSelected && !isExpanded) {
                      e.preventDefault(); // Prevent checkbox toggle
                      expandServer(serverName);
                    }
                  }}
                >
                  {serverName}
                  {isServerSelected && isServerToolsArray && serverTools.length > 0 && (
                    <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                      ({toolMode === 'all' ? 'all tools' : `${selectedTools.filter(t => t.startsWith(`${serverName}:`) && !t.endsWith(':*')).length}/${serverTools.length} tools`})
                    </span>
                  )}
                </label>
              </div>

              {/* Tool Mode Selection and Tool List (when expanded) */}
              {isExpanded && isServerSelected && (
                <div className="pl-10 pr-3 pb-3 space-y-3">
                  {/* Tool Mode Selection */}
                  <div className="flex items-center space-x-4">
                    <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                      Tool selection:
                    </label>
                    <div className="flex items-center space-x-2">
                      <Checkbox
                        id={`all-tools-${serverName}`}
                        checked={toolMode === 'all'}
                        onCheckedChange={(checked) => {
                          if (checked) {
                            handleServerToolModeChange(serverName, 'all');
                          }
                        }}
                      />
                      <label htmlFor={`all-tools-${serverName}`} className="text-sm cursor-pointer">
                        Use all tools
                      </label>
                    </div>
                    <div className="flex items-center space-x-2">
                      <Checkbox
                        id={`specific-tools-${serverName}`}
                        checked={toolMode === 'specific'}
                        onCheckedChange={(checked) => {
                          if (checked) {
                            handleServerToolModeChange(serverName, 'specific');
                          }
                        }}
                      />
                      <label htmlFor={`specific-tools-${serverName}`} className="text-sm cursor-pointer">
                        Select specific tools
                      </label>
                    </div>
                  </div>

                          {/* Tool List (only when specific mode is selected) */}
                          {toolMode === 'specific' && (
                            <div className="space-y-2">
                              {isLoading ? (
                        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 py-2">
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Loading tools...
                        </div>
                      ) : isServerToolsArray && serverTools.length > 0 ? (
                        <>
                          {/* Select All Tools Button */}
                          <button
                            type="button"
                            onClick={() => handleSelectAllServerTools(serverName)}
                            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 flex items-center gap-1"
                          >
                            {allToolsSelected ? (
                              <>
                                <Check className="w-3 h-3" />
                                Deselect all
                              </>
                            ) : (
                              <>Select all tools</>
                            )}
                          </button>
                          
                          {serverTools.map((tool) => {
                            const fullName = `${serverName}:${tool.name}`;
                            const isToolSelected = selectedTools.includes(fullName);
                            
                            return (
                              <div key={tool.name} className="flex items-start space-x-2">
                                <Checkbox
                                  id={`tool-${fullName}`}
                                  checked={isToolSelected}
                                  onCheckedChange={() => handleToolToggle(serverName, tool.name)}
                                  className="mt-1"
                                />
                                <label
                                  htmlFor={`tool-${fullName}`}
                                  className="text-sm cursor-pointer flex-1"
                                >
                                  <div className="font-medium text-gray-900 dark:text-gray-100">{tool.name}</div>
                                  {tool.description && (
                                    <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                                      {tool.description}
                                    </div>
                                  )}
                                </label>
                              </div>
                            );
                          })}
                        </>
                      ) : (
                        <div className="text-sm text-gray-500 dark:text-gray-400 py-2">
                          {isServerToolsArray ? 'No tools available for this server' : 'Error loading tools for this server'}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Selection Summary */}
      {selectedTools.length > 0 && (
        <div className="text-xs text-gray-500 dark:text-gray-400 mt-2">
          Selected: {selectedTools.length} tool{selectedTools.length !== 1 ? 's' : ''} from {selectedServers.length} server{selectedServers.length !== 1 ? 's' : ''}
        </div>
      )}

      {/* Debug Panel */}
      <div className="mt-4 pt-4 border-t border-gray-300 dark:border-gray-600">
        <button
          type="button"
          onClick={() => setShowDebug(!showDebug)}
          className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 flex items-center gap-1"
        >
          {showDebug ? '▼' : '▶'} Debug Info
        </button>
        {showDebug && (
          <div className="mt-2 p-3 bg-gray-100 dark:bg-gray-800 rounded-md border border-gray-300 dark:border-gray-700">
            <pre className="text-xs text-gray-700 dark:text-gray-300 overflow-auto max-h-96">
              {JSON.stringify({
                stepId: stepId || 'N/A',
                selectedServers,
                selectedTools,
                selectedToolsLength: selectedTools.length,
                selectedToolsDetails: selectedTools.map(t => {
                  const [server, tool] = t.split(':');
                  return { server, tool, full: t };
                }),
                serverToolMode,
                expandedServers: Array.from(expandedServers),
                loadingServers: Array.from(loadingServers),
                toolDetailsKeys: Object.keys(toolDetails),
                toolDetailsCounts: Object.entries(toolDetails).reduce((acc, [server, tools]) => {
                  acc[server] = tools.length;
                  return acc;
                }, {} as Record<string, number>),
                // Calculate mode for each server
                calculatedModes: selectedServers.reduce((acc, server) => {
                  const hasAllToolsMarker = selectedTools.includes(`${server}:*`);
                  const serverTools = selectedTools.filter(t => 
                    t.startsWith(`${server}:`) && !t.endsWith(':*')
                  );
                  acc[server] = {
                    mode: hasAllToolsMarker ? 'all' : (serverTools.length > 0 ? 'specific' : 'all'),
                    hasAllToolsMarker,
                    specificTools: serverTools,
                    specificToolsCount: serverTools.length
                  };
                  return acc;
                }, {} as Record<string, { mode: 'all' | 'specific'; hasAllToolsMarker: boolean; specificTools: string[]; specificToolsCount: number }>)
              }, null, 2)}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
};