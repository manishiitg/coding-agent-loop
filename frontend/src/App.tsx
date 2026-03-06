import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useCallback, useRef, useState, forwardRef } from "react";
import { ThemeProvider } from "./contexts/ThemeContext.tsx";
import WorkspaceSidebar from "./components/WorkspaceSidebar";
import Workspace from "./components/Workspace.tsx";
import ChatArea, { type ChatAreaRef } from "./components/ChatArea.tsx";
import { MarkdownRenderer, MermaidDiagram } from "./components/ui/MarkdownRenderer";
import { CsvRenderer } from "./components/ui/CsvRenderer";
import { XlsxRenderer } from "./components/ui/XlsxRenderer";
import { DocxRenderer } from "./components/ui/DocxRenderer";
import { PdfRenderer } from "./components/ui/PdfRenderer";
import { HtmlRenderer } from "./components/ui/HtmlRenderer";
import { resetSessionId, agentApi } from "./services/api";
import { AuthWrapper } from "./components/AuthWrapper";
import type { ActiveSessionInfo, FileVersion } from "./services/api-types";
import FileRevisionsModal from "./components/workspace/FileRevisionsModal";
import PushToGistDialog from "./components/workspace/PushToGistDialog";
import FileEditor from "./components/workspace/FileEditor";
import { isValidJSON } from "./utils/event-helpers";
import { prepareDomForPdfExport } from "./utils/pdfExport";
import { Edit, Save, X, Loader2, Download, Link, Github } from "lucide-react";
import { ModeSelectionModal } from "./components/ModeSelectionModal";
import { WorkflowLayout } from "./components/workflow";
import { ModePresetBar } from "./components/ModePresetBar";
import { ChatTabs } from "./components/ChatTabs";
import { useAppStore, useMCPStore, useGlobalPresetStore, useWorkspaceStore, useWorkflowStore, useChatStore } from "./stores";
import { restoreSession } from "./utils/sessionRestore";
import { useModeStore } from "./stores/useModeStore";
import { useAuthStore } from "./stores/useAuthStore";
import { useLLMDefaults } from "./hooks/useLLMDefaults";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./components/ui/tooltip";
import "./App.css";

// Extend window interface for global functions
declare global {
  interface Window {
    highlightFile?: (filepath: string) => void;
    toggleAutoScroll?: () => void;
  }
}

import { copyToClipboard } from './utils/textUtils'

const queryClient = new QueryClient();


// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so header with mode/preset selectors is visible
// Uses Zustand hooks to reactively update when tabs change
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, { onNewChat: () => void }>(({ onNewChat }, ref) => {
  // Use Zustand hooks to reactively subscribe to tab changes
  const activeTabId = useChatStore(state => state.activeTabId)
  
  // Always render ChatArea - it will show header even without sessionId
  // This allows users to select mode/preset even when no tab exists
  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      tabId={activeTabId || undefined}
    />
  )
})

// Utility function to detect code files and get their language
const getCodeFileLanguage = (filepath: string): string | null => {
  const ext = filepath.toLowerCase().split('.').pop() || ''
  const codeExtensions: Record<string, string> = {
    'go': 'go',
    'py': 'python',
    'ts': 'typescript',
    'tsx': 'typescript',
    'js': 'javascript',
    'jsx': 'javascript',
    'java': 'java',
    'c': 'c',
    'cpp': 'cpp',
    'cc': 'cpp',
    'cxx': 'cpp',
    'cs': 'csharp',
    'php': 'php',
    'rb': 'ruby',
    'sql': 'sql',
    'html': 'html',
    'htm': 'html',
    'css': 'css',
    'scss': 'scss',
    'sass': 'sass',
    'sh': 'shell',
    'bash': 'shell',
    'zsh': 'shell',
    'yaml': 'yaml',
    'yml': 'yaml',
    'xml': 'xml',
    'vue': 'vue',
    'svelte': 'svelte',
    'rs': 'rust',
    'swift': 'swift',
    'kt': 'kotlin',
    'scala': 'scala',
    'r': 'r',
    'lua': 'lua',
    'pl': 'perl',
    'dart': 'dart',
    'ex': 'elixir',
    'exs': 'elixir',
    'clj': 'clojure',
    'hs': 'haskell',
    'ml': 'ocaml',
    'fs': 'fsharp',
    'vb': 'vbnet',
    'ps1': 'powershell',
    'dockerfile': 'dockerfile',
    'makefile': 'makefile',
    'mk': 'makefile'
  }
  return codeExtensions[ext] || null
}

