import React, { useRef, useCallback, useMemo, useState, useEffect } from 'react'
import { Send, Loader2, Square, Plus, Code2, Sparkles } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import CommandSelectionDialog from './CommandSelectionDialog'
import type { PlannerFile } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { usePresetState, usePresetApplication } from '../stores/useGlobalPresetStore'
import { agentApi } from '../services/api'

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
  onNewChat: () => void
}

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming,
  onNewChat
}) => {
  // Store subscriptions
  const {
    agentMode,
    chatFileContext,
    removeFileFromContext,
    clearFileContext,
    addFileToContext,
    useCodeExecutionMode,
    setUseCodeExecutionMode
  } = useAppStore()
  
  // Get current query from global preset store for consistency
  const { setCurrentQuery: setGlobalCurrentQuery } = usePresetState()
  const { getActivePreset, activePresetIds, customPresets, predefinedPresets } = usePresetApplication()
  
  // Local state for input to prevent global re-renders on every keystroke
  const [localQuery, setLocalQuery] = useState('')
  
  const { selectedModeCategory } = useModeStore()
  
  const {
    isStreaming,
    observerId,
    sessionId
  } = useChatStore()
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)
  
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
      provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex',
      model_id: llm.model
    })
  }, [setPrimaryConfig])

  // Computed values
  const primaryLLM = getCurrentLLMOption()
  
  // Check if a preset is active for the current mode
  const chatActivePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')
  
  // Sync localQuery with preset query when preset is selected in chat mode
  useEffect(() => {
    // Only sync in chat mode
    if (selectedModeCategory === 'chat') {
      const activePresetId = activePresetIds['chat']
      
      if (activePresetId) {
        // Find the preset
        const preset = customPresets.find(p => p.id === activePresetId) || 
                      predefinedPresets.find(p => p.id === activePresetId)
        
        if (preset && preset.query) {
          // Sync localQuery with preset query
          setLocalQuery(preset.query)
        }
      } else {
        // No preset active, clear localQuery
        setLocalQuery('')
      }
    }
  }, [selectedModeCategory, activePresetIds, customPresets, predefinedPresets])
  
  const isRequiredFolderSelected = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return true; // No validation needed for other modes
    
    if (selectedModeCategory === 'workflow') {
      // Workflow mode requires Workflow/ folder
      const hasWorkflowFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Workflow/')
      );
      return hasWorkflowFolder;
    }
    
    return true;
  }, [selectedModeCategory, chatFileContext])

  // Helper function for dynamic button text based on agent mode
  const getButtonText = useCallback(() => {
    if (selectedModeCategory === 'workflow') return 'Start Workflow'
    return 'Start Chat'
  }, [selectedModeCategory])

  // Helper function for dynamic tooltip text based on agent mode
  const getButtonTooltip = useCallback(() => {
    if (selectedModeCategory === 'workflow') return 'Start workflow execution with this preset'
    return 'Start a new chat with this preset'
  }, [selectedModeCategory])

  // Preset folder selection (for workflow mode)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text

  // Command selection dialog state
  const [showCommandDialog, setShowCommandDialog] = useState(false)
  const [commandDialogPosition, setCommandDialogPosition] = useState({ top: 0, left: 0 })
  const [commandSearchQuery, setCommandSearchQuery] = useState('')
  const [slashPosition, setSlashPosition] = useState(-1) // Position of / in text

  // Handle preset folder selection - now handled by global store
  // The global store's applyPreset method handles workspace selection and folder expansion
  // No need to add to file context here as it's handled by workspace selection

  // Debug logging for currentQuery prop changes

  // Get active preset for current mode - directly reactive to store changes
  const activePreset = useMemo(() => {
    if (selectedModeCategory === 'workflow') {
      const presetId = activePresetIds['workflow']
      if (!presetId) return null
      
      // Find preset in custom or predefined presets
      const customPreset = customPresets.find(p => p.id === presetId)
      if (customPreset) return customPreset
      
      const predefinedPreset = predefinedPresets.find(p => p.id === presetId)
      if (predefinedPreset) return predefinedPreset
      
      return null
    }
    return null
  }, [selectedModeCategory, activePresetIds, customPresets, predefinedPresets])

  // Consolidated query selection logic
  const queryToSubmit = useMemo(() => {
    return selectedModeCategory === 'workflow' && activePreset 
      ? activePreset.query 
      : localQuery
  }, [selectedModeCategory, activePreset, localQuery])

  // Guard to prevent submission before observer is ready
  const canSubmit = useMemo(() => {
    return queryToSubmit?.trim() && !isStreaming && observerId
  }, [queryToSubmit, isStreaming, observerId])

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setLocalQuery(newValue) // Only update local state - no global updates during typing

    const cursorPosition = e.target.selectionStart || 0
    const textBeforeCursor = newValue.substring(0, cursorPosition)
    
    // Check for / symbol and update command dialog state (prioritize over @)
    const lastSlashIndex = textBeforeCursor.lastIndexOf('/')
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')
    
    // Determine which dialog to show (prioritize the one closest to cursor)
    const slashDistance = lastSlashIndex >= 0 ? cursorPosition - lastSlashIndex : Infinity
    const atDistance = lastAtIndex >= 0 ? cursorPosition - lastAtIndex : Infinity
    
    // Check for / command
    if (lastSlashIndex >= 0 && (lastAtIndex < 0 || slashDistance < atDistance)) {
      const textAfterSlash = textBeforeCursor.substring(lastSlashIndex + 1)
      const hasValidSlash = textAfterSlash === '' || textAfterSlash.match(/^[a-zA-Z0-9_]*$/)
      
      if (hasValidSlash) {
        setSlashPosition(lastSlashIndex)
        setCommandSearchQuery(textAfterSlash)
        setShowCommandDialog(true)
        setShowFileDialog(false) // Hide file dialog if command dialog is active
        
        // Calculate dialog position
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        const dialogHeight = 200 // Approximate dialog height
        const spaceAbove = rect.top
        const spaceBelow = window.innerHeight - rect.bottom
        const shouldPositionAbove = spaceAbove > dialogHeight || spaceAbove > spaceBelow

        setCommandDialogPosition({
          top: shouldPositionAbove
            ? rect.top + window.scrollY - dialogHeight - 10
            : rect.bottom + window.scrollY + 10,
          left: rect.left + window.scrollX
        })
      } else {
        setShowCommandDialog(false)
        setSlashPosition(-1)
        setCommandSearchQuery('')
      }
    }
    // Check for @ symbol and update file dialog state (only if no / command active)
    else if (lastAtIndex >= 0 && !showCommandDialog) {
      const textAfterAt = textBeforeCursor.substring(lastAtIndex + 1)
      const hasValidAt = textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)

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
    } else {
      // Close both dialogs if neither is active
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
      setShowCommandDialog(false)
      setSlashPosition(-1)
      setCommandSearchQuery('')
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
  }, [chatFileContext, removeFileFromContext, showCommandDialog])

  // Handle manual summarization
  // If messageToSendAfter is provided, it will be sent as a user message after summarization completes
  const handleSummarize = useCallback(async (messageToSendAfter?: string) => {
    if (!sessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true)
    try {
      const response = await agentApi.summarizeConversation(sessionId)
      console.log('[SUMMARIZATION] Success:', response)
      // Show success notification (you can add a toast library here)
      alert(`Conversation summarized successfully!\nOriginal: ${response.original_count} messages\nNew: ${response.new_count} messages\nReduced by: ${response.reduced_by} messages`)
      
      // If there's a message to send after summarization, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && observerId) {
        // Small delay to ensure summarization is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[SUMMARIZATION] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      alert(`Failed to summarize conversation: ${errorMessage}`)
    } finally {
      setIsSummarizing(false)
    }
  }, [sessionId, isSummarizing, isStreaming, observerId, onSubmit])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // If command dialog is open, let it handle keyboard events
    if (showCommandDialog) {
      // Don't prevent default for arrow keys, enter, escape - let dialog handle them
      if (['ArrowUp', 'ArrowDown', 'Enter', 'Escape'].includes(e.key)) {
        return
      }
    }
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
      
      // Check for slash commands
      const trimmedQuery = queryToSubmit?.trim() || ''
      const summarizeIndex = trimmedQuery.indexOf('/summarize')
      
      if (summarizeIndex >= 0) {
        // Handle summarize command
        if (sessionId && !isSummarizing && !isStreaming) {
          // Extract text before /summarize
          const textBeforeSummarize = trimmedQuery.substring(0, summarizeIndex).trim()
          
          // If there's text before /summarize, send it after summarization
          // Otherwise, just summarize
          handleSummarize(textBeforeSummarize || undefined)
          setLocalQuery('') // Clear input after command
        }
        return
      }
      
      if (canSubmit) {
        // Clear local state immediately for UI responsiveness (only for non-preset modes and when observer is ready)
        if (selectedModeCategory !== 'workflow' && observerId) {
          setLocalQuery('')
        }
        // Call onSubmit with the query directly - no global state coordination needed!
        onSubmit(queryToSubmit)
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
  }, [localQuery, onSubmit, showFileDialog, showCommandDialog, selectedModeCategory, observerId, canSubmit, queryToSubmit, sessionId, isSummarizing, isStreaming, handleSummarize])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()
    
    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    const summarizeIndex = trimmedQuery.indexOf('/summarize')
    
    if (summarizeIndex >= 0) {
      // Handle summarize command
      if (sessionId && !isSummarizing && !isStreaming) {
        // Extract text before /summarize
        const textBeforeSummarize = trimmedQuery.substring(0, summarizeIndex).trim()
        
        // If there's text before /summarize, send it after summarization
        // Otherwise, just summarize
        handleSummarize(textBeforeSummarize || undefined)
        setLocalQuery('') // Clear input after command
      }
      return
    }
    
    if (canSubmit && isRequiredFolderSelected) {
      // Clear local state immediately for UI responsiveness (only for non-preset modes and when observer is ready)
      if (selectedModeCategory !== 'workflow' && observerId) {
        setLocalQuery('')
      }
      // Call onSubmit with the query directly - no global state coordination needed!
      onSubmit(queryToSubmit)
    }
  }, [canSubmit, isRequiredFolderSelected, selectedModeCategory, observerId, queryToSubmit, onSubmit, sessionId, isSummarizing, isStreaming, handleSummarize])

  // File selection handlers
  const handleCommandSelect = useCallback((command: string) => {
    if (!textareaRef.current || slashPosition === -1) return
    
    // Replace / and search text with /command + space
    const beforeSlash = localQuery.substring(0, slashPosition)
    const afterSearch = localQuery.substring(slashPosition + 1 + commandSearchQuery.length)
    const newQuery = beforeSlash + '/' + command + ' ' + afterSearch
    
    setLocalQuery(newQuery) // Only update local state
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')
    
    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        // Position cursor after the command and space
        const cursorPosition = beforeSlash.length + '/'.length + command.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [localQuery, slashPosition, commandSearchQuery])

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

  const handleCommandDialogClose = useCallback(() => {
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const handleFileDialogClose = useCallback(() => {
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  // State for editing preset query
  const [isEditingQuery, setIsEditingQuery] = useState(false)

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
  const isWorkflowReady = selectedModeCategory !== 'workflow' || (getActivePreset('workflow') && isRequiredFolderSelected)
  
  // Preset modes require a non-empty preset query; chat modes require non-empty local input
  const hasValidQuery =
    selectedModeCategory === 'workflow'
      ? Boolean(activePreset?.query?.trim())
      : Boolean(localQuery?.trim())
  
  const readyForMode =
    selectedModeCategory === 'workflow' ? isWorkflowReady :
    true
  const submitButtonDisabled = !hasValidQuery || !observerId || !readyForMode

  // Memoized placeholder to prevent re-computation
  const placeholder = useMemo(() => {
    return selectedModeCategory === 'workflow'
      ? "Enter your objective for workflow execution... I'll create a todo-list and execute tasks sequentially!"
      : "Ask me anything... I can use tools to help you! (Type /summarize to summarize, or 'text /summarize' to summarize then send text)"
  }, [selectedModeCategory])

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

      {/* Validation message for Workflow mode - no preset selected */}
      {selectedModeCategory === 'workflow' && !getActivePreset('workflow') && (
        <div className="px-4">
          <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-blue-600 dark:text-blue-400 font-medium">
              ℹ️ Workflow mode requires a preset to be selected first. Use the mode selector to choose a preset.
            </span>
          </div>
        </div>
      )}

      {/* Validation message for Workflow mode - no Workflow folder selected */}
      {selectedModeCategory === 'workflow' && getActivePreset('workflow') && !isRequiredFolderSelected && (
        <div className="px-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-yellow-600 dark:text-yellow-400">
              💡 Select a folder from Workflow/ directory to proceed with workflow
            </span>
          </div>
        </div>
      )}

      {/* Hint when no files in context */}
      {chatFileContext.length === 0 && (
        <div className="px-4">
          <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-0.5 mb-0">
            <span className="text-xs text-gray-500 dark:text-gray-400">
              💡 Click chat icon in workspace to add files, or type @ to search and add files
            </span>
          </div>
        </div>
      )}

      {/* Input Form */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            {/* Show compact preset info with action buttons for workflow mode */}
            {selectedModeCategory === 'workflow' && activePreset && !isEditingQuery ? (
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
                          ('Workflow')
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
                            onClick={() => {
                              if (canSubmit) {
                                // Clear local state immediately for UI responsiveness (only for non-preset modes and when observer is ready)
                                if (selectedModeCategory !== 'workflow' && observerId) {
                                  setLocalQuery('')
                                }
                                onSubmit(queryToSubmit)
                              }
                            }}
                            disabled={!observerId || !isRequiredFolderSelected}
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
            {selectedModeCategory === 'workflow' && isEditingQuery && (
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
                
                {/* Agent Mode Selector - Only for Chat Mode and when no preset is active */}
                {selectedModeCategory === 'chat' && !chatActivePreset && (
                  <div className="flex items-center gap-2">
                    {/* Code Execution Mode Toggle */}
                    <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => setUseCodeExecutionMode(false)}
                            disabled={isStreaming}
                            className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                              !useCodeExecutionMode
                                ? 'agent-mode-selected rounded-l-md rounded-r-none'
                                : 'agent-mode-unselected rounded-none'
                            }`}
                          >
                            <Sparkles className="w-3 h-3 inline mr-1" />
                            Simple
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Simple mode - Direct MCP tool access</p>
                        </TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => setUseCodeExecutionMode(true)}
                            disabled={isStreaming}
                            className={`px-2 py-1 text-xs font-medium transition-colors ${
                              useCodeExecutionMode
                                ? 'agent-mode-selected rounded-r-md rounded-l-none'
                                : 'agent-mode-unselected rounded-none'
                            }`}
                          >
                            <Code2 className="w-3 h-3 inline mr-1" />
                            Code Exec
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Code Exec mode - MCP tools accessed via generated Go code</p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                )}
                
                {/* Server and LLM Selection for Chat mode - only show when no preset is active */}
                {selectedModeCategory === 'chat' && !chatActivePreset && (
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
      
      {/* Command Selection Dialog */}
      <CommandSelectionDialog
        isOpen={showCommandDialog}
        onClose={handleCommandDialogClose}
        onSelectCommand={handleCommandSelect}
        searchQuery={commandSearchQuery}
        position={commandDialogPosition}
      />
      
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
}

ChatInputComponent.displayName = 'ChatInput'

export const ChatInput = React.memo(ChatInputComponent)
