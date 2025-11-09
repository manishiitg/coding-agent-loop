import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useCallback, useRef, useState } from "react";
import { ThemeProvider } from "./contexts/ThemeContext.tsx";
import WorkspaceSidebar from "./components/WorkspaceSidebar";
import Workspace from "./components/Workspace.tsx";
import ChatArea, { type ChatAreaRef } from "./components/ChatArea.tsx";
import { MarkdownRenderer } from "./components/ui/MarkdownRenderer";
import { resetSessionId } from "./services/api";
import type { ActiveSessionInfo } from "./services/api-types";
import FileRevisionsModal from "./components/workspace/FileRevisionsModal";
import FileEditor from "./components/workspace/FileEditor";
import { isValidJSON } from "./utils/event-helpers";
import { Edit, Save, X, Loader2 } from "lucide-react";
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
    setShowRevisionsModal,
    isEditMode,
    setIsEditMode,
    editedContent,
    setEditedContent,
    isSaving,
    getHasUnsavedChanges,
    saveFile
  } = useWorkspaceStore()
  
  const [commitMessage, setCommitMessage] = useState('')
  const [showCommitDialog, setShowCommitDialog] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  
  const { clearActivePreset, applyPreset, getActivePreset } = useGlobalPresetStore()

  // Initialize editedContent when entering edit mode
  useEffect(() => {
    if (isEditMode && editedContent === '' && fileContent) {
      setEditedContent(fileContent)
    }
  }, [isEditMode, fileContent, editedContent, setEditedContent])

  // Handle edit mode toggle
  const handleEdit = useCallback(() => {
    setEditedContent(fileContent)
    setIsEditMode(true)
    setSaveError(null)
  }, [fileContent, setEditedContent, setIsEditMode])

  // Handle cancel edit
  const handleCancelEdit = useCallback(() => {
    if (getHasUnsavedChanges()) {
      if (window.confirm('You have unsaved changes. Are you sure you want to cancel?')) {
        setEditedContent('')
        setIsEditMode(false)
        setSaveError(null)
      }
    } else {
      setEditedContent('')
      setIsEditMode(false)
      setSaveError(null)
    }
  }, [getHasUnsavedChanges, setEditedContent, setIsEditMode])

  // Handle save
  const handleSave = useCallback(async () => {
    // Validate JSON if it's a JSON file
    if (selectedFile?.path?.toLowerCase().endsWith('.json') || isValidJSON(editedContent)) {
      try {
        JSON.parse(editedContent)
      } catch {
        setSaveError('Invalid JSON. Please fix the syntax errors before saving.')
        return
      }
    }

    // Check file size (warn if > 1MB)
    if (editedContent.length > 1024 * 1024) {
      if (!window.confirm('File is larger than 1MB. Continue saving?')) {
        return
      }
    }

    setShowCommitDialog(true)
  }, [selectedFile?.path, editedContent])

  // Handle save with commit message
  const handleSaveWithCommit = async () => {
    setSaveError(null)
    const result = await saveFile(commitMessage || undefined)
    if (result.success) {
      setShowCommitDialog(false)
      setCommitMessage('')
      setSaveError(null)
    } else {
      setSaveError(result.error || 'Failed to save file')
      // Keep dialog open on error
    }
  }

  // Keyboard shortcuts
  useEffect(() => {
    if (!showFileContent) return

    const handleKeyDown = (e: KeyboardEvent) => {
      // Ctrl+S or Cmd+S: Save
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        if (isEditMode && getHasUnsavedChanges()) {
          handleSave()
        }
      }
      // Ctrl+E or Cmd+E: Toggle edit mode
      if ((e.ctrlKey || e.metaKey) && e.key === 'e') {
        e.preventDefault()
        if (isEditMode) {
          handleCancelEdit()
        } else {
          handleEdit()
        }
      }
      // Esc: Cancel edit mode
      if (e.key === 'Escape' && isEditMode) {
        if (!getHasUnsavedChanges()) {
          handleCancelEdit()
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [showFileContent, isEditMode, getHasUnsavedChanges, handleSave, handleEdit, handleCancelEdit])

  // Prevent navigation with unsaved changes
  useEffect(() => {
    if (!showFileContent || !getHasUnsavedChanges()) return

    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [showFileContent, getHasUnsavedChanges])

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
                <div className="flex items-center gap-3 min-w-0 flex-1">
                  <button
                    onClick={() => {
                      if (getHasUnsavedChanges()) {
                        if (window.confirm('You have unsaved changes. Are you sure you want to close?')) {
                          setEditedContent('')
                          setIsEditMode(false)
                          setShowFileContent(false)
                        }
                      } else {
                        setShowFileContent(false)
                      }
                    }}
                    className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 flex-shrink-0"
                  >
                    ← Back to Chat
                  </button>
                  <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 truncate min-w-0">
                    {selectedFile?.path}
                    {getHasUnsavedChanges() && (
                      <span className="ml-2 text-xs text-orange-500">●</span>
                    )}
                  </h2>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  {!isEditMode ? (
                    <>
                      <button
                        onClick={handleEdit}
                        className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                        title="Edit file (Ctrl+E)"
                      >
                        <Edit className="w-4 h-4" />
                        Edit
                      </button>
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
                    </>
                  ) : (
                    <>
                      <button
                        onClick={handleCancelEdit}
                        className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                        title="Cancel edit (Esc)"
                        disabled={isSaving}
                      >
                        <X className="w-4 h-4" />
                        Cancel
                      </button>
                      {getHasUnsavedChanges() && (
                        <button
                          onClick={handleSave}
                          disabled={isSaving}
                          className="flex items-center gap-1 px-3 py-1.5 text-sm text-white bg-blue-500 hover:bg-blue-600 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                          title="Save file (Ctrl+S)"
                        >
                          {isSaving ? (
                            <Loader2 className="w-4 h-4 animate-spin" />
                          ) : (
                            <Save className="w-4 h-4" />
                          )}
                          Save
                        </button>
                      )}
                    </>
                  )}
                </div>
              </div>
              
              {/* Save Error Message */}
              {saveError && (
                <div className="px-4 py-2 bg-red-50 dark:bg-red-900/20 border-b border-red-200 dark:border-red-800">
                  <p className="text-sm text-red-600 dark:text-red-400">{saveError}</p>
                </div>
              )}
              
              {/* Commit Message Dialog */}
              {showCommitDialog && (
                <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
                  <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 w-full max-w-md">
                    <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
                      Save File
                    </h3>
                    <div className="mb-4">
                      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                        Commit Message (optional)
                      </label>
                      <input
                        type="text"
                        value={commitMessage}
                        onChange={(e) => setCommitMessage(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && !e.shiftKey) {
                            e.preventDefault()
                            handleSaveWithCommit()
                          } else if (e.key === 'Escape') {
                            setShowCommitDialog(false)
                            setCommitMessage('')
                          }
                        }}
                        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                        placeholder="Enter commit message..."
                        autoFocus
                      />
                    </div>
                    <div className="flex justify-end gap-2">
                      <button
                        onClick={() => {
                          setShowCommitDialog(false)
                          setCommitMessage('')
                        }}
                        className="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100"
                      >
                        Cancel
                      </button>
                      <button
                        onClick={handleSaveWithCommit}
                        disabled={isSaving}
                        className="px-4 py-2 text-sm text-white bg-blue-500 hover:bg-blue-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        {isSaving ? 'Saving...' : 'Save'}
                      </button>
                    </div>
                  </div>
                </div>
              )}
              
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
                  <>
                    {fileContent.startsWith('data:image/') ? (
                      <div className="flex flex-col items-center justify-center h-full p-4">
                        <img 
                          src={fileContent} 
                          alt="File content" 
                          className="max-w-full max-h-full object-contain rounded-lg shadow-lg"
                          onError={(e) => console.error('❌ Image failed to load:', e)}
                        />
                        <p className="text-sm text-gray-500 mt-2">Image file</p>
                      </div>
                    ) : isEditMode ? (
                      <div className="h-full overflow-hidden">
                        <FileEditor
                          value={editedContent}
                          filepath={selectedFile?.path || ''}
                          readOnly={false}
                          onChange={(value) => setEditedContent(value || '')}
                          height="100%"
                        />
                      </div>
                    ) : (
                      <div className="p-4">
                        {(() => {
                          if (selectedFile?.path?.toLowerCase().endsWith('.json') || isValidJSON(fileContent)) {
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
                                  <pre className="text-xs font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words leading-relaxed">
                                    {fileContent}
                                  </pre>
                                </div>
                              </div>
                            )
                          } else {
                            return (
                              <div className="text-xs prose prose-sm max-w-none dark:prose-invert [&_*]:text-xs">
                                <MarkdownRenderer 
                                  content={fileContent} 
                                  className="max-w-none"
                                  showScrollbar={true}
                                />
                              </div>
                            )
                          }
                        })()}
                      </div>
                    )}
                  </>
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
