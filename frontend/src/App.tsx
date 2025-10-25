import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useCallback, useRef } from "react";
import { ThemeProvider } from "./contexts/ThemeContext.tsx";
import WorkspaceSidebar from "./components/WorkspaceSidebar";
import Workspace from "./components/Workspace.tsx";
import ChatArea, { type ChatAreaRef } from "./components/ChatArea.tsx";
import { MarkdownRenderer } from "./components/ui/MarkdownRenderer";
import { resetSessionId } from "./services/api";
import type { ActiveSessionInfo } from "./services/api-types";
import FileRevisionsModal from "./components/workspace/FileRevisionsModal";
import { ModeSelectionModal } from "./components/ModeSelectionModal";
import { useAppStore, useLLMStore, useMCPStore, useGlobalPresetStore, useWorkspaceStore } from "./stores";
import { useModeStore } from "./stores/useModeStore";
import { useLLMDefaults } from "./hooks/useLLMDefaults";
import "./App.css";

// Extend window interface for global functions
declare global {
  interface Window {
    highlightFile?: (filepath: string) => void;
    toggleAutoScroll?: () => void;
  }
}

const queryClient = new QueryClient();

function App() {
  // Ref for ChatArea component to access its methods
  const chatAreaRef = useRef<ChatAreaRef>(null)

  // Store subscriptions
  const { setAgentMode, setSidebarMinimized } = useAppStore()
  const { hasCompletedInitialSetup, selectedModeCategory } = useModeStore()
  
  // Load LLM defaults from backend
  useLLMDefaults()
  
  // App Store subscriptions for workspace and chat
  const {
    clearFileContext,
    setChatSessionId,
    setChatSessionTitle,
    setSelectedPresetId,
    sidebarMinimized,
    workspaceMinimized,
    setWorkspaceMinimized
  } = useAppStore()
  
  const {
    selectedFile,
    fileContent,
    loadingFileContent,
    showFileContent,
    setShowFileContent,
    showRevisionsModal,
    setShowRevisionsModal
  } = useWorkspaceStore()
  
  const { clearActivePreset, applyPreset, getActivePreset } = useGlobalPresetStore()

  const hasInitializedRef = useRef(false)

  // Initialize stores on mount
  useEffect(() => {
    // Prevent double calls in React StrictMode
    if (hasInitializedRef.current) {
      return
    }
    hasInitializedRef.current = true

    // Initialize MCP store
    useMCPStore.getState().refreshTools()
    
    // Initialize LLM store
    useLLMStore.getState().refreshAvailableLLMs()
    
    // Initialize global preset store
    useGlobalPresetStore.getState().refreshPresets()
  }, [])

  // Restore active presets after stores are initialized
  useEffect(() => {
    // Only restore presets if initial setup is completed and we have a mode category
    if (hasCompletedInitialSetup && selectedModeCategory) {
      // Add a small delay to ensure stores are fully initialized
      const timer = setTimeout(() => {
        console.log('[APP] Restoring active preset for mode:', selectedModeCategory)
        const activePreset = getActivePreset(selectedModeCategory)
        if (activePreset) {
          console.log('[APP] Found active preset:', activePreset.label)
          const result = applyPreset(activePreset.id, selectedModeCategory)
          if (!result.success) {
            console.error('[APP] Failed to restore preset:', result.error)
          } else {
            console.log('[APP] Successfully restored preset:', activePreset.label)
          }
        } else {
          console.log('[APP] No active preset found for mode:', selectedModeCategory)
        }
      }, 500) // 500ms delay to ensure stores are ready

      return () => clearTimeout(timer)
    }
  }, [hasCompletedInitialSetup, selectedModeCategory, getActivePreset, applyPreset])

  // Auto-minimize sidebar when mode is selected or preset is selected
  useEffect(() => {
    if (selectedModeCategory && !sidebarMinimized) {
      setSidebarMinimized(true)
    }
    // NOTE: Only include selectedModeCategory and setSidebarMinimized in dependencies
    // Do NOT include sidebarMinimized as it would cause the effect to re-run every time
    // the sidebar state changes, preventing manual toggle functionality after auto-minimize
  }, [selectedModeCategory, setSidebarMinimized])

  // Show mode selection modal if initial setup not completed
  const showModeSelection = !hasCompletedInitialSetup

  // Start new chat function
  const startNewChat = useCallback(() => {
    
    // Use ChatArea's resetChatState method to clear all chat state without circular call
    if (chatAreaRef.current) {
      chatAreaRef.current.resetChatState();
    }
    
    // Clear App-level state
    clearFileContext();
    setChatSessionId(''); // Clear chat session ID to exit historical mode
    setChatSessionTitle('');
    
    // Preserve active preset for workflow and deep-research modes, clear for other modes
    if (selectedModeCategory === 'workflow' || selectedModeCategory === 'deep-research') {
      // For workflow and deep-research modes, preserve the active preset
      const { getActivePreset } = useGlobalPresetStore.getState()
      const activePreset = getActivePreset(selectedModeCategory)
      if (activePreset) {
        // Keep the preset selected, just clear the filter
        setSelectedPresetId(null) // Clear filter but keep preset active
        // Don't clear the activePresetId in global store for these modes
        // The preset will be re-applied after chat state is reset
      } else {
        // No preset selected, clear everything
        setSelectedPresetId(null)
        clearActivePreset(selectedModeCategory)
      }
    } else {
      // For other modes (chat), clear preset state as before
      setSelectedPresetId(null); // Clear selected preset filter
      if (selectedModeCategory) {
        clearActivePreset(selectedModeCategory); // Also clear in global store
      }
    }
    
    // Reset the global session ID to force generation of a new one
    resetSessionId();
    
    // Clear the requiresNewChat flag after successful new chat initialization
    useAppStore.getState().clearRequiresNewChat();
    
    // Re-apply active preset for workflow and deep-research modes after chat reset
    if (selectedModeCategory === 'workflow' || selectedModeCategory === 'deep-research') {
      const { getActivePreset } = useGlobalPresetStore.getState()
      const activePreset = getActivePreset(selectedModeCategory)
      if (activePreset) {
        // Use setTimeout to ensure chat state reset is complete before applying preset
        setTimeout(() => {
          const result = applyPreset(activePreset.id, selectedModeCategory)
          if (!result.success) {
            console.error('[NEW_CHAT] Failed to re-apply preset:', result.error)
          }
        }, 100)
      }
    }
  }, [clearFileContext, setChatSessionId, setChatSessionTitle, setSelectedPresetId, clearActivePreset, selectedModeCategory, applyPreset]);

  // Handle chat session selection
  const handleChatSessionSelect = useCallback((sessionId: string, sessionTitle?: string, sessionType?: 'active' | 'completed', activeSessionInfo?: ActiveSessionInfo) => {
    
    if (sessionType === 'active' && activeSessionInfo) {
      // For active sessions, we'll let ChatArea handle the reconnection
      // Just set the session ID without timestamp to allow reconnection
      setChatSessionId(sessionId);
    } else {
      // For completed sessions, add timestamp to force reload
      setChatSessionId(`${sessionId}_${Date.now()}`);
    }
    
    setChatSessionTitle(sessionTitle || '');
    
    // Clear file content view when selecting a chat session
    setShowFileContent(false);
  }, [setChatSessionId, setChatSessionTitle, setShowFileContent]);


  // Minimize toggle functions
  const toggleSidebarMinimize = useCallback(() => {
    setSidebarMinimized(!sidebarMinimized)
  }, [sidebarMinimized, setSidebarMinimized])

  const toggleWorkspaceMinimize = useCallback(() => {
    setWorkspaceMinimized(!workspaceMinimized)
  }, [workspaceMinimized, setWorkspaceMinimized])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Ctrl/Cmd + 1 for Simple agent mode
      if ((event.ctrlKey || event.metaKey) && event.key === '1') {
        event.preventDefault()
        setAgentMode('simple')
      }
      // Ctrl/Cmd + 2 for ReAct agent mode
      if ((event.ctrlKey || event.metaKey) && event.key === '2') {
        event.preventDefault()
        setAgentMode('ReAct')
      }
      // Ctrl/Cmd + 3 for Deep Search agent mode
      if ((event.ctrlKey || event.metaKey) && event.key === '3') {
        event.preventDefault()
        setAgentMode('orchestrator')
      }
      // Ctrl/Cmd + 4 for Workflow agent mode
      if ((event.ctrlKey || event.metaKey) && event.key === '4') {
        event.preventDefault()
        setAgentMode('workflow')
      }
      // Ctrl/Cmd + 5 for sidebar minimize
      if ((event.ctrlKey || event.metaKey) && event.key === '5') {
        event.preventDefault()
        toggleSidebarMinimize()
      }
      // Ctrl/Cmd + 6 for workspace minimize
      if ((event.ctrlKey || event.metaKey) && event.key === '6') {
        event.preventDefault()
        toggleWorkspaceMinimize()
      }
      // Ctrl/Cmd + 8 for event mode cycling
      if ((event.ctrlKey || event.metaKey) && event.key === '8') {
        event.preventDefault()
        if ((window as Window & { cycleEventMode?: () => void }).cycleEventMode) {
          (window as Window & { cycleEventMode?: () => void }).cycleEventMode!()
        }
      }
      // Ctrl/Cmd + N for new chat
      if ((event.ctrlKey || event.metaKey) && event.key === 'n') {
        event.preventDefault()
        // Use ChatArea's handleNewChat method to properly clear events
        if (chatAreaRef.current) {
          chatAreaRef.current.handleNewChat()
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [toggleSidebarMinimize, toggleWorkspaceMinimize, setAgentMode, startNewChat])

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        {/* Mode Selection Modal */}
        <ModeSelectionModal 
          isOpen={showModeSelection}
          onClose={() => {}} // Modal handles its own closing
        />
        
        <div className="h-screen bg-background flex">
          {/* Left Sidebar */}
          <div className={`${sidebarMinimized ? 'w-16' : 'w-72'} transition-all duration-300 ease-in-out`}>
            <WorkspaceSidebar
              onPresetAdded={() => {
                // Refresh workflow presets when a new preset is added
                if (chatAreaRef.current) {
                  chatAreaRef.current.refreshWorkflowPresets()
                }
              }}
              onChatSessionSelect={handleChatSessionSelect}
              minimized={sidebarMinimized}
              onToggleMinimize={toggleSidebarMinimize}
            />
          </div>

          {/* Middle Chat Area */}
          <div className="flex-1 flex flex-col min-w-0 relative">
            {/* ChatArea - always rendered and mounted to preserve state */}
            <div className="flex-1 flex flex-col h-full min-w-0">
              <ChatArea
                ref={chatAreaRef}
                onNewChat={startNewChat}
              />
            </div>
            
            {/* File Content View - overlay when showing file content */}
            {showFileContent && (
              <div className="absolute inset-0 bg-white dark:bg-gray-900 z-10 flex flex-col">
              {/* Fixed Header */}
              <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
                <div className="flex items-center gap-3">
                  <button
                    onClick={() => setShowFileContent(false)}
                    className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                  >
                    ← Back to Chat
                  </button>
                  <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    {selectedFile?.path}
                  </h2>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => setShowRevisionsModal(true)}
                    className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                    title="View file revisions"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    Revisions
                  </button>
                </div>
              </div>
              
              {/* Scrollable Content */}
              <div className="flex-1 overflow-y-auto">
                {loadingFileContent ? (
                  <div className="flex items-center justify-center h-full">
                    <div className="text-center">
                      <div className="w-8 h-8 border-4 border-gray-300 border-t-blue-500 rounded-full animate-spin mx-auto mb-4"></div>
                      <p className="text-gray-500">Loading file content...</p>
                    </div>
                  </div>
                ) : (
                  <div className="p-4">
                    {(() => {
                      if (fileContent.startsWith('data:image/')) {
                        return (
                          <div className="flex flex-col items-center">
                            <img 
                              src={fileContent} 
                              alt="File content" 
                              className="max-w-full max-h-96 object-contain rounded-lg shadow-lg"
                              onError={(e) => console.error('❌ Image failed to load:', e)}
                            />
                            <p className="text-sm text-gray-500 mt-2">Image file</p>
                          </div>
                        )
                      } else if (selectedFile?.path?.toLowerCase().endsWith('.json')) {
                        // Check if content looks like formatted JSON (has proper indentation)
                        const isFormattedJson = fileContent.includes('{\n  ') || fileContent.includes('[\n  ')
                        
                        return (
                          <div className="space-y-2">
                            <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                              <span className="font-medium">📄 JSON File</span>
                              {isFormattedJson && (
                                <span className="text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 px-2 py-1 rounded">
                                  Formatted
                                </span>
                              )}
                            </div>
                            <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                              <pre className="text-sm font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words leading-relaxed">
                                {fileContent}
                              </pre>
                            </div>
                          </div>
                        )
                      } else {
                        return (
                          <MarkdownRenderer 
                            content={fileContent} 
                            className="prose-sm max-w-none dark:prose-invert"
                            showScrollbar={true}
                          />
                        )
                      }
                    })()}
                  </div>
                )}
              </div>
              </div>
            )}
          </div>

          {/* Right Workspace Area */}
          <div className={`${workspaceMinimized ? 'w-16' : 'w-96'} transition-all duration-300 ease-in-out border-l border-gray-200 dark:border-gray-700`}>
            <Workspace 
              minimized={workspaceMinimized}
              onToggleMinimize={toggleWorkspaceMinimize}
            />
          </div>
        </div>

        {/* File Revisions Modal */}
        <FileRevisionsModal
          isOpen={showRevisionsModal}
          onClose={() => setShowRevisionsModal(false)}
          filepath={selectedFile?.path || ''}
          onRestoreVersion={() => {
            // TODO: Implement version restoration
            setShowRevisionsModal(false)
          }}
        />
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
