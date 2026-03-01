import React, { useEffect, useState } from 'react'
import { X, Trash2, Loader2, Plus } from 'lucide-react'
import { codePrototypeApi, type PrototypeProject } from '../../api/codePrototype'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { NewProjectWizard } from './NewProjectWizard'

interface Props {
  onClose: () => void
}

export const ProjectListModal: React.FC<Props> = ({ onClose }) => {
  const { currentProject, projectList, setCurrentProject, setProjectList } = useCodePrototypeStore()
  // Seed from store immediately; refresh from server in background
  const [projects, setProjects] = useState<PrototypeProject[]>(projectList)
  const [loading, setLoading] = useState(projectList.length === 0)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [showNewProject, setShowNewProject] = useState(false)

  useEffect(() => {
    codePrototypeApi.listProjects()
      .then(list => { setProjects(list); setProjectList(list) })
      .finally(() => setLoading(false))
  }, [setProjectList])

  const handleSelect = (p: PrototypeProject) => {
    setCurrentProject(p)
    onClose()
  }

  const handleDelete = async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!window.confirm(`Delete project "${name}"?`)) return
    setDeleting(name)
    try {
      await codePrototypeApi.deleteProject(name)
      const updated = projects.filter(p => p.name !== name)
      setProjects(updated)
      setProjectList(updated)
      if (currentProject?.name === name) {
        setCurrentProject(null)
      }
    } finally {
      setDeleting(null)
    }
  }

  return (
    <>
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-base font-semibold text-gray-900 dark:text-white">Projects</h2>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowNewProject(true)}
              className="flex items-center gap-1 px-2.5 py-1 text-xs font-medium text-emerald-700 dark:text-emerald-300 bg-emerald-50 dark:bg-emerald-900/30 hover:bg-emerald-100 dark:hover:bg-emerald-900/50 rounded-md transition-colors"
            >
              <Plus className="w-3.5 h-3.5" /> New project
            </button>
            <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="max-h-72 overflow-y-auto">
          {loading ? (
            <div className="flex justify-center p-6">
              <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
            </div>
          ) : projects.length === 0 ? (
            <p className="text-sm text-gray-500 dark:text-gray-400 text-center p-6">No projects yet.</p>
          ) : (
            projects.map(p => (
              <div
                key={p.name}
                onClick={() => handleSelect(p)}
                className={`flex items-center justify-between px-5 py-3 hover:bg-gray-100 dark:hover:bg-gray-700/50 cursor-pointer border-b border-gray-100 dark:border-gray-700 last:border-0 ${currentProject?.name === p.name ? 'bg-gray-100 dark:bg-gray-700' : ''}`}
              >
                <div>
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium text-gray-900 dark:text-white">{p.name}</p>
                    {currentProject?.name === p.name && (
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                    )}
                  </div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">{p.type}</p>
                </div>
                <button
                  onClick={e => handleDelete(p.name, e)}
                  disabled={deleting === p.name}
                  className="p-1.5 text-gray-400 hover:text-red-500 disabled:opacity-50 transition-colors"
                >
                  {deleting === p.name
                    ? <Loader2 className="w-4 h-4 animate-spin" />
                    : <Trash2 className="w-4 h-4" />}
                </button>
              </div>
            ))
          )}
        </div>

      </div>
    </div>

    {showNewProject && (
      <NewProjectWizard onClose={() => setShowNewProject(false)} />
    )}
  </>
  )
}
