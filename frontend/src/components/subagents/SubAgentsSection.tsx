import { useState, useEffect, useCallback } from 'react'
import { Bot, Loader2, AlertCircle, RefreshCw, Lightbulb } from 'lucide-react'
import { subagentsApi } from '../../api/subagents'
import type { SubAgent } from '../../types/subagents'
import SubAgentCard from './SubAgentCard'
import { useChatStore } from '../../stores'

export default function SubAgentsSection() {
  const [subagents, setSubAgents] = useState<SubAgent[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showDetails, setShowDetails] = useState(false)

  const { getActiveTab, setTabConfig } = useChatStore()

  const loadSubAgents = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const response = await subagentsApi.listSubAgents()
      setSubAgents(response.subagents || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sub-agent templates')
      setSubAgents([])
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadSubAgents()
  }, [loadSubAgents])

  const handleDelete = async (folderName: string) => {
    if (!confirm(`Are you sure you want to delete the sub-agent template "${folderName}"?`)) {
      return
    }

    try {
      await subagentsApi.deleteSubAgent(folderName)
      loadSubAgents()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete sub-agent template')
    }
  }

  const handleOpenSubAgentBuilder = () => {
    const activeTab = getActiveTab()
    if (activeTab) {
      const currentSkills = activeTab.config.selectedSkills || []
      const updatedSkills = currentSkills.includes('subagent-creator')
        ? currentSkills
        : [...currentSkills, 'subagent-creator']
      setTabConfig(activeTab.tabId, {
        selectedSkills: updatedSkills,
        inputText: '/build-subagent ',
      })
    }
    setShowDetails(false)
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <Bot className="w-4 h-4 text-blue-600 dark:text-blue-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">Sub-Agents</span>
        </div>
        <span className="px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded-full">
          {subagents?.length || 0}
        </span>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <Loader2 className="w-4 h-4 animate-spin" />
          <span>Loading templates...</span>
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
                <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {subagents?.length || 0} {(subagents?.length || 0) === 1 ? 'Template' : 'Templates'}
                </span>
              </div>
              <span className="text-xs text-gray-500">
                {showDetails ? '\u25BC' : '\u25B6'}
              </span>
            </div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Click to manage sub-agent templates
            </div>
          </button>

          {/* Sub-Agents Modal */}
          {showDetails && (
            <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-4xl h-[80vh] overflow-hidden flex flex-col">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                    Sub-Agent Templates
                  </h3>
                  <div className="flex items-center gap-2">
                    <button
                      onClick={loadSubAgents}
                      className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                      title="Refresh templates"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </button>
                    <button
                      onClick={handleOpenSubAgentBuilder}
                      className="px-3 py-1.5 text-sm font-medium text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50 rounded-md transition-colors flex items-center gap-2"
                    >
                      <Lightbulb className="w-4 h-4" />
                      Build Sub-Agent
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
                  {(subagents?.length || 0) === 0 ? (
                    <div className="flex flex-col items-center justify-center h-full text-gray-500 dark:text-gray-400">
                      <Bot className="w-12 h-12 mb-4 opacity-50" />
                      <p className="text-lg font-medium mb-2">No sub-agent templates</p>
                      <p className="text-sm text-center mb-4">
                        Create reusable sub-agent profiles for delegation
                      </p>
                      <button
                        onClick={handleOpenSubAgentBuilder}
                        className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md transition-colors flex items-center gap-2"
                      >
                        <Lightbulb className="w-4 h-4" />
                        Build Sub-Agent
                      </button>
                    </div>
                  ) : (
                    <div className="grid gap-4">
                      {(subagents || []).map((sa) => (
                        <SubAgentCard
                          key={sa.folder_name}
                          subagent={sa}
                          onDelete={() => handleDelete(sa.folder_name)}
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
    </div>
  )
}
