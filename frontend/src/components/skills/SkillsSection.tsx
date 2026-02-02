import { useState, useEffect, useCallback } from 'react'
import { Sparkles, Loader2, AlertCircle, Plus, RefreshCw, Lightbulb } from 'lucide-react'
import { skillsApi } from '../../api/skills'
import type { Skill } from '../../types/skills'
import SkillCard from './SkillCard'
import SkillImportDialog from './SkillImportDialog'
import { useChatStore } from '../../stores'

export default function SkillsSection() {
  const [skills, setSkills] = useState<Skill[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showDetails, setShowDetails] = useState(false)
  const [showImportDialog, setShowImportDialog] = useState(false)
  
  const { getActiveTab, setTabConfig } = useChatStore()

  const loadSkills = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const response = await skillsApi.listSkills()
      setSkills(response.skills || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load skills')
      setSkills([])
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadSkills()
  }, [loadSkills])

  const handleDelete = async (folderName: string) => {
    if (!confirm(`Are you sure you want to delete the skill "${folderName}"?`)) {
      return
    }

    try {
      await skillsApi.deleteSkill(folderName)
      loadSkills()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete skill')
    }
  }

  const handleImportSuccess = () => {
    setShowImportDialog(false)
    loadSkills()
  }
  
  const handleOpenSkillBuilder = () => {
    const activeTab = getActiveTab()
    if (activeTab) {
      const currentSkills = activeTab.config.selectedSkills || []
      const updatedSkills = currentSkills.includes('skill-creator')
        ? currentSkills
        : [...currentSkills, 'skill-creator']
      setTabConfig(activeTab.tabId, {
        selectedSkills: updatedSkills,
        inputText: '/build-skill ',
      })
    }
    setShowDetails(false)
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <Sparkles className="w-4 h-4 text-purple-600 dark:text-purple-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">Skills</span>
        </div>
        <span className="px-2 py-0.5 text-xs bg-purple-100 dark:bg-purple-900 text-purple-700 dark:text-purple-300 rounded-full">
          {skills?.length || 0}
        </span>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <Loader2 className="w-4 h-4 animate-spin" />
          <span>Loading skills...</span>
        </div>
      )}

      {error && (
        <div className="flex items-center gap-2 text-sm text-red-500 dark:text-red-400">
          <AlertCircle className="w-4 h-4" />
          <span>Error: {error}</span>
        </div>
      )}

      {!isLoading && !error && (
        <div className="space-y-2">
          <button
            onClick={() => setShowDetails(!showDetails)}
            className="w-full p-2 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors text-left"
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {skills?.length || 0} {(skills?.length || 0) === 1 ? 'Skill' : 'Skills'}
                </span>
              </div>
              <span className="text-xs text-gray-500">
                {showDetails ? '▼' : '▶'}
              </span>
            </div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Click to manage skills
            </div>
          </button>

          {/* Skills Modal */}
          {showDetails && (
            <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-4xl h-[80vh] overflow-hidden flex flex-col">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    Skills Manager
                  </h3>
                  <div className="flex items-center gap-2">
                    <button
                      onClick={loadSkills}
                      className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                      title="Refresh skills"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </button>
                    <button
                      onClick={handleOpenSkillBuilder}
                      className="px-3 py-1.5 text-sm font-medium text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-900/30 hover:bg-emerald-100 dark:hover:bg-emerald-900/50 rounded-md transition-colors flex items-center gap-2"
                    >
                      <Lightbulb className="w-4 h-4" />
                      Build Skill
                    </button>
                    <button
                      onClick={() => setShowImportDialog(true)}
                      className="px-3 py-1.5 text-sm font-medium text-purple-600 dark:text-purple-400 bg-purple-50 dark:bg-purple-900/30 hover:bg-purple-100 dark:hover:bg-purple-900/50 rounded-md transition-colors flex items-center gap-2"
                    >
                      <Plus className="w-4 h-4" />
                      Import from GitHub
                    </button>
                    <button
                      onClick={() => setShowDetails(false)}
                      className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                    >
                      ✕
                    </button>
                  </div>
                </div>

                <div className="flex-1 overflow-y-auto">
                  {(skills?.length || 0) === 0 ? (
                    <div className="flex flex-col items-center justify-center h-full text-gray-500 dark:text-gray-400">
                      <Sparkles className="w-12 h-12 mb-4 opacity-50" />
                      <p className="text-lg font-medium mb-2">No skills installed</p>
                      <p className="text-sm text-center mb-4">
                        Import skills from GitHub to extend your agent's capabilities
                      </p>
                      <div className="flex gap-2">
                        <button
                          onClick={handleOpenSkillBuilder}
                          className="px-4 py-2 text-sm font-medium text-white bg-emerald-600 hover:bg-emerald-700 rounded-md transition-colors flex items-center gap-2"
                        >
                          <Lightbulb className="w-4 h-4" />
                          Build Skill
                        </button>
                        <button
                          onClick={() => setShowImportDialog(true)}
                          className="px-4 py-2 text-sm font-medium text-white bg-purple-600 hover:bg-purple-700 rounded-md transition-colors flex items-center gap-2"
                        >
                          <Plus className="w-4 h-4" />
                          Import from GitHub
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="grid gap-4">
                      {(skills || []).map((skill) => (
                        <SkillCard
                          key={skill.folder_name}
                          skill={skill}
                          onDelete={() => handleDelete(skill.folder_name)}
                        />
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Import Dialog */}
      {showImportDialog && (
        <SkillImportDialog
          onClose={() => setShowImportDialog(false)}
          onSuccess={handleImportSuccess}
        />
      )}
    </div>
  )
}
