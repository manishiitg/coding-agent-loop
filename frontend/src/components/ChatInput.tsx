import React, { useRef, useCallback, useMemo, useState, useEffect } from 'react'
import { Send, Square, Code2, Sparkles } from 'lucide-react'
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
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { agentApi } from '../services/api'

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
}

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming
}) => {
  // Store subscriptions
  const {
    agentMode
  } = useAppStore()
  
  const {
    sessionId,
    getActiveTab,
    setTabConfig,
    activeTabId
  } = useChatStore()
  
  // Get active tab and its config (ChatInput is only rendered in chat mode)
  const activeTab = getActiveTab()
  const tabConfig = activeTab?.config
  
  // CRITICAL: Always use tab's status - never fall back to global to prevent mixing
  // If no active tab, this is an error condition (tabs should always exist)
  const isStreaming = activeTab?.isStreaming ?? false
  const tabSessionId = activeTab?.sessionId ?? null
  
  // Warn if no active tab (tabs should always exist)
  if (!activeTab) {
    console.warn(`[ChatInput] No active tab - this should not happen in tab mode`)
  }
  
  // Always use tab-specific config (ChatInput is only in chat mode)
  const chatFileContext = tabConfig?.fileContext || []
  const useCodeExecutionMode = tabConfig?.useCodeExecutionMode || false
  
  // File context operations (always update tab config)
  const removeFileFromContext = useCallback((path: string) => {
    if (activeTab) {
      const newFileContext = chatFileContext.filter(f => f.path !== path)
      setTabConfig(activeTab.tabId, { fileContext: newFileContext })
    }
  }, [activeTab, chatFileContext, setTabConfig])
  
  const clearFileContext = useCallback(() => {
    if (activeTab) {
      setTabConfig(activeTab.tabId, { fileContext: [] })
    }
  }, [activeTab, setTabConfig])
  
  const addFileToContext = useCallback((file: { name: string; path: string; type: 'file' | 'folder' }) => {
    if (activeTab) {
      const newFileContext = [...chatFileContext, file]
      setTabConfig(activeTab.tabId, { fileContext: newFileContext })
    }
  }, [activeTab, chatFileContext, setTabConfig])
  
  const setUseCodeExecutionMode = useCallback((enabled: boolean) => {
    if (activeTab) {
      setTabConfig(activeTab.tabId, { useCodeExecutionMode: enabled })
    }
  }, [activeTab, setTabConfig])
  
  // Get preset info for chat mode
  const { getActivePreset, activePresetIds, customPresets, predefinedPresets } = usePresetApplication()
  
  // Local state for input to prevent global re-renders on every keystroke
  // Sync with tab config
  const [localQuery, setLocalQuery] = useState('')
  
  // Sync localQuery with tab config when tab changes
  useEffect(() => {
    if (tabConfig) {
      setLocalQuery(tabConfig.inputText)
    }
  }, [activeTabId, tabConfig])
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)
  
  const {
    enabledServers: availableServers
  } = useMCPStore()
  
  // Use tab-specific servers
  const manualSelectedServers = tabConfig?.selectedServers || []
  
  // Server operations (always update tab config)
  const onManualServerToggle = useCallback((server: string) => {
    if (activeTab) {
      const newServers = manualSelectedServers.includes(server)
        ? manualSelectedServers.filter(s => s !== server)
        : [...manualSelectedServers, server]
      setTabConfig(activeTab.tabId, { selectedServers: newServers })
    }
  }, [activeTab, manualSelectedServers, setTabConfig])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTab) {
      // availableServers is already an array of server names (strings)
      setTabConfig(activeTab.tabId, { selectedServers: [...availableServers] })
    }
  }, [activeTab, availableServers, setTabConfig])
  
  const onClearAllServers = useCallback(() => {
    if (activeTab) {
      setTabConfig(activeTab.tabId, { selectedServers: [] })
    }
  }, [activeTab, setTabConfig])
  
  const {
    availableLLMs,
    getCurrentLLMOption,
    refreshAvailableLLMs: onRefreshAvailableLLMs
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()

  // LLM selection (always update tab config)
  const onPrimaryLLMSelect = useCallback((llm: LLMOption) => {
    if (activeTab) {
      // Get current config to preserve fallback models and cross-provider fallback
      const currentConfig = tabConfig?.llmConfig || {
        provider: 'openrouter',
        model_id: '',
        fallback_models: [],
        cross_provider_fallback: undefined
      }
      
      const newConfig = {
        ...currentConfig, // ✅ Preserve all existing configuration
        provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex',
        model_id: llm.model
      }
      
      setTabConfig(activeTab.tabId, { llmConfig: newConfig })
    }
  }, [activeTab, tabConfig?.llmConfig, setTabConfig])

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (tabConfig?.llmConfig) {
      // Convert tab's LLM config to LLMOption format
      const config = tabConfig.llmConfig
      const allLLMs = availableLLMs
      return allLLMs.find(llm => 
        llm.provider === config.provider && llm.model === config.model_id
      ) || getCurrentLLMOption()
    }
    return getCurrentLLMOption()
  }, [tabConfig?.llmConfig, availableLLMs, getCurrentLLMOption])
  
  // Preset folder selection
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

  // Get active preset for chat mode (used for preset query sync and UI)
  const chatActivePreset = getActivePreset('chat')
  
  // Sync localQuery with preset query when preset is selected
  useEffect(() => {
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
  }, [activePresetIds, customPresets, predefinedPresets])

  // Consolidated query selection logic
  const queryToSubmit = useMemo(() => {
    return localQuery
  }, [localQuery])

  // Guard to prevent submission before session is ready
  // Use active tab's status: allow submission if not streaming (completion is fine, user can continue conversation)
  const canSubmit = useMemo(() => {
    return queryToSubmit?.trim() && !isStreaming && tabSessionId
  }, [queryToSubmit, isStreaming, tabSessionId])
  
  // Debug logging for submission issues
  React.useEffect(() => {
    const hasValidQuery = Boolean(localQuery?.trim())
    const canSubmitValue = queryToSubmit?.trim() && !isStreaming && tabSessionId
    const submitButtonDisabledValue = !hasValidQuery || !tabSessionId
    
    console.log('[ChatInput] Session ID check:', {
      activeTab: activeTab?.tabId,
      tabSessionId: activeTab?.sessionId,
      effectiveSessionId: tabSessionId,
      canSubmit: canSubmitValue,
      hasValidQuery,
      submitButtonDisabled: submitButtonDisabledValue,
      localQuery: localQuery?.substring(0, 50) // First 50 chars for debugging
    })
  }, [activeTab?.tabId, activeTab?.sessionId, tabSessionId, queryToSubmit, isStreaming, localQuery, canSubmit])

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    setLocalQuery(newValue) // Update local state
    
    // Also update tab config (debounced updates handled separately)
    if (activeTab) {
      setTabConfig(activeTab.tabId, { inputText: newValue })
    }

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
  }, [chatFileContext, removeFileFromContext, showCommandDialog, activeTab, setTabConfig])

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
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
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
  }, [sessionId, isSummarizing, isStreaming, tabSessionId, onSubmit])

  // Handle manual context compaction (context editing)
  // If messageToSendAfter is provided, it will be sent as a user message after compaction completes
  const handleCompact = useCallback(async (messageToSendAfter?: string) => {
    if (!sessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true) // Reuse the same loading state
    try {
      const response = await agentApi.compactContext(sessionId)
      console.log('[CONTEXT_EDITING] Success:', response)
      // Show success notification
      alert(`Context compacted successfully!\nCompacted: ${response.compacted_count} tool responses\nTokens saved: ${response.total_tokens_saved?.toLocaleString() || 0}`)
      
      // If there's a message to send after compaction, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
        // Small delay to ensure compaction is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[CONTEXT_EDITING] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      alert(`Failed to compact context: ${errorMessage}`)
    } finally {
      setIsSummarizing(false)
    }
  }, [sessionId, isSummarizing, isStreaming, tabSessionId, onSubmit])

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
      console.log('[ChatInput] Enter key pressed, handleKeyDown:', JSON.stringify({
        queryToSubmit: queryToSubmit?.substring(0, 50),
        queryToSubmitLength: queryToSubmit?.length,
        canSubmit,
        sessionId: tabSessionId,
        isStreaming,
        activeTab: activeTab?.tabId,
        tabSessionId: activeTab?.sessionId,
        localQuery: localQuery?.substring(0, 50),
        localQueryLength: localQuery?.length
      }, null, 2))
      
      // Check for slash commands
      const trimmedQuery = queryToSubmit?.trim() || ''
      const summarizeIndex = trimmedQuery.indexOf('/summarize')
      const compactIndex = trimmedQuery.indexOf('/compact')
      
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
      
      if (compactIndex >= 0) {
        // Handle compact command
        if (sessionId && !isSummarizing && !isStreaming) {
          // Extract text before /compact
          const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
          
          // If there's text before /compact, send it after compaction
          // Otherwise, just compact
          handleCompact(textBeforeCompact || undefined)
          setLocalQuery('') // Clear input after command
        }
        return
      }
      
      if (canSubmit) {
        console.log('[ChatInput] canSubmit is true, calling onSubmit:', queryToSubmit)
        // Clear local state immediately for UI responsiveness (only for non-preset modes and when session is ready)
        if (tabSessionId) {
          setLocalQuery('')
        }
        // Call onSubmit with the query directly - no global state coordination needed!
        onSubmit(queryToSubmit)
      } else {
        console.warn('[ChatInput] canSubmit is false, not submitting:', JSON.stringify({
          hasQuery: Boolean(queryToSubmit?.trim()),
          queryLength: queryToSubmit?.length,
          isStreaming,
          hasSessionId: Boolean(tabSessionId),
          sessionIdValue: tabSessionId,
          tabSessionId: activeTab?.sessionId,
          canSubmit,
          activeTab: activeTab?.tabId,
          localQuery: localQuery?.substring(0, 50),
          localQueryLength: localQuery?.length
        }, null, 2))
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
  }, [localQuery, onSubmit, showFileDialog, showCommandDialog, tabSessionId, canSubmit, queryToSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, activeTab?.tabId, activeTab?.sessionId, sessionId])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()
    
    console.log('[ChatInput] handleSubmit called:', {
      queryToSubmit,
      canSubmit,
      sessionId: tabSessionId,
      isStreaming,
      activeTab: activeTab?.tabId
    })
    
    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    const summarizeIndex = trimmedQuery.indexOf('/summarize')
    const compactIndex = trimmedQuery.indexOf('/compact')
    
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
    
    if (compactIndex >= 0) {
      // Handle compact command
      if (sessionId && !isSummarizing && !isStreaming) {
        // Extract text before /compact
        const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
        
        // If there's text before /compact, send it after compaction
        // Otherwise, just compact
        handleCompact(textBeforeCompact || undefined)
        setLocalQuery('') // Clear input after command
      }
      return
    }
    
    if (canSubmit) {
      console.log('[ChatInput] Calling onSubmit with query:', queryToSubmit)
      // Clear local state immediately for UI responsiveness when observer is ready
      if (tabSessionId) {
        setLocalQuery('')
      }
      // Call onSubmit with the query directly - no global state coordination needed!
      onSubmit(queryToSubmit)
    } else {
      console.warn('[ChatInput] Cannot submit:', {
        hasQuery: Boolean(queryToSubmit?.trim()),
        isStreaming,
          hasSessionId: Boolean(tabSessionId),
        canSubmit
      })
    }
  }, [canSubmit, tabSessionId, queryToSubmit, onSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, setLocalQuery, activeTab, sessionId])

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

  // Removed editing preset query functionality - not needed for chat mode

  // Check if query is valid
  const hasValidQuery = Boolean(localQuery?.trim())
  const submitButtonDisabled = !hasValidQuery || !tabSessionId
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    return "Ask me anything... I can use tools to help you! (Type /summarize to summarize, /compact to compact context, or 'text /command' to run command then send text)"
  }, [])

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
            isRequiredFolderSelected={true}
          />
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
            {/* Show text input */}
            <Textarea
              ref={textareaRef}
              value={localQuery}
              onChange={handleTextChange}
              onKeyDown={handleKeyDown}
              placeholder={placeholder}
              className="min-h-[60px] max-h-[100px] resize-none text-sm"
              disabled={isStreaming || !tabSessionId}
              data-testid="chat-input-textarea"
            />
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                
                {/* Agent Mode Selector - Only show when no preset is active */}
                {!chatActivePreset && (
                  <div className="flex items-center gap-2">
                    {/* Code Execution Mode Toggle */}
                    <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => setUseCodeExecutionMode(false)}
                            disabled={isStreaming}
                            data-testid="agent-mode-simple"
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
                            data-testid="agent-mode-code-exec"
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
                
                {/* Server and LLM Selection - only show when no preset is active */}
                {!chatActivePreset && (
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
                
                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-2">
                  {isStreaming ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button 
                          type="button"
                          variant="destructive"
                          onClick={onStopStreaming}
                          size="sm"
                          className="px-3"
                          data-testid="chat-stop-button"
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
                          data-testid="chat-submit-button"
                        >
                          <Send className="w-4 h-4" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>
                          {!localQuery?.trim()
                            ? 'Type a message to send'
                            : !tabSessionId 
                              ? 'Session not ready yet' 
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
