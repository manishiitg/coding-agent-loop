import React, { useState, useEffect, useRef } from 'react'
import { Layers } from 'lucide-react'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'

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
  searchQuery,
  position
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredWorkflows, setFilteredWorkflows] = useState<WorkflowItem[]>([])
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      } else if (event.key === 'Enter') {
        event.preventDefault()
        if (filteredWorkflows.length > 0 && selectedIndex >= 0 && selectedIndex < filteredWorkflows.length) {
          onSelectWorkflow(filteredWorkflows[selectedIndex])
        }
      } else if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredWorkflows.length - 1))
      } else if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose, onSelectWorkflow, filteredWorkflows, selectedIndex])

  // Build workflow items from presets and filter
  useEffect(() => {
    const presetStore = useGlobalPresetStore.getState()
    const allPresets: (CustomPreset | PredefinedPreset)[] = [
      ...presetStore.customPresets,
      ...presetStore.predefinedPresets
    ]

    // Filter to workflow presets that have a selectedFolder
    const workflowItems: WorkflowItem[] = allPresets
      .filter(p => p.agentMode === 'workflow' && p.selectedFolder?.filepath)
      .map(p => ({
        presetId: p.id,
        label: p.label,
        workspacePath: p.selectedFolder!.filepath
      }))

    if (!searchQuery.trim()) {
      setFilteredWorkflows(workflowItems)
      setSelectedIndex(0)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    const filtered = workflowItems.filter(w =>
      w.label.toLowerCase().includes(query) ||
      w.workspacePath.toLowerCase().includes(query)
    )

    // Sort by relevance
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

    setFilteredWorkflows(filtered)
    setSelectedIndex(0)
  }, [searchQuery])

  // Scroll selected item into view
  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const selectedElement = listRef.current.children[selectedIndex] as HTMLElement
      if (selectedElement) {
        selectedElement.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
      }
    }
  }, [selectedIndex])

  if (!isOpen) return null

  return (
    <div
      ref={dialogRef}
      className="fixed z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg min-w-[300px] max-w-[400px]"
      style={{
        bottom: `${position.bottom}px`,
        left: `${position.left}px`
      }}
    >
      {/* Header */}
      <div className="px-3 py-2 border-b border-border bg-secondary">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-muted-foreground" />
          <span className="text-sm font-medium">Workflows</span>
        </div>
      </div>

      {/* Workflow List */}
      <div
        ref={listRef}
        className="overflow-y-auto max-h-96"
      >
        {filteredWorkflows.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {searchQuery ? 'No workflows found' : 'No workflow presets available'}
          </div>
        ) : (
          filteredWorkflows.map((workflow, index) => (
            <div
              key={workflow.presetId}
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onClick={() => onSelectWorkflow(workflow)}
            >
              <div className="text-muted-foreground">
                <Layers className="w-4 h-4" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="font-medium">{workflow.label}</div>
                <div className="text-xs text-muted-foreground truncate">
                  {workflow.workspacePath}
                </div>
              </div>
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>↑↓ to navigate</span>
          <span>Enter to select • Esc to close</span>
        </div>
      </div>
    </div>
  )
}

export default WorkflowSelectionDialog
