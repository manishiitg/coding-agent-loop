import { useState } from 'react'
import { AlertTriangle, ChevronDown, ChevronRight } from 'lucide-react'
import type { FriendlyError, RecoveryAction } from '../../services/connectionsApi'

const ACTION_LABELS: Record<RecoveryAction, string> = {
  reconnect: 'Reconnect',
  retry: 'Try again',
  connect: 'Connect',
  enter_token: 'Enter credential',
  open_advanced: 'Open Advanced',
  contact_admin: 'Contact administrator',
}

interface ErrorNoticeProps {
  error: FriendlyError
  /** Invoked when the user takes the suggested recovery action. */
  onAction?: (action: RecoveryAction) => void
  compact?: boolean
}

/**
 * Renders a failure as a recovery path. The raw transport error stays behind
 * an Advanced disclosure so power users keep their diagnostics.
 */
export default function ErrorNotice({ error, onAction, compact = false }: ErrorNoticeProps) {
  const [showRaw, setShowRaw] = useState(false)

  // contact_admin is informational — there is nothing for the user to click.
  const actionable = error.action && error.action !== 'contact_admin'

  return (
    <div
      className={`rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900/50 dark:bg-amber-900/20 ${
        compact ? 'p-2' : 'p-3'
      }`}
    >
      <div className="flex items-start gap-2">
        <AlertTriangle
          className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
          aria-hidden="true"
        />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-amber-900 dark:text-amber-200">
            {error.title}
          </p>
          {error.message && (
            <p className="mt-0.5 text-xs text-amber-800 dark:text-amber-300">
              {error.message}
            </p>
          )}

          <div className="mt-2 flex flex-wrap items-center gap-3">
            {actionable && onAction && (
              <button
                type="button"
                onClick={() => onAction(error.action as RecoveryAction)}
                className="rounded-md bg-amber-600 px-2.5 py-1 text-xs font-medium text-white transition-colors hover:bg-amber-700"
              >
                {ACTION_LABELS[error.action as RecoveryAction]}
              </button>
            )}

            {error.raw && (
              <button
                type="button"
                onClick={() => setShowRaw(v => !v)}
                className="flex items-center gap-1 text-xs text-amber-700 hover:underline dark:text-amber-400"
                aria-expanded={showRaw}
              >
                {showRaw ? (
                  <ChevronDown className="h-3 w-3" aria-hidden="true" />
                ) : (
                  <ChevronRight className="h-3 w-3" aria-hidden="true" />
                )}
                Advanced
              </button>
            )}
          </div>

          {showRaw && error.raw && (
            <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap break-all rounded bg-amber-100/70 p-2 font-mono text-[11px] text-amber-900 dark:bg-slate-900/60 dark:text-amber-200">
              {error.raw}
            </pre>
          )}
        </div>
      </div>
    </div>
  )
}
