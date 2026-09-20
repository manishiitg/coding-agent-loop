import { RefreshCw, type LucideIcon } from 'lucide-react'

interface WorkspaceViewIconButtonProps {
  /** Accessible label and tooltip. */
  label: string
  onClick: () => void | Promise<void>
  disabled?: boolean
  /** Icon rendered at h-3.5. Defaults to the refresh arrow. */
  icon?: LucideIcon
  /** Spins the icon (loading state). */
  spinning?: boolean
  /** Rare extra classes (e.g. cursor-wait). The standard look is built in. */
  className?: string
}

/**
 * The one action-icon button for right-pane headers: fixed h-8 w-8,
 * Ask-AI-matching border and hover. Every header action icon (refresh,
 * per-tab actions) uses this so the right side looks identical everywhere.
 */
export function WorkspaceViewIconButton({
  label,
  onClick,
  disabled = false,
  icon: Icon = RefreshCw,
  spinning = false,
  className = '',
}: WorkspaceViewIconButtonProps) {
  return (
    <button
      type="button"
      onClick={() => { void onClick() }}
      disabled={disabled}
      aria-label={label}
      title={label}
      className={`inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary disabled:opacity-60${className ? ` ${className}` : ''}`}
    >
      <Icon className={`h-3.5 w-3.5${spinning ? ' animate-spin' : ''}`} />
    </button>
  )
}
