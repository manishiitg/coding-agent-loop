import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { Layers, Search, MessageSquare, Loader2 } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useChatStore } from '../stores'
import { restoreSession } from '../utils/sessionRestore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { ChatHistorySummary } from '../services/api-types'

interface QuickSwitcherProps {
  isOpen: boolean
  onClose: () => void
}

interface WorkflowItem {
  type: 'workflow'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  preset: CustomPreset | PredefinedPreset
}

interface ChatItem {
  type: 'chat'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  session: ChatHistorySummary
}

type QuickSwitcherItem = WorkflowItem | ChatItem

export const QuickSwitcher: React.FC<QuickSwitcherProps> = ({
  isOpen,
  onClose
}) => {
  const [query, setQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [chatSessions, setChatSessions] = useState<ChatHistorySummary[]>([])
  const [isLoadingChats, setIsLoadingChats] = useState(false)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isWorkflowMode = selectedModeCategory === 'workflow'
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const activeTabId = useChatStore(state => state.activeTabId)

  // Track Shift key state to show "minimize" hint on selected item
  const [shiftHeld, setShiftHeld] = useState(false)
  useEffect(() => {
    if (!isOpen) return
    const onKey = (e: KeyboardEvent) => setShiftHeld(e.shiftKey)
    window.addEventListener('keydown', onKey)
    window.addEventListener('keyup', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('keyup', onKey)
      setShiftHeld(false)
    }
  }, [isOpen])

  // Reset state on open
  useEffect(() => {
    if (isOpen) {
      setQuery('')
      setSelectedIndex(0)
      setTimeout(() => searchInputRef.current?.focus(), 50)

      // Load chat history for non-workflow modes
      if (!isWorkflowMode) {
        setIsLoadingChats(true)
        const modeKey = 'multi-agent'
        useChatStore.getState().getChatHistory(modeKey, true)
          .then(sessions => setChatSessions(sessions))
          .catch(err => console.error('[QuickSwitcher] Failed to load chat history:', err))
          .finally(() => setIsLoadingChats(false))
      }
    }
  }, [isOpen, isWorkflowMode, selectedModeCategory])

  // Build items based on mode
  const allItems = useMemo<QuickSwitcherItem[]>(() => {
    if (!isOpen) return []

    if (isWorkflowMode) {
      const { customPresets, predefinedPresets, recentPresetOrder } = useGlobalPresetStore.getState()
      const allPresets: (CustomPreset | PredefinedPreset)[] = [
        ...customPresets,
        ...predefinedPresets
      ]
      const items = allPresets
        .filter(p => p.agentMode === 'workflow' && p.selectedFolder?.filepath)
        .map(p => ({
          type: 'workflow' as const,
          id: p.id,
          label: p.label,
          subtitle: p.selectedFolder!.filepath,
          isActive: p.id === activePresetId,
          preset: p
        }))

      // Sort by recent access order (most recently used first)
      items.sort((a, b) => {
        const aIdx = recentPresetOrder.indexOf(a.id)
        const bIdx = recentPresetOrder.indexOf(b.id)
        // Items in recentPresetOrder come first, ordered by recency
        if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
        if (aIdx !== -1) return -1
        if (bIdx !== -1) return 1
        return a.label.localeCompare(b.label)
      })

      return items
    } else {
      // Chat/Multi-agent mode: show previous sessions
      return chatSessions
        .filter(s => {
          const title = (s.title || '').toLowerCase()
          if (title === 'organization assistant' || title.startsWith('org chat ')) return false
          // Filter out workflow sessions
          if ((s.agent_mode || '').toLowerCase() === 'workflow') return false
          // Mode-based filtering
          const isMultiAgent = s.config?.delegation_mode === 'plan' || s.config?.delegation_mode === 'spawn'
          if (selectedModeCategory === 'multi-agent' && !isMultiAgent) return false
          return true
        })
        .map(s => ({
          type: 'chat' as const,
          id: s.session_id,
          label: s.title || 'Untitled',
          subtitle: formatTime(s.last_activity || s.created_at),
          isActive: false,
          session: s
        }))
    }
  }, [isOpen, isWorkflowMode, activePresetId, chatSessions, selectedModeCategory])

  // Filter and sort
  const filteredItems = useMemo<QuickSwitcherItem[]>(() => {
    if (!query.trim()) return allItems

    const q = query.toLowerCase().trim()
    const filtered = allItems.filter(item =>
      item.label.toLowerCase().includes(q) ||
      item.subtitle.toLowerCase().includes(q)
    )

    filtered.sort((a, b) => {
      const aExact = a.label.toLowerCase() === q
      const bExact = b.label.toLowerCase() === q
      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1
      const aStarts = a.label.toLowerCase().startsWith(q)
      const bStarts = b.label.toLowerCase().startsWith(q)
      if (aStarts && !bStarts) return -1
      if (!aStarts && bStarts) return 1
      return a.label.localeCompare(b.label)
    })

    return filtered
  }, [query, allItems])

  // Reset index when results change
  useEffect(() => { setSelectedIndex(0) }, [filteredItems])

  // Scroll selected item into view
  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const el = listRef.current.children[selectedIndex] as HTMLElement
      el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }, [selectedIndex])

  // Switch to a workflow/chat. When `minimize` is true, the old workflow's tabs
  // are set to 'summary' viewMode (lightweight rendering — agent outputs only,
  // no tool calls, no canvas updates) and the new workflow's tabs to 'detailed'.
  const handleSelect = useCallback(async (item: QuickSwitcherItem, minimize = false) => {
    if (item.type === 'workflow') {
      console.log(`%c[QuickSwitcher] Switching to workflow: ${item.label?.slice(0,30)} (${item.id?.slice(0,8)})`, 'color: #FF9800; font-weight: bold')
      console.time('[QuickSwitcher] workflow-switch-total')
      const chatStore = useChatStore.getState()
      const presetStore = useGlobalPresetStore.getState()

      if (minimize) {
        // Set ALL tabs of the OLD (current) preset to summary mode — they're going to background
        const oldPresetId = presetStore.activePresetIds.workflow
        if (oldPresetId) {
          Object.values(chatStore.chatTabs).forEach(tab => {
            if (tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === oldPresetId) {
              chatStore.setTabViewMode(tab.tabId, 'summary')
            }
          })
        }

        // Set ALL tabs of the NEW preset to detailed mode — they're coming to foreground
        Object.values(chatStore.chatTabs).forEach(tab => {
          if (tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === item.id) {
            chatStore.setTabViewMode(tab.tabId, 'detailed')
          }
        })
      }

      presetStore.applyPreset(item.preset, 'workflow')
      console.timeEnd('[QuickSwitcher] workflow-switch-total')
    } else {
      // Restore chat session
      const tabId = await restoreSession(item.session.session_id, {
        title: item.session.title,
        source: 'quick-switcher',
      })
      useChatStore.getState().switchTab(tabId)
    }
    onClose()
  }, [onClose])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIndex(prev => Math.min(prev + 1, filteredItems.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIndex(prev => Math.max(prev - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (filteredItems.length > 0 && selectedIndex >= 0 && selectedIndex < filteredItems.length) {
        // Shift+Enter: switch AND minimize the old workflow (set to summary mode).
        // Plain Enter: switch normally (keep old workflow in detailed mode).
        handleSelect(filteredItems[selectedIndex], e.shiftKey)
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }, [filteredItems, selectedIndex, handleSelect, onClose])

  if (!isOpen) return null

  const placeholder = isWorkflowMode ? 'Switch workflow...' : 'Search conversations...'
  const emptyText = isWorkflowMode
    ? (query ? 'No matching workflows' : 'No workflow presets available')
    : (query ? 'No matching conversations' : 'No previous conversations')
  const Icon = isWorkflowMode ? Layers : MessageSquare

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]"
      onClick={onClose}
    >
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/50" />

      {/* Dialog */}
      <div
        className="relative w-full max-w-md bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-2xl overflow-hidden text-gray-900 dark:text-gray-100"
        onClick={e => e.stopPropagation()}
      >
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-700">
          <Search className="w-5 h-5 text-gray-400 flex-shrink-0" />
          <input
            ref={searchInputRef}
            type="text"
            placeholder={placeholder}
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground focus:outline-none"
          />
          <kbd className="hidden sm:inline-flex px-1.5 py-0.5 text-[10px] font-mono text-gray-400 bg-gray-100 dark:bg-gray-700 rounded">
            ESC
          </kbd>
        </div>

        {/* Item list */}
        <div ref={listRef} className="overflow-y-auto max-h-72">
          {!isWorkflowMode && isLoadingChats ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredItems.length === 0 ? (
            <div className="px-4 py-8 text-center text-muted-foreground text-sm">
              {emptyText}
            </div>
          ) : (
            filteredItems.map((item, index) => {
              const isSelected = index === selectedIndex
              return (
                <div
                  key={item.id}
                  className={`px-4 py-2.5 cursor-pointer flex items-center gap-3 transition-colors ${
                    isSelected
                      ? 'bg-blue-50 dark:bg-blue-900/30'
                      : 'hover:bg-gray-50 dark:hover:bg-gray-700/50'
                  }`}
                  onMouseEnter={() => setSelectedIndex(index)}
                  onMouseDown={e => { e.preventDefault(); handleSelect(item, e.shiftKey) }}
                >
                  <Icon className={`w-4 h-4 flex-shrink-0 ${item.isActive ? 'text-blue-500' : 'text-gray-400 dark:text-gray-500'}`} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`text-sm font-medium truncate ${item.isActive ? 'text-blue-600 dark:text-blue-400' : 'text-gray-900 dark:text-gray-100'}`}>
                        {item.label}
                      </span>
                      {item.isActive && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400 font-medium flex-shrink-0">
                          active
                        </span>
                      )}
                      {item.type === 'chat' && item.session.status === 'running' && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-green-100 dark:bg-green-900/50 text-green-600 dark:text-green-400 font-medium flex-shrink-0">
                          running
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground truncate">{item.subtitle}</div>
                  </div>
                  {/* Show "minimize current" hint when Shift is held on a non-active workflow item */}
                  {isSelected && shiftHeld && isWorkflowMode && item.type === 'workflow' && !item.isActive && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-600 dark:text-amber-300 font-medium flex-shrink-0 animate-in fade-in duration-150">
                      minimize current
                    </span>
                  )}
                </div>
              )
            })
          )}
        </div>

        {/* Footer */}
        <div className="px-4 py-2 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50 text-[11px] text-gray-400 dark:text-gray-500 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↑↓</kbd> navigate</span>
            <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">↵</kbd> switch</span>
            {isWorkflowMode && (
              <span><kbd className="px-1 py-0.5 bg-amber-200 dark:bg-amber-800 text-amber-700 dark:text-amber-300 rounded text-[10px]">⇧↵</kbd> switch &amp; minimize</span>
            )}
          </div>
          <span><kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px]">esc</kbd> close</span>
        </div>
      </div>
    </div>
  )
}

function formatTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return date.toLocaleDateString()
}

export default QuickSwitcher
