import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { Layers, MessageSquare, Search } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useChatStore } from '../stores'
import type { ChatTab } from '../stores/useChatStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'

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

interface ChatTabItem {
  type: 'chat'
  id: string
  label: string
  subtitle: string
  isActive: boolean
  tabId: string
}

type QuickSwitcherItem = WorkflowItem | ChatTabItem

const EMPTY_CHAT_TABS: Record<string, ChatTab> = {}

export const QuickSwitcher: React.FC<QuickSwitcherProps> = ({
  isOpen,
  onClose
}) => {
  const [query, setQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isWorkflowMode = selectedModeCategory === 'workflow'
  const isChatMode = selectedModeCategory === 'multi-agent'
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  // Scope chat-store subscriptions to chat mode — chatTabs gets a new
  // reference on every streaming event, which would otherwise re-render this
  // component (and recompute filteredItems) many times per second in
  // workflow mode.
  const activeTabId = useChatStore(state => (isChatMode ? state.activeTabId : null))
  const chatTabs = useChatStore(state => (isChatMode ? state.chatTabs : EMPTY_CHAT_TABS))

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

  // Reset state on open.
  useEffect(() => {
    if (isOpen) {
      setQuery('')
      setSelectedIndex(0)
      setTimeout(() => searchInputRef.current?.focus(), 50)
    }
  }, [isOpen])

  // Build items list based on current mode.
  // Workflow mode: workflow presets. Chat mode: chat tabs.
  const allItems = useMemo<QuickSwitcherItem[]>(() => {
    if (!isOpen) return []

    if (isWorkflowMode) {
      const { workflowPresets, recentPresetOrder } = useGlobalPresetStore.getState()
      const items: QuickSwitcherItem[] = workflowPresets
        .filter(p => p.selectedFolder?.filepath)
        .map(p => ({
          type: 'workflow' as const,
          id: p.id,
          label: p.label,
          subtitle: p.selectedFolder!.filepath,
          isActive: p.id === activePresetId,
          preset: p
        }))

      items.sort((a, b) => {
        const aIdx = recentPresetOrder.indexOf(a.id)
        const bIdx = recentPresetOrder.indexOf(b.id)
        if (aIdx !== -1 && bIdx !== -1) return aIdx - bIdx
        if (aIdx !== -1) return -1
        if (bIdx !== -1) return 1
        return a.label.localeCompare(b.label)
      })

      return items
    }

    if (isChatMode) {
      const tabs = Object.values(chatTabs)
        .filter(tab => tab.metadata?.mode === 'multi-agent' && !tab.metadata?.isOrganizationAssistant)
        .sort((a, b) => a.createdAt - b.createdAt)
        .map(tab => ({
          type: 'chat' as const,
          id: tab.tabId,
          label: tab.name,
          subtitle: tab.isStreaming ? 'Streaming...' : tab.isCompleted ? 'Completed' : tab.sessionId ? 'Active' : 'New',
          isActive: tab.tabId === activeTabId,
          tabId: tab.tabId
        }))

      return tabs
    }

    return []
  }, [isOpen, isWorkflowMode, isChatMode, activePresetId, chatTabs, activeTabId])

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

  // Read filteredItems via a ref so this effect does NOT fire on every new
  // array reference — only on real user intent changes (query, mode, open).
  // Otherwise streaming re-renders would snap the selection back on each tick.
  const filteredItemsRef = useRef(filteredItems)
  filteredItemsRef.current = filteredItems
  useEffect(() => {
    if (!query.trim() && (isChatMode || isWorkflowMode)) {
      const firstNonActive = filteredItemsRef.current.findIndex(item => !item.isActive)
      setSelectedIndex(firstNonActive >= 0 ? firstNonActive : 0)
    } else {
      setSelectedIndex(0)
    }
  }, [isOpen, isChatMode, isWorkflowMode, query])

  // Clamp (don't reset) when the list length changes so a narrowing filter
  // keeps a valid index without discarding the user's position.
  useEffect(() => {
    setSelectedIndex(prev => {
      if (filteredItems.length === 0) return 0
      return Math.min(prev, filteredItems.length - 1)
    })
  }, [filteredItems.length])

  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const el = listRef.current.children[selectedIndex] as HTMLElement
      el?.scrollIntoView({ block: 'nearest', behavior: 'auto' })
    }
  }, [selectedIndex])

  // Switch to a workflow or chat tab. When `minimize` is true (workflow only),
  // the old workflow's tabs are set to 'summary' viewMode.
  const handleSelect = useCallback((item: QuickSwitcherItem, minimize = false) => {
    if (item.type === 'chat') {
      console.log(`%c[QuickSwitcher] Switching to chat tab: ${item.label} (${item.tabId})`, 'color: #FF9800; font-weight: bold')
      useChatStore.getState().switchTab(item.tabId)
      onClose()
      return
    }

    // Workflow switching
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

    // Switch to the correct tab for the new preset (the App.tsx effect only
    // runs on mode change, not on preset change within workflow mode)
    const updatedChatStore = useChatStore.getState()
    const currentTab = updatedChatStore.activeTabId ? updatedChatStore.getTab(updatedChatStore.activeTabId) : null
    const hasValidTab = currentTab &&
      currentTab.metadata?.mode === 'workflow' &&
      currentTab.metadata?.presetQueryId === item.id

    if (!hasValidTab) {
      // Find the most recent workflow tab for the new preset
      const presetTabs = Object.values(updatedChatStore.chatTabs)
        .filter(tab => tab.metadata?.mode === 'workflow' && tab.metadata?.presetQueryId === item.id && (tab.sessionId || tab.isStreaming))
        .sort((a, b) => b.createdAt - a.createdAt)

      if (presetTabs.length > 0) {
        updatedChatStore.switchTab(presetTabs[0].tabId)
        useWorkflowStore.getState().setShowChatArea(true)
      } else {
        // No tabs for this preset yet — clear activeTabId so ChatArea doesn't show stale content
        useChatStore.setState({ activeTabId: null })
      }
    }

    console.timeEnd('[QuickSwitcher] workflow-switch-total')
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

  const placeholder = isWorkflowMode ? 'Switch workflow...' : 'Switch chat tab...'
  const emptyText = (!isWorkflowMode && !isChatMode)
    ? 'Quick switcher is only available in workflow or chat mode'
    : isWorkflowMode
      ? (query ? 'No matching workflows' : 'No workflow presets available')
      : (query ? 'No matching chat tabs' : 'No chat tabs available')
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
          {filteredItems.length === 0 ? (
            <div className="px-4 py-8 text-center text-muted-foreground text-sm">
              {emptyText}
            </div>
          ) : (
            filteredItems.map((item, index) => {
              const isSelected = index === selectedIndex
              const ItemIcon = item.type === 'workflow' ? Layers : MessageSquare
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
                  <ItemIcon className={`w-4 h-4 flex-shrink-0 ${item.isActive ? 'text-blue-500' : 'text-gray-400 dark:text-gray-500'}`} />
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
                    </div>
                    <div className="text-xs text-muted-foreground truncate">{item.subtitle}</div>
                  </div>
                  {/* Show "minimize current" hint when Shift is held on a non-active workflow item */}
                  {isSelected && shiftHeld && !item.isActive && item.type === 'workflow' && (
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

export default QuickSwitcher
