import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useCallback, useRef, useState, forwardRef } from "react";
import { ThemeProvider } from "./contexts/ThemeContext.tsx";
import WorkspaceSidebar from "./components/WorkspaceSidebar";
import { UpdateProgressToast } from "./components/UpdateProgressToast";
import Workspace from "./components/Workspace.tsx";
import { MemoryPanel, OrgGoalsPanel, OrgPulsePanel } from "./components/org/OrgHtmlPanels";
import { ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, getOrgHtmlPreviewDevice, type OrgHtmlPreviewDevice } from "./components/org/orgHtmlPreview";
import ChatArea, { type ChatAreaRef } from "./components/ChatArea.tsx";
import { MarkdownRenderer, MermaidDiagram } from "./components/ui/MarkdownRenderer";
import { CsvRenderer } from "./components/ui/CsvRenderer";
import { XlsxRenderer } from "./components/ui/XlsxRenderer";
import { DocxRenderer } from "./components/ui/DocxRenderer";
import { PdfRenderer } from "./components/ui/PdfRenderer";
import { HtmlRenderer } from "./components/ui/HtmlRenderer";
import { ConversationRenderer, isConversationJSON } from "./components/ui/ConversationRenderer";
import { DiffRenderer } from "./components/ui/DiffRenderer";
import { RenderedContentSearchBar, RenderedContentSearchButton, useRenderedContentSearch } from "./components/ui/RenderedContentSearch";
import { resetSessionId, agentApi } from "./services/api";
import { AuthWrapper } from "./components/AuthWrapper";
import type { FileVersion } from "./services/api-types";
import FileRevisionsModal from "./components/workspace/FileRevisionsModal";
import PushToGistDialog from "./components/workspace/PushToGistDialog";
import FileEditor from "./components/workspace/FileEditor";
import { isValidJSON } from "./utils/event-helpers";
import { prepareDomForPdfExport } from "./utils/pdfExport";
import { convertToSlackMarkdown } from "./utils/slackMarkdown";
import { isDiffFilePath, looksLikeDiffContent } from "./utils/diff";
import { Edit, Save, X, Loader2, Download, Link, Github, PanelRightClose, PanelRightOpen } from "lucide-react";
import { WorkflowLayout } from "./components/workflow";
import { WorkflowsOverviewPage } from "./components/WorkflowsOverviewPage";
import { ModePresetBar } from "./components/ModePresetBar";
import { QuickSwitcher } from "./components/QuickSwitcher";
import { ChatTabs } from "./components/ChatTabs";
import ConfirmationDialog from "./components/ui/ConfirmationDialog";
import { useAppStore, useMCPStore, useGlobalPresetStore, useWorkspaceStore, useWorkflowStore, useChatStore } from "./stores";
import { useModeStore } from "./stores/useModeStore";
import { useLLMStore } from "./stores/useLLMStore";
import { useAuthStore } from "./stores/useAuthStore";
import { normalizeEventViewMode, waitForChatStoreHydration, type ChatTab } from "./stores/useChatStore";
import { useLLMDefaults } from "./hooks/useLLMDefaults";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./components/ui/tooltip";
import "./App.css";

// Extend window interface for global functions
declare global {
  interface Window {
    highlightFile?: (filepath: string) => void;
    toggleAutoScroll?: () => void;
    perfDiag?: () => void;
  }
}

import { copyToClipboard } from './utils/textUtils'

const queryClient = new QueryClient();

const WORKSPACE_COLLAPSING_POPUP_SELECTOR = [
  '[data-workspace-popup="true"]',
  '[role="dialog"]',
  '[class~="fixed"][class~="inset-0"]'
].join(',')
const WORKSPACE_COLLAPSE_IGNORE_SELECTOR = '[data-workspace-collapse-ignore="true"]'
const READ_ONLY_WORKFLOW_RESTORE_SELECTION_WINDOW_MS = 60 * 1000

const workflowTabSortTimestamp = (tab: ChatTab) => tab.lastAccessedAt ?? tab.createdAt ?? 0

const isInteractiveWorkflowTab = (tab: ChatTab | null | undefined): tab is ChatTab =>
  !!tab && tab.metadata?.mode === 'workflow' && tab.metadata?.isViewOnly !== true

const isRecentExplicitReadOnlyWorkflowTab = (tab: ChatTab | null | undefined): tab is ChatTab => {
  const restoredAt = tab?.metadata?.readOnlyRestoredAt
  return !!tab &&
    tab.metadata?.mode === 'workflow' &&
    tab.metadata?.isViewOnly === true &&
    typeof restoredAt === 'number' &&
    Date.now() - restoredAt <= READ_ONLY_WORKFLOW_RESTORE_SELECTION_WINDOW_MS
}

const hasOpenWorkspaceCollapsingPopup = () => {
  if (typeof document === 'undefined') return false

  return Array.from(document.querySelectorAll<HTMLElement>(WORKSPACE_COLLAPSING_POPUP_SELECTOR))
    .some((element) => {
      if (element.closest(WORKSPACE_COLLAPSE_IGNORE_SELECTOR)) {
        return false
      }
      const style = window.getComputedStyle(element)
      return (
        style.display !== 'none' &&
        style.visibility !== 'hidden' &&
        style.position === 'fixed'
      )
    })
}


