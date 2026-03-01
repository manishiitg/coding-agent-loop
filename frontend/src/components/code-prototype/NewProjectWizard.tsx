import React, { useState } from 'react'
import { X, Loader2, Code } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'

interface Props {
  onClose: () => void
}

export const NewProjectWizard: React.FC<Props> = ({ onClose }) => {
  const [name, setName] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState('')
  const { setCurrentProject } = useCodePrototypeStore()

  const nameValid = /^[a-z0-9-]+$/.test(name) && name.length > 0

  const handleSubmit = async () => {
    if (!nameValid || isSubmitting) return
    setIsSubmitting(true)
    setError('')
    try {
      const project = await codePrototypeApi.createProject({
        name,
        type: 'fullstack',
        config: {
          selected_servers: [],
          selected_tools: [],
          selected_secrets: [],
          selected_skills: [],
          selected_subagents: [],
        },
      })
      setCurrentProject(project)
      onClose()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to create project')
      setIsSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60] p-4">
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-sm">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-2">
            <Code className="w-4 h-4 text-emerald-500" />
            <h2 className="text-base font-semibold text-gray-900 dark:text-white">New project</h2>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 py-5 space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1.5">
              Project name
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
              onKeyDown={e => e.key === 'Enter' && handleSubmit()}
              placeholder="my-app"
              autoFocus
              className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
            />
            <p className="mt-1.5 text-xs text-gray-400">Lowercase letters, numbers and hyphens only.</p>
          </div>

          {error && <p className="text-xs text-red-500">{error}</p>}
        </div>

        <div className="flex justify-end gap-2 px-5 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={!nameValid || isSubmitting}
            className="flex items-center gap-1.5 px-4 py-1.5 text-sm font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg disabled:opacity-50 transition-colors"
          >
            {isSubmitting && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            Create
          </button>
        </div>
      </div>
    </div>
  )
}
