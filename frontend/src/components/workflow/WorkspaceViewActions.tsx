import { RefreshCw } from 'lucide-react'
import { AskAIButton } from './AskAIButton'

interface WorkspaceViewActionsProps {
  workspacePath: string | null
  message: string
  onRefresh: () => void | Promise<void>
  refreshing?: boolean
  refreshLabel?: string
}

/** The standard action pair shown in a right-side workspace view header. */
export function WorkspaceViewActions({
  workspacePath,
  message,
  onRefresh,
  refreshing = false,
  refreshLabel = 'Refresh view',
}: WorkspaceViewActionsProps) {
  return (
    <div className="flex shrink-0 items-center gap-2">
      <button
        type="button"
        onClick={() => { void onRefresh() }}
        disabled={refreshing}
        aria-label={refreshLabel}
        title={refreshLabel}
        className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary disabled:opacity-60"
      >
        <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
      </button>
      <AskAIButton workspacePath={workspacePath} message={message} iconOnly />
    </div>
  )
}
