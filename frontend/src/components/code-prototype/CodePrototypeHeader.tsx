import React, { useState } from 'react'
import { Code, ChevronDown, Settings, Rocket, ExternalLink, Plus, Loader2, Monitor, SquarePen } from 'lucide-react'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { codePrototypeApi } from '../../api/codePrototype'
import { ProjectListModal } from './ProjectListModal'
import { ProjectConfigModal } from './ProjectConfigModal'
import { NewProjectWizard } from './NewProjectWizard'

export const CodePrototypeHeader: React.FC<{ onNewChat: () => void }> = ({ onNewChat }) => {
  const {
    currentProject,
    isDeploying,
    lastDeployedUrl,
    selectedProvider,
    showPreview,
    setSelectedProvider,
    setDeploying,
    appendDeployLog,
    clearDeployLog,
    setLastDeployedUrl,
    setShowPreview,
  } = useCodePrototypeStore()

  const [showProjectList, setShowProjectList] = useState(false)
  const [showConfig, setShowConfig] = useState(false)
  const [showNewProject, setShowNewProject] = useState(false)

  const handleDeploy = async () => {
    if (!currentProject || isDeploying) return
    console.log('[CODE-PROTOTYPE] deploy start:', currentProject.name, 'provider:', selectedProvider)
    clearDeployLog()
    setDeploying(true)
    setLastDeployedUrl(null)

    appendDeployLog(`▶ Deploying "${currentProject.name}" to ${selectedProvider}…`)
    try {
      const result = await codePrototypeApi.deploy({
        project_name: currentProject.name,
        provider: selectedProvider,
      })
      console.log('[CODE-PROTOTYPE] deploy success:', result.url)
      result.logs.split('\n').filter(Boolean).forEach(l => appendDeployLog(l))
      appendDeployLog(`✓ Deployed: ${result.url}`)
      setLastDeployedUrl(result.url)
    } catch (e: unknown) {
      console.error('[CODE-PROTOTYPE] deploy error:', e)
      appendDeployLog(`✗ Deploy failed: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setDeploying(false)
    }
  }

  return (
    <>
      <div className="flex items-center gap-2 px-4 py-2 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 flex-shrink-0">
        <Code className="w-4 h-4 text-emerald-500 flex-shrink-0" />

        <button
          onClick={() => setShowProjectList(true)}
          className="flex items-center gap-1 text-sm font-medium text-gray-900 dark:text-gray-100 hover:text-emerald-600 dark:hover:text-emerald-400 transition-colors"
        >
          {currentProject ? currentProject.name : 'Select project'}
          <ChevronDown className="w-3 h-3 text-gray-400" />
        </button>

        <button
          onClick={() => setShowNewProject(true)}
          className="p-1 text-gray-400 hover:text-emerald-500 transition-colors"
          title="New project"
        >
          <Plus className="w-4 h-4" />
        </button>

        <button
          onClick={onNewChat}
          title="New chat"
          className="p-1 text-gray-400 hover:text-emerald-500 transition-colors"
        >
          <SquarePen className="w-4 h-4" />
        </button>

        <div className="flex-1" />

        {lastDeployedUrl && (
          <a
            href={lastDeployedUrl}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-1 text-xs text-emerald-500 hover:text-emerald-400 truncate max-w-[200px]"
          >
            {lastDeployedUrl.replace(/^https?:\/\//, '')}
            <ExternalLink className="w-3 h-3 flex-shrink-0" />
          </a>
        )}

        {currentProject && (
          <button
            onClick={() => setShowConfig(true)}
            className="p-1.5 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
            title="Project config"
          >
            <Settings className="w-4 h-4" />
          </button>
        )}

        <button
          onClick={() => setShowPreview(!showPreview)}
          title="Toggle preview"
          className={`flex items-center gap-1 px-2 py-1.5 text-xs rounded-md border transition-colors ${
            showPreview
              ? 'bg-emerald-50 dark:bg-emerald-900/30 border-emerald-400 dark:border-emerald-600 text-emerald-700 dark:text-emerald-300'
              : 'bg-gray-100 dark:bg-gray-700 border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
          }`}
        >
          <Monitor className="w-3.5 h-3.5" />
          <span className="font-medium">Preview</span>
        </button>

        <select
          value={selectedProvider}
          onChange={e => setSelectedProvider(e.target.value as 'k8s' | 'vercel' | 'railway')}
          className="text-xs border border-gray-300 dark:border-gray-600 rounded-md px-2 py-1 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200"
        >
          <option value="k8s">Kubernetes</option>
          <option value="vercel">Vercel</option>
          <option value="railway">Railway</option>
        </select>

        <button
          onClick={handleDeploy}
          disabled={!currentProject || isDeploying}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-md disabled:opacity-50 transition-colors"
        >
          {isDeploying ? <Loader2 className="w-3 h-3 animate-spin" /> : <Rocket className="w-3 h-3" />}
          {isDeploying ? 'Deploying…' : 'Deploy'}
        </button>
      </div>

      {showProjectList && <ProjectListModal onClose={() => setShowProjectList(false)} />}
      {showConfig && currentProject && (
        <ProjectConfigModal project={currentProject} onClose={() => setShowConfig(false)} />
      )}
      {showNewProject && (
        <NewProjectWizard onClose={() => setShowNewProject(false)} />
      )}
    </>
  )
}
