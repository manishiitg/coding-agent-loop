import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Checkbox } from './ui/checkbox';
import { Check, Loader2 } from 'lucide-react';
import { useToolSelectionStore } from '../stores/useToolSelectionStore';
import { useMCPStore } from '../stores';
import { agentApi } from '../services/api';

interface ToolSelectionSectionProps {
  availableServers: string[];
  selectedServers: string[];
  selectedTools: string[]; // Array of "server:tool"
  onServerChange: (servers: string[]) => void;
  onToolChange: (tools: string[]) => void;
  stepId?: string; // Optional step ID for debugging
  agentMode: string; // Add agentMode prop
}

export const ToolSelectionSection: React.FC<ToolSelectionSectionProps> = ({
  availableServers,
  selectedServers,
  selectedTools,
  onServerChange,
  onToolChange,
  stepId,
}) => {
  // Generate instance ID from stepId or use a default
  const instanceId = useMemo(() => stepId || `preset-${Date.now()}`, [stepId]);
  
  // Get store state and actions
  // Select instance directly - use a stable selector to avoid infinite loop
  const rawInstance = useToolSelectionStore((state) => {
    const instance = state.instances[instanceId];
    // Return the instance directly (Zustand will handle memoization)
    return instance;
  });
  
  // Get actions directly from store (not as selectors to avoid re-renders)
  const storeActions = useMemo(() => ({
    syncServerToolMode: useToolSelectionStore.getState().syncServerToolMode,
    loadServerTools: useToolSelectionStore.getState().loadServerTools,
    getServerTools: useToolSelectionStore.getState().getServerTools,
    isServerLoading: useToolSelectionStore.getState().isServerLoading,
    toggleExpandedServer: useToolSelectionStore.getState().toggleExpandedServer,
    updateServerToolMode: useToolSelectionStore.getState().updateServerToolMode,
    removeInstance: useToolSelectionStore.getState().removeInstance,
    getInstanceState: useToolSelectionStore.getState().getInstanceState,
  }), []);
  
  // Get MCP server connection status
  const mcpToolList = useMCPStore((state) => state.toolList);
  const serverStatusMap = useMemo(() => {
    const map: Record<string, 'ok' | 'error' | 'loading' | 'unknown'> = {};
    mcpToolList.forEach(tool => {
      if (tool.server) {
        const current = map[tool.server];
        const toolStatus = (tool.status as string) || 'unknown';
        // ok wins over error wins over loading wins over unknown
        if (!current || (toolStatus === 'ok') || (current !== 'ok' && toolStatus === 'error')) {
          map[tool.server] = toolStatus as 'ok' | 'error' | 'loading' | 'unknown';
        }
      }
    });
    return map;
  }, [mcpToolList]);

  // Camofox browser connection status (mirrors chat input behaviour)
  const [camofoxConnected, setCamofoxConnected] = useState<boolean | null>(null)
  const [camofoxStarting, setCamofoxStarting] = useState(false)
  const camofoxCheckRef = useRef(false)
  const hasCamofox = availableServers.includes('camofox')

  useEffect(() => {
    if (!hasCamofox || camofoxCheckRef.current) return
    camofoxCheckRef.current = true
    let cancelled = false
    const check = async () => {
      setCamofoxStarting(true)
      try {
        const result = await agentApi.startCamofox(true)
        if (!cancelled) setCamofoxConnected(result.connected)
      } catch {
        if (!cancelled) setCamofoxConnected(false)
      } finally {
        if (!cancelled) setCamofoxStarting(false)
      }
    }
    const t = setTimeout(check, 300)
    return () => { cancelled = true; clearTimeout(t) }
  }, [hasCamofox])

  // Use fallback instance to avoid null checks everywhere
  // Create a stable default instance that won't change
  const defaultInstance = useMemo(() => ({
    expandedServers: new Set<string>(),
    serverToolMode: {} as Record<string, 'all' | 'specific'>,
    loadingServers: new Set<string>(),
  }), []);
  
  const instance = rawInstance || defaultInstance;

  // Initialize instance if it doesn't exist
  useEffect(() => {
    if (!rawInstance) {
      storeActions.getInstanceState(instanceId);
    }
  }, [rawInstance, instanceId, storeActions]);
  
  // Sync mode when selectedServers or selectedTools change
  useEffect(() => {
    if (rawInstance) {
      storeActions.syncServerToolMode(instanceId, selectedServers, selectedTools);
    }
  }, [rawInstance, instanceId, selectedServers, selectedTools, storeActions]);
  
  // Auto-load tools for servers in specific mode that haven't been loaded yet
  useEffect(() => {
    if (!rawInstance) return;
    
    selectedServers.forEach(serverName => {
      // Calculate mode the same way as in render (check store mode first, then calculated)
      const hasAllToolsMarker = selectedTools.includes(`${serverName}:*`);
      const serverSpecificTools = selectedTools.filter(t => 
        t.startsWith(`${serverName}:`) && !t.endsWith(':*')
      );
      const calculatedMode = hasAllToolsMarker ? 'all' : (serverSpecificTools.length > 0 ? 'specific' : 'all');
      // Access serverToolMode from rawInstance to avoid dependency on instance object
      const toolMode = rawInstance.serverToolMode[serverName] || calculatedMode;
      
      // If in specific mode and tools haven't been loaded, load them
      if (toolMode === 'specific') {
        const toolsFromStore = storeActions.getServerTools(serverName);
        const hasLoadedTools = toolsFromStore !== undefined;
        const isLoading = storeActions.isServerLoading(instanceId, serverName);
        
        if (!hasLoadedTools && !isLoading) {
          console.log('[ToolSelection] Auto-loading tools for server in specific mode:', serverName);
          // Trigger load
          const setLoadingServer = useToolSelectionStore.getState().setLoadingServer;
          setLoadingServer(instanceId, serverName, true);
          storeActions.loadServerTools(serverName)
            .then(() => {
              console.log('[ToolSelection] Tools loaded successfully for:', serverName);
              setLoadingServer(instanceId, serverName, false);
            })
            .catch(err => {
              console.error('[ToolSelection] Failed to load tools:', serverName, err);
              setLoadingServer(instanceId, serverName, false);
            });
        }
      }
    });
  }, [rawInstance, instanceId, selectedServers, selectedTools, storeActions]);
  
  // Cleanup on unmount
  useEffect(() => {
    return () => {
      storeActions.removeInstance(instanceId);
    };
  }, [instanceId, storeActions]);
  
  // Auto-expand server when selected
  const expandServer = useCallback((serverName: string) => {
    if (!instance.expandedServers.has(serverName)) {
      storeActions.toggleExpandedServer(instanceId, serverName);
    }
    // Load tools if not already loaded
    const tools = storeActions.getServerTools(serverName);
    if (!tools) {
      // Set loading state before loading
      const setLoadingServer = useToolSelectionStore.getState().setLoadingServer;
      setLoadingServer(instanceId, serverName, true);
      storeActions.loadServerTools(serverName)
        .then(() => {
          setLoadingServer(instanceId, serverName, false);
        })
        .catch(err => {
          console.error('[ToolSelection] Failed to load tools:', serverName, err);
          setLoadingServer(instanceId, serverName, false);
        });
    }
  }, [instanceId, instance.expandedServers, storeActions]);

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
    } else {
      // Add server - check if we already have specific tools for this server
      const existingServerTools = selectedTools.filter(t => 
        t.startsWith(`${serverName}:`) && !t.endsWith(':*')
      );
      const hasSpecificTools = existingServerTools.length > 0;
      
      onServerChange([...selectedServers, serverName]);
      
      if (!hasSpecificTools) {
        // No specific tools - use default 'all' mode and set "all tools" marker
        const newTools = [...selectedTools, `${serverName}:*`];
        onToolChange(newTools);
      }
      
      // Always expand when server is selected so user can choose tool mode
      expandServer(serverName);
    }
  }, [selectedServers, selectedTools, onServerChange, onToolChange, expandServer]);

  // Handle switching between "all tools" and "specific tools" for a server
  const handleServerToolModeChange = useCallback((serverName: string, mode: 'all' | 'specific') => {
    console.log('[ToolSelection] Mode change:', serverName, '->', mode);
    
    // Update mode in store immediately
    storeActions.updateServerToolMode(instanceId, serverName, mode);
    
    if (mode === 'all') {
      // Set special marker "server:*" to indicate "all tools" mode
      // Remove all specific tools for this server
      const newTools = selectedTools.filter(t => !t.startsWith(`${serverName}:`));
      newTools.push(`${serverName}:*`);
      console.log('[ToolSelection] Setting all tools mode, newTools:', newTools);
      onToolChange(newTools);
    } else {
      // Remove the special marker and switch to specific mode
      // Keep any existing specific tools for this server
      const newTools = selectedTools.filter(t => t !== `${serverName}:*`);
      console.log('[ToolSelection] Setting specific tools mode, newTools:', newTools);
      
      onToolChange(newTools);
      // Load tools for this server when switching to specific mode (force reload)
      const setLoadingServer = useToolSelectionStore.getState().setLoadingServer;
      setLoadingServer(instanceId, serverName, true);
      storeActions.loadServerTools(serverName, true)
        .then(() => {
          setLoadingServer(instanceId, serverName, false);
        })
        .catch(err => {
          console.error('[ToolSelection] Failed to load tools:', serverName, err);
          setLoadingServer(instanceId, serverName, false);
        });
      // Expand the server when switching to specific mode so user can see tools
      expandServer(serverName);
    }
  }, [instanceId, selectedTools, onToolChange, expandServer, storeActions]);

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
    const serverTools = storeActions.getServerTools(serverName) || [];
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
  }, [storeActions, selectedTools, onToolChange]);

  // Check if all tools from a server are selected
  const areAllServerToolsSelected = useCallback((serverName: string) => {
    // Check if in "all tools" mode first
    if (selectedTools.includes(`${serverName}:*`)) {
      return true;
    }
    
    const serverTools = storeActions.getServerTools(serverName) || [];
    if (!Array.isArray(serverTools) || serverTools.length === 0) return false;
    
    // Filter out "*" marker when counting specific tools
    const specificTools = selectedTools.filter(t => 
      t.startsWith(`${serverName}:`) && !t.endsWith(':*')
    );
    
    return specificTools.length > 0 && serverTools.every(t => selectedTools.includes(`${serverName}:${t.name}`));
  }, [storeActions, selectedTools]);

  return (
    <div className="space-y-3">
      <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 mb-2">
        MCP Server Selection
      </label>

      <div className="text-xs text-gray-500 dark:text-gray-400 mb-2">
        Select servers and choose whether to use all tools or select specific tools for each server.
      </div>

      {/* Server and Tool List */}
      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-96 overflow-y-auto">
        {availableServers
          .filter(serverName => serverName !== 'mcp')
          .sort((a, b) => {
            const aSelected = selectedServers.includes(a);
            const bSelected = selectedServers.includes(b);
            if (aSelected && !bSelected) return -1;
            if (!aSelected && bSelected) return 1;
            return a.localeCompare(b);
          })
          .map((serverName) => {
          const isExpanded = instance.expandedServers.has(serverName);
          const isLoading = storeActions.isServerLoading(instanceId, serverName);
          const isServerSelected = selectedServers.includes(serverName);
          // Check if tools have been loaded (undefined = not loaded yet, array = loaded)
          const toolsFromStore = storeActions.getServerTools(serverName);
          const hasLoadedTools = toolsFromStore !== undefined;
          const serverTools = hasLoadedTools ? toolsFromStore : [];
          const allToolsSelected = areAllServerToolsSelected(serverName);
          
          // Calculate mode from selectedTools if not in serverToolMode (fallback for initial render)
          const hasAllToolsMarker = selectedTools.includes(`${serverName}:*`);
          const serverSpecificTools = selectedTools.filter(t => 
            t.startsWith(`${serverName}:`) && !t.endsWith(':*')
          );
          const calculatedMode = hasAllToolsMarker ? 'all' : (serverSpecificTools.length > 0 ? 'specific' : 'all');
          const toolMode = instance.serverToolMode[serverName] || calculatedMode;
          const isServerToolsArray = Array.isArray(serverTools);

          return (
            <div key={serverName} className="border-b border-gray-200 dark:border-gray-700 last:border-b-0">
              {/* Server Row */}
              <div className="flex flex-col p-3 hover:bg-gray-100 dark:hover:bg-gray-700">
                <div className="flex items-center">
                <Checkbox
                  id={`server-${serverName}`}
                  checked={isServerSelected}
                  onCheckedChange={() => handleServerToggle(serverName)}
                />
                
                <label
                  htmlFor={`server-${serverName}`}
                  className="ml-2 text-sm font-medium text-gray-900 dark:text-gray-100 cursor-pointer flex-1 select-none flex items-center gap-1.5"
                  onClick={(e) => {
                    // Only expand if server is selected and not already expanded
                    if (isServerSelected && !isExpanded) {
                      e.stopPropagation();
                      expandServer(serverName);
                    }
                  }}
                >
                  {/* Connection status dot */}
                  {(() => {
                    const st = serverStatusMap[serverName];
                    if (st === 'ok') return <span className="w-2 h-2 rounded-full bg-green-500 flex-shrink-0" title="Connected" />;
                    if (st === 'error') return <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" title="Error" />;
                    if (st === 'loading') return <span className="w-2 h-2 rounded-full bg-yellow-400 flex-shrink-0" title="Connecting..." />;
                    return <span className="w-2 h-2 rounded-full bg-gray-400 flex-shrink-0" title="Unknown / not started" />;
                  })()}
                  {serverName}
                  {isServerSelected && isServerToolsArray && serverTools.length > 0 && (
                    <span className="ml-1 text-xs text-gray-500 dark:text-gray-400">
                      ({toolMode === 'all' ? 'all tools' : `${selectedTools.filter(t => t.startsWith(`${serverName}:`) && !t.endsWith(':*')).length}/${serverTools.length} tools`})
                    </span>
                  )}
                </label>
                </div>
                {/* Camofox browser status — mirrors chat input */}
                {serverName === 'camofox' && (
                  <div className="ml-6 mt-1">
                    {camofoxStarting && (
                      <div className="text-xs text-yellow-500 flex items-center gap-1">
                        <Loader2 className="w-3 h-3 animate-spin" />
                        Checking camofox-browser...
                      </div>
                    )}
                    {!camofoxStarting && camofoxConnected === true && (
                      <div className="text-xs text-green-500 flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full bg-green-500 flex-shrink-0" />
                        camofox-browser connected
                      </div>
                    )}
                    {!camofoxStarting && camofoxConnected === false && (
                      <div className="text-xs text-red-400 flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                        camofox-browser not running
                      </div>
                    )}
                  </div>
                )}
              </div>

              {/* Tool Mode Selection and Tool List (when expanded) */}
              {isExpanded && isServerSelected && (
                <div className="pl-10 pr-3 pb-3 space-y-3">
                  {/* Tool Mode Selection */}
                  <div className="flex items-center space-x-4">
                    <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                      Tool selection:
                    </label>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        console.log('[ToolSelection] All tools button clicked:', serverName);
                        handleServerToolModeChange(serverName, 'all');
                      }}
                      className={`flex items-center space-x-1.5 px-2 py-1 rounded border transition-colors ${
                        toolMode === 'all'
                          ? 'bg-blue-50 dark:bg-blue-900/30 border-blue-300 dark:border-blue-700 text-blue-700 dark:text-blue-300'
                          : 'bg-gray-50 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
                      }`}
                    >
                      <div className={`w-3.5 h-3.5 rounded border-2 flex items-center justify-center flex-shrink-0 ${
                        toolMode === 'all'
                          ? 'border-blue-600 dark:border-blue-400 bg-blue-600 dark:bg-blue-400'
                          : 'border-gray-400 dark:border-gray-500'
                      }`}>
                        {toolMode === 'all' && (
                          <Check className="w-2.5 h-2.5 text-white" />
                        )}
                      </div>
                      <span className="text-xs font-medium whitespace-nowrap">Use all tools</span>
                    </button>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        console.log('[ToolSelection] Specific tools button clicked:', serverName);
                        handleServerToolModeChange(serverName, 'specific');
                      }}
                      className={`flex items-center space-x-1.5 px-2 py-1 rounded border transition-colors ${
                        toolMode === 'specific'
                          ? 'bg-blue-50 dark:bg-blue-900/30 border-blue-300 dark:border-blue-700 text-blue-700 dark:text-blue-300'
                          : 'bg-gray-50 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
                      }`}
                    >
                      <div className={`w-3.5 h-3.5 rounded border-2 flex items-center justify-center flex-shrink-0 ${
                        toolMode === 'specific'
                          ? 'border-blue-600 dark:border-blue-400 bg-blue-600 dark:bg-blue-400'
                          : 'border-gray-400 dark:border-gray-500'
                      }`}>
                        {toolMode === 'specific' && (
                          <Check className="w-2.5 h-2.5 text-white" />
                        )}
                      </div>
                      <span className="text-xs font-medium whitespace-nowrap">Select specific tools</span>
                    </button>
                  </div>

                  {/* Tool List (only when specific mode is selected) */}
                  {toolMode === 'specific' && (
                    <div className="space-y-2">
                      {isLoading || !hasLoadedTools ? (
                        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 py-2">
                          <Loader2 className="w-4 h-4 animate-spin" />
                          Loading tools...
                        </div>
                      ) : serverTools.length > 0 ? (
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
                              <div 
                                key={tool.name} 
                                className="flex items-start space-x-2"
                              >
                                <Checkbox
                                  id={`tool-${fullName}`}
                                  checked={isToolSelected}
                                  onCheckedChange={() => handleToolToggle(serverName, tool.name)}
                                  className="mt-1"
                                />
                                <label
                                  htmlFor={`tool-${fullName}`}
                                  className="text-sm cursor-pointer flex-1 select-none"
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
                          No tools available for this server
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

    </div>
  );
};
