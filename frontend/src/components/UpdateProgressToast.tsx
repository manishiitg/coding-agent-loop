import { useEffect, useState } from 'react'
import { Download, RotateCw, X } from 'lucide-react'

// Surfaces the desktop app's background update download (driven by the Electron
// main process via electronAPI.onUpdateProgress). Shows a bottom-corner card
// with a progress bar while downloading, then a "Restart & Install" prompt when
// the dmg is ready. No-op in the web build (electronAPI absent).

interface UpdateProgress {
  status: 'downloading' | 'ready' | 'error'
  version?: string
  percent?: number
  transferred?: number
  total?: number
  message?: string
}

function formatMB(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  return `${(bytes / (1024 * 1024)).toFixed(0)} MB`
}

export function UpdateProgressToast() {
  const [progress, setProgress] = useState<UpdateProgress | null>(null)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    const api = (window as any).electronAPI
    if (!api?.onUpdateProgress) return
    const unsubscribe = api.onUpdateProgress((p: UpdateProgress) => {
      setProgress(p)
      setDismissed(false) // a new event (e.g. ready/error) re-surfaces the card
    })
    return () => { try { unsubscribe?.() } catch { /* noop */ } }
  }, [])

  if (!progress || dismissed) return null

  const pct = Math.round(Math.min(1, Math.max(0, progress.percent ?? 0)) * 100)
  const restart = () => { try { (window as any).electronAPI?.restartToInstall?.() } catch { /* noop */ } }

  return (
    <div className="fixed bottom-4 right-4 z-[9999] w-80 rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900">
      <div className="flex items-start gap-3 p-3">
        <div className="mt-0.5 shrink-0 text-blue-600 dark:text-blue-400">
          {progress.status === 'ready' ? <RotateCw className="h-5 w-5" /> : <Download className="h-5 w-5" />}
        </div>
        <div className="min-w-0 flex-1">
          {progress.status === 'downloading' && (
            <>
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">
                Downloading update{progress.version ? ` v${progress.version}` : ''}…
              </div>
              <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
                <div className="h-full rounded-full bg-blue-600 transition-[width] duration-200 dark:bg-blue-500" style={{ width: `${pct}%` }} />
              </div>
              <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {pct}%{progress.total ? ` · ${formatMB(progress.transferred)} / ${formatMB(progress.total)}` : ''} · you can keep working
              </div>
            </>
          )}

          {progress.status === 'ready' && (
            <>
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">
                Update{progress.version ? ` v${progress.version}` : ''} ready to install
              </div>
              <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                Installing takes a few seconds and relaunches the app.
              </div>
              <div className="mt-2 flex gap-2">
                <button
                  onClick={restart}
                  className="rounded bg-blue-600 px-2.5 py-1 text-xs font-medium text-white transition-colors hover:bg-blue-700"
                >
                  Restart &amp; Install
                </button>
                <button
                  onClick={() => setDismissed(true)}
                  className="rounded px-2.5 py-1 text-xs text-gray-600 transition-colors hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
                >
                  Later
                </button>
              </div>
            </>
          )}

          {progress.status === 'error' && (
            <>
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Update download failed</div>
              <div className="mt-1 break-words text-xs text-gray-500 dark:text-gray-400">
                {progress.message || 'Please try Check for Updates again.'}
              </div>
            </>
          )}
        </div>
        <button
          onClick={() => setDismissed(true)}
          className="shrink-0 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-gray-200"
          aria-label="Dismiss"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  )
}
