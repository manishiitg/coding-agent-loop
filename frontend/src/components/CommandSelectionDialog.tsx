import React, { useState, useEffect, useRef } from 'react'
import { Terminal, Plus, Pencil, Trash2 } from 'lucide-react'
import type { ModeCategory } from '../stores/useModeStore'
import { getCommands, type CommandDefinition } from '../commands'
import { loadAndRegisterUserCommands } from '../commands'

interface CommandSelectionDialogProps {
  isOpen: boolean
  onClose: () => void
  onSelectCommand: (command: string) => void
  searchQuery: string
  position: { bottom: number; left: number }
  modeCategory?: ModeCategory
  onManageCommands?: () => void
  onEditCommand?: (command: CommandDefinition) => void
  onDeleteCommand?: (command: CommandDefinition) => void
}

export const CommandSelectionDialog: React.FC<CommandSelectionDialogProps> = ({
  isOpen,
  onClose,
  onSelectCommand,
  searchQuery,
  position,
  modeCategory,
  onManageCommands,
  onEditCommand,
  onDeleteCommand
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredCommands, setFilteredCommands] = useState<CommandDefinition[]>([])
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Load user commands when dialog opens
  useEffect(() => {
    if (isOpen) {
      loadAndRegisterUserCommands().catch(() => {})
    }
  }, [isOpen])

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      } else if (event.key === 'Enter') {
        event.preventDefault()
        if (filteredCommands.length > 0 && selectedIndex >= 0 && selectedIndex < filteredCommands.length) {
          onSelectCommand(filteredCommands[selectedIndex].command)
        }
      } else if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredCommands.length - 1))
      } else if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose, onSelectCommand, filteredCommands, selectedIndex])

  // Filter commands based on search query and current mode
  useEffect(() => {
    const allCommands = getCommands(modeCategory)

    if (!searchQuery.trim()) {
      setFilteredCommands(allCommands)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    const filtered = allCommands.filter(cmd =>
      cmd.command.toLowerCase().includes(query) ||
      cmd.description.toLowerCase().includes(query)
    )

    // Sort by relevance: exact match first, then partial match
    filtered.sort((a, b) => {
      const aExact = a.command.toLowerCase() === query
      const bExact = b.command.toLowerCase() === query
      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1

      const aStarts = a.command.toLowerCase().startsWith(query)
      const bStarts = b.command.toLowerCase().startsWith(query)
      if (aStarts && !bStarts) return -1
      if (!aStarts && bStarts) return 1

      return a.command.localeCompare(b.command)
    })

    setFilteredCommands(filtered)
    setSelectedIndex(0) // Reset selection when filtering
  }, [searchQuery, modeCategory])

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
          <Terminal className="w-4 h-4 text-muted-foreground" />
          <span className="text-sm font-medium">Commands</span>
        </div>
      </div>

      {/* Command List */}
      <div
        ref={listRef}
        className="overflow-y-auto max-h-96"
      >
        {filteredCommands.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {searchQuery ? 'No commands found' : 'No commands available'}
          </div>
        ) : (
          filteredCommands.map((cmd, index) => (
            <div
              key={cmd.command}
              className={`group px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onClick={() => onSelectCommand(cmd.command)}
            >
              <div className="text-muted-foreground">
                {cmd.icon}
              </div>
              <div className="flex-1 min-w-0">
                <div className="font-medium">/{cmd.command}</div>
                <div className="text-xs text-muted-foreground truncate">
                  {cmd.description}
                </div>
              </div>
              {cmd.source === 'user' && (
                <div className="hidden group-hover:flex items-center gap-1">
                  {onEditCommand && (
                    <button
                      className="p-1 hover:bg-gray-200 dark:hover:bg-gray-600 rounded"
                      onClick={(e) => { e.stopPropagation(); onEditCommand(cmd) }}
                      title="Edit command"
                    >
                      <Pencil className="w-3 h-3" />
                    </button>
                  )}
                  {onDeleteCommand && (
                    <button
                      className="p-1 hover:bg-red-100 dark:hover:bg-red-900/30 rounded text-red-500"
                      onClick={(e) => { e.stopPropagation(); onDeleteCommand(cmd) }}
                      title="Delete command"
                    >
                      <Trash2 className="w-3 h-3" />
                    </button>
                  )}
                </div>
              )}
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>↑↓ to navigate</span>
          <div className="flex items-center gap-2">
            <span>Enter to select</span>
            {onManageCommands && (
              <button
                className="flex items-center gap-1 hover:text-primary transition-colors"
                onClick={(e) => { e.stopPropagation(); onManageCommands() }}
                title="Create custom command"
              >
                <Plus className="w-3 h-3" />
                <span>New</span>
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default CommandSelectionDialog
