import React, { useState, useEffect, useRef } from 'react'
import { Terminal, FileText, Lightbulb, Download, Server, Cpu, History, GitBranch, ClipboardList } from 'lucide-react'

interface Command {
  command: string
  description: string
  icon: React.ReactNode
}

const AVAILABLE_COMMANDS: Command[] = [
  {
    command: 'summarize',
    description: 'Summarize conversation history',
    icon: <FileText className="w-4 h-4" />
  },
  {
    command: 'build-skill',
    description: 'Build a new skill using the skill-creator',
    icon: <Lightbulb className="w-4 h-4" />
  },
  {
    command: 'add-skill',
    description: 'Import a skill from GitHub',
    icon: <Download className="w-4 h-4" />
  },
  {
    command: 'mcp',
    description: 'View MCP server details and tools',
    icon: <Server className="w-4 h-4" />
  },
  {
    command: 'mcp-add',
    description: 'Add or edit MCP server configuration',
    icon: <Server className="w-4 h-4" />
  },
  {
    command: 'models',
    description: 'Open LLM model configuration',
    icon: <Cpu className="w-4 h-4" />
  },
  {
    command: 'resume',
    description: 'Resume a previous conversation',
    icon: <History className="w-4 h-4" />
  },
  {
    command: 'spawn',
    description: 'Enable simple sub-agent delegation (fire-and-forget)',
    icon: <GitBranch className="w-4 h-4" />
  },
  {
    command: 'plan',
    description: 'Enable plan-driven delegation with multi-LLM tiers',
    icon: <ClipboardList className="w-4 h-4" />
  },
  {
    command: 'nospawn',
    description: 'Disable all sub-agent delegation',
    icon: <GitBranch className="w-4 h-4" />
  }
]

interface CommandSelectionDialogProps {
  isOpen: boolean
  onClose: () => void
  onSelectCommand: (command: string) => void
  searchQuery: string
  position: { bottom: number; left: number }
}

export const CommandSelectionDialog: React.FC<CommandSelectionDialogProps> = ({
  isOpen,
  onClose,
  onSelectCommand,
  searchQuery,
  position
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredCommands, setFilteredCommands] = useState<Command[]>([])
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

  // Filter commands based on search query
  useEffect(() => {
    if (!searchQuery.trim()) {
      setFilteredCommands(AVAILABLE_COMMANDS)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    const filtered = AVAILABLE_COMMANDS.filter(cmd => 
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
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
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

export default CommandSelectionDialog

