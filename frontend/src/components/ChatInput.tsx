import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'
import { Send, Square, Code2, Sparkles, Loader2, FolderOpen } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import { Checkbox } from './ui/checkbox'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { CircularProgress } from './ui/CircularProgress'
import { getEventData, isEventType } from '../generated/event-types'
import type { TokenUsageEvent } from '../generated/events'
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

// Stable empty array reference to avoid infinite loops in selectors
const EMPTY_EVENTS: never[] = []

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming
}) => {
  // Store subscriptions
  const {
    agentMode,
    setWorkspaceMinimized
  } = useAppStore()
  
  // Use selectors to subscribe only to specific values, reducing re-renders
  const activeTabId = useChatStore(state => state.activeTabId)
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const sessionId = useChatStore(state => state.sessionId)
  
  // Get active tab and its config (ChatInput is only rendered in chat mode)
  // Use selector to get only the tab we need, preventing re-renders when other tabs change
  const activeTab = useChatStore(state => 
    activeTabId ? state.chatTabs[activeTabId] : undefined
  )
  
  // Memoize tabConfig to prevent unnecessary re-renders
  const tabConfig = useMemo(() => activeTab?.config, [activeTab?.config])
  
  // CRITICAL: Always use tab's status - never fall back to global to prevent mixing
  // If no active tab, this is an error condition (tabs should always exist)
  const isStreaming = activeTab?.isStreaming ?? false
  const tabSessionId = activeTab?.sessionId ?? null
  
  // Note: activeTab may be undefined during initial render before tabs are created
  // This is expected and will resolve once the tab store initializes
  
  // Get all tab events from the store (stable selector)
  const allTabEvents = useChatStore(state => state.tabEvents)

  // Derive tab-specific events with useMemo (avoids selector closure issues)
  const tabEvents = useMemo(() => {
    if (!tabSessionId) return EMPTY_EVENTS
    return allTabEvents[tabSessionId] ?? EMPTY_EVENTS
  }, [tabSessionId, allTabEvents])

  // Throttle recalculation - only update when event count changes by 5+
  const eventCountBatch = Math.floor(tabEvents.length / 5)

  // Find the latest token usage (optimized with backward iteration)
  const { contextUsagePercent, latestTokenUsage } = useMemo(() => {
    if (tabEvents.length === 0) return { contextUsagePercent: null, latestTokenUsage: null }

    // Iterate backwards (newest first) to find the latest quickly
    let latestTokenUsageEvent = null
    let latestTotalEvent = null

    for (let i = tabEvents.length - 1; i >= 0 && !latestTotalEvent; i--) {
      const event = tabEvents[i]
      if (event.type === 'token_usage') {
        const tokenUsageData = event.data?.data?.token_usage
        if (tokenUsageData?.context === 'conversation_total') {
          latestTotalEvent = event
          break
        }
        if (!latestTokenUsageEvent) {
          latestTokenUsageEvent = event
        }
      }
    }

    const latestEvent = latestTotalEvent || latestTokenUsageEvent

    if (latestEvent) {
      let tokenUsage: TokenUsageEvent | null = null
      if (isEventType(latestEvent, 'token_usage')) {
        tokenUsage = getEventData(latestEvent) as TokenUsageEvent
      } else {
        tokenUsage = latestEvent.data?.data?.token_usage as TokenUsageEvent | undefined || null
      }

      if (tokenUsage) {
        const isTotalEvent = tokenUsage.context === 'conversation_total'
        const contextPercent = isTotalEvent
          ? (tokenUsage.generation_info?.context_usage_percent as number | undefined) ?? tokenUsage.context_usage_percent
          : tokenUsage.context_usage_percent ?? (tokenUsage.generation_info?.context_usage_percent as number | undefined)

        return {
          contextUsagePercent: contextPercent !== undefined && contextPercent !== null ? contextPercent : null,
          latestTokenUsage: tokenUsage
        }
      }
    }

    // Fallback: Check LLM generation end events (iterate backwards)
    for (let i = tabEvents.length - 1; i >= 0; i--) {
      const event = tabEvents[i]
      if (event.type === 'llm_generation_end') {
        const llmData = event.data?.data?.llm_generation_end
        const contextPercent = llmData?.metadata?.context_usage_percent as number | undefined

        if (contextPercent && contextPercent > 0) {
          const minimalTokenUsage = {
            context_usage_percent: contextPercent,
            model_context_window: llmData?.metadata?.model_context_window as number | undefined,
            context_window_usage: llmData?.metadata?.current_context_window_usage as number | undefined,
          }
          return {
            contextUsagePercent: contextPercent,
            latestTokenUsage: minimalTokenUsage
          }
        }
      }
    }

    return { contextUsagePercent: null, latestTokenUsage: null }
  }, [tabEvents, eventCountBatch])
  
  // Always use tab-specific config (ChatInput is only in chat mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  // Use ?? instead of || to preserve false values (user's selection)
  // Only default to false if the value is undefined/null (not explicitly set)
  const useCodeExecutionMode = useMemo(() => tabConfig?.useCodeExecutionMode ?? false, [tabConfig?.useCodeExecutionMode])
  const enableWorkspaceAccess = useMemo(() => tabConfig?.enableWorkspaceAccess ?? true, [tabConfig?.enableWorkspaceAccess])
  
  // File context operations (always update tab config)
  const removeFileFromContext = useCallback((path: string) => {
    if (activeTabId && activeTab) {
      const newFileContext = chatFileContext.filter(f => f.path !== path)
      setTabConfig(activeTabId, { fileContext: newFileContext })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const clearFileContext = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { fileContext: [] })
    }
  }, [activeTabId, setTabConfig])
  
  const addFileToContext = useCallback((file: { name: string; path: string; type: 'file' | 'folder' }) => {
    if (activeTabId && activeTab) {
      const newFileContext = [...chatFileContext, file]
      setTabConfig(activeTabId, { fileContext: newFileContext })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const setUseCodeExecutionMode = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { useCodeExecutionMode: enabled })
    }
  }, [activeTabId, setTabConfig])

  const setEnableWorkspaceAccess = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { enableWorkspaceAccess: enabled })
      // Open workspace sidebar when workspace access is enabled
      if (enabled) {
        setWorkspaceMinimized(false)
      }
    }
  }, [activeTabId, setTabConfig, setWorkspaceMinimized])
  
  // Get preset info for chat mode
  const { getActivePreset, activePresetIds, customPresets, predefinedPresets } = usePresetApplication()
  
  // Get input text from tab config (source of truth for persistence)
  const storedInputText = tabConfig?.inputText || ''

  // Local state for immediate UI updates (prevents Zustand updates on every keystroke)
  const [localInputText, setLocalInputText] = useState(storedInputText)
  const inputText = localInputText

  // Debounce ref for syncing to store
  const syncToStoreTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Sync local state FROM store when store changes externally (preset sync, etc.)
  useLayoutEffect(() => {
    // Only sync if store value differs and we're not in the middle of typing
    if (storedInputText !== localInputText && !syncToStoreTimeoutRef.current) {
      setLocalInputText(storedInputText)
    }
  }, [storedInputText]) // Intentionally exclude localInputText to avoid loops

  // Cleanup timeout refs on unmount
  useEffect(() => {
    return () => {
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
      }
    }
  }, [])

  // Get queued messages from tab config
  const queuedMessages = useMemo(() => tabConfig?.queuedMessages || [], [tabConfig?.queuedMessages])
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)
  
  const {
    enabledServers: availableServers,
    setChatSelectedServers
  } = useMCPStore()
  
  // Use tab-specific servers - memoize to prevent re-renders
  const manualSelectedServers = useMemo(() => tabConfig?.selectedServers || [], [tabConfig?.selectedServers])
  
  // Server operations (always update tab config AND sync to chat-specific MCP store)
  // This ensures new chat tabs inherit the user's manual server selection
  const onManualServerToggle = useCallback((server: string) => {
    if (activeTabId) {
      const newServers = manualSelectedServers.includes(server)
        ? manualSelectedServers.filter(s => s !== server)
        : [...manualSelectedServers, server]
      setTabConfig(activeTabId, { selectedServers: newServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, manualSelectedServers, setTabConfig, setChatSelectedServers])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTabId) {
      // availableServers is already an array of server names (strings)
      const allServers = [...availableServers]
      setTabConfig(activeTabId, { selectedServers: allServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(allServers)
    }
  }, [activeTabId, availableServers, setTabConfig, setChatSelectedServers])

  const onClearAllServers = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedServers: ["NO_SERVERS"] })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(["NO_SERVERS"])
    }
  }, [activeTabId, setTabConfig, setChatSelectedServers])
  
  const {
    availableLLMs,
    getCurrentLLMOption,
    refreshAvailableLLMs: onRefreshAvailableLLMs
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()

  // LLM selection (always update tab config)
  const onPrimaryLLMSelect = useCallback((llm: LLMOption) => {
    if (activeTabId) {
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
      
      setTabConfig(activeTabId, { llmConfig: newConfig })
    }
  }, [activeTabId, tabConfig?.llmConfig, setTabConfig])

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (tabConfig?.llmConfig) {
      const config = tabConfig.llmConfig
      // Try to find matching LLM in available list for richer metadata
      const foundLLM = availableLLMs.find(llm =>
        llm.provider === config.provider && llm.model === config.model_id
      )
      if (foundLLM) return foundLLM

      // If not found in available list, create option from tab config
      // This preserves user's selection even if model list hasn't loaded
      if (config.provider && config.model_id) {
        return {
          provider: config.provider,
          model: config.model_id,
          label: `${config.provider} - ${config.model_id}`,
          description: 'Selected model'
        }
      }
    }
    return getCurrentLLMOption()
  }, [tabConfig?.llmConfig, availableLLMs, getCurrentLLMOption])
  
  // Preset folder selection
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  
  // Track previous input value to distinguish user deletion from programmatic clearing
  const prevInputTextRef = useRef<string>('')
  
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

  // Auto-resize textarea based on content
  const adjustTextareaHeight = useCallback(() => {
    if (textareaRef.current) {
      const textarea = textareaRef.current
      // Reset height to auto to get correct scrollHeight
      textarea.style.height = 'auto'
      // Calculate new height (min 40px for 2 lines, max 100px)
      // scrollHeight includes padding, so we get the exact content height
      const newHeight = Math.min(Math.max(textarea.scrollHeight, 40), 100)
      textarea.style.height = `${newHeight}px`
    }
  }, [])

  // Get active preset for chat mode (used for preset query sync and UI)
  const chatActivePreset = getActivePreset('chat')
  
  // Sync tab config inputText with preset query when preset is selected
  useEffect(() => {
    const activePresetId = activePresetIds['chat']
    
    if (activePresetId && activeTabId) {
      // Find the preset
      const preset = customPresets.find(p => p.id === activePresetId) || 
                    predefinedPresets.find(p => p.id === activePresetId)
      
      if (preset && preset.query) {
        // Sync tab config with preset query
        setTabConfig(activeTabId, { inputText: preset.query })
      }
    } else if (!activePresetId && activeTabId) {
      // No preset active, clear input text
      setTabConfig(activeTabId, { inputText: '' })
    }
  }, [activePresetIds, customPresets, predefinedPresets, activeTabId, setTabConfig])

  // Sync ref with inputText when it changes externally (preset sync, programmatic clearing, etc.)
  useEffect(() => {
    prevInputTextRef.current = inputText || ''
  }, [inputText])

  // Set initial height and auto-resize textarea when inputText changes
  useEffect(() => {
    if (textareaRef.current) {
      // Set initial height to 2 lines (40px) if empty
      if (!inputText || inputText.trim() === '') {
        textareaRef.current.style.height = '40px'
      } else {
        adjustTextareaHeight()
      }
    }
  }, [inputText, adjustTextareaHeight])
  
  // Set initial height on mount
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = '40px'
    }
  }, [])

  // Open/close workspace sidebar based on workspace access setting
  // Also close file dialog if workspace access is disabled
  useEffect(() => {
    if (enableWorkspaceAccess) {
      setWorkspaceMinimized(false) // Open workspace sidebar
    } else {
      setWorkspaceMinimized(true) // Close workspace sidebar
      // Close file dialog if workspace access is disabled
      if (showFileDialog) {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
    }
  }, [enableWorkspaceAccess, setWorkspaceMinimized, showFileDialog])

  // Consolidated query selection logic
  const queryToSubmit = useMemo(() => {
    return inputText
  }, [inputText])

  // Guard to prevent submission before session is ready
  // Allow submission if not streaming, or allow queuing if streaming
  const canSubmit = useMemo(() => {
    return queryToSubmit?.trim() && tabSessionId
  }, [queryToSubmit, tabSessionId])
  
  // Can submit immediately (not streaming) or can queue (streaming)
  const canSubmitImmediately = useMemo(() => {
    return queryToSubmit?.trim() && !isStreaming && tabSessionId
  }, [queryToSubmit, isStreaming, tabSessionId])

  // Ref for debounced file removal check
  const fileRemovalTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    const previousValue = prevInputTextRef.current

    // Update local state immediately for fast UI response
    setLocalInputText(newValue)

    // Debounce sync to Zustand store (300ms delay)
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
    }
    syncToStoreTimeoutRef.current = setTimeout(() => {
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newValue })
      }
      syncToStoreTimeoutRef.current = null
    }, 300)

    // Update ref for next comparison
    prevInputTextRef.current = newValue

    // Auto-resize textarea
    adjustTextareaHeight()

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
    // Check for @ symbol and update file dialog state (only if no / command active and workspace access is enabled)
    else if (lastAtIndex >= 0 && !showCommandDialog && enableWorkspaceAccess) {
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

    // Debounce file reference removal check (500ms delay)
    // This prevents expensive iteration on every keystroke
    if (fileRemovalTimeoutRef.current) {
      clearTimeout(fileRemovalTimeoutRef.current)
    }
    fileRemovalTimeoutRef.current = setTimeout(() => {
      // Check if any @file references were removed and remove them from context
      // Only remove if:
      // 1. The file reference existed in the previous input
      // 2. The file reference is missing in the new input
      // 3. The new input is shorter than the previous (user deleted it, not cleared programmatically)
      if (previousValue.length > newValue.length) {
        const removedFiles: string[] = []
        chatFileContext.forEach((file: { path: string }) => {
          const fileReference = '@' + file.path
          const wasInPrevious = previousValue.includes(fileReference)
          const isInNew = newValue.includes(fileReference)

          if (wasInPrevious && !isInNew) {
            removedFiles.push(file.path)
          }
        })
        removedFiles.forEach(filePath => {
          removeFileFromContext(filePath)
        })
      }
      fileRemovalTimeoutRef.current = null
    }, 500)
  }, [chatFileContext, removeFileFromContext, showCommandDialog, activeTabId, setTabConfig, enableWorkspaceAccess, adjustTextareaHeight])

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
          // Clear input after command (both local and store)
          setLocalInputText('')
          if (activeTabId) {
            setTabConfig(activeTabId, { inputText: '' })
          }
        }
        return
      }

      if (compactIndex >= 0) {
        // Handle compact command
        if (sessionId && !isSummarizing && !isStreaming) {
          const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
          handleCompact(textBeforeCompact || undefined)
          // Clear input after command (both local and store)
          setLocalInputText('')
          if (activeTabId) {
            setTabConfig(activeTabId, { inputText: '' })
          }
        }
        return
      }
      
      if (canSubmitImmediately) {
        // Clear input text immediately (both local and store)
        setLocalInputText('')
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
        onSubmit(queryToSubmit)
      } else if (canSubmit && isStreaming) {
        // Queue message when streaming - clear input (both local and store)
        setLocalInputText('')
        if (activeTabId) {
          const currentQueued = tabConfig?.queuedMessages || []
          setTabConfig(activeTabId, {
            inputText: '',
            queuedMessages: [...currentQueued, queryToSubmit.trim()]
          })
        }
      }
    }
    // Handle CTRL+Enter (Windows/Linux) or CMD+Enter (Mac) to add new line
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      const textarea = e.target as HTMLTextAreaElement
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const newValue = inputText.substring(0, start) + '\n' + inputText.substring(end)
      // Update local state immediately for fast UI
      setLocalInputText(newValue)

      // Set cursor position after the newline
      setTimeout(() => {
        textarea.selectionStart = textarea.selectionEnd = start + 1
      }, 0)
    }
  }, [inputText, onSubmit, showFileDialog, showCommandDialog, tabSessionId, canSubmit, canSubmitImmediately, queryToSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, activeTabId, activeTab?.tabId, activeTab?.sessionId, sessionId, setTabConfig, adjustTextareaHeight, tabConfig?.queuedMessages])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()

    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    const summarizeIndex = trimmedQuery.indexOf('/summarize')
    const compactIndex = trimmedQuery.indexOf('/compact')

    if (summarizeIndex >= 0) {
      if (sessionId && !isSummarizing && !isStreaming) {
        const textBeforeSummarize = trimmedQuery.substring(0, summarizeIndex).trim()
        handleSummarize(textBeforeSummarize || undefined)
        setLocalInputText('')
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
      }
      return
    }

    if (compactIndex >= 0) {
      if (sessionId && !isSummarizing && !isStreaming) {
        const textBeforeCompact = trimmedQuery.substring(0, compactIndex).trim()
        handleCompact(textBeforeCompact || undefined)
        setLocalInputText('')
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: '' })
        }
      }
      return
    }

    if (canSubmitImmediately) {
      setLocalInputText('')
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: '' })
      }
      onSubmit(queryToSubmit)
    } else if (canSubmit && isStreaming) {
      setLocalInputText('')
      if (activeTabId) {
        const currentQueued = tabConfig?.queuedMessages || []
        setTabConfig(activeTabId, {
          inputText: '',
          queuedMessages: [...currentQueued, queryToSubmit.trim()]
        })
      }
    }
  }, [canSubmit, canSubmitImmediately, activeTabId, tabSessionId, queryToSubmit, onSubmit, isSummarizing, isStreaming, handleSummarize, handleCompact, setTabConfig, sessionId, tabConfig?.queuedMessages])

  // File selection handlers
  const handleCommandSelect = useCallback((command: string) => {
    if (!textareaRef.current || slashPosition === -1 || !activeTabId) return

    const beforeSlash = inputText.substring(0, slashPosition)
    const afterSearch = inputText.substring(slashPosition + 1 + commandSearchQuery.length)
    const newQuery = beforeSlash + '/' + command + ' ' + afterSearch

    // Update local state immediately for fast UI
    setLocalInputText(newQuery)
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')

    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeSlash.length + '/'.length + command.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, slashPosition, commandSearchQuery, activeTabId])

  const handleFileSelect = useCallback((file: PlannerFile) => {
    if (!textareaRef.current || atPosition === -1 || !activeTabId) return

    const beforeAt = inputText.substring(0, atPosition)
    const afterSearch = inputText.substring(atPosition + 1 + fileSearchQuery.length)
    const newQuery = beforeAt + '@' + file.filepath + ' ' + afterSearch

    // Update local state immediately for fast UI
    setLocalInputText(newQuery)
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')

    // Add file/folder to context
    const fileContextItem = {
      name: file.filepath.split('/').pop() || file.filepath,
      path: file.filepath,
      type: file.type || 'file' as const
    }

    const isAlreadyInContext = chatFileContext.some((item: { path: string }) => item.path === file.filepath)
    if (!isAlreadyInContext) {
      addFileToContext(fileContextItem)
      scrollToFile(file.filepath)
    }

    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeAt.length + '@'.length + file.filepath.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile, activeTabId])

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
  const hasValidQuery = Boolean(inputText?.trim())
  const submitButtonDisabled = !hasValidQuery || !tabSessionId
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    return "Ask me anything... I can use tools to help you! (Type /summarize to summarize)"
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


      {/* Hint when no files in context - only show if workspace access is enabled */}
      {chatFileContext.length === 0 && enableWorkspaceAccess && (
        <div className="px-4">
          <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-1.5 py-0.5 mb-0">
            <span className="text-[10px] text-gray-500 dark:text-gray-400">
              💡 Click chat icon in workspace to add files, or type @ to search and add files
            </span>
          </div>
        </div>
      )}

      {/* Input Form */}
      <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            {/* Queued messages indicator */}
            {queuedMessages.length > 0 && (
              <div className="space-y-1">
                {queuedMessages.map((msg: string, index: number) => (
                  <div key={index} className="flex items-start gap-2 px-2 py-1 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded text-xs text-blue-700 dark:text-blue-300">
                    <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse mt-1.5 flex-shrink-0"></div>
                    <span className="flex-1 break-words">
                      <span className="font-medium">#{index + 1}:</span> "{msg}"
                    </span>
                    <button
                      type="button"
                      onClick={() => {
                        if (activeTabId) {
                          const updated = queuedMessages.filter((_: string, i: number) => i !== index)
                          setTabConfig(activeTabId, { queuedMessages: updated })
                        }
                      }}
                      className="flex items-center justify-center w-5 h-5 rounded hover:bg-blue-200 dark:hover:bg-blue-800 text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 transition-colors flex-shrink-0 mt-0.5"
                      title="Delete from queue"
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  </div>
                ))}
              </div>
            )}
            {/* Show text input */}
            <Textarea
              ref={textareaRef}
              value={inputText}
              onChange={handleTextChange}
              onKeyDown={handleKeyDown}
              placeholder={placeholder}
              className="!min-h-[40px] max-h-[100px] resize-none text-xs overflow-y-auto leading-[1.3] !py-1 !px-3 placeholder:text-xs"
              disabled={isSummarizing || !tabSessionId}
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
                            disabled={isStreaming || isSummarizing}
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
                            disabled={isStreaming || isSummarizing}
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
                      disabled={isStreaming || isSummarizing}
                    />
                    <LLMSelectionDropdown
                      availableLLMs={availableLLMs}
                      selectedLLM={primaryLLM}
                      onLLMSelect={onPrimaryLLMSelect}
                      onRefresh={onRefreshAvailableLLMs}
                      disabled={isStreaming || isSummarizing}
                      openDirection="up"
                    />
                    {/* Workspace Access Toggle */}
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex items-center gap-1.5 px-2 py-1 border border-gray-300 dark:border-gray-600 rounded-md">
                          <Checkbox
                            id="workspace-access"
                            checked={enableWorkspaceAccess}
                            onCheckedChange={(checked) => setEnableWorkspaceAccess(checked === true)}
                            disabled={isStreaming || isSummarizing}
                            className="h-3.5 w-3.5"
                          />
                          <label
                            htmlFor="workspace-access"
                            className="text-xs font-medium cursor-pointer flex items-center gap-1 text-gray-700 dark:text-gray-300"
                          >
                            <FolderOpen className="w-3 h-3" />
                            Workspace
                          </label>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>{enableWorkspaceAccess ? 'Workspace file access enabled' : 'Workspace file access disabled'}</p>
                      </TooltipContent>
                    </Tooltip>
                  </div>
                )}
                
                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-2">
                  {/* Context completion indicator */}
                  {contextUsagePercent !== null && (
                    <CircularProgress 
                      percentage={contextUsagePercent} 
                      size={24}
                      strokeWidth={2.5}
                      tokenUsage={latestTokenUsage}
                    />
                  )}
                  {isSummarizing ? (
                    <div className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400">
                      <Loader2 className="w-4 h-4 animate-spin" />
                      <span>Summarizing...</span>
                    </div>
                  ) : isStreaming ? (
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
                          {!inputText?.trim()
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