// Helper component to get observerId and render ChatArea
// Always renders ChatArea (even without observerId) so header with mode/preset selectors is visible
// Uses Zustand hooks to reactively update when tabs change
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, { onNewChat: () => void }>(({ onNewChat }, ref) => {
  // Pass null (not undefined) when the active tab is a workflow tab so this hidden
  // instance doesn't steal SSE connections, polling, or queue processing from
  // WorkflowLayout's ChatArea which is the primary instance for workflow tabs.
  const activeTabId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    return tab?.metadata?.mode === 'workflow' ? null : (tabId || undefined)
  })

  return (
    <ChatArea
      ref={ref}
      onNewChat={onNewChat}
      tabId={activeTabId}
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

const multiAgentPanelTabClass = (active: boolean) =>
  `rounded px-2.5 py-1 text-xs font-medium whitespace-nowrap transition-colors ${
    active ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
  }`

function App() {
  // Ref for ChatArea component to access its methods
  const chatAreaRef = useRef<ChatAreaRef>(null)
  const [orgHtmlPreviewDevice, setOrgHtmlPreviewDevice] = useState<OrgHtmlPreviewDevice>(() => getOrgHtmlPreviewDevice())

  // Store subscriptions
  const { setAgentMode, setSidebarMinimized } = useAppStore()
  const { hasCompletedInitialSetup, selectedModeCategory, setModeCategory, completeInitialSetup } = useModeStore()
  const defaultsLoaded = useLLMStore(state => state.defaultsLoaded)
  const savedLLMs = useLLMStore(state => state.savedLLMs)
  const llmConfigLocked = useLLMStore(state => state.llmConfigLocked)
  const isConfigValid = useLLMStore(state => state.isConfigValid)
  const setShowLLMModal = useLLMStore(state => state.setShowLLMModal)
  
  // Load LLM defaults from backend
  useLLMDefaults()
  
  // App Store subscriptions for workspace and chat
  const {
    setSelectedPresetId,
    sidebarMinimized,
    workspaceMinimized,
    workspaceMinimizedByMode,
    setWorkspaceMinimized,
    setWorkspaceMinimizedForLayout,
    multiAgentRightPanelView,
    setMultiAgentRightPanelView,
    showWorkflowsOverview,
    setShowWorkflowsOverview
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

  useEffect(() => {
    const handler = (event: Event) => {
      const preference = (event as CustomEvent).detail?.preference
      if (preference === 'mobile' || preference === 'tablet' || preference === 'desktop') {
        setOrgHtmlPreviewDevice(preference)
      }
    }
    window.addEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
    return () => window.removeEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
  }, [])

  const submitMultiAgentPanelCommand = useCallback((query: string) => {
    setWorkspaceMinimized(false)
    chatAreaRef.current?.submitQuery(query).catch(error => {
      console.error('[App] Failed to submit org panel command:', error)
    })
  }, [setWorkspaceMinimized])

  const [videoObjectUrl, setVideoObjectUrl] = useState<string | null>(null)
  const [audioObjectUrl, setAudioObjectUrl] = useState<string | null>(null)

  useEffect(() => {
    const filePath = selectedFile?.path?.toLowerCase() || ''
    const isVideoFile = filePath.endsWith('.webm') || filePath.endsWith('.mp4') || filePath.endsWith('.mov')

    if (!isVideoFile || !binaryFileData) {
      setVideoObjectUrl((current) => {
        if (current) {
          URL.revokeObjectURL(current)
        }
        return null
      })
      return
    }

    const mimeType = filePath.endsWith('.webm')
      ? 'video/webm'
      : filePath.endsWith('.mov')
        ? 'video/quicktime'
        : 'video/mp4'
    const blob = new Blob([binaryFileData], { type: mimeType })
    const nextUrl = URL.createObjectURL(blob)

    setVideoObjectUrl((current) => {
      if (current) {
        URL.revokeObjectURL(current)
      }
      return nextUrl
    })

    return () => {
      URL.revokeObjectURL(nextUrl)
    }
  }, [binaryFileData, selectedFile?.path])

  useEffect(() => {
    const filePath = selectedFile?.path?.toLowerCase() || ''
    const isAudioFile = ['.mp3', '.wav', '.m4a', '.aac', '.ogg', '.oga', '.flac', '.opus'].some(ext => filePath.endsWith(ext))

    if (!isAudioFile || !binaryFileData) {
      setAudioObjectUrl((current) => {
        if (current) {
          URL.revokeObjectURL(current)
        }
        return null
      })
      return
    }

    const mimeType = filePath.endsWith('.wav')
      ? 'audio/wav'
      : filePath.endsWith('.m4a')
        ? 'audio/mp4'
        : filePath.endsWith('.aac')
          ? 'audio/aac'
          : filePath.endsWith('.ogg') || filePath.endsWith('.oga')
            ? 'audio/ogg'
            : filePath.endsWith('.flac')
              ? 'audio/flac'
              : filePath.endsWith('.opus')
                ? 'audio/opus'
                : 'audio/mpeg'
    const blob = new Blob([binaryFileData], { type: mimeType })
    const nextUrl = URL.createObjectURL(blob)

    setAudioObjectUrl((current) => {
      if (current) {
        URL.revokeObjectURL(current)
      }
      return nextUrl
    })

    return () => {
      URL.revokeObjectURL(nextUrl)
    }
  }, [binaryFileData, selectedFile?.path])
  
  // Expose performance diagnostics on window for DevTools console
  useEffect(() => {
    window.perfDiag = () => {
      const chatState = useChatStore.getState()
      const tabs = Object.values(chatState.chatTabs)
      const workflowTabs = tabs.filter(t => t.metadata?.mode === 'workflow')
      const streamingTabs = tabs.filter(t => t.isStreaming)
      const sseConns = chatState.sseConnections
      const sseCount = Object.keys(sseConns).length

      let totalEvents = 0
      let totalEventBytes = 0
      let largestEventBytes = 0
      const eventDetails: Array<{ session: string; tabs: number; tabNames: string; events: number; evtSizeKB: number; avgEventKB: number; largestEventKB: number; largestEventType: string; mode: string; preset: string; streaming: boolean; hasSSE: boolean }> = []
      const duplicateSessionDetails: Array<{ session: string; tabs: number; tabNames: string; events: number; evtSizeKB: number }> = []
      const largestEvents: Array<{ session: string; type: string; sizeKB: number; id: string; tabNames: string }> = []
      const tabsBySession = new Map<string, typeof tabs>()
      for (const tab of tabs) {
        if (tab.sessionId) {
          tabsBySession.set(tab.sessionId, [...(tabsBySession.get(tab.sessionId) || []), tab])
        }
      }
      for (const [sid, sessionTabs] of tabsBySession.entries()) {
        const events = chatState.tabEvents[sid] || []
        const count = events.length
        const sizeEstimate = JSON.stringify(events).length
        const tabNames = sessionTabs.map(t => t.name.slice(0, 24)).join(', ')
        totalEvents += count
        totalEventBytes += sizeEstimate

        let largestForSessionBytes = 0
        let largestForSessionType = ''
        for (const event of events) {
          const eventBytes = JSON.stringify(event).length
          if (eventBytes > largestForSessionBytes) {
            largestForSessionBytes = eventBytes
            largestForSessionType = event.type || '(unknown)'
          }
          if (eventBytes > largestEventBytes) largestEventBytes = eventBytes
          if (eventBytes >= 50 * 1024) {
            largestEvents.push({
              session: sid.slice(0, 8),
              type: event.type || '(unknown)',
              sizeKB: Math.round(eventBytes / 1024),
              id: (event.id || '').slice(0, 16),
              tabNames,
            })
          }
        }

        if (count > 0) {
          const firstTab = sessionTabs[0]
          eventDetails.push({
            session: sid.slice(0, 8),
            tabs: sessionTabs.length,
            tabNames,
            events: count,
            evtSizeKB: Math.round(sizeEstimate / 1024),
            avgEventKB: count > 0 ? Math.round(sizeEstimate / count / 1024) : 0,
            largestEventKB: Math.round(largestForSessionBytes / 1024),
            largestEventType: largestForSessionType,
            mode: firstTab.metadata?.mode || '?',
            preset: (firstTab.metadata?.presetQueryId || '').slice(0, 8),
            streaming: sessionTabs.some(t => t.isStreaming),
            hasSSE: !!sseConns[sid]
          })
        }

        if (sessionTabs.length > 1) {
          duplicateSessionDetails.push({
            session: sid.slice(0, 8),
            tabs: sessionTabs.length,
            tabNames,
            events: count,
            evtSizeKB: Math.round(sizeEstimate / 1024),
          })
        }
      }
      largestEvents.sort((a, b) => b.sizeKB - a.sizeKB)

      // Streaming text sizes
      const streamingTextSizes: Array<{ session: string; sizeKB: number; chars: number }> = []
      let totalStreamingBytes = 0
      for (const [sid, text] of Object.entries(chatState.streamingText)) {
        if (text && text.length > 0) {
          const size = text.length * 2 // approx bytes (UTF-16)
          totalStreamingBytes += size
          streamingTextSizes.push({ session: sid.slice(0, 8), sizeKB: Math.round(size / 1024), chars: text.length })
        }
      }
      const completedStreamingTextSizes: Array<{ session: string; sizeKB: number; chars: number }> = []
      let totalCompletedStreamingBytes = 0
      for (const [sid, text] of Object.entries(chatState.completedStreamingText || {})) {
        if (text && text.length > 0) {
          const size = text.length * 2
          totalCompletedStreamingBytes += size
          completedStreamingTextSizes.push({ session: sid.slice(0, 8), sizeKB: Math.round(size / 1024), chars: text.length })
        }
      }

      // SSE connection details
      const sseDetails: Array<{ session: string; tab: string }> = []
      for (const [sid, _conn] of Object.entries(sseConns)) {
        const tab = tabs.find(t => t.sessionId === sid)
        sseDetails.push({ session: sid.slice(0, 8), tab: tab?.name?.slice(0, 25) || '(orphan!)' })
      }
      // Detect orphan SSE connections (no matching tab)
      const orphanSSE = sseDetails.filter(s => s.tab === '(orphan!)')

      // Orphan tabEvents (no matching tab)
      const tabSessionIds = new Set(tabs.map(t => t.sessionId).filter(Boolean))
      const orphanEvents: Array<{ session: string; events: number; sizeKB: number }> = []
      for (const [sid, events] of Object.entries(chatState.tabEvents)) {
        if (!tabSessionIds.has(sid)) {
          orphanEvents.push({ session: sid.slice(0, 8), events: events.length, sizeKB: Math.round(JSON.stringify(events).length / 1024) })
        }
      }

      // localStorage sizes
      const storeKeys = ['chat-store', 'workflow-store', 'global-preset-storage', 'mode-store', 'mcp-store']
      const storageSizes: Record<string, number> = {}
      let totalLSBytes = 0
      for (const key of storeKeys) {
        const val = localStorage.getItem(key)
        if (val) {
          storageSizes[key] = Math.round(val.length / 1024)
          totalLSBytes += val.length
        }
      }

      // Memory usage (if available)
      const mem = (performance as any).memory
      const memInfo = mem ? {
        usedHeap: Math.round(mem.usedJSHeapSize / 1024 / 1024),
        totalHeap: Math.round(mem.totalJSHeapSize / 1024 / 1024),
        limit: Math.round(mem.jsHeapSizeLimit / 1024 / 1024)
      } : null

      // DOM node count + breakdown of heavy subtrees
      const domNodes = document.querySelectorAll('*').length
      const domBreakdown: Array<{ selector: string; nodes: number }> = []
      const selectors = ['[class*="chat"]', '[class*="event"]', '[class*="message"]', '[class*="editor"]', '[class*="monaco"]', 'svg', 'pre', 'code', '.react-flow', '.react-flow__node', '.react-flow__edge']
      for (const sel of selectors) {
        try {
          const count = document.querySelectorAll(sel).length
          if (count > 50) domBreakdown.push({ selector: sel, nodes: count })
        } catch { /* ignore invalid selectors */ }
      }

      // Active timers/intervals estimate
      // Check for event listeners on window
      const eventListenerCount = typeof (window as any).getEventListeners === 'function'
        ? Object.values((window as any).getEventListeners(window) as Record<string, unknown[]>).reduce((sum: number, arr) => sum + (arr as unknown[]).length, 0)
        : 'N/A (use DevTools)'

      // Long task detection — start monitoring
      if (!(window as any).__longTaskObserver) {
        try {
          const longTasks: Array<{ duration: number; time: string }> = [];
          (window as any).__longTasks = longTasks
          const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
              longTasks.push({ duration: Math.round(entry.duration), time: new Date().toLocaleTimeString() })
              if (longTasks.length > 100) longTasks.shift()
            }
          })
          observer.observe({ entryTypes: ['longtask'] })
          ;(window as any).__longTaskObserver = observer
        } catch { /* longtask not supported */ }
      }
      const longTasks = ((window as any).__longTasks || []) as Array<{ duration: number; time: string }>
      const recentLongTasks = longTasks.slice(-10)

      // Frame rate measurement — start if not already running
      if (!(window as any).__fpsSamples) {
        const fpsSamples: number[] = [];
        (window as any).__fpsSamples = fpsSamples
        let lastTime = performance.now()
        let frameCount = 0
        const measureFPS = () => {
          frameCount++
          const now = performance.now()
          if (now - lastTime >= 1000) {
            fpsSamples.push(frameCount)
            if (fpsSamples.length > 30) fpsSamples.shift()
            frameCount = 0
            lastTime = now
          }
          requestAnimationFrame(measureFPS)
        }
        requestAnimationFrame(measureFPS)
      }
      const fpsSamples = (window as any).__fpsSamples as number[]
      const avgFPS = fpsSamples.length > 0 ? Math.round(fpsSamples.reduce((a, b) => a + b, 0) / fpsSamples.length) : 'measuring...'
      const minFPS = fpsSamples.length > 0 ? Math.min(...fpsSamples) : 'measuring...'

      // --- OUTPUT ---
      console.log('%c === PERF DIAGNOSTICS ===', 'color: cyan; font-weight: bold; font-size: 14px')

      // Memory
      if (memInfo) {
        const usedPct = Math.round((memInfo.usedHeap / memInfo.limit) * 100)
        const memColor = usedPct > 80 ? 'red' : usedPct > 50 ? 'orange' : 'green'
        console.log(`%c Memory: ${memInfo.usedHeap} MB / ${memInfo.totalHeap} MB (limit: ${memInfo.limit} MB) [${usedPct}% used]`, `color: ${memColor}; font-weight: bold`)
      } else {
        console.log('Memory: N/A (use Chrome-based browser)')
      }

      // FPS
      const fpsColor = (typeof avgFPS === 'number' && avgFPS < 30) ? 'red' : (typeof avgFPS === 'number' && avgFPS < 50) ? 'orange' : 'green'
      console.log(`%c FPS: avg=${avgFPS}, min=${minFPS} (last ${fpsSamples.length}s)`, `color: ${fpsColor}; font-weight: bold`)

      // Tabs & SSE
      console.log(`\nTabs: ${tabs.length} total (${workflowTabs.length} workflow, ${streamingTabs.length} streaming)`)
      console.log(`SSE connections: ${sseCount}${orphanSSE.length > 0 ? ` ⚠️ ${orphanSSE.length} ORPHAN` : ''}`)
      if (orphanSSE.length > 0) {
        console.log('%c Orphan SSE connections (no tab):', 'color: red; font-weight: bold')
        console.table(orphanSSE)
      }

      // Events
      console.log(`\nEvents in memory: ${totalEvents} across ${eventDetails.length} unique sessions (~${Math.round(totalEventBytes / 1024)} KB, largest event ${Math.round(largestEventBytes / 1024)} KB)`)
      if (eventDetails.length > 0) {
        // Sort by estimated memory descending to show biggest first
        eventDetails.sort((a, b) => b.evtSizeKB - a.evtSizeKB)
        console.table(eventDetails)
      }
      if (duplicateSessionDetails.length > 0) {
        console.log(`%c Duplicate tab references: ${duplicateSessionDetails.length} session(s) shown in multiple tabs`, 'color: orange; font-weight: bold')
        console.table(duplicateSessionDetails)
      }
      if (largestEvents.length > 0) {
        console.log(`%c Large retained events (>=50 KB): ${largestEvents.length} total, top 20 shown`, 'color: orange; font-weight: bold')
        console.table(largestEvents.slice(0, 20))
      }

      // Orphan events
      if (orphanEvents.length > 0) {
        console.log(`%c Orphan tabEvents (no matching tab): ${orphanEvents.length} sessions, ${orphanEvents.reduce((s, o) => s + o.events, 0)} events`, 'color: red; font-weight: bold')
        console.table(orphanEvents)
      }

      // Streaming text
      if (streamingTextSizes.length > 0) {
        console.log(`\nStreaming text buffers: ${streamingTextSizes.length} active (~${Math.round(totalStreamingBytes / 1024)} KB)`)
        console.table(streamingTextSizes)
      }
      if (completedStreamingTextSizes.length > 0) {
        console.log(`\nCompleted streaming buffers: ${completedStreamingTextSizes.length} retained (~${Math.round(totalCompletedStreamingBytes / 1024)} KB)`)
        console.table(completedStreamingTextSizes.sort((a, b) => b.sizeKB - a.sizeKB))
      }

      // DOM
      const domColor = domNodes > 10000 ? 'red' : domNodes > 5000 ? 'orange' : 'green'
      console.log(`%c \nDOM nodes: ${domNodes}`, `color: ${domColor}; font-weight: bold`)
      if (domBreakdown.length > 0) {
        console.table(domBreakdown)
      }

      // Long tasks
      console.log(`\nLong tasks (>50ms): ${longTasks.length} total`)
      if (recentLongTasks.length > 0) {
        const avgDuration = Math.round(recentLongTasks.reduce((s, t) => s + t.duration, 0) / recentLongTasks.length)
        const maxDuration = Math.max(...recentLongTasks.map(t => t.duration))
        console.log(`  Recent ${recentLongTasks.length}: avg=${avgDuration}ms, max=${maxDuration}ms`)
        console.table(recentLongTasks)
      }

      // localStorage
      console.log(`\nlocalStorage: ${Math.round(totalLSBytes / 1024)} KB total`)
      console.table(storageSizes)

      // Window event listeners
      console.log(`\nWindow event listeners: ${eventListenerCount}`)

      // React Flow specific diagnostics
      const rfNodes = document.querySelectorAll('.react-flow__node').length
      const rfEdges = document.querySelectorAll('.react-flow__edge').length
      const rfContainers = document.querySelectorAll('.react-flow').length
      if (rfContainers > 0) {
        console.log(`%c \nReact Flow: ${rfContainers} container(s), ${rfNodes} nodes, ${rfEdges} edges`, 'color: #9C27B0; font-weight: bold')
        if (rfContainers > 1) {
          console.log(`%c  ⚠️ Multiple React Flow containers detected — possible leak!`, 'color: red')
        }
      }

      // Workflow store state
      try {
        const wfStore = (window as any).__ZUSTAND_DEVTOOLS__?.['workflow-store'] || null
        if (!wfStore) {
          // Try direct import path
          const presetStates = JSON.parse(localStorage.getItem('workflow-store') || '{}')?.state?._presetStates
          if (presetStates) {
            const presetCount = Object.keys(presetStates).length
            const presetSizeKB = Math.round(JSON.stringify(presetStates).length / 1024)
            console.log(`\nWorkflow preset states cached: ${presetCount} (${presetSizeKB} KB in localStorage)`)
          }
        }
      } catch { /* ignore */ }

      // Summary warnings
      const warnings: string[] = []
      if (memInfo && memInfo.usedHeap > memInfo.limit * 0.8) warnings.push(`Heap at ${Math.round((memInfo.usedHeap / memInfo.limit) * 100)}% of limit!`)
      if (domNodes > 10000) warnings.push(`${domNodes} DOM nodes — UI will lag`)
      if (totalEvents > 5000) warnings.push(`${totalEvents} events in memory — consider clearing old tabs`)
      if (orphanSSE.length > 0) warnings.push(`${orphanSSE.length} orphan SSE connections leaking`)
      if (orphanEvents.length > 0) warnings.push(`${orphanEvents.reduce((s, o) => s + o.events, 0)} orphan events in memory (no tab)`)
      if (totalStreamingBytes > 5 * 1024 * 1024) warnings.push(`${Math.round(totalStreamingBytes / 1024 / 1024)} MB in streaming text buffers`)
      if (totalLSBytes > 5 * 1024 * 1024) warnings.push(`localStorage is ${Math.round(totalLSBytes / 1024 / 1024)} MB — may cause slow persistence`)
      if (typeof avgFPS === 'number' && avgFPS < 30) warnings.push(`FPS avg=${avgFPS} — UI is janky`)
      if (rfContainers > 1) warnings.push(`${rfContainers} React Flow containers — possible mount leak`)
      if (rfNodes > 200) warnings.push(`${rfNodes} React Flow nodes in DOM — heavy canvas`)

      if (warnings.length > 0) {
        console.log('%c \n⚠️  WARNINGS:', 'color: red; font-weight: bold; font-size: 13px')
        warnings.forEach(w => console.log(`%c  • ${w}`, 'color: red'))
      } else {
        console.log('%c \n✅ No obvious issues detected', 'color: green; font-weight: bold')
      }

      console.log('%c ========================', 'color: cyan; font-weight: bold')
      console.log('%c Tip: Run perfDiag() again after interacting to see trends', 'color: gray; font-style: italic')
    }
    return () => { delete window.perfDiag }
  }, [])

  const [commitMessage, setCommitMessage] = useState('')
  const [showCommitDialog, setShowCommitDialog] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [isRestoring, setIsRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)
  const [isExportingPdf, setIsExportingPdf] = useState(false)
  const [showPushToGistDialog, setShowPushToGistDialog] = useState(false)
  const [exportProgress, setExportProgress] = useState<string | null>(null)
  const [shareCopied, setShareCopied] = useState(false)
  const [contentCopied, setContentCopied] = useState(false)
  const [slackCopied, setSlackCopied] = useState(false)
  const [showQuickSwitcher, setShowQuickSwitcher] = useState(false)
  const [quickSwitcherInitialQuery, setQuickSwitcherInitialQuery] = useState('')
  const markdownContentRef = useRef<HTMLDivElement>(null)
  const selectedFilePathLower = selectedFile?.path?.toLowerCase() || ''
  const isRenderedMarkdownSearchAvailable = (
    showFileContent &&
    !loadingFileContent &&
    !isEditMode &&
    !!fileContent &&
    !fileContent.startsWith('data:image/') &&
    !isCodeFile(selectedFile?.path || '') &&
    !binaryFileData &&
    !selectedFilePathLower.endsWith('.csv') &&
    !selectedFilePathLower.endsWith('.html') &&
    !selectedFilePathLower.endsWith('.htm') &&
    !selectedFilePathLower.endsWith('.mmd') &&
    !selectedFilePathLower.endsWith('.mermaid') &&
    !isValidJSON(fileContent) &&
    !looksLikeDiffContent(fileContent)
  )
  const renderedContentSearch = useRenderedContentSearch({
    targetRef: markdownContentRef,
    contentKey: `${selectedFile?.path || ''}:${fileContent.length}`,
    enabled: isRenderedMarkdownSearchAvailable,
  })
  
  // Ref to prevent duplicate default tab creation (React StrictMode runs effects twice)
  const hasCreatedDefaultTabRef = useRef<string | null>(null)

  
  const { clearActivePreset, applyPreset, getActivePreset } = useGlobalPresetStore()

  useEffect(() => {
    const handleOpenQuickSwitcher = (event: Event) => {
      const detail = (event as CustomEvent<{ query?: string }>).detail
      setQuickSwitcherInitialQuery(detail?.query || '')
      setShowQuickSwitcher(true)
    }

    window.addEventListener('open-quick-switcher', handleOpenQuickSwitcher)
    return () => window.removeEventListener('open-quick-switcher', handleOpenQuickSwitcher)
  }, [])

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
          // Web: clone content into a top-level wrapper for clean full-page printing
          const printTarget = markdownContentRef.current
          const clone = printTarget.cloneNode(true) as HTMLElement
          const wrapper = document.createElement('div')
          wrapper.id = 'pdf-print-wrapper'
          wrapper.style.cssText = 'position:absolute;top:0;left:0;width:100%;background:white;padding:40px;z-index:99999;'
          wrapper.appendChild(clone)
          // Set document title to filename for the PDF name
          const prevTitle = document.title
          document.title = filename.replace(/\.pdf$/, '')
          const style = document.createElement('style')
          style.textContent = `@media print {
            body > *:not(#pdf-print-wrapper) { display: none !important; }
            #pdf-print-wrapper { position: static !important; width: 100% !important; }
            #pdf-print-wrapper * { max-width: 100% !important; }
            html, body { overflow: visible !important; height: auto !important; }
          }`
          document.head.appendChild(style)
          document.body.appendChild(wrapper)
          await new Promise<void>((resolve) => {
            window.addEventListener('afterprint', () => resolve(), { once: true })
            window.print()
          })
          document.body.removeChild(wrapper)
          document.head.removeChild(style)
          document.title = prevTitle
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
  const hasCheckedInitialLLMConfigRef = useRef(false)

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

  // First launch now defaults directly to Chat. If no LLM is configured once
  // backend defaults are loaded, open the LLM configuration dialog instead of
  // asking the user to choose between Chat and Workflow.
  useEffect(() => {
    if (!hasCompletedInitialSetup || !selectedModeCategory) {
      setModeCategory('multi-agent')
      completeInitialSetup()
    }
  }, [hasCompletedInitialSetup, selectedModeCategory, setModeCategory, completeInitialSetup])

  useEffect(() => {
    if (hasCheckedInitialLLMConfigRef.current) return
    if (!defaultsLoaded) return

    hasCheckedInitialLLMConfigRef.current = true
    const hasConfiguredLLM = isConfigValid() || savedLLMs.length > 0 || llmConfigLocked
    if (!hasConfiguredLLM) {
      setShowLLMModal(true)
    }
  }, [defaultsLoaded, isConfigValid, llmConfigLocked, savedLLMs.length, setShowLLMModal])
  
  // Create default tab on page load (only for multi-agent mode, not workflow mode)
  // In workflow mode, tabs are created when user starts a phase/execution
  useEffect(() => {
    if (!hasCompletedInitialSetup) return

    // Only create default tab for multi-agent mode
    // (workflow tabs are created by WorkflowLayout)
    if (selectedModeCategory !== 'multi-agent') {
      return
    }

    let cancelled = false

    const createDefaultTab = async () => {
      await waitForChatStoreHydration()
      if (cancelled) return

      // Prevent duplicate execution (React StrictMode runs effects twice)
      if (hasCreatedDefaultTabRef.current === selectedModeCategory) {
        return
      }

      const chatStore = useChatStore.getState()
      const modeTabs = Object.values(chatStore.chatTabs).filter(tab =>
        tab.metadata?.mode === selectedModeCategory
      )

      // If tabs already exist for this mode, skip
      if (modeTabs.length > 0) {
        return
      }

      // Mark as in progress for this mode
      hasCreatedDefaultTabRef.current = selectedModeCategory

      try {
        // This effect only runs for multi-agent mode (guarded above); workflow
        // tabs are created by WorkflowLayout.
        await chatStore.createChatTab('Chief of Staff', { mode: 'multi-agent' })
      } catch (error) {
        console.error('Failed to create default tab:', error)
        // Reset flag on error so it can retry
        hasCreatedDefaultTabRef.current = null
      }
    }

    void createDefaultTab()

    return () => {
      cancelled = true
    }
  }, [hasCompletedInitialSetup, selectedModeCategory])

  // Ensure a chat tab is selected after restore (fix for page reload issue)
  // This ensures that when tabs are restored from localStorage, we select the first tab of the current mode
  // if activeTabId is null or invalid or belongs to a different mode
  useEffect(() => {
    if (!hasCompletedInitialSetup) return

    let cancelled = false

    const ensureActiveTab = async () => {
      await waitForChatStoreHydration()
      if (cancelled) return

      // When switching back to workflow mode, restore the active workflow execution tab and
      // ensure the chat panel is visible — otherwise activeTabId stays on whatever mode the
      // user came from (e.g. multi-agent) and the ChatArea inside WorkflowLayout shows wrong content.
      if (selectedModeCategory === 'workflow') {
        const chatStore = useChatStore.getState()
        const workflowStore = useWorkflowStore.getState()
        const activeTabId = chatStore.activeTabId
        const activeTab = activeTabId ? chatStore.getTab(activeTabId) : null
        const activePresetId = useGlobalPresetStore.getState().activePresetIds.workflow

        const activeTabMatchesPreset = activeTab &&
          activeTab.metadata?.mode === 'workflow' &&
          activeTab.metadata?.presetQueryId === activePresetId
        const explicitReadOnlyActiveTab = activeTabMatchesPreset && isRecentExplicitReadOnlyWorkflowTab(activeTab)
          ? activeTab
          : null
        // Tab must match workflow mode and the active preset. Read-only Schedule/Bot
        // tabs only stay active immediately after an explicit open action.
        const hasValidActiveTab = activeTabMatchesPreset &&
          (isInteractiveWorkflowTab(activeTab) || !!explicitReadOnlyActiveTab)

        // Prefer the workflow tab the user last had active for this preset.
        let workflowTabs = Object.values(chatStore.chatTabs)
          .filter(tab => isInteractiveWorkflowTab(tab) && (tab.sessionId || tab.isStreaming))
          .sort((a, b) => workflowTabSortTimestamp(b) - workflowTabSortTimestamp(a))

        if (activePresetId) {
          const presetTabs = workflowTabs.filter(tab => tab.metadata?.presetQueryId === activePresetId)
          if (presetTabs.length > 0) workflowTabs = presetTabs
        }

        const rememberedWorkflowTab = workflowStore.activeWorkflowTabId
          ? chatStore.getTab(workflowStore.activeWorkflowTabId)
          : null
        const rememberedWorkflowTabMatchesPreset = rememberedWorkflowTab &&
          isInteractiveWorkflowTab(rememberedWorkflowTab) &&
          rememberedWorkflowTab.metadata?.presetQueryId === activePresetId
        const builderTab = workflowTabs.find(tab => tab.metadata?.phaseId === 'workflow-builder')
        const streamingTab = workflowTabs.find(tab => chatStore.getTabStreamingStatus(tab.tabId) || tab.isStreaming)
        const activeWorkflowViewMode = normalizeEventViewMode(
          activeTab?.metadata?.mode === 'workflow'
            ? activeTab.viewMode
            : chatStore.eventViewModePreference
        )
        const targetWorkflowTab = explicitReadOnlyActiveTab || (
          activeWorkflowViewMode === 'terminal'
            ? streamingTab ||
              (hasValidActiveTab ? activeTab : null) ||
              (rememberedWorkflowTabMatchesPreset ? rememberedWorkflowTab : null) ||
              builderTab ||
              workflowTabs[0]
            : builderTab ||
              (hasValidActiveTab ? activeTab : null) ||
              (rememberedWorkflowTabMatchesPreset ? rememberedWorkflowTab : null) ||
              streamingTab ||
              workflowTabs[0]
        )

        if (targetWorkflowTab) {
          if (!hasValidActiveTab || activeTabId !== targetWorkflowTab.tabId) {
            chatStore.switchTab(targetWorkflowTab.tabId)
          }

          const shouldShowWorkflowChat =
            workflowStore.showChatArea ||
            targetWorkflowTab.metadata?.phaseId === 'workflow-builder'

          if (shouldShowWorkflowChat) {
            workflowStore.setShowChatArea(true)
          }
        } else {
          // No active workflow tabs - clear activeTabId so WorkflowLayout's ChatArea
          // doesn't display content from another mode
          useChatStore.setState({ activeTabId: null })
        }
        return
      }

      // For multi-agent: select the first tab of the current mode
      // if activeTabId is null, invalid, or belongs to a different mode
      if (selectedModeCategory !== 'multi-agent') {
        return
      }

      const chatStore = useChatStore.getState()

      // Single-tab invariant: older sessions may have persisted multiple
      // multi-agent tabs. Collapse them to the most-recently-accessed one and
      // close the rest so multi-agent chat always shows exactly one tab.
      const multiAgentTabs = Object.values(chatStore.chatTabs).filter(tab =>
        tab.metadata?.mode === 'multi-agent' &&
        tab.metadata?.isOrganizationAssistant !== true
      )
      if (multiAgentTabs.length > 1) {
        const keep = multiAgentTabs.reduce((best, t) =>
          (t.lastAccessedAt ?? t.createdAt ?? 0) > (best.lastAccessedAt ?? best.createdAt ?? 0) ? t : best
        , multiAgentTabs[0])
        for (const tab of multiAgentTabs) {
          if (tab.tabId !== keep.tabId) {
            // keepEvents=false, stopSession=false: discard the extra tab's UI
            // state without killing its backend session.
            await chatStore.closeTab(tab.tabId, false)
          }
        }
        chatStore.switchTab(keep.tabId)
        return
      }

      const activeTabId = chatStore.activeTabId

      // Check if activeTabId is null, points to a non-existent tab, or belongs to a different mode
      const activeTab = activeTabId ? chatStore.getTab(activeTabId) : null
      const hasValidActiveTab = activeTab &&
        activeTab.metadata?.mode === selectedModeCategory &&
        activeTab.metadata?.isOrganizationAssistant !== true

      if (!hasValidActiveTab && multiAgentTabs.length === 1) {
        chatStore.switchTab(multiAgentTabs[0].tabId)
      }
    }

    void ensureActiveTab()

    return () => {
      cancelled = true
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
        } else if (selectedModeCategory === 'multi-agent') {
          // For multi-agent mode, if there's no active preset, clear any stale preset server state
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

  // Retry preset restoration for workflow mode after presets finish loading from manifests
  // The 500ms timer above may fire before refreshPresets() completes
  const workflowPresetsForRestore = useGlobalPresetStore(state => state.workflowPresets)
  useEffect(() => {
    if (hasRestoredPresetRef.current) return
    if (!hasCompletedInitialSetup || selectedModeCategory !== 'workflow') return
    if (workflowPresetsForRestore.length === 0) return // Presets not loaded yet

    const activePreset = getActivePreset('workflow')
    if (activePreset) {
      hasRestoredPresetRef.current = true
      applyPreset(activePreset.id, 'workflow')
    }
  }, [hasCompletedInitialSetup, selectedModeCategory, workflowPresetsForRestore, getActivePreset, applyPreset])


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

  // Minimize toggle functions
  const toggleSidebarMinimize = useCallback(() => {
    setSidebarMinimized(!sidebarMinimized)
  }, [sidebarMinimized, setSidebarMinimized])

  const toggleWorkspaceMinimize = useCallback(() => {
    setWorkspaceMinimized(!workspaceMinimized)
  }, [workspaceMinimized, setWorkspaceMinimized])

  // Multi-agent chat is single-tab: "New Chat" resets the current chat in
  // place (clears the conversation + starts a fresh backend session) instead
  // of opening another tab. Confirm first so the running session isn't lost
  // by accident.
  const [showNewChatConfirm, setShowNewChatConfirm] = useState(false)
  const requestNewMultiAgentChat = useCallback(() => {
    setShowNewChatConfirm(true)
  }, [])
  const confirmNewMultiAgentChat = useCallback(() => {
    setShowNewChatConfirm(false)
    // handleNewChat clears the backend session and resetTabChat's the single
    // multi-agent tab in place (see ChatArea.handleNewChat).
    void chatAreaRef.current?.handleNewChat()
  }, [])

  // After Ctrl+1/Ctrl+2 mode switch, restore the most recently-accessed
  // tab matching the new mode. Without this the activeTabId stays on
  // whatever was selected before (often a tab in the *other* mode), so
  // the workflow's chat panel doesn't pick up the running session and
  // the user has to click the tab manually.
  const restoreMostRecentTabForMode = useCallback((mode: 'workflow' | 'multi-agent') => {
    const chatStore = useChatStore.getState()
    const currentTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : null
    if (
      currentTab &&
      currentTab.metadata?.mode === mode &&
      (mode !== 'workflow' || isInteractiveWorkflowTab(currentTab) || isRecentExplicitReadOnlyWorkflowTab(currentTab))
    ) return
    const candidates = Object.values(chatStore.chatTabs).filter(t =>
      t.metadata?.mode === mode &&
      (mode !== 'workflow' || isInteractiveWorkflowTab(t))
    )
    if (candidates.length === 0) return
    const mostRecent = candidates.reduce((best, t) =>
      workflowTabSortTimestamp(t) > workflowTabSortTimestamp(best) ? t : best
    , candidates[0])
    chatStore.switchTab(mostRecent.tabId)
  }, [])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Ctrl/Cmd + 1 for Workflow mode
      if ((event.ctrlKey || event.metaKey) && event.key === '1') {
        event.preventDefault()
        const { setModeCategory } = useModeStore.getState()
        setModeCategory('workflow')
        setShowWorkflowsOverview(false)
        restoreMostRecentTabForMode('workflow')
        return
      }
      // Ctrl/Cmd + 2 for Chat mode
      if ((event.ctrlKey || event.metaKey) && event.key === '2') {
        event.preventDefault()
        const { setModeCategory } = useModeStore.getState()
        setModeCategory('multi-agent')
        setShowWorkflowsOverview(false)
        restoreMostRecentTabForMode('multi-agent')
        return
      }
      // Ctrl/Cmd + 3 for Organization view
      if ((event.ctrlKey || event.metaKey) && event.key === '3') {
        event.preventDefault()
        setShowWorkflowsOverview(true)
        return
      }
      // Ctrl/Cmd + 5 for sidebar minimize
      if ((event.ctrlKey || event.metaKey) && event.key === '5') {
        event.preventDefault()
        toggleSidebarMinimize()
        return
      }
      // Ctrl/Cmd + 6 for workspace minimize
      if ((event.ctrlKey || event.metaKey) && event.key === '6') {
        event.preventDefault()
        toggleWorkspaceMinimize()
        return
      }
      // Ctrl/Cmd + 7 for auto-scroll
      if ((event.ctrlKey || event.metaKey) && event.key === '7') {
        event.preventDefault()
        const chatStore = useChatStore.getState()
        chatStore.setAutoScroll(!chatStore.autoScroll)
        return
      }
      // Ctrl/Cmd + K for the global quick switcher
      if ((event.ctrlKey || event.metaKey) && event.key === 'k') {
        event.preventDefault()
        setQuickSwitcherInitialQuery('')
        setShowQuickSwitcher(prev => !prev)
        return
      }
      // Ctrl/Cmd + N for new chat
      if ((event.ctrlKey || event.metaKey) && event.key === 'n') {
        event.preventDefault()
        if (selectedModeCategory === 'multi-agent' && !showWorkflowsOverview) {
          requestNewMultiAgentChat()
          return
        }
        // Outside chat mode, preserve the existing reset-current-chat behavior.
        if (chatAreaRef.current) {
          chatAreaRef.current.handleNewChat()
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [requestNewMultiAgentChat, restoreMostRecentTabForMode, selectedModeCategory, showWorkflowsOverview, toggleSidebarMinimize, toggleWorkspaceMinimize, setAgentMode, setShowWorkflowsOverview, startNewChat])

  useEffect(() => {
    if (showWorkflowsOverview) {
      setWorkspaceMinimizedForLayout(true)
      return
    }

    if (selectedModeCategory === 'workflow' || selectedModeCategory === 'multi-agent') {
      const { workspaceMinimizedByMode } = useAppStore.getState()
      setWorkspaceMinimizedForLayout(Boolean(workspaceMinimizedByMode?.[selectedModeCategory]))
    }
  }, [selectedModeCategory, showWorkflowsOverview, setWorkspaceMinimizedForLayout])

  useEffect(() => {
    const collapseWorkspaceForPopup = () => {
      const { workspaceMinimized: currentWorkspaceMinimized } = useAppStore.getState()
      if (!currentWorkspaceMinimized && hasOpenWorkspaceCollapsingPopup()) {
        setWorkspaceMinimizedForLayout(true)
      }
    }

    collapseWorkspaceForPopup()

    const observer = new MutationObserver(collapseWorkspaceForPopup)
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['class', 'style', 'role', 'data-workspace-popup', 'data-workspace-collapse-ignore'],
    })

    return () => observer.disconnect()
  }, [setWorkspaceMinimizedForLayout])

  const multiAgentPanelDesktop = orgHtmlPreviewDevice === 'desktop'
  const layoutWorkspaceMinimized =
    showWorkflowsOverview
      ? true
      : selectedModeCategory === 'workflow' || selectedModeCategory === 'multi-agent'
        ? Boolean(workspaceMinimizedByMode?.[selectedModeCategory])
        : workspaceMinimized
  const toggleMultiAgentPanelMinimize = useCallback(() => {
    setWorkspaceMinimized(!layoutWorkspaceMinimized)
  }, [layoutWorkspaceMinimized, setWorkspaceMinimized])
  const multiAgentPanelStyle = multiAgentPanelDesktop
    ? undefined
    : { width: orgHtmlPreviewDevice === 'tablet' ? 'min(880px, 72vw)' : 'min(480px, 56vw)' }
  const multiAgentPanelClass = multiAgentPanelDesktop ? 'flex-1' : 'flex-none'
  const multiAgentPanelTabs = (
    <div className="inline-flex min-w-0 flex-none items-center gap-0.5 rounded-lg border border-border bg-muted/70 p-0.5 shadow-sm backdrop-blur-sm">
      <button
        type="button"
        onClick={() => setMultiAgentRightPanelView('org-pulse')}
        title="Org Pulse"
        aria-label="Org Pulse"
        className={multiAgentPanelTabClass(multiAgentRightPanelView === 'org-pulse')}
      >
        Pulse
      </button>
      <button
        type="button"
        onClick={() => setMultiAgentRightPanelView('org-goals')}
        className={multiAgentPanelTabClass(multiAgentRightPanelView === 'org-goals')}
      >
        Goals
      </button>
      <button
        type="button"
        onClick={() => setMultiAgentRightPanelView('memory')}
        className={multiAgentPanelTabClass(multiAgentRightPanelView === 'memory')}
      >
        Memory
      </button>
      <button
        type="button"
        onClick={() => setMultiAgentRightPanelView('files')}
        className={multiAgentPanelTabClass(multiAgentRightPanelView === 'files')}
      >
        Files
      </button>
    </div>
  )
  const multiAgentPanelCloseButton = (
    <button
      type="button"
      onClick={toggleMultiAgentPanelMinimize}
      title="Hide panel"
      aria-label="Hide panel"
      className="inline-flex h-7 w-7 flex-none items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
    >
      <PanelRightClose className="h-4 w-4" />
    </button>
  )

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthWrapper>
        <TooltipProvider>
        <UpdateProgressToast />
        <div className="h-screen bg-background flex">
          {/* Left Sidebar */}
          <div className={`${sidebarMinimized ? 'w-16' : 'w-72'} transition-all duration-300 ease-in-out relative z-30`}>
            <WorkspaceSidebar
              minimized={sidebarMinimized}
              onToggleMinimize={toggleSidebarMinimize}
            />
          </div>

          {/* Middle Content Area - WorkflowLayout (workflow mode) or ChatArea (other modes) */}
          <div className="flex-1 flex flex-col min-w-0 min-h-0 relative z-10 overflow-hidden">
            {/* Quick Switcher (Ctrl+K) - constrained to the main content area */}
            <QuickSwitcher
              isOpen={showQuickSwitcher}
              onClose={() => setShowQuickSwitcher(false)}
              initialQuery={quickSwitcherInitialQuery}
            />

            {/* Global Mode & Preset Bar - only above middle content area, not sidebars */}
            <ModePresetBar />
            
            {/* Chat Tabs - global navigation for both chat and workflow modes */}
            <ChatTabs
              onNewChat={requestNewMultiAgentChat}
              autoScroll={useChatStore(state => state.autoScroll)}
              onSubmitOrgCommand={submitMultiAgentPanelCommand}
              onToggleAutoScroll={() => {
                const chatStore = useChatStore.getState()
                chatStore.setAutoScroll(!chatStore.autoScroll)
              }}
            />

            <ConfirmationDialog
              isOpen={showNewChatConfirm}
              onClose={() => setShowNewChatConfirm(false)}
              onConfirm={confirmNewMultiAgentChat}
              title="Start a new chat?"
              message="This stops the current chat session and clears the conversation. This can't be undone."
              confirmText="New Chat"
              cancelText="Cancel"
              type="warning"
            />
            
              <div className="flex-1 min-h-0 overflow-hidden relative">
                <div className={showWorkflowsOverview ? 'h-full' : 'hidden'}>
                  <WorkflowsOverviewPage />
                </div>
                <div className={!showWorkflowsOverview ? 'h-full' : 'hidden'}>
                  <div className={selectedModeCategory === 'workflow' ? 'h-full' : 'hidden'}>
                    <WorkflowLayout
                      className="h-full"
                      onNewChat={startNewChat}
                    />
                  </div>
                  <div className={selectedModeCategory !== 'workflow' ? 'h-full relative' : 'hidden'}>
                    {layoutWorkspaceMinimized && (
                      <button
                        type="button"
                        onClick={() => setWorkspaceMinimized(false)}
                        title="Show side panel"
                        aria-label="Show side panel"
                        className="absolute right-0 top-1/2 z-30 hidden -translate-y-1/2 flex-col items-center gap-1.5 rounded-l-lg border border-r-0 border-border bg-background/95 py-3 pl-1.5 pr-1 text-muted-foreground shadow-md backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground md:flex"
                      >
                        <PanelRightOpen className="h-4 w-4" />
                        <span className="[writing-mode:vertical-rl] text-[10px] font-semibold uppercase tracking-wider">Panel</span>
                      </button>
                    )}
                    <div className="flex h-full min-w-0">
                      <div className={`min-w-0 flex-1 ${multiAgentPanelDesktop ? 'hidden' : ''}`}>
                        <ChatAreaWithObserverId
                          ref={chatAreaRef}
                          onNewChat={startNewChat}
                        />
                      </div>
                      {!layoutWorkspaceMinimized && (
                        <div
                          className={`flex flex-col overflow-hidden border-l border-gray-200 bg-background dark:border-gray-700 ${multiAgentPanelClass}`}
                          style={multiAgentPanelStyle}
                        >
                          {multiAgentRightPanelView === 'files' && (
                            <div className="flex flex-wrap items-center justify-between gap-1 border-b border-border bg-muted/40 px-2 py-2">
                              {multiAgentPanelTabs}
                              {multiAgentPanelCloseButton}
                            </div>
                          )}
                          <div className="min-h-0 flex-1 overflow-hidden">
                            {multiAgentRightPanelView === 'files' ? (
                              <Workspace
                                minimized={false}
                                onToggleMinimize={toggleMultiAgentPanelMinimize}
                              />
                            ) : multiAgentRightPanelView === 'org-goals' ? (
                              <OrgGoalsPanel
                                toolbarLeading={multiAgentPanelTabs}
                                onClosePanel={toggleMultiAgentPanelMinimize}
                              />
                            ) : multiAgentRightPanelView === 'memory' ? (
                              <MemoryPanel
                                toolbarLeading={multiAgentPanelTabs}
                                onClosePanel={toggleMultiAgentPanelMinimize}
                              />
                            ) : (
                              <OrgPulsePanel
                                toolbarLeading={multiAgentPanelTabs}
                                onClosePanel={toggleMultiAgentPanelMinimize}
                              />
                            )}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
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
                        {isRenderedMarkdownSearchAvailable && (
                          <RenderedContentSearchButton
                            search={renderedContentSearch}
                            className="flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                          />
                        )}
                        <button
                          onClick={async () => {
                            if (!fileContent) return
                            await navigator.clipboard.writeText(fileContent)
                            setContentCopied(true)
                            setTimeout(() => setContentCopied(false), 2000)
                          }}
                          className="flex items-center gap-1 p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                          title="Copy formatted content"
                        >
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <rect x="9" y="9" width="13" height="13" rx="2" ry="2" strokeWidth={2} />
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
                          </svg>
                          {contentCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
                        </button>
                        <button
                          onClick={async () => {
                            if (!fileContent) return
                            const slack = convertToSlackMarkdown(fileContent)
                            await navigator.clipboard.writeText(slack)
                            setSlackCopied(true)
                            setTimeout(() => setSlackCopied(false), 2000)
                          }}
                          className="flex items-center gap-1 p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                          title="Copy as Slack format"
                        >
                          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                            <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zm1.271 0a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zm0 1.271a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zm-1.27 0a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.163 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.163 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.163 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zm0-1.27a2.527 2.527 0 0 1-2.52-2.523 2.527 2.527 0 0 1 2.52-2.52h6.315A2.528 2.528 0 0 1 24 15.163a2.528 2.528 0 0 1-2.522 2.523h-6.315z"/>
                          </svg>
                          {slackCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
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

              {isRenderedMarkdownSearchAvailable && (
                <RenderedContentSearchBar search={renderedContentSearch} />
              )}
              
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
                    ) : (selectedFile?.path && isCodeFile(selectedFile.path) && !/\.html?$/i.test(selectedFile.path)) ? (
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

                          // Video files
                          if ((filePath.endsWith('.webm') || filePath.endsWith('.mp4') || filePath.endsWith('.mov')) && videoObjectUrl) {
                            return (
                              <div className="h-[calc(100vh-120px)] w-full flex items-center justify-center bg-black rounded-lg">
                                <video
                                  controls
                                  autoPlay
                                  className="max-h-full max-w-full"
                                  src={videoObjectUrl}
                                />
                              </div>
                            )
                          }

                          // Audio files
                          if ((filePath.endsWith('.mp3') || filePath.endsWith('.wav') || filePath.endsWith('.m4a') || filePath.endsWith('.aac') || filePath.endsWith('.ogg') || filePath.endsWith('.oga') || filePath.endsWith('.flac') || filePath.endsWith('.opus')) && audioObjectUrl) {
                            return (
                              <div className="min-h-[260px] w-full flex items-center justify-center rounded-lg border border-gray-200 bg-gray-50 p-8 dark:border-gray-700 dark:bg-gray-900">
                                <audio
                                  controls
                                  autoPlay
                                  className="w-full max-w-3xl"
                                  src={audioObjectUrl}
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

                          // Conversation log files (-conversation.json)
                          if (selectedFile?.path && isValidJSON(fileContent)) {
                            try {
                              const parsed = JSON.parse(fileContent)
                              console.log('[ConversationRenderer] path:', selectedFile.path, 'isConv:', isConversationJSON(selectedFile.path, parsed))
                              if (isConversationJSON(selectedFile.path, parsed)) {
                                return <ConversationRenderer content={fileContent} />
                              }
                            } catch { /* fall through to generic JSON */ }
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

                          if ((selectedFile?.path && isDiffFilePath(selectedFile.path)) || looksLikeDiffContent(fileContent)) {
                            return <DiffRenderer content={fileContent} />
                          }
 
                          // Default: render as markdown
                          return (
                            <div ref={markdownContentRef} className="max-w-4xl mx-auto">
                              <div className="prose prose-sm max-w-none dark:prose-invert prose-headings:font-semibold prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline prose-strong:text-gray-900 dark:prose-strong:text-gray-100 prose-code:text-blue-600 dark:prose-code:text-blue-400 prose-pre:bg-gray-50 dark:prose-pre:bg-gray-900 prose-blockquote:border-l-blue-500 prose-blockquote:text-gray-700 dark:prose-blockquote:text-gray-300">
                                <MarkdownRenderer 
                                  content={fileContent} 
                                  className="max-w-none"
                                  showScrollbar={true}
                                  basePath={selectedFile?.path}
                                />
                              </div>
                            </div>
                          )
                        })()}
                      </div>
                    )}
                  </>
                )}
              </div>
              </div>
            )}
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
