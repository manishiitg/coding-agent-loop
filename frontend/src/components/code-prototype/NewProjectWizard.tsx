import React, { useState, useMemo, useEffect } from 'react'
import { X, Loader2 } from 'lucide-react'
import { codePrototypeApi } from '../../api/codePrototype'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { useMCPStore } from '../../stores'
import { ToolSelectionSection } from '../ToolSelectionSection'
import { SecretSelectionSection } from '../secrets/SecretSelectionSection'
import { subagentsApi } from '../../api/subagents'
import type { SubAgent } from '../../types/subagents'

interface Props {
  onClose: () => void
}

export const NewProjectWizard: React.FC<Props> = ({ onClose }) => {
  const [name, setName] = useState('')
  const [selectedServers, setSelectedServers] = useState<string[]>([])
  const [selectedTools, setSelectedTools] = useState<string[]>([])
  const [selectedSecrets, setSelectedSecrets] = useState<string[]>([])
  const [selectedSubAgents, setSelectedSubAgents] = useState<string[]>([])
  const [subAgents, setSubAgents] = useState<SubAgent[]>([])
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    subagentsApi.listSubAgents().then(r => setSubAgents(r.subagents || [])).catch(() => {})
  }, [])

  const toolList = useMCPStore(s => s.toolList)
  const availableServers = useMemo(
    () => [...new Set(toolList.map(t => t.server).filter(Boolean) as string[])],
    [toolList]
  )
  const { setCurrentProject } = useCodePrototypeStore()

  const nameSlugValid = /^[a-z0-9-]+$/.test(name) && name.length > 0

  const handleSubmit = async () => {
    if (!nameSlugValid) return
    setIsSubmitting(true)
    setError('')
    const payload = {
      name,
      type: 'fullstack' as const,
      config: {
        selected_servers: selectedServers,
        selected_tools: selectedTools,
        selected_secrets: selectedSecrets,
        selected_skills: [],
        selected_subagents: selectedSubAgents,
      },
    }
    console.log('[CODE-PROTOTYPE] creating project:', payload)
    try {
      const project = await codePrototypeApi.createProject(payload)
      console.log('[CODE-PROTOTYPE] project created:', project)
      setCurrentProject(project)
      onClose()
    } catch (e: unknown) {
      console.error('[CODE-PROTOTYPE] create project error:', e)
      setError(e instanceof Error ? e.message : 'Failed to create project')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg max-h-[90vh] flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-base font-semibold text-gray-900 dark:text-white">New Code Prototype</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 py-4 space-y-4 overflow-y-auto flex-1">
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              Project name <span className="text-gray-400">(lowercase, hyphens only)</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
              placeholder="my-app"
              className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
            />
          </div>

          <ToolSelectionSection
            availableServers={availableServers}
            selectedServers={selectedServers}
            selectedTools={selectedTools}
            onServerChange={setSelectedServers}
            onToolChange={setSelectedTools}
            agentMode="simple"
            stepId="new-prototype-wizard"
          />

          <SecretSelectionSection
            selectedSecrets={selectedSecrets}
            onSecretChange={setSelectedSecrets}
          />

          {subAgents.length > 0 && (
            <div>
              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">
                Sub-agents <span className="text-gray-400">(selected for every session)</span>
              </label>
              <div className="space-y-1 max-h-36 overflow-y-auto">
                {subAgents.map(sa => (
                  <label key={sa.folder_name} className="flex items-start gap-2 cursor-pointer group">
                    <input
                      type="checkbox"
                      checked={selectedSubAgents.includes(sa.folder_name)}
                      onChange={() => {
                        setSelectedSubAgents(prev =>
                          prev.includes(sa.folder_name)
                            ? prev.filter(n => n !== sa.folder_name)
                            : [...prev, sa.folder_name]
                        )
                      }}
                      className="mt-0.5 accent-emerald-600"
                    />
                    <span className="text-xs text-gray-700 dark:text-gray-300 leading-4">
                      {sa.frontmatter.name}
                      {sa.frontmatter.description && (
                        <span className="text-gray-400 ml-1">— {sa.frontmatter.description}</span>
                      )}
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )}

          {error && <p className="text-xs text-red-500">{error}</p>}
        </div>

        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-200 dark:border-gray-700 flex-shrink-0">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-xs text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={!nameSlugValid || isSubmitting}
            className="px-4 py-1.5 text-xs bg-emerald-600 hover:bg-emerald-700 text-white rounded-md disabled:opacity-50 flex items-center gap-1.5 transition-colors"
          >
            {isSubmitting && <Loader2 className="w-3 h-3 animate-spin" />}
            Create Project
          </button>
        </div>
      </div>
    </div>
  )
}
