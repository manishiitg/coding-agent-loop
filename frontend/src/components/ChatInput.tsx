import React, { useRef, useCallback, useMemo, useState } from 'react'
import { Send, Loader2, Zap, Brain, Square, Plus } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import type { PlannerFile } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { usePresetState, usePresetApplication } from '../stores/useGlobalPresetStore'

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
  onNewChat: () => void
}

// Completely isolated input component that doesn't re-render when events change
export const ChatInput = React.memo<ChatInputProps>(({
  onSubmit,
  onStopStreaming,
  onNewChat
}) => {
  // Store subscriptions
  const {
    agentMode,
    setAgentMode,
    chatFileContext,
    removeFileFromContext,
    clearFileContext,
    addFileToContext
  } = useAppStore()
  
  // Get current query from global preset store for consistency
  const { setCurrentQuery: setGlobalCurrentQuery } = usePresetState()
  const { getActivePreset, activePresetIds, customPresets, predefinedPresets } = usePresetApplication()
  
  // Local state for input to prevent global re-renders on every keystroke
  const [localQuery, setLocalQuery] = useState('')
  
  const { selectedModeCategory } = useModeStore()
  
  const {
    isStreaming,
    observerId
  } = useChatStore()
  
  const {
    enabledServers: availableServers,
    selectedServers: manualSelectedServers,
    toggleServer: onManualServerToggle,
    selectAllServers: onSelectAllServers,
    clearAllServers: onClearAllServers
  } = useMCPStore()
  
  const {
    availableLLMs,
    getCurrentLLMOption,
    setPrimaryConfig,
    refreshAvailableLLMs: onRefreshAvailableLLMs
  } = useLLMStore()
  
  const { scrollToFile } = useWorkspaceStore()

  // Wrapper for LLM selection to convert LLMOption to LLMConfiguration
  const onPrimaryLLMSelect = useCallback((llm: LLMOption) => {
    // Get current config to preserve fallback models and cross-provider fallback
    const currentPrimaryConfig = useLLMStore.getState().primaryConfig
    
    setPrimaryConfig({
      ...currentPrimaryConfig, // ✅ Preserve all existing configuration
      provider: llm.provider as 'openrouter' | 'bedrock' | 'openai',
      model_id: llm.model
    })
  }, [setPrimaryConfig])

  // Computed values
  const primaryLLM = getCurrentLLMOption()
  const isRequiredFolderSelected = useMemo(() => {
    if (agentMode !== 'orchestrator' && agentMode !== 'workflow') return true; // No validation needed for other modes
    
    if (agentMode === 'orchestrator') {
      // Deep Search mode requires Tasks/ folder
      const hasTasksFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Tasks/')
      );
      return hasTasksFolder;
    } else if (agentMode === 'workflow') {
      // Workflow mode requires Workflow/ folder
      const hasWorkflowFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Workflow/')
      );
      return hasWorkflowFolder;
    }
    
    return true;
  }, [agentMode, chatFileContext])

  // Helper function for dynamic button text based on agent mode
  const getButtonText = useCallback(() => {
    if (agentMode === 'workflow') return 'Start Workflow'
    if (agentMode === 'orchestrator') return 'Start Deep Search'
    return 'Start Chat'
  }, [agentMode])

  // Helper function for dynamic tooltip text based on agent mode
  const getButtonTooltip = useCallback(() => {
    if (agentMode === 'workflow') return 'Start workflow execution with this preset'
    if (agentMode === 'orchestrator') return 'Start deep research with this preset'
    return 'Start a new chat with this preset'
  }, [agentMode])

  // Preset folder selection (for Deep Search/workflow modes)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text

  // Handle preset folder selection - now handled by global store
  // The global store's applyPreset method handles workspace selection and folder expansion
  // No need to add to file context here as it's handled by workspace selection

  // Debug logging for currentQuery prop changes

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setLocalQuery(newValue) // Only update local state - no global updates during typing

    // Check for @ symbol and update file dialog state
    const cursorPosition = e.target.selectionStart || 0
    const textBeforeCursor = newValue.substring(0, cursorPosition)
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')

    // Check if @ is at the end or followed by whitespace/newline
    const textAfterAt = textBeforeCursor.substring(lastAtIndex + 1)
    const hasValidAt = lastAtIndex >= 0 &&
      (textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)) // Updated regex

    if (hasValidAt) {
      setAtPosition(lastAtIndex)
      setFileSearchQuery(textAfterAt)
      setShowFileDialog(true)

      // Calculate dialog position - smart positioning to avoid overlap
      const textarea = e.target
      const rect = textarea.getBoundingClientRect()
      const dialogHeight = 320 // Approximate dialog height
      const spaceAbove = rect.top
      const spaceBelow = window.innerHeight - rect.bottom

      // Position above if there's more space above, otherwise position below
      const shouldPositionAbove = spaceAbove > dialogHeight || spaceAbove > spaceBelow

      setFileDialogPosition({
        top: shouldPositionAbove
          ? rect.top + window.scrollY - dialogHeight - 10 // Above with gap
          : rect.bottom + window.scrollY + 10, // Below with gap
        left: rect.left + window.scrollX
      })
    } else {
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
    }

    // Check if any @file references were removed and remove them from context
    const removedFiles: string[] = []
    chatFileContext.forEach((file: { path: string }) => {
      const fileReference = '@' + file.path
      if (!newValue.includes(fileReference)) {
        removedFiles.push(file.path)
      }
    })

    // Remove files that are no longer referenced
    removedFiles.forEach(filePath => {
      removeFileFromContext(filePath)
    })
  }, [chatFileContext, removeFileFromContext])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // If file dialog is open, let it handle keyboard events
    if (showFileDialog) {
      // Don't prevent default for arrow keys, enter, escape - let dialog handle them
      if (['ArrowUp', 'ArrowDown', 'Enter', 'Escape'].includes(e.key)) {
        return
      }
    }
    
    // Handle normal Enter to submit
    if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()
      if (localQuery?.trim() && !isStreaming) {
        // Clear local state immediately for UI responsiveness
        setLocalQuery('')
        // Call onSubmit with the query directly - no global state coordination needed!
        onSubmit(localQuery)
      }
    }
    // Handle CTRL+Enter (Windows/Linux) or CMD+Enter (Mac) to add new line
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      // Insert newline at cursor position
      const textarea = e.target as HTMLTextAreaElement
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const value = localQuery
      const newValue = value.substring(0, start) + '\n' + value.substring(end)
      setLocalQuery(newValue) // Only update local state
      
      // Set cursor position after the newline
      setTimeout(() => {
        textarea.selectionStart = textarea.selectionEnd = start + 1
      }, 0)
    }
  }, [localQuery, isStreaming, onSubmit, showFileDialog])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()
    if (localQuery?.trim() && !isStreaming && isRequiredFolderSelected) {
      // Clear local state immediately for UI responsiveness
      setLocalQuery('')
      // Call onSubmit with the query directly - no global state coordination needed!
      onSubmit(localQuery)
    }
  }, [localQuery, isStreaming, onSubmit, isRequiredFolderSelected])

  // File selection handlers
  const handleFileSelect = useCallback((file: PlannerFile) => {
    if (!textareaRef.current || atPosition === -1) return
    
    // Replace @ and search text with @filepath + space
    const beforeAt = localQuery.substring(0, atPosition)
    const afterSearch = localQuery.substring(atPosition + 1 + fileSearchQuery.length)
    const newQuery = beforeAt + '@' + file.filepath + ' ' + afterSearch
    
    setLocalQuery(newQuery) // Only update local state
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    
    // Add file/folder to context
    const fileContextItem = {
      name: file.filepath.split('/').pop() || file.filepath,
      path: file.filepath,
      type: file.type || 'file' as const
    }
    
    // Check if file is already in context (avoid duplicates)
    const isAlreadyInContext = chatFileContext.some((item: { path: string }) => item.path === file.filepath)
    if (!isAlreadyInContext) {
      addFileToContext(fileContextItem)
      
      // Auto-scroll to the file in workspace
      scrollToFile(file.filepath)
    }
    
    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        // Position cursor after the file path and space
        const cursorPosition = beforeAt.length + '@'.length + file.filepath.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [localQuery, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile])

  const handleFileDialogClose = useCallback(() => {
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  // State for editing preset query
  const [isEditingQuery, setIsEditingQuery] = useState(false)
  
  // Get active preset for current mode - directly reactive to store changes
  const activePreset = useMemo(() => {
    if (agentMode === 'workflow') {
      const presetId = activePresetIds['workflow']
      if (!presetId) return null
      
      // Find preset in custom or predefined presets
      const customPreset = customPresets.find(p => p.id === presetId)
      if (customPreset) return customPreset
      
      const predefinedPreset = predefinedPresets.find(p => p.id === presetId)
      if (predefinedPreset) return predefinedPreset
      
      return null
    } else if (agentMode === 'orchestrator') {
      const presetId = activePresetIds['deep-research']
      if (!presetId) return null
      
      // Find preset in custom or predefined presets
      const customPreset = customPresets.find(p => p.id === presetId)
      if (customPreset) return customPreset
      
      const predefinedPreset = predefinedPresets.find(p => p.id === presetId)
      if (predefinedPreset) return predefinedPreset
      
      return null
    }
    return null
  }, [agentMode, activePresetIds, customPresets, predefinedPresets])

  // Handle editing preset query
  const handleEditQuery = useCallback(() => {
    setIsEditingQuery(true)
    // Set the current query to the preset query for editing
    if (activePreset) {
      setLocalQuery(activePreset.query)
      setGlobalCurrentQuery(activePreset.query)
      useAppStore.getState().setCurrentQuery(activePreset.query)
      
      // IMPORTANT: Ensure the preset's file context is preserved
      // If the preset has a selected folder, make sure it's still in the file context
      if (activePreset.selectedFolder) {
        const folderPath = activePreset.selectedFolder.filepath
        const folderName = folderPath.split('/').pop() || folderPath
        
        // Check if the folder is already in the file context
        const isFolderInContext = chatFileContext.some((item: { path: string }) => item.path === folderPath)
        
        if (!isFolderInContext) {
          // Re-add the folder to file context if it's missing
          addFileToContext({
            name: folderName,
            path: folderPath,
            type: 'folder'
          })
          
          // Also ensure the workspace selection is preserved
          useWorkspaceStore.getState().setSelectedFile({
            name: folderName,
            path: folderPath
          })
        }
      }
    }
  }, [activePreset, setGlobalCurrentQuery, chatFileContext, addFileToContext])

  // Handle canceling query edit
  const handleCancelEdit = useCallback(() => {
    setIsEditingQuery(false)
    // Reset to preset query
    if (activePreset) {
      setLocalQuery(activePreset.query)
      setGlobalCurrentQuery(activePreset.query)
      useAppStore.getState().setCurrentQuery(activePreset.query)
      
      // IMPORTANT: Ensure the preset's file context is preserved when canceling edit
      // If the preset has a selected folder, make sure it's still in the file context
      if (activePreset.selectedFolder) {
        const folderPath = activePreset.selectedFolder.filepath
        const folderName = folderPath.split('/').pop() || folderPath
        
        // Check if the folder is already in the file context
        const isFolderInContext = chatFileContext.some((item: { path: string }) => item.path === folderPath)
        
        if (!isFolderInContext) {
          // Re-add the folder to file context if it's missing
          addFileToContext({
            name: folderName,
            path: folderPath,
            type: 'folder'
          })
          
          // Also ensure the workspace selection is preserved
          useWorkspaceStore.getState().setSelectedFile({
            name: folderName,
            path: folderPath
          })
        }
      }
    }
  }, [activePreset, setGlobalCurrentQuery, chatFileContext, addFileToContext])

  // Handle saving edited query
  const handleSaveEdit = useCallback(() => {
    setIsEditingQuery(false)
    // The current query is already updated by the text input
  }, [])

  // Check if workflow mode requires preset selection
  const isWorkflowReady = agentMode !== 'workflow' || (getActivePreset('workflow') && isRequiredFolderSelected)
  
  // Check if deep research mode requires preset selection
  const isDeepResearchReady = agentMode !== 'orchestrator' || (getActivePreset('deep-research') && isRequiredFolderSelected)
  
  // Debug the submit button disable condition
  const submitButtonDisabled = !localQuery?.trim() || !observerId || !isRequiredFolderSelected || !isWorkflowReady || !isDeepResearchReady
  

  // Memoized placeholder to prevent re-computation
  const placeholder = useMemo(() => {
    return agentMode === 'ReAct' 
      ? "Ask me anything... I'll think step-by-step and use tools to help you!" 
      : agentMode === 'orchestrator'
      ? "Enter your objective for plan creation... I'll create a detailed plan using simple agent!"
      : agentMode === 'workflow'
      ? "Enter your objective for workflow execution... I'll create a todo-list and execute tasks sequentially!"
      : "Ask me anything... I can use tools to help you!"
  }, [agentMode])

  return (
    <TooltipProvider>
      <div className="space-y-2">
      {/* File Context Display */}
      {chatFileContext.length > 0 && (
        <div className="px-4 border-t border-gray-200 dark:border-gray-700">
          <FileContextDisplay
            files={chatFileContext}
            onRemoveFile={removeFileFromContext}
            onClearAll={clearFileContext}
            agentMode={agentMode}
            isRequiredFolderSelected={isRequiredFolderSelected}
          />
        </div>
      )}

      {/* Validation message for Deep Search mode - no files in context */}
      {agentMode === 'orchestrator' && !isRequiredFolderSelected && chatFileContext.length === 0 && (
        <div className="px-4">
          <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-red-600 dark:text-red-400 font-medium">
              ⚠️ Deep Search mode requires a Tasks folder to be selected
            </span>
          </div>
        </div>
      )}

      {/* Validation message for Workflow mode - no preset selected */}
      {agentMode === 'workflow' && !getActivePreset('workflow') && (
        <div className="px-4">
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-blue-600 dark:text-blue-400 font-medium">
              ℹ️ Workflow mode requires a preset to be selected first. Use the mode selector to choose a preset.
            </span>
          </div>
        </div>
      )}

      {/* Validation message for Deep Research mode - no preset selected */}
      {agentMode === 'orchestrator' && !getActivePreset('deep-research') && (
        <div className="px-4">
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-blue-600 dark:text-blue-400 font-medium">
              ℹ️ Deep Research mode requires a preset to be selected first. Use the mode selector to choose a preset.
            </span>
          </div>
        </div>
      )}

      {/* Validation message for Workflow mode - no Workflow folder selected */}
      {agentMode === 'workflow' && getActivePreset('workflow') && !isRequiredFolderSelected && (
        <div className="px-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-yellow-600 dark:text-yellow-400">
              💡 Select a folder from Workflow/ directory to proceed with workflow
            </span>
          </div>
        </div>
      )}

      {/* Hint when no files in context and not Deep Search mode */}
      {chatFileContext.length === 0 && agentMode !== 'orchestrator' && (
        <div className="px-4">
          <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-gray-500 dark:text-gray-400">
              💡 Click chat icon in workspace to add files, or type @ to search and add files
            </span>
          </div>
        </div>
      )}

      {/* Hint when files in context but no Tasks folder for Deep Search */}
      {agentMode === 'orchestrator' && chatFileContext.length > 0 && !isRequiredFolderSelected && (
        <div className="px-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-yellow-600 dark:text-yellow-400">
              💡 Select a folder from Tasks/ directory to proceed
            </span>
          </div>
        </div>
      )}

      {/* Input Form */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            {/* Show compact preset info with action buttons for workflow and deep research modes */}
            {(agentMode === 'workflow' || agentMode === 'orchestrator') && activePreset && !isEditingQuery ? (
              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md px-3 py-2">
                <div className="flex items-center justify-between gap-2">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="flex items-center gap-2 flex-1 min-w-0 cursor-default">
                        <div className="w-1.5 h-1.5 bg-blue-500 rounded-full flex-shrink-0"></div>
                        <span className="text-sm font-medium text-blue-900 dark:text-blue-100 truncate">
                          {activePreset.label}
                        </span>
                        <span className="text-xs text-blue-600 dark:text-blue-400 flex-shrink-0">
                          ({agentMode === 'workflow' ? 'Workflow' : 'Deep Research'})
                        </span>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <div className="max-w-sm">
                        <p className="font-medium mb-1">{activePreset.label}</p>
                        <p className="text-sm">{activePreset.query}</p>
                      </div>
                    </TooltipContent>
                  </Tooltip>
                  
                  <div className="flex items-center gap-1 flex-shrink-0">
                    {/* Edit button - only show when not streaming */}
                    {!isStreaming && (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={handleEditQuery}
                            className="px-2 py-0.5 text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 hover:bg-blue-100 dark:hover:bg-blue-800/30 rounded transition-colors"
                          >
                            Edit
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Edit the preset query</p>
                        </TooltipContent>
                      </Tooltip>
                    )}
                    
                    {/* Dynamic button - Start or Stop based on streaming state */}
                    {isStreaming ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={onStopStreaming}
                            className="px-2 py-0.5 text-xs bg-red-600 text-white rounded hover:bg-red-700 transition-colors"
                          >
                            Stop
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Stop the current execution</p>
                        </TooltipContent>
                      </Tooltip>
                    ) : (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => onSubmit(localQuery || activePreset?.query || '')}
                            disabled={!observerId || !isRequiredFolderSelected || !(localQuery || activePreset?.query)}
                            className="px-2 py-0.5 text-xs bg-green-600 text-white rounded hover:bg-green-700 disabled:bg-gray-400 disabled:cursor-not-allowed transition-colors"
                          >
                            {getButtonText()}
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>{getButtonTooltip()}</p>
                        </TooltipContent>
                      </Tooltip>
                    )}
                    
                    {/* New Chat button - only show when not streaming */}
                    {!isStreaming && (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={onNewChat}
                            className="px-2 py-0.5 text-xs bg-gray-600 text-white rounded hover:bg-gray-700 transition-colors"
                          >
                            New Chat
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Create a new chat session</p>
                        </TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                </div>
              </div>
            ) : (
              /* Show text input for chat mode or when editing preset query */
              <Textarea
                ref={textareaRef}
                value={localQuery}
                onChange={handleTextChange}
                onKeyDown={handleKeyDown}
                placeholder={placeholder}
                className="min-h-[60px] max-h-[100px] resize-none text-sm"
                disabled={isStreaming || !observerId}
              />
            )}
            
            {/* Show compact edit controls when editing preset query */}
            {(agentMode === 'workflow' || agentMode === 'orchestrator') && isEditingQuery && (
              <div className="flex items-center gap-1 px-2 py-1 bg-gray-50 dark:bg-gray-800 rounded text-xs">
                <span className="text-gray-600 dark:text-gray-400">Editing:</span>
                <button
                  type="button"
                  onClick={handleSaveEdit}
                  className="px-2 py-0.5 bg-green-600 text-white rounded hover:bg-green-700 transition-colors"
                >
                  Save
                </button>
                <button
                  type="button"
                  onClick={handleCancelEdit}
                  className="px-2 py-0.5 bg-gray-600 text-white rounded hover:bg-gray-700 transition-colors"
                >
                  Cancel
                </button>
              </div>
            )}
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                
                {/* Agent Mode Selector - Only for Chat Mode */}
                {selectedModeCategory === 'chat' && (
                  <div className="flex items-center gap-1">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => setAgentMode('simple')}
                          className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                            agentMode === 'simple'
                              ? 'agent-mode-selected'
                              : 'agent-mode-unselected'
                          }`}
                        >
                          <Zap className="w-3 h-3 inline mr-1" />
                          Simple
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Simple mode - Ctrl+1</p>
                      </TooltipContent>
                    </Tooltip>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => setAgentMode('ReAct')}
                          className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                            agentMode === 'ReAct'
                              ? 'agent-mode-selected'
                              : 'agent-mode-unselected'
                          }`}
                        >
                          <Brain className="w-3 h-3 inline mr-1" />
                          ReAct
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>ReAct mode - Ctrl+2</p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                )}
                
                {/* Server and LLM Selection for Simple/ReAct modes */}
                {(agentMode === 'simple' || agentMode === 'ReAct') && (
                  <div className="flex items-center gap-2">
                    <ServerSelectionDropdown
                      availableServers={availableServers}
                      selectedServers={manualSelectedServers}
                      onServerToggle={onManualServerToggle}
                      onSelectAll={onSelectAllServers}
                      onClearAll={onClearAllServers}
                      disabled={isStreaming}
                    />
                    <LLMSelectionDropdown
                      availableLLMs={availableLLMs}
                      selectedLLM={primaryLLM}
                      onLLMSelect={onPrimaryLLMSelect}
                      onRefresh={onRefreshAvailableLLMs}
                      disabled={isStreaming}
                      openDirection="up"
                    />
                  </div>
                )}
                
                {/* Status text */}
                <div className="text-xs text-gray-500">
                  {!observerId ? (
                    <span>
                      <Loader2 className="w-3 h-3 inline animate-spin mr-1" />
                      Initializing observer... (retrying if needed)
                    </span>
                  ) : (
                    ''
                  )}
                </div>
              </div>
              {/* Show old buttons only for chat mode */}
              {selectedModeCategory === 'chat' && (
                <div className="flex items-center gap-2">
                  {/* New Chat Button */}
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={onNewChat}
                        disabled={isStreaming}
                        className="px-3"
                      >
                        <Plus className="w-4 h-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Start a new chat (Ctrl+N / Cmd+N)</p>
                    </TooltipContent>
                  </Tooltip>
                  
                  {isStreaming ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button 
                          type="button"
                          variant="destructive"
                          onClick={onStopStreaming}
                          size="sm"
                          className="px-3"
                        >
                          <Square className="w-4 h-4" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Stop streaming</p>
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button 
                          type="submit" 
                          disabled={submitButtonDisabled}
                          size="sm"
                          className="px-3"
                        >
                          <Send className="w-4 h-4" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>
                          {!localQuery?.trim()
                            ? 'Type a message to send'
                            : !observerId 
                              ? 'Observer not ready yet' 
                              : 'Send message'
                          }
                        </p>
                      </TooltipContent>
                    </Tooltip>
                  )}
                </div>
              )}
            </div>
          </div>
        </form>
      </div>
      
      {/* File Selection Dialog */}
      <FileSelectionDialog
        isOpen={showFileDialog}
        onClose={handleFileDialogClose}
        onSelectFile={handleFileSelect}
        searchQuery={fileSearchQuery}
        position={fileDialogPosition}
      />
      </div>
    </TooltipProvider>
  )
})

ChatInput.displayName = 'ChatInput'
