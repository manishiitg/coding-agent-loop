import React, { useEffect, useState } from 'react'
import { Loader2, RotateCcw, FlaskConical, GitBranch } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'
import type { Checkpoint, Experiment } from '../../api/codePrototype'
import { ExperimentModal } from './ExperimentModal'

interface Props {
  projectName: string
  isConnected: boolean
  onClose: () => void
  onStatusChange: () => void
}

export const GitHubVersionsDropdown: React.FC<Props> = ({ projectName, isConnected, onClose, onStatusChange }) => {
  const [history, setHistory] = useState<Checkpoint[]>([])
  const [experiments, setExperiments] = useState<Experiment[]>([])
  const [loading, setLoading] = useState(true)
  const [restoring, setRestoring] = useState<string | null>(null)
  const [confirmRestore, setConfirmRestore] = useState<Checkpoint | null>(null)
  const [showExperimentModal, setShowExperimentModal] = useState(false)

  useEffect(() => {
    if (!isConnected) {
      setLoading(false)
      return
    }
    Promise.all([
      codePrototypeApi.githubHistory(projectName),
      codePrototypeApi.githubListExperiments(projectName),
    ]).then(([h, e]) => {
      setHistory(h)
      setExperiments(e)
    }).finally(() => setLoading(false))
  }, [projectName, isConnected])

  const handleRestore = async (checkpoint: Checkpoint) => {
    setConfirmRestore(null)
    setRestoring(checkpoint.hash)
    try {
      await codePrototypeApi.githubRestore(projectName, checkpoint.hash, checkpoint.message)
      onStatusChange()
      onClose()
    } catch {
      // silently fail — user can retry
    } finally {
      setRestoring(null)
    }
  }

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 z-40" onClick={onClose} />

      {/* Dropdown panel */}
      <div className="absolute right-0 top-full mt-1 z-50 w-80 bg-white dark:bg-gray-800 rounded-xl shadow-xl border border-gray-200 dark:border-gray-700 overflow-hidden">
        <div className="px-4 py-2.5 border-b border-gray-100 dark:border-gray-700">
          <span className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide">Versions</span>
        </div>

        {!isConnected ? (
          <div className="px-4 py-6 text-center">
            <p className="text-sm text-gray-400">Connect to GitHub to use version control.</p>
          </div>
        ) : loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
          </div>
        ) : (
          <div className="max-h-96 overflow-y-auto">
            {/* Checkpoints */}
            {history.length > 0 && (
              <div>
                <div className="px-4 py-1.5 bg-gray-50 dark:bg-gray-700/30">
                  <span className="text-xs text-gray-400 dark:text-gray-500 uppercase tracking-wide">Saved checkpoints</span>
                </div>
                {history.map((c) => (
                  <div key={c.hash} className="flex items-center justify-between px-4 py-2.5 hover:bg-gray-50 dark:hover:bg-gray-700/40 group">
                    <div className="min-w-0">
                      <p className="text-sm text-gray-700 dark:text-gray-400 truncate">📌 {c.message}</p>
                      <p className="text-xs text-gray-400 dark:text-gray-500">{c.timestamp}</p>
                    </div>
                    <button
                      onClick={() => setConfirmRestore(c)}
                      disabled={restoring === c.hash}
                      className="ml-2 flex-shrink-0 text-xs text-gray-400 hover:text-emerald-500 dark:hover:text-emerald-400 disabled:opacity-50 transition-colors opacity-0 group-hover:opacity-100"
                    >
                      {restoring === c.hash ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : 'Restore'}
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* Experiments */}
            {experiments.length > 0 && (
              <div>
                <div className="px-4 py-1.5 bg-gray-50 dark:bg-gray-700/30 border-t border-gray-100 dark:border-gray-700">
                  <span className="text-xs text-gray-400 dark:text-gray-500 uppercase tracking-wide">Experiments</span>
                </div>
                {experiments.map((e) => (
                  <div key={e.branch} className="flex items-center justify-between px-4 py-2.5 hover:bg-gray-50 dark:hover:bg-gray-700/40">
                    <div className="flex items-center gap-2 min-w-0">
                      <FlaskConical className="w-3.5 h-3.5 text-violet-400 dark:text-violet-600 flex-shrink-0" />
                      <div>
                        <p className="text-sm text-gray-700 dark:text-gray-400 truncate">{e.label}</p>
                        <p className="text-xs text-gray-400 dark:text-gray-500">{e.timestamp}</p>
                      </div>
                    </div>
                    <button
                      onClick={async () => {
                        onStatusChange()
                        onClose()
                      }}
                      className="ml-2 flex-shrink-0 text-xs text-gray-400 hover:text-violet-500 dark:hover:text-violet-400 transition-colors"
                    >
                      Switch
                    </button>
                  </div>
                ))}
              </div>
            )}

            {history.length === 0 && experiments.length === 0 && (
              <div className="px-4 py-6 text-center text-sm text-gray-400">
                No checkpoints yet. Save one to start tracking versions.
              </div>
            )}
          </div>
        )}

        {/* Try something new */}
        <div className="border-t border-gray-100 dark:border-gray-700 p-2">
          <button
            onClick={() => setShowExperimentModal(true)}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-600 dark:text-gray-300 hover:bg-violet-50 dark:hover:bg-violet-900/20 hover:text-violet-600 dark:hover:text-violet-400 rounded-lg transition-colors"
          >
            <GitBranch className="w-4 h-4" />
            + Try something new…
          </button>
        </div>
      </div>

      {/* Restore confirm inline dialog */}
      {confirmRestore && (
        <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50">
          <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-sm mx-4 p-6">
            <div className="flex items-center gap-2 mb-3">
              <RotateCcw className="w-5 h-5 text-amber-500" />
              <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">Restore checkpoint?</h3>
            </div>
            <p className="text-sm font-medium text-gray-800 dark:text-gray-200 mb-1">"{confirmRestore.message}"</p>
            <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">saved {confirmRestore.timestamp}</p>
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Your current work will be saved as a new checkpoint first, so nothing is lost.
            </p>
            <div className="flex justify-end gap-2 mt-5">
              <button
                onClick={() => setConfirmRestore(null)}
                className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => handleRestore(confirmRestore)}
                className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-amber-500 hover:bg-amber-600 text-white rounded-lg transition-colors"
              >
                <RotateCcw className="w-3.5 h-3.5" /> Restore
              </button>
            </div>
          </div>
        </div>
      )}

      {showExperimentModal && (
        <ExperimentModal
          projectName={projectName}
          onClose={() => setShowExperimentModal(false)}
          onStarted={() => { onStatusChange(); onClose() }}
        />
      )}
    </>
  )
}
