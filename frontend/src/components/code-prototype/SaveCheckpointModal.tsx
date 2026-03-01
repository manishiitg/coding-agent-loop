import React, { useState } from 'react'
import { Loader2, Save } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'

interface Props {
  projectName: string
  defaultMessage: string
  onClose: () => void
  onSaved: () => void
}

export const SaveCheckpointModal: React.FC<Props> = ({ projectName, defaultMessage, onClose, onSaved }) => {
  const [message, setMessage] = useState(defaultMessage)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSave = async () => {
    if (!message.trim()) return
    setLoading(true)
    setError('')
    try {
      const result = await codePrototypeApi.githubSaveCheckpoint(projectName, message.trim())
      if (result.message === 'nothing to save') {
        setError('No changes to save.')
        setLoading(false)
        return
      }
      onSaved()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed')
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
        <div className="flex items-center gap-2 mb-4">
          <Save className="w-5 h-5 text-emerald-500" />
          <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">Save checkpoint</h2>
        </div>

        <p className="text-sm text-gray-500 dark:text-gray-400 mb-3">
          Describe this save <span className="text-gray-400">(optional)</span>
        </p>
        <input
          type="text"
          value={message}
          onChange={e => setMessage(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSave()}
          autoFocus
          className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
          placeholder="e.g. Added counter button"
        />
        {defaultMessage && defaultMessage !== message && (
          <p className="mt-1.5 text-xs text-gray-400">✨ Pre-filled from your last agent message</p>
        )}

        {error && <p className="mt-2 text-xs text-red-500">{error}</p>}

        <div className="flex justify-end gap-2 mt-5">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={loading || !message.trim()}
            className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
