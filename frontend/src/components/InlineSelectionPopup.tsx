import React, { useState, useEffect, useRef } from 'react'
import { Check, Loader2 } from 'lucide-react'

export interface InlineSelectionItem {
  id: string
  name: string
  description?: string
  isSelected: boolean
}

interface InlineSelectionPopupProps {
  isOpen: boolean
  onClose: () => void
  onToggleItem: (id: string) => void
  items: InlineSelectionItem[]
  searchQuery: string
  position: { bottom: number; left: number }
  title: string
  icon: React.ReactNode
  emptyMessage: string
  isLoading?: boolean
}

export const InlineSelectionPopup: React.FC<InlineSelectionPopupProps> = ({
  isOpen,
  onClose,
  onToggleItem,
  items,
  searchQuery,
  position,
  title,
  icon,
  emptyMessage,
  isLoading = false
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [filteredItems, setFilteredItems] = useState<InlineSelectionItem[]>([])
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      } else if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        if (filteredItems.length > 0 && selectedIndex >= 0 && selectedIndex < filteredItems.length) {
          onToggleItem(filteredItems[selectedIndex].id)
        }
      } else if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredItems.length - 1))
      } else if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose, onToggleItem, filteredItems, selectedIndex])

  // Filter items based on search query
  useEffect(() => {
    if (!searchQuery.trim()) {
      setFilteredItems(items)
      setSelectedIndex(0)
      return
    }

    const query = searchQuery.toLowerCase().trim()
    const filtered = items.filter(item =>
      item.name.toLowerCase().includes(query) ||
      item.id.toLowerCase().includes(query) ||
      (item.description && item.description.toLowerCase().includes(query))
    )

    // Sort by relevance
    filtered.sort((a, b) => {
      const aExact = a.name.toLowerCase() === query
      const bExact = b.name.toLowerCase() === query
      if (aExact && !bExact) return -1
      if (!aExact && bExact) return 1

      const aStarts = a.name.toLowerCase().startsWith(query)
      const bStarts = b.name.toLowerCase().startsWith(query)
      if (aStarts && !bStarts) return -1
      if (!aStarts && bStarts) return 1

      return a.name.localeCompare(b.name)
    })

    setFilteredItems(filtered)
    setSelectedIndex(0)
  }, [searchQuery, items])

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
          {icon}
          <span className="text-sm font-medium">{title}</span>
        </div>
      </div>

      {/* Item List */}
      <div
        ref={listRef}
        className="overflow-y-auto max-h-96"
      >
        {isLoading ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm flex items-center justify-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" />
            Loading...
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {searchQuery ? `No ${title.toLowerCase()} found` : emptyMessage}
          </div>
        ) : (
          filteredItems.map((item, index) => (
            <div
              key={item.id}
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onClick={() => onToggleItem(item.id)}
            >
              <div className="flex-1 min-w-0">
                <div className="font-medium">{item.name}</div>
                {item.description && (
                  <div className="text-xs text-muted-foreground truncate">
                    {item.description}
                  </div>
                )}
              </div>
              {item.isSelected && (
                <Check className="w-4 h-4 text-green-500 flex-shrink-0" />
              )}
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        <div className="flex items-center justify-between">
          <span>↑↓ to navigate</span>
          <span>Enter/Space to toggle • Esc to close</span>
        </div>
      </div>
    </div>
  )
}

export default InlineSelectionPopup