// Check if a file is a code file
const isCodeFile = (filepath: string): boolean => {
  return getCodeFileLanguage(filepath) !== null
}

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
    setFileContent,
    setLoadingFileContent,
    showRevisionsModal,
    setShowRevisionsModal,
    isEditMode,
    setIsEditMode,
    editedContent,
    setEditedContent,
    isSaving,
    getHasUnsavedChanges,
    saveFile,
    binaryFileData
  } = useWorkspaceStore()
  
  const [commitMessage, setCommitMessage] = useState('')
  const [showCommitDialog, setShowCommitDialog] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [isRestoring, setIsRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)
  const [isExportingPdf, setIsExportingPdf] = useState(false)
  const [showPushToGistDialog, setShowPushToGistDialog] = useState(false)
  const [exportProgress, setExportProgress] = useState<string | null>(null)
  const [shareCopied, setShareCopied] = useState(false)
  const markdownContentRef = useRef<HTMLDivElement>(null)
  
  // Ref to prevent duplicate default tab creation (React StrictMode runs effects twice)
  const hasCreatedDefaultTabRef = useRef<string | null>(null)

  
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

  // Handle download
  const handleDownload = useCallback(() => {
    if (!selectedFile || !fileContent) return

    const blob = new Blob([fileContent], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = selectedFile.path.split('/').pop() || 'download'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }, [selectedFile, fileContent])

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

  // Handle restore version
  const handleRestoreVersion = useCallback(async (version: FileVersion) => {
    if (!selectedFile) {
      setRestoreError('No file selected')
      return
    }

    setIsRestoring(true)
    setRestoreError(null)

    try {
      // Call restore API
      const response = await agentApi.restoreFileVersion(
        selectedFile.path,
        version.commit_hash,
        `Restore to version ${version.commit_hash.substring(0, 8)}: ${version.commit_message}`
      )

      if (response.success) {
        // Reload file content after successful restore
        setLoadingFileContent(true)
        try {
          const contentResponse = await agentApi.getPlannerFileContent(selectedFile.path)
          if (contentResponse.success && contentResponse.data) {
            let processedContent = contentResponse.data.content
            
            // Process the content to convert escaped newlines to actual newlines
            processedContent = processedContent
              .replace(/\\n/g, '\n')
              .replace(/\\t/g, '\t')
              .replace(/\\r/g, '\r')
            
            // Check if this is a JSON file
            const extensionIsJson = selectedFile.path.toLowerCase().endsWith('.json')
            const contentIsJson = isValidJSON(processedContent)
            
            if (extensionIsJson || contentIsJson) {
              try {
                const parsed = JSON.parse(processedContent)
                processedContent = JSON.stringify(parsed, null, 2)
              } catch {
                // Keep original content if JSON parsing fails
              }
            }
            
            setFileContent(processedContent)
            
            // Exit edit mode if we were in it
            if (isEditMode) {
              setIsEditMode(false)
              setEditedContent('')
            }
          }
        } catch (err) {
          console.error('Failed to reload file content after restore:', err)
          // Still close modal even if reload fails
        } finally {
          setLoadingFileContent(false)
        }

        // Close modal on success
        setShowRevisionsModal(false)
      } else {
        setRestoreError(response.message || 'Failed to restore file version')
      }
    } catch (error) {
      console.error('Failed to restore file version:', error)
      setRestoreError(error instanceof Error ? error.message : 'Failed to restore file version')
    } finally {
      setIsRestoring(false)
    }
  }, [selectedFile, isEditMode, setIsEditMode, setEditedContent, setFileContent, setLoadingFileContent, setShowRevisionsModal])

  // Handle export to PDF
  const handleExportPdf = useCallback(async () => {
    if (!markdownContentRef.current || !selectedFile) return
    setIsExportingPdf(true)
    setExportProgress(null)
    const filename = (selectedFile.name || selectedFile.path?.split('/').pop() || 'document')
      .replace(/\.[^.]+$/, '') + '.pdf'
    const isElectron = !!(window as any).electronAPI?.printToPDF

    try {
      const { restore } = await prepareDomForPdfExport(markdownContentRef.current)
      try {
        if (isElectron) {
          // Electron: printToPDF via IPC → direct file save
          await (window as any).electronAPI.printToPDF(filename)
        } else {
          // Web: inject @media print CSS to isolate the markdown panel, then print
          const printTarget = markdownContentRef.current
          printTarget.setAttribute('data-pdf-print', 'true')
          const style = document.createElement('style')
          style.textContent = `@media print {
            body * { visibility: hidden !important; }
            [data-pdf-print] { visibility: visible !important; position: fixed !important;
              top: 0 !important; left: 0 !important; width: 100% !important;
              background: white !important; }
            [data-pdf-print] * { visibility: visible !important; }
          }`
          document.head.appendChild(style)
          await new Promise<void>((resolve) => {
            window.addEventListener('afterprint', resolve, { once: true })
            window.print()
          })
          document.head.removeChild(style)
          printTarget.removeAttribute('data-pdf-print')
        }
      } finally {
        restore()
      }
    } catch (err) {
      console.error('PDF export failed:', err)
    } finally {
      setIsExportingPdf(false)
      setExportProgress(null)
    }
  }, [selectedFile])

  // Handle download raw markdown file

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
    
    // LLM list is refreshed after loadDefaultsFromBackend() in useLLMDefaults (so supported_providers is set)
    
    // Initialize global preset store
    useGlobalPresetStore.getState().refreshPresets()
    
    // Initialize workflow store (load phases)
    useWorkflowStore.getState().loadPhases()
  }, [])
  
  // Create default tab on page load (only for chat mode, not workflow mode)
  // In workflow mode, tabs are created when user starts a phase/execution
  useEffect(() => {
    if (!hasCompletedInitialSetup) return
    
    // Only create default tab for chat and multi-agent modes
    // (workflow tabs are created by WorkflowLayout)
    if (selectedModeCategory !== 'chat' && selectedModeCategory !== 'multi-agent') {
      return
    }
    
    // Prevent duplicate execution (React StrictMode runs effects twice)
    if (hasCreatedDefaultTabRef.current === selectedModeCategory) {
      return
    }
    
    const chatStore = useChatStore.getState()
    const existingTabs = Object.values(chatStore.chatTabs)
    
    // Filter to only tabs for current mode
    const modeTabs = existingTabs.filter(tab => 
      tab.metadata?.mode === selectedModeCategory
    )
    
    // If tabs already exist for this mode, skip
    if (modeTabs.length > 0) {
      return
    }
    
    // Mark as in progress for this mode
    hasCreatedDefaultTabRef.current = selectedModeCategory
    
    // Create default tab with new session ID
    const createDefaultTab = async () => {
      // Double-check tabs don't exist right before creating (race condition protection)
      const currentTabs = Object.values(useChatStore.getState().chatTabs)
      const currentModeTabs = currentTabs.filter(tab => 
        tab.metadata?.mode === selectedModeCategory
      )
      if (currentModeTabs.length === 0) {
        try {
          const tabName = selectedModeCategory === 'multi-agent' ? 'Agent Chat 1' : 'Chat 1'
          await chatStore.createChatTab(tabName, { mode: selectedModeCategory as 'chat' | 'workflow' | 'multi-agent' })
        } catch (error) {
          console.error('Failed to create default tab:', error)
          // Reset flag on error so it can retry
          hasCreatedDefaultTabRef.current = null
        }
      }
    }
    
    createDefaultTab()
  }, [hasCompletedInitialSetup, selectedModeCategory])

  // Ensure a chat tab is selected after restore (fix for page reload issue)
  // This ensures that when tabs are restored from localStorage, we select the first tab of the current mode
  // if activeTabId is null or invalid or belongs to a different mode
  useEffect(() => {
    if (!hasCompletedInitialSetup) return

    // When switching back to workflow mode, restore the active workflow execution tab and
    // ensure the chat panel is visible — otherwise activeTabId stays on whatever mode the
    // user came from (e.g. multi-agent) and the ChatArea inside WorkflowLayout shows wrong content.
    if (selectedModeCategory === 'workflow') {
      const chatStore = useChatStore.getState()
      const activeTabId = chatStore.activeTabId
      const activeTab = activeTabId ? chatStore.getTab(activeTabId) : null
      const hasValidActiveTab = activeTab && activeTab.metadata?.mode === 'workflow'

      if (!hasValidActiveTab) {
        // Find the most recent workflow execution tab (prefer tabs with sessionId)
        const workflowTabs = Object.values(chatStore.chatTabs)
          .filter(tab => tab.metadata?.mode === 'workflow' && (tab.sessionId || tab.isStreaming))
          .sort((a, b) => b.createdAt - a.createdAt)

        if (workflowTabs.length > 0) {
          console.log(`[App] Switching to workflow tab on mode restore: ${workflowTabs[0].tabId}`)
          chatStore.switchTab(workflowTabs[0].tabId)
          // Ensure the chat panel inside WorkflowLayout is visible
          useWorkflowStore.getState().setShowChatArea(true)
        } else {
          // No active workflow tabs - clear activeTabId so WorkflowLayout's ChatArea
          // doesn't display content from another mode (e.g. multi-agent chat)
          useChatStore.setState({ activeTabId: null })
        }
      }
      return
    }

    // For chat and multi-agent: select the first tab of the current mode
    // if activeTabId is null, invalid, or belongs to a different mode
    if (selectedModeCategory !== 'chat' && selectedModeCategory !== 'multi-agent') {
      return
    }

    const chatStore = useChatStore.getState()
    const activeTabId = chatStore.activeTabId

    // Check if activeTabId is null, points to a non-existent tab, or belongs to a different mode
    const activeTab = activeTabId ? chatStore.getTab(activeTabId) : null
    const hasValidActiveTab = activeTab && activeTab.metadata?.mode === selectedModeCategory

    if (!hasValidActiveTab) {
      // Find the first tab for the current mode
      const modeTabs = Object.values(chatStore.chatTabs).filter(tab =>
        tab.metadata?.mode === selectedModeCategory
      ).sort((a, b) => a.createdAt - b.createdAt)

      if (modeTabs.length > 0) {
        // Select the first tab
        console.log(`[App] No valid active tab found for mode ${selectedModeCategory}, selecting first tab: ${modeTabs[0].tabId}`)
        chatStore.switchTab(modeTabs[0].tabId)
      }
    }
  }, [hasCompletedInitialSetup, selectedModeCategory])

  // Restore active presets after stores are initialized
  const hasRestoredPresetRef = useRef(false)
  useEffect(() => {
    // Only restore presets if initial setup is completed and we have a mode category
    if (hasCompletedInitialSetup && selectedModeCategory) {
      // Add a small delay to ensure stores are fully initialized
      const timer = setTimeout(() => {
        const activePreset = getActivePreset(selectedModeCategory)
        if (activePreset) {
          hasRestoredPresetRef.current = true
          const result = applyPreset(activePreset.id, selectedModeCategory)
          if (!result.success) {
            console.error('[APP] Failed to restore preset:', result.error)
          }
        } else if (selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') {
          // For chat/multi-agent mode, if there's no active preset, clear any stale preset server state
          // This prevents old preset servers from persisting when no preset is selected
          hasRestoredPresetRef.current = true
          const { setCurrentPresetServers } = useGlobalPresetStore.getState()
          setCurrentPresetServers([])
        }
        // For workflow mode with no preset found, don't mark as restored — retry below
      }, 500) // 500ms delay to ensure stores are ready

      return () => clearTimeout(timer)
    }
  }, [hasCompletedInitialSetup, selectedModeCategory, getActivePreset, applyPreset])

  // Retry preset restoration for workflow mode after presets finish loading from API
  // The 500ms timer above may fire before refreshPresets() completes
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  useEffect(() => {
    if (hasRestoredPresetRef.current) return
    if (!hasCompletedInitialSetup || selectedModeCategory !== 'workflow') return
    if (customPresets.length === 0) return // Presets not loaded yet

    const activePreset = getActivePreset('workflow')
    if (activePreset) {
      hasRestoredPresetRef.current = true
      applyPreset(activePreset.id, 'workflow')
    }
  }, [hasCompletedInitialSetup, selectedModeCategory, customPresets, getActivePreset, applyPreset])


  // Auto-minimize sidebar when mode is selected or preset is selected
  useEffect(() => {
    if (selectedModeCategory && !sidebarMinimized) {
      setSidebarMinimized(true)
    }
    // NOTE: Only include selectedModeCategory and setSidebarMinimized in dependencies
    // Do NOT include sidebarMinimized as it would cause the effect to re-run every time
    // the sidebar state changes, preventing manual toggle functionality after auto-minimize
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedModeCategory, setSidebarMinimized])

  // Show mode selection modal if initial setup not completed
  const showModeSelection = !hasCompletedInitialSetup

  // Start new chat function
  const startNewChat = useCallback(() => {
    
    // Use ChatArea's resetChatState method to clear all chat state without circular call
    if (chatAreaRef.current) {
      chatAreaRef.current.resetChatState();
    }
    
    // Preserve active preset for workflow mode, clear for other modes
    if (selectedModeCategory === 'workflow') {
      // For workflow mode, preserve the active preset
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
      // For other modes (chat, multi-agent), clear preset state as before
      setSelectedPresetId(null); // Clear selected preset filter
      if (selectedModeCategory) {
        clearActivePreset(selectedModeCategory); // Also clear in global store
      }
    }
    
    // Reset the global session ID to force generation of a new one
    resetSessionId();
    
    // Clear the requiresNewChat flag after successful new chat initialization
    useAppStore.getState().clearRequiresNewChat();
    
    // Re-apply active preset for workflow mode after chat reset
    if (selectedModeCategory === 'workflow') {
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
  }, [setSelectedPresetId, clearActivePreset, selectedModeCategory, applyPreset]);

  // Handle chat session selection
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const handleChatSessionSelect = useCallback(async (sessionId: string, sessionTitle?: string, _sessionType?: 'active' | 'completed', _activeSessionInfo?: ActiveSessionInfo) => {
    const chatStore = useChatStore.getState()
    chatStore.setIsLoadingHistory(true)
    try {
      const tabId = await restoreSession(sessionId, {
        title: sessionTitle,
        source: 'sidebar',
      })
      useChatStore.getState().switchTab(tabId)
      setSidebarMinimized(true)
      setShowFileContent(false)
    } finally {
      chatStore.setIsLoadingHistory(false)
    }
  }, [setSidebarMinimized, setShowFileContent]);

  // Minimize toggle functions
  const toggleSidebarMinimize = useCallback(() => {
    setSidebarMinimized(!sidebarMinimized)
  }, [sidebarMinimized, setSidebarMinimized])

  const toggleWorkspaceMinimize = useCallback(() => {
    setWorkspaceMinimized(!workspaceMinimized)
  }, [workspaceMinimized, setWorkspaceMinimized])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Ctrl/Cmd + 1 for Multi-agent mode
      if ((event.ctrlKey || event.metaKey) && event.key === '1') {
        event.preventDefault()
        const { setModeCategory } = useModeStore.getState()
        setModeCategory('multi-agent')
      }
      // Ctrl/Cmd + 2 for Workflow mode
      if ((event.ctrlKey || event.metaKey) && event.key === '2') {
        event.preventDefault()
        const { setModeCategory } = useModeStore.getState()
        setModeCategory('workflow')
      }
      // Ctrl/Cmd + 4 for Chat mode
      if ((event.metaKey || event.ctrlKey) && event.key === '4') {
        event.preventDefault()
        const { setModeCategory } = useModeStore.getState()
        setModeCategory('chat')
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
        <AuthWrapper>
        <TooltipProvider>
        {/* Mode Selection Modal */}
        <ModeSelectionModal
          isOpen={showModeSelection}
          onClose={() => {}} // Modal handles its own closing
        />

        <div className="h-screen bg-background flex">
          {/* Left Sidebar */}
          <div className={`${sidebarMinimized ? 'w-16' : 'w-72'} transition-all duration-300 ease-in-out relative z-30`}>
            <WorkspaceSidebar
              onChatSessionSelect={handleChatSessionSelect}
              minimized={sidebarMinimized}
              onToggleMinimize={toggleSidebarMinimize}
            />
          </div>

          {/* Middle Content Area - WorkflowLayout (workflow mode) or ChatArea (other modes) */}
          <div className="flex-1 flex flex-col min-w-0 min-h-0 relative z-10 overflow-hidden">
            {/* Global Mode & Preset Bar - only above middle content area, not sidebars */}
            <ModePresetBar />
            
            {/* Chat Tabs - global navigation for both chat and workflow modes */}
            <ChatTabs 
              autoScroll={useChatStore(state => state.autoScroll)}
              onToggleAutoScroll={() => {
                const chatStore = useChatStore.getState()
                chatStore.setAutoScroll(!chatStore.autoScroll)
              }}
            />
            
              <div className="flex-1 min-h-0 overflow-hidden relative">
                {/* Both layouts stay mounted to preserve running state across mode switches.
                    Only the active mode is visible; the other is hidden via CSS. */}
                <div className={selectedModeCategory === 'workflow' ? 'h-full' : 'hidden'}>
                  <WorkflowLayout
                    className="h-full"
                    onNewChat={startNewChat}
                  />
                </div>
                <div className={selectedModeCategory !== 'workflow' ? 'h-full' : 'hidden'}>
                  <ChatAreaWithObserverId
                    ref={chatAreaRef}
                    onNewChat={startNewChat}
                  />
                </div>
              </div>

            {/* Running Workflows Tracking - Global */}
            {/* File Content View - overlay when showing file content */}
            {showFileContent && (
              <div className="absolute inset-0 bg-white dark:bg-gray-900 z-10 flex flex-col">
              {/* Fixed Header */}
              <div className="flex items-center justify-between px-4 py-2 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
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
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex flex-col min-w-0 cursor-help gap-0.5">
                          {selectedFile?.path && (
                            <>
                              <div className="flex items-center gap-2">
                                <h2 className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">
                                  {selectedFile.path.split('/').pop() || selectedFile.path}
                                </h2>
                                {getHasUnsavedChanges() && (
                                  <span className="text-[10px] text-orange-500">●</span>
                                )}
                              </div>
                              <p className="text-[10px] text-gray-500 dark:text-gray-400 truncate">
                                {selectedFile.path}
                              </p>
                            </>
                          )}
                        </div>
                      </TooltipTrigger>
                      {selectedFile?.path && (
                        <TooltipContent>
                          <p className="max-w-md break-all">{selectedFile.path}</p>
                        </TooltipContent>
                      )}
                    </Tooltip>
                  </TooltipProvider>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  {!isEditMode ? (
                    <>
                      <div className="flex items-center gap-0.5">
                        {/* Hide edit for binary files and code files (code is written by the agent) */}
                        {!selectedFile?.path?.toLowerCase().endsWith('.xls') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.xlsx') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.docx') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.pdf') &&
                         !isCodeFile(selectedFile?.path || '') && (
                          <>
                            <button
                              onClick={handleEdit}
                              className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                              title="Edit file (Ctrl+E)"
                            >
                              <Edit className="w-4 h-4" />
                            </button>
                          </>
                        )}
                        <button
                          onClick={handleDownload}
                          className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                          title="Download file"
                        >
                          <Download className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => {
                            if (!selectedFile?.path) return
                            const encoded = btoa(unescape(encodeURIComponent(selectedFile.path)))
                            const uid = useAuthStore.getState().user?.id || ''
                            const shareUrl = `${window.location.origin}/file?path=${encoded}${uid ? `&uid=${encodeURIComponent(uid)}` : ''}`
                            copyToClipboard(shareUrl).then((ok) => {
                              if (ok) {
                                setShareCopied(true)
                                setTimeout(() => setShareCopied(false), 2000)
                              }
                            })
                          }}
                          className="flex items-center gap-1 p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                          title="Copy public share link"
                        >
                          <Link className="w-4 h-4" />
                          {shareCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
                        </button>
                        {!selectedFile?.path?.toLowerCase().endsWith('.xls') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.xlsx') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.docx') &&
                         !selectedFile?.path?.toLowerCase().endsWith('.pdf') && (
                          <button
                            onClick={() => setShowRevisionsModal(true)}
                            className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                            title="View file revisions"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                            </svg>
                          </button>
                        )}
                        {selectedFile?.path && (
                         selectedFile.path.toLowerCase().endsWith('.md') ||
                         selectedFile.path.toLowerCase().endsWith('.markdown')) && (
                          <button
                            onClick={handleExportPdf}
                            disabled={isExportingPdf}
                            className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            title="Export as PDF"
                          >
                            {isExportingPdf ? (
                              <>
                                <Loader2 className="w-4 h-4 animate-spin" />
                                {exportProgress && (
                                  <span className="ml-1 text-xs text-gray-500 dark:text-gray-400">{exportProgress}</span>
                                )}
                              </>
                            ) : (
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 21h10a2 2 0 002-2V9l-5-5H7a2 2 0 00-2 2v13a2 2 0 002 2z" />
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14 4v5h5" />
                                <text x="7" y="18" fontSize="6" fontWeight="bold" fill="currentColor" stroke="none">PDF</text>
                              </svg>
                            )}
                          </button>
                        )}
                        {selectedFile?.path && (
                         selectedFile.path.toLowerCase().endsWith('.md') ||
                         selectedFile.path.toLowerCase().endsWith('.markdown')) && (
                          <button
                            onClick={() => setShowPushToGistDialog(true)}
                            className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            title="Push to GitHub Gist"
                          >
                            <Github className="w-4 h-4" />
                          </button>
                        )}
                      </div>
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
                    ) : (selectedFile?.path && isCodeFile(selectedFile.path)) ? (
                      <div className="h-full overflow-hidden">
                        <FileEditor
                          value={fileContent}
                          filepath={selectedFile.path}
                          readOnly={true}
                          height="100%"
                        />
                      </div>
                    ) : (
                      <div className={(selectedFile?.path?.toLowerCase().endsWith('.pdf') || selectedFile?.path?.toLowerCase().endsWith('.html') || selectedFile?.path?.toLowerCase().endsWith('.htm')) ? "" : "p-6"}>
                        {(() => {
                          const filePath = selectedFile?.path?.toLowerCase() || ''

                          // CSV files
                          if (filePath.endsWith('.csv')) {
                            return <CsvRenderer content={fileContent} />
                          }

                          // Excel files (binary)
                          if ((filePath.endsWith('.xlsx') || filePath.endsWith('.xls')) && binaryFileData) {
                            return <XlsxRenderer data={binaryFileData} />
                          }

                          // DOCX files (binary)
                          if (filePath.endsWith('.docx') && binaryFileData) {
                            return <DocxRenderer data={binaryFileData} />
                          }

                          // PDF files (binary)
                          if (filePath.endsWith('.pdf') && binaryFileData) {
                            return (
                              <div className="h-[calc(100vh-120px)] w-full">
                                <PdfRenderer data={binaryFileData} />
                              </div>
                            )
                          }

                          // Video files (webm, mp4)
                          if ((filePath.endsWith('.webm') || filePath.endsWith('.mp4')) && binaryFileData) {
                            const mimeType = filePath.endsWith('.webm') ? 'video/webm' : 'video/mp4'
                            const blob = new Blob([binaryFileData], { type: mimeType })
                            const url = URL.createObjectURL(blob)
                            return (
                              <div className="h-[calc(100vh-120px)] w-full flex items-center justify-center bg-black rounded-lg">
                                <video
                                  controls
                                  autoPlay
                                  className="max-h-full max-w-full"
                                  src={url}
                                />
                              </div>
                            )
                          }

                          // HTML files
                          if (filePath.endsWith('.html') || filePath.endsWith('.htm')) {
                            return (
                              <div className="h-[calc(100vh-120px)] w-full">
                                <HtmlRenderer content={fileContent} />
                              </div>
                            )
                          }

                          // Mermaid diagram files (.mmd, .mermaid)
                          if (filePath.endsWith('.mmd') || filePath.endsWith('.mermaid')) {
                            return (
                              <div className="space-y-2">
                                <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                                  <span className="font-medium">Mermaid Diagram</span>
                                  <span className="text-xs bg-purple-100 dark:bg-purple-900 text-purple-800 dark:text-purple-200 px-2 py-1 rounded font-mono">
                                    {selectedFile?.path?.split('.').pop()}
                                  </span>
                                </div>
                                <MermaidDiagram content={fileContent} />
                              </div>
                            )
                          }

                          // Check for JSON files
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
                          } 
 
                          // Default: render as markdown
                          else {
                            return (
                              <div ref={markdownContentRef} className="max-w-4xl mx-auto">
                                <div className="prose prose-sm max-w-none dark:prose-invert prose-headings:font-semibold prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline prose-strong:text-gray-900 dark:prose-strong:text-gray-100 prose-code:text-blue-600 dark:prose-code:text-blue-400 prose-pre:bg-gray-50 dark:prose-pre:bg-gray-900 prose-blockquote:border-l-blue-500 prose-blockquote:text-gray-700 dark:prose-blockquote:text-gray-300">
                                  <MarkdownRenderer 
                                    content={fileContent} 
                                    className="max-w-none"
                                    showScrollbar={true}
                                  />
                                </div>
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

          {/* Right Workspace Area - auto-minimize in workflow mode */}
          <div className={`${
            // Use workspaceMinimized state directly - user can toggle regardless of mode
            workspaceMinimized ? 'w-16' : 'w-96'
          } transition-all duration-300 ease-in-out border-l border-gray-200 dark:border-gray-700 relative z-20`}>
            <Workspace 
              minimized={workspaceMinimized}
              onToggleMinimize={toggleWorkspaceMinimize}
            />
          </div>
        </div>

        {/* Push to Gist Dialog */}
        <PushToGistDialog
          isOpen={showPushToGistDialog}
          onClose={() => setShowPushToGistDialog(false)}
          fileContent={fileContent}
          fileName={selectedFile?.name || selectedFile?.path?.split('/').pop() || 'document.md'}
        />

        {/* File Revisions Modal */}
        <FileRevisionsModal
          isOpen={showRevisionsModal}
          onClose={() => {
            setShowRevisionsModal(false)
            setRestoreError(null)
          }}
          filepath={selectedFile?.path || ''}
          onRestoreVersion={handleRestoreVersion}
        />
        
        {/* Restore Error Toast */}
        {restoreError && (
          <div className="fixed bottom-4 right-4 bg-red-500 text-white px-4 py-3 rounded-lg shadow-lg z-50 flex items-center gap-3 max-w-md">
            <div className="flex-1">
              <p className="font-medium">Restore Failed</p>
              <p className="text-sm text-red-100">{restoreError}</p>
            </div>
            <button
              onClick={() => setRestoreError(null)}
              className="text-white hover:text-red-100"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        )}
        
        {/* Restore Loading Overlay */}
        {isRestoring && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 flex flex-col items-center gap-4">
              <Loader2 className="w-8 h-8 animate-spin text-blue-500" />
              <p className="text-gray-900 dark:text-gray-100">Restoring file version...</p>
            </div>
          </div>
        )}
        </TooltipProvider>
        </AuthWrapper>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
