import React, { useState, useEffect, useRef, useMemo } from 'react'
import { Layers, Search } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import type { CustomPreset } from '../types/preset'

interface WorkflowItem {
  presetId: string
  label: string
  workspacePath: string
}

interface WorkflowSelectionDialogProps {
  isOpen: boolean
  onClose: () => void
  onSelectWorkflow: (workflow: WorkflowItem) => void
  searchQuery: string
  position: { bottom: number; left: number }
}

export const WorkflowSelectionDialog: React.FC<WorkflowSelectionDialogProps> = ({
  isOpen,
  onClose,
  onSelectWorkflow,
  searchQuery: externalSearchQuery,
  position
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [localQuery, setLocalQuery] = useState('')
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Refs so keyboard handler always has fresh values
  const selectedIndexRef = useRef(selectedIndex)
  const onCloseRef = useRef(onClose)
  const onSelectWorkflowRef = useRef(onSelectWorkflow)
  useEffect(() => { selectedIndexRef.current = selectedIndex }, [selectedIndex])
  useEffect(() => { onCloseRef.current = onClose }, [onClose])
  useEffect(() => { onSelectWorkflowRef.current = onSelectWorkflow }, [onSelectWorkflow])

  // Sync external search query into local input
  useEffect(() => {
    if (isOpen) setLocalQuery(externalSearchQuery)
  }, [externalSearchQuery, isOpen])

  // Auto-focus on open, reset on close
  useEffect(() => {
    if (isOpen) {
      setSelectedIndex(0)
      setTimeout(() => searchInputRef.current?.focus(), 50)
    } else {
      setLocalQuery('')
      setSelectedIndex(0)
    }
  }, [isOpen])

  // Build all workflow items from preset store
  const allWorkflows = useMemo<WorkflowItem[]>(() => {
    const presetStore = useGlobalPresetStore.getState()
    // Use manifest-based workflow presets only
    const allPresets = presetStore.workflowPresets
    return allPresets
      .filter(p => p.agentMode === 'workflow' && p.selectedFolder?.filepath)
      .map(p => ({
        presetId: p.id,
        label: p.label,
        workspacePath: p.selectedFolder!.filepath
      }))
  }, [isOpen]) // re-compute when popup opens to pick up new presets

  // Filter synchronously
  const filteredWorkflows = useMemo<WorkflowItem[]>(() => {
    if (!localQuery.trim()) return allWorkflows

    const query = localQuery.toLowerCase().trim()
    const filtered = allWorkflows.filter(w =>
      w.label.toLowerCase().includes(query) ||
      w.workspacePath.toLowerCase().includes(query)
    )

    filtered.sort((a, b) => {
      const aExact = a.label.toLowerCase() === query
      const bExact = b.label.toLowerCase() === query
      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1
      const aStarts = a.label.toLowerCase().startsWith(query)
      const bStarts = b.label.toLowerCase().startsWith(query)
      if (aStarts && !bStarts) return -1
      if (!aStarts && bStarts) return 1
      return a.label.localeCompare(b.label)
    })

    return filtered
  }, [localQuery, allWorkflows])

  const filteredWorkflowsRef = useRef(filteredWorkflows)
  useEffect(() => { filteredWorkflowsRef.current = filteredWorkflows }, [filteredWorkflows])

  // Reset selected index when results change
  useEffect(() => { setSelectedIndex(0) }, [localQuery, allWorkflows])

  // Scroll selected item into view
  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const el = listRef.current.children[selectedIndex] as HTMLElement
      el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }, [selectedIndex])

  // Stable document-level keydown listener using refs
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCloseRef.current()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredWorkflowsRef.current.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen])

  if (!isOpen) return null

  const handleEnter = () => {
    const items = filteredWorkflowsRef.current
    const idx = selectedIndexRef.current
    if (items.length > 0 && idx >= 0 && idx < items.length) {
      onSelectWorkflowRef.current(items[idx])
    }
  }

  return (
    <div
      className="fixed z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg min-w-[300px] max-w-[400px]"
      style={{ bottom: `${position.bottom}px`, left: `${position.left}px` }}
    >
      {/* Header */}
      <div className="px-3 py-2 border-b border-border bg-secondary">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-muted-foreground" />
          <span className="text-sm font-medium">Automations</span>
        </div>
      </div>

      {/* Search input */}
      <div className="px-3 py-2 border-b border-border">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
          <input
            ref={searchInputRef}
            type="text"
            placeholder="Search automations..."
            value={localQuery}
            onChange={e => setLocalQuery(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); handleEnter() }
              else if (e.key === 'ArrowDown') { e.preventDefault(); setSelectedIndex(prev => Math.min(prev + 1, filteredWorkflowsRef.current.length - 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setSelectedIndex(prev => Math.max(prev - 1, 0)) }
              else if (e.key === 'Escape') { e.preventDefault(); onCloseRef.current() }
            }}
            className="w-full pl-7 pr-2 py-1.5 text-xs rounded-md border border-border bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
      </div>

      {/* Workflow list */}
      <div ref={listRef} className="overflow-y-auto max-h-64">
        {filteredWorkflows.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {localQuery ? 'No automations found' : 'No automation presets available'}
          </div>
        ) : (
          filteredWorkflows.map((workflow, index) => (
            <div
              key={`${workflow.presetId}-${index}`}
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onMouseDown={e => { e.preventDefault(); onSelectWorkflow(workflow) }}
            >
              <div className="text-muted-foreground">
                <Layers className="w-4 h-4" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="font-medium">{workflow.label}</div>
                <div className="text-xs text-muted-foreground truncate">{workflow.workspacePath}</div>
              </div>
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>↑↓ navigate</span>
          <span>Enter to select • Esc to close</span>
        </div>
      </div>
    </div>
  )
}

export default WorkflowSelectionDialog
