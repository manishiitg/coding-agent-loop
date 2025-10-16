import { useState } from 'react'
import { Settings, ChevronDown } from 'lucide-react'
import PresetQueries from '../PresetQueries'
import { usePresetApplication } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'

interface PresetQueriesSectionProps {
  availableServers: string[]
  onPresetFolderSelect?: (folderPath?: string) => void
  setCurrentQuery: (query: string) => void
  isStreaming: boolean
  onPresetAdded?: () => void
}

export default function PresetQueriesSection({
  availableServers,
  onPresetFolderSelect,
  setCurrentQuery,
  isStreaming,
  onPresetAdded
}: PresetQueriesSectionProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['presets']))
  const [triggerAddPreset, setTriggerAddPreset] = useState(false)
  const [showPresetSelector, setShowPresetSelector] = useState(false)
  
  // Store subscriptions
  const { selectedModeCategory } = useModeStore()
  const { getActivePresetId, getActivePreset } = usePresetApplication()

  const toggleSection = (section: string) => {
    setExpandedSections(prev => {
      const newSet = new Set(prev)
      if (newSet.has(section)) {
        newSet.delete(section)
      } else {
        newSet.add(section)
      }
      return newSet
    })
  }


  return (
    <div className="space-y-2">
      {/* Current Preset Display */}
      {selectedModeCategory && selectedModeCategory !== 'chat' && (
        <div className="space-y-2">
          {(() => {
            const activePresetId = getActivePresetId(selectedModeCategory as 'deep-research' | 'workflow')
            const activePreset = getActivePreset(selectedModeCategory as 'deep-research' | 'workflow')
            
            return (
              <div className="space-y-2">
                {/* Active Preset */}
                {activePresetId && activePreset ? (
                  <div className="flex items-center justify-between p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                    <div className="flex items-center gap-2">
                      <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                      <span className="text-sm font-medium text-blue-900 dark:text-blue-100">
                        {activePreset.label || 'Preset Selected'}
                      </span>
                    </div>
                    <button
                      onClick={() => setShowPresetSelector(!showPresetSelector)}
                      className="p-1 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-200 transition-colors"
                    >
                      <ChevronDown className={`w-3 h-3 transition-transform ${showPresetSelector ? 'rotate-180' : ''}`} />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg">
                    <span className="text-sm text-gray-600 dark:text-gray-400">
                      No preset selected
                    </span>
                    <button
                      onClick={() => setShowPresetSelector(!showPresetSelector)}
                      className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
                    >
                      <ChevronDown className={`w-3 h-3 transition-transform ${showPresetSelector ? 'rotate-180' : ''}`} />
                    </button>
                  </div>
                )}
              </div>
            )
          })()}
        </div>
      )}

      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <Settings className="w-4 h-4 text-gray-600 dark:text-gray-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
            {selectedModeCategory === 'chat' ? 'Preset Queries' : 'Available Presets'}
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => setTriggerAddPreset(true)}
            className="flex items-center justify-center w-6 h-6 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
            title="Add Preset"
          >
            <span className="text-sm font-medium">+</span>
          </button>
          <button
            onClick={() => toggleSection('presets')}
            className="text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
            title={expandedSections.has('presets') ? "Collapse" : "Expand"}
          >
            <svg className={`w-3 h-3 transition-transform ${expandedSections.has('presets') ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </button>
        </div>
      </div>
      
      {expandedSections.has('presets') && (
        <div className="space-y-2">
          <PresetQueries
            setCurrentQuery={setCurrentQuery}
            isStreaming={isStreaming}
            availableServers={availableServers}
            onPresetFolderSelect={onPresetFolderSelect}
            triggerAddPreset={triggerAddPreset}
            onAddPresetTriggered={() => setTriggerAddPreset(false)}
            onPresetAdded={onPresetAdded}
          />
        </div>
      )}
    </div>
  )
}
