import React, { useRef, useCallback, useMemo, useState, useEffect } from 'react'
import { Send, Loader2, Zap, Brain, Workflow, Square, Plus } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import type { PlannerFile } from '../services/api-types'
import type { LLMOption } from '../stores/types'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: () => void
  onStopStreaming: () => void
  onNewChat: () => void
  
  // Selected preset folder path
  selectedPresetFolder?: string | null
}

// Completely isolated input component that doesn't re-render when events change
export const ChatInput = React.memo<ChatInputProps>(({
  onSubmit,
  onStopStreaming,
  onNewChat,
  selectedPresetFolder
}) => {
  // Store subscriptions
  const {
    currentQuery,
    setCurrentQuery,
    agentMode,
    setAgentMode,
    chatFileContext,
    removeFileFromContext,
    clearFileContext,
    addFileToContext
  } = useAppStore()
  
  const {
    isStreaming,
    observerId,
    selectedWorkflowPreset
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

  // Preset folder selection (for Deep Search/workflow modes)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text

  // Handle preset folder selection
  useEffect(() => {
    if (selectedPresetFolder && (agentMode === 'orchestrator' || agentMode === 'workflow')) {
      // Check if the folder is already in context
      const isAlreadyInContext = chatFileContext.some((item: { path: string }) => item.path === selectedPresetFolder)
      
      if (!isAlreadyInContext && selectedPresetFolder) {
        // Add the preset folder to file context
        const folderName = (selectedPresetFolder as string).split('/').pop() || selectedPresetFolder
        addFileToContext({
          name: folderName,
          path: selectedPresetFolder,
          type: 'folder'
        })
        
        // Auto-scroll to the preset folder in workspace
        scrollToFile(selectedPresetFolder)
      }
    }
  }, [selectedPresetFolder, agentMode, chatFileContext, addFileToContext, scrollToFile])

  // Debug logging for currentQuery prop changes

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setCurrentQuery(newValue)

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
  }, [setCurrentQuery, chatFileContext, removeFileFromContext])

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
      if (currentQuery?.trim() && !isStreaming) {
        onSubmit()
      }
    }
    // Handle CTRL+Enter (Windows/Linux) or CMD+Enter (Mac) to add new line
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      // Insert newline at cursor position
      const textarea = e.target as HTMLTextAreaElement
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const value = currentQuery
      const newValue = value.substring(0, start) + '\n' + value.substring(end)
      setCurrentQuery(newValue)
      
      // Set cursor position after the newline
      setTimeout(() => {
        textarea.selectionStart = textarea.selectionEnd = start + 1
      }, 0)
    }
  }, [currentQuery, isStreaming, onSubmit, setCurrentQuery, showFileDialog])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()
    if (currentQuery?.trim() && !isStreaming && isRequiredFolderSelected) {
      onSubmit()
    }
  }, [currentQuery, isStreaming, onSubmit, isRequiredFolderSelected])

  // File selection handlers
  const handleFileSelect = useCallback((file: PlannerFile) => {
    if (!textareaRef.current || atPosition === -1) return
    
    // Replace @ and search text with @filepath + space
    const beforeAt = currentQuery.substring(0, atPosition)
    const afterSearch = currentQuery.substring(atPosition + 1 + fileSearchQuery.length)
    const newQuery = beforeAt + '@' + file.filepath + ' ' + afterSearch
    
    setCurrentQuery(newQuery)
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
  }, [currentQuery, atPosition, fileSearchQuery, setCurrentQuery, chatFileContext, addFileToContext, scrollToFile])

  const handleFileDialogClose = useCallback(() => {
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    textareaRef.current?.focus()
  }, [])


  // Check if workflow mode requires preset selection
  const isWorkflowReady = agentMode !== 'workflow' || (selectedWorkflowPreset && isRequiredFolderSelected)
  
  // Debug the submit button disable condition
  const submitButtonDisabled = !currentQuery?.trim() || !observerId || !isRequiredFolderSelected || !isWorkflowReady
  

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
            <div className="flex items-center gap-1">
              <span className="text-xs text-red-600 dark:text-red-400 font-medium">
                ⚠️ Deep Search mode requires a Tasks folder to be selected
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Validation message for Workflow mode - no preset selected */}
      {agentMode === 'workflow' && !selectedWorkflowPreset && (
        <div className="px-4">
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded px-1.5 py-0.5 mb-0">
            <div className="flex items-center gap-1">
              <span className="text-xs text-blue-600 dark:text-blue-400 font-medium">
                ℹ️ Workflow mode requires a preset to be selected first
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Validation message for Workflow mode - no Workflow folder selected */}
      {agentMode === 'workflow' && selectedWorkflowPreset && !isRequiredFolderSelected && (
        <div className="px-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded px-1.5 py-0.5 mb-0">
            <div className="flex items-center gap-1">
              <span className="text-xs text-yellow-600 dark:text-yellow-400 font-medium">
                💡 Select a folder from Workflow/ directory to proceed with workflow
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Hint when no files in context and not Deep Search mode */}
      {chatFileContext.length === 0 && agentMode !== 'orchestrator' && (
        <div className="px-4">
          <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-0.5 mb-0">
            <div className="flex items-center gap-1">
              <span className="text-xs text-gray-500 dark:text-gray-400">
                💡 Click chat icon in workspace to add files, or type @ to search and add files
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Hint when files in context but no Tasks folder for Deep Search */}
      {agentMode === 'orchestrator' && chatFileContext.length > 0 && !isRequiredFolderSelected && (
        <div className="px-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded px-1.5 py-0.5 mb-0">
            <div className="flex items-center gap-1">
              <span className="text-xs text-yellow-600 dark:text-yellow-400">
                💡 Select a folder from Tasks/ directory to proceed
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Input Form */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            <Textarea
              ref={textareaRef}
              value={currentQuery}
              onChange={handleTextChange}
              onKeyDown={handleKeyDown}
              placeholder={placeholder}
              className="min-h-[60px] max-h-[100px] resize-none text-sm"
              disabled={isStreaming || !observerId}
            />
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                {/* Agent Mode Selector */}
                <div className="flex items-center gap-1">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
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
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setAgentMode('orchestrator')}
                        className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                          agentMode === 'orchestrator'
                            ? 'agent-mode-selected'
                            : 'agent-mode-unselected'
                        }`}
                      >
                        <Workflow className="w-3 h-3 inline mr-1" />
                        Deep Search
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Deep Search mode - Ctrl+3</p>
                    </TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setAgentMode('workflow')}
                        className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                          agentMode === 'workflow'
                            ? 'agent-mode-selected'
                            : 'agent-mode-unselected'
                        }`}
                      >
                        <Workflow className="w-3 h-3 inline mr-1" />
                        Workflow
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Workflow mode - Ctrl+4</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                
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
                    />
                  </div>
                )}
                
                {/* Status text */}
                <div className="text-xs text-gray-500">
                  {!observerId ? (
                    <span>
                      <Loader2 className="w-3 h-3 inline animate-spin mr-1" />
                      Initializing observer...
                    </span>
                  ) : (
                    ''
                  )}
                </div>
              </div>
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
                        {!currentQuery?.trim()
                          ? 'Type a message to send'
                          : !observerId 
                            ? 'Observer not ready yet' 
                            : !isRequiredFolderSelected 
                              ? (agentMode === 'workflow' ? 'Select a Workflow folder and preset for workflow mode' : 'Select a Tasks folder to proceed')
                              : !isWorkflowReady
                                ? 'Select a preset for workflow mode'
                                : 'Send message'
                        }
                      </p>
                    </TooltipContent>
                  </Tooltip>
                )}
              </div>
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
