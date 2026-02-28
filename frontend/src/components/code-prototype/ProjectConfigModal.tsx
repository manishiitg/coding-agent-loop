import React, { useState, useMemo } from 'react'
import { X, Loader2 } from 'lucide-react'
import { codePrototypeApi, type PrototypeProject, type PrototypeProjectConfig } from '../../api/codePrototype'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { useMCPStore } from '../../stores'
import { ToolSelectionSection } from '../ToolSelectionSection'
import { SecretSelectionSection } from '../secrets/SecretSelectionSection'

interface Props {
  project: PrototypeProject
  onClose: () => void
}

export const ProjectConfigModal: React.FC<Props> = ({ project, onClose }) => {
  const [selectedServers, setSelectedServers] = useState<string[]>(project.config.selected_servers ?? [])
  const [selectedTools, setSelectedTools] = useState<string[]>(project.config.selected_tools ?? [])
  const [selectedSecrets, setSelectedSecrets] = useState<string[]>(project.config.selected_secrets ?? [])
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState('')

  const toolList = useMCPStore(s => s.toolList)
  const availableServers = useMemo(
    () => [...new Set(toolList.map(t => t.server).filter(Boolean) as string[])],
    [toolList]
  )
  const { setCurrentProject } = useCodePrototypeStore()

  const handleSave = async () => {
    setIsSubmitting(true)
    setError('')
    const config: PrototypeProjectConfig = {
      ...project.config,
      selected_servers: selectedServers,
      selected_tools: selectedTools,
      selected_secrets: selectedSecrets,
    }
    try {
      await codePrototypeApi.updateConfig(project.name, config)
      setCurrentProject({ ...project, config })
      onClose()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to save config')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg max-h-[90vh] flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-base font-semibold text-gray-900 dark:text-white">Config — {project.name}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 py-4 space-y-4 overflow-y-auto flex-1">
          <ToolSelectionSection
            availableServers={availableServers}
            selectedServers={selectedServers}
            selectedTools={selectedTools}
            onServerChange={setSelectedServers}
            onToolChange={setSelectedTools}
            agentMode="simple"
            stepId="config-modal-prototype"
          />

          <SecretSelectionSection
            selectedSecrets={selectedSecrets}
            onSecretChange={setSelectedSecrets}
          />

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
            onClick={handleSave}
            disabled={isSubmitting}
            className="px-4 py-1.5 text-xs bg-emerald-600 hover:bg-emerald-700 text-white rounded-md disabled:opacity-50 flex items-center gap-1.5 transition-colors"
          >
            {isSubmitting && <Loader2 className="w-3 h-3 animate-spin" />}
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
