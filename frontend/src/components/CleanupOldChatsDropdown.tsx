import React, { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, Loader2, Trash2 } from 'lucide-react'

export const CHAT_HISTORY_CLEANUP_AGE_OPTIONS = [14, 7, 3] as const
export type ChatHistoryCleanupAgeDays = typeof CHAT_HISTORY_CLEANUP_AGE_OPTIONS[number]

type CleanupCounts = Record<ChatHistoryCleanupAgeDays, number>

interface CleanupOldChatsDropdownProps {
  counts: CleanupCounts
  isLoading: boolean
  onSelect: (days: ChatHistoryCleanupAgeDays) => void
  label?: string
  className?: string
}

export const CleanupOldChatsDropdown: React.FC<CleanupOldChatsDropdownProps> = ({
  counts,
  isLoading,
  onSelect,
  label = 'Delete old',
  className = '',
}) => {
  const [isOpen, setIsOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const maxCount = useMemo(
    () => Math.max(...CHAT_HISTORY_CLEANUP_AGE_OPTIONS.map(days => counts[days] || 0)),
    [counts]
  )

  useEffect(() => {
    if (!isOpen) return

    const handleMouseDown = (event: MouseEvent) => {
      if (rootRef.current?.contains(event.target as Node)) return
      setIsOpen(false)
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setIsOpen(false)
    }

    document.addEventListener('mousedown', handleMouseDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handleMouseDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen])

  if (maxCount === 0) return null

  const handleSelect = (days: ChatHistoryCleanupAgeDays) => {
    if (isLoading || counts[days] === 0) return
    setIsOpen(false)
    onSelect(days)
  }

  return (
    <div ref={rootRef} className="relative inline-flex shrink-0">
      <button
        type="button"
        onMouseDown={event => event.preventDefault()}
        onClick={() => setIsOpen(current => !current)}
        disabled={isLoading}
        className={`inline-flex h-7 shrink-0 items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        title="Delete old chats"
      >
        {isLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
        <span>{label}</span>
        <ChevronDown className="h-3.5 w-3.5 opacity-70" />
      </button>

      {isOpen && (
        <div
          role="menu"
          className="absolute right-0 top-full z-[60] mt-1 min-w-44 rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg"
        >
          {CHAT_HISTORY_CLEANUP_AGE_OPTIONS.map(days => {
            const count = counts[days] || 0
            return (
              <button
                key={days}
                type="button"
                role="menuitem"
                disabled={count === 0 || isLoading}
                onMouseDown={event => event.preventDefault()}
                onClick={() => handleSelect(days)}
                className="flex w-full items-center justify-between gap-3 rounded px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent hover:text-accent-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                <span>Delete &gt;{days}d</span>
                <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
                  {count}
                </span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

export default CleanupOldChatsDropdown
