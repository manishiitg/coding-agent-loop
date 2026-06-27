import React, { useState } from 'react'
import { AlertTriangle, ListTree, Terminal } from 'lucide-react'
import ModalPortal from './ui/ModalPortal'

const TREE_VIEW_ALPHA_WARNING_SUPPRESSED_KEY = 'runloop.treeViewAlphaWarningSuppressed'

export function shouldShowTreeViewAlphaWarning(): boolean {
  try {
    return window.localStorage.getItem(TREE_VIEW_ALPHA_WARNING_SUPPRESSED_KEY) !== 'true'
  } catch {
    return true
  }
}

interface TreeViewAlphaDialogProps {
  isOpen: boolean
  onContinue: () => void
  onCancel: () => void
}

export const TreeViewAlphaDialog: React.FC<TreeViewAlphaDialogProps> = ({
  isOpen,
  onContinue,
  onCancel,
}) => {
  const [dontAskAgain, setDontAskAgain] = useState(false)

  if (!isOpen) return null

  const handleContinue = () => {
    if (dontAskAgain) {
      try {
        window.localStorage.setItem(TREE_VIEW_ALPHA_WARNING_SUPPRESSED_KEY, 'true')
      } catch {
        // Ignore storage failures; the user can still continue.
      }
    }
    onContinue()
  }

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-black/50 px-4">
        <div className="w-full max-w-md rounded-lg border border-amber-200 bg-white shadow-xl dark:border-amber-900/60 dark:bg-gray-900">
          <div className="flex items-start gap-3 border-b border-gray-200 p-5 dark:border-gray-800">
            <div className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-amber-50 text-amber-600 dark:bg-amber-950/40 dark:text-amber-300">
              <AlertTriangle className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold text-gray-950 dark:text-gray-100">
                Tree view is in alpha
              </h2>
              <p className="mt-1 text-sm leading-5 text-gray-600 dark:text-gray-300">
                Terminal view is the recommended view for live automation progress. Tree view is still experimental and can show incomplete or confusing status.
              </p>
            </div>
          </div>

          <div className="space-y-4 p-5">
            <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 rounded-md border border-gray-200 bg-gray-50 p-3 text-sm dark:border-gray-800 dark:bg-gray-950/40">
              <Terminal className="mt-0.5 h-4 w-4 text-blue-600 dark:text-blue-300" />
              <div>
                <div className="font-medium text-gray-900 dark:text-gray-100">Recommended</div>
                <div className="text-gray-600 dark:text-gray-400">Use Terminal view for the most accurate live state.</div>
              </div>
              <ListTree className="mt-0.5 h-4 w-4 text-amber-600 dark:text-amber-300" />
              <div>
                <div className="font-medium text-gray-900 dark:text-gray-100">Alpha</div>
                <div className="text-gray-600 dark:text-gray-400">Continue to Tree view if you want to inspect grouped events.</div>
              </div>
            </div>

            <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                type="checkbox"
                checked={dontAskAgain}
                onChange={(event) => setDontAskAgain(event.target.checked)}
                className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-700"
              />
              Do not ask again
            </label>
          </div>

          <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-4 dark:border-gray-800">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-md px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
            >
              Stay in Terminal
            </button>
            <button
              type="button"
              onClick={handleContinue}
              className="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"
            >
              Continue to Tree
            </button>
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
