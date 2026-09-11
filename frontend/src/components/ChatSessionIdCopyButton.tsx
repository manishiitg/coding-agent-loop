import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { copyToClipboard } from '../utils/textUtils'

type CopyState = 'idle' | 'copied' | 'failed'

interface ChatSessionIdCopyButtonProps {
  sessionId: string
  compact?: boolean
}

export function ChatSessionIdCopyButton({ sessionId, compact = false }: ChatSessionIdCopyButtonProps) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const resetTimer = useRef<number | null>(null)

  useEffect(() => () => {
    if (resetTimer.current !== null) window.clearTimeout(resetTimer.current)
  }, [])

  const handleCopy = async () => {
    const copied = await copyToClipboard(sessionId)
    setCopyState(copied ? 'copied' : 'failed')
    if (resetTimer.current !== null) window.clearTimeout(resetTimer.current)
    resetTimer.current = window.setTimeout(() => setCopyState('idle'), 2000)
  }

  const label = copyState === 'copied' ? 'Copied' : copyState === 'failed' ? 'Copy failed' : 'Copy ID'
  const accessibleLabel = copyState === 'copied'
    ? `Copied chat ID ${sessionId}`
    : copyState === 'failed'
      ? `Failed to copy chat ID ${sessionId}`
      : `Copy chat ID ${sessionId}`

  return (
    <button
      type="button"
      onClick={event => {
        event.stopPropagation()
        void handleCopy()
      }}
      className={`inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium transition-colors hover:border-primary/40 hover:text-foreground ${
        copyState === 'copied'
          ? 'text-emerald-600 dark:text-emerald-400'
          : copyState === 'failed'
            ? 'text-destructive'
            : 'text-muted-foreground'
      }`}
      title={`${label}: ${sessionId}`}
      aria-label={accessibleLabel}
    >
      {copyState === 'copied' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      {!compact && <span>{label}</span>}
    </button>
  )
}
