import React, { useState } from 'react'
import { Loader2, FlaskConical } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'

interface Props {
  projectName: string
  onClose: () => void
  onStarted: (label: string) => void
}

export const ExperimentModal: React.FC<Props> = ({ projectName, onClose, onStarted }) => {
  const [label, setLabel] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleStart = async () => {
    const trimmed = label.trim() || 'experiment'
    setLoading(true)
    setError('')
    try {
      await codePrototypeApi.githubStartExperiment(projectName, trimmed)
      onStarted(trimmed)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to start experiment')
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
        <div className="flex items-center gap-2 mb-4">
          <FlaskConical className="w-5 h-5 text-violet-500" />
          <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Start an experiment</h2>
        </div>

        <label className="block text-sm text-gray-600 dark:text-gray-400 mb-2">
          What are you trying?
        </label>
        <input
          type="text"
          value={label}
          onChange={e => setLabel(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleStart()}
          autoFocus
          placeholder='e.g. "new dark theme"'
          className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-violet-500"
        />

        <p className="mt-3 text-xs text-gray-400">
          Your current version is safe — changes won't affect it until you decide to keep them.
        </p>

        {error && <p className="mt-2 text-xs text-red-500">{error}</p>}

        <div className="flex justify-end gap-2 mt-5">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleStart}
            disabled={loading}
            className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-violet-600 hover:bg-violet-700 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <FlaskConical className="w-3.5 h-3.5" />}
            Start
          </button>
        </div>
      </div>
    </div>
  )
}
