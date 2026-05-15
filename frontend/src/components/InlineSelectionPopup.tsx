import React, { useState, useEffect, useRef, useMemo } from 'react'
import { Check, Loader2, Search } from 'lucide-react'

const DBG = '[skill-popup]'

export interface InlineSelectionItem {
  id: string
  name: string
  description?: string
  isSelected: boolean
  leadingIcon?: React.ReactNode
  badge?: string
}

export interface InlineSelectionFilterTab {
  id: string
  label: string
  count?: number
  icon?: React.ReactNode
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
  filterTabs?: InlineSelectionFilterTab[]
  activeFilterId?: string
  onFilterChange?: (id: string) => void
  footerSummary?: string
  footerActions?: React.ReactNode
  searchPlaceholder?: string
  widthClassName?: string
  enterHint?: string
}

export const InlineSelectionPopup: React.FC<InlineSelectionPopupProps> = ({
  isOpen,
  onClose,
  onToggleItem,
  items,
  searchQuery: externalSearchQuery,
  position,
  title,
  icon,
  emptyMessage,
  isLoading = false,
  filterTabs,
  activeFilterId,
  onFilterChange,
  footerSummary,
  footerActions,
  searchPlaceholder,
  widthClassName = 'min-w-[300px] max-w-[400px]',
  enterHint = 'Enter to toggle'
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [localQuery, setLocalQuery] = useState('')
  const searchInputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Use refs so keyboard handler always has fresh values without re-registering
  const selectedIndexRef = useRef(selectedIndex)
  const onCloseRef = useRef(onClose)
  const onToggleItemRef = useRef(onToggleItem)

  useEffect(() => { selectedIndexRef.current = selectedIndex }, [selectedIndex])
  useEffect(() => { onCloseRef.current = onClose }, [onClose])
  useEffect(() => { onToggleItemRef.current = onToggleItem }, [onToggleItem])

  // Sync external search query (from typing after ! in textarea) into local input
  useEffect(() => {
    // console.log(`${DBG} externalSearchQuery changed:`, externalSearchQuery, 'isOpen:', isOpen)
    if (isOpen) {
      setLocalQuery(externalSearchQuery)
    }
  }, [externalSearchQuery, isOpen])

  // Auto-focus search input when popup opens; reset on close
  useEffect(() => {
    // console.log(`${DBG} isOpen changed:`, isOpen, 'items count:', items.length)
    if (isOpen) {
      setSelectedIndex(0)
      setTimeout(() => {
        const focused = searchInputRef.current?.focus()
        // console.log(`${DBG} focused input:`, !!searchInputRef.current)
      }, 50)
    } else {
      setLocalQuery('')
      setSelectedIndex(0)
    }
  }, [isOpen])

  // Log items changes
  useEffect(() => {
    // console.log(`${DBG} items updated, count:`, items.length, items.map(i => i.name))
  }, [items])

  // Filter items synchronously (useMemo, no render-cycle delay)
  const filteredItems = useMemo(() => {
    // console.log(`${DBG} filtering — localQuery: "${localQuery}", items:`, items.length)
    if (!localQuery.trim()) return items

    const query = localQuery.toLowerCase().trim()
    const filtered = items.filter(item =>
      item.name.toLowerCase().includes(query) ||
      item.id.toLowerCase().includes(query) ||
      (item.description && item.description.toLowerCase().includes(query))
    )

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

    // console.log(`${DBG} filtered result:`, filtered.length, filtered.map(i => i.name))
    return filtered
  }, [localQuery, items])

  // Keep a ref to filteredItems for use in keyboard handler
  const filteredItemsRef = useRef(filteredItems)
  useEffect(() => { filteredItemsRef.current = filteredItems }, [filteredItems])

  // Reset selected index when results change
  useEffect(() => {
    setSelectedIndex(0)
  }, [localQuery, items])

  // Scroll selected item into view
  useEffect(() => {
    if (listRef.current && selectedIndex >= 0) {
      const el = listRef.current.children[selectedIndex] as HTMLElement
      el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }, [selectedIndex])

  // Single stable document-level keydown listener using refs (no stale closures)
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (e: KeyboardEvent) => {
      const items = filteredItemsRef.current
      const idx = selectedIndexRef.current
      // console.log(`${DBG} keydown: "${e.key}", filteredItems: ${items.length}, selectedIndex: ${idx}`)

      if (e.key === 'Escape') {
        e.preventDefault()
        onCloseRef.current()
      } else if (e.key === 'Enter') {
        e.preventDefault()
        // console.log(`${DBG} Enter — toggling:`, items[idx]?.id)
        if (items.length > 0 && idx >= 0 && idx < items.length) {
          onToggleItemRef.current(items[idx].id)
        }
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, filteredItemsRef.current.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, 0))
      }
    }
    // console.log(`${DBG} registered keydown listener`)

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen])

  if (!isOpen) return null

  return (
    <div
      className={`fixed z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg ${widthClassName}`}
      style={{ bottom: `${position.bottom}px`, left: `${position.left}px` }}
    >
      {/* Header */}
      <div className="px-3 py-2 border-b border-border bg-secondary">
        <div className="flex items-center gap-2">
          {icon}
          <span className="text-sm font-medium">{title}</span>
        </div>
      </div>

      {/* Search input */}
      <div className="px-3 py-2 border-b border-border">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
          <input
            ref={searchInputRef}
            type="text"
            placeholder={searchPlaceholder || `Search ${title.toLowerCase()}...`}
            value={localQuery}
            onChange={e => setLocalQuery(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') {
                e.preventDefault()
                e.stopPropagation()
                const items = filteredItemsRef.current
                const idx = selectedIndexRef.current
                // console.log(`${DBG} input Enter — toggling:`, items[idx]?.id)
                if (items.length > 0 && idx >= 0 && idx < items.length) {
                  onToggleItemRef.current(items[idx].id)
                }
              } else if (e.key === 'ArrowDown') {
                e.preventDefault()
                e.stopPropagation()
                setSelectedIndex(prev => Math.min(prev + 1, filteredItemsRef.current.length - 1))
              } else if (e.key === 'ArrowUp') {
                e.preventDefault()
                e.stopPropagation()
                setSelectedIndex(prev => Math.max(prev - 1, 0))
              } else if (e.key === 'Escape') {
                e.preventDefault()
                e.stopPropagation()
                onCloseRef.current()
              }
            }}
            className="w-full pl-7 pr-2 py-1.5 text-xs rounded-md border border-border bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
      </div>

      {filterTabs && filterTabs.length > 0 && activeFilterId && onFilterChange && (
        <div className="px-3 py-2 border-b border-border bg-background">
          <div className="inline-flex max-w-full rounded-lg border border-border bg-secondary/60 p-0.5 shadow-sm">
            {filterTabs.map(tab => {
              const active = tab.id === activeFilterId
              return (
                <button
                  key={tab.id}
                  type="button"
                  data-testid={`inline-selection-filter-${tab.id}`}
                  onMouseDown={e => e.preventDefault()}
                  onClick={() => onFilterChange(tab.id)}
                  className={`flex min-w-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors ${
                    active
                      ? 'bg-background text-foreground shadow-sm ring-1 ring-border'
                      : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'
                  }`}
                >
                  {tab.icon && (
                    <span className={active ? 'text-primary' : 'text-muted-foreground'}>
                      {tab.icon}
                    </span>
                  )}
                  <span className="truncate">{tab.label}</span>
                  {typeof tab.count === 'number' && (
                    <span className={`rounded-full px-1.5 py-0.5 text-[10px] leading-none ${
                      active
                        ? 'bg-primary/10 text-primary'
                        : 'bg-background/80 text-muted-foreground'
                    }`}>
                      {tab.count}
                    </span>
                  )}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Item list */}
      <div ref={listRef} className="overflow-y-auto max-h-64">
        {isLoading ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm flex items-center justify-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" />
            Loading...
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="px-3 py-4 text-center text-muted-foreground text-sm">
            {localQuery ? `No ${title.toLowerCase()} found` : emptyMessage}
          </div>
        ) : (
          filteredItems.map((item, index) => (
            <div
              key={`${item.id}-${index}`}
              data-testid={`inline-selection-item-${item.id}`}
              className={`px-3 py-2 cursor-pointer flex items-center gap-2 text-sm transition-colors ${
                index === selectedIndex
                  ? 'bg-primary/10 text-primary border-l-2 border-primary'
                  : 'hover:bg-secondary'
              }`}
              onMouseDown={e => { e.preventDefault(); onToggleItem(item.id) }}
            >
              {item.leadingIcon && (
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-secondary text-muted-foreground">
                  {item.leadingIcon}
                </div>
              )}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="min-w-0 truncate font-medium">{item.name}</span>
                  {item.badge && (
                    <span className="shrink-0 text-[10px] px-1 py-0.5 rounded bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 font-medium leading-none">
                      {item.badge}
                    </span>
                  )}
                  {item.id.startsWith('custom/') && (
                    <span className="text-[10px] px-1 py-0.5 rounded bg-purple-100 dark:bg-purple-900/40 text-purple-600 dark:text-purple-400 font-medium leading-none">custom</span>
                  )}
                </div>
                {item.description && (
                  <div className="text-xs text-muted-foreground truncate">{item.description}</div>
                )}
              </div>
              {item.isSelected && <Check className="w-4 h-4 text-green-500 flex-shrink-0" />}
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-2 border-t border-border bg-secondary text-xs text-muted-foreground">
        {(footerSummary || footerActions) && (
          <div className="mb-1 flex items-center justify-between gap-2">
            {footerSummary && (
              <div className="min-w-0 truncate" data-testid="inline-selection-footer-summary">
                {footerSummary}
              </div>
            )}
            {footerActions && (
              <div className="shrink-0">
                {footerActions}
              </div>
            )}
          </div>
        )}
        <div className="flex items-center justify-between">
          <span>↑↓ navigate</span>
          <span>{enterHint} • Esc to close</span>
        </div>
      </div>
    </div>
  )
}

export default InlineSelectionPopup
