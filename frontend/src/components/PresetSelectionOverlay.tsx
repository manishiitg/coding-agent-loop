import React, { useState, useEffect } from 'react'
import { Search, Workflow, Plus, Folder, Check } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'
import PresetModal from './PresetModal'
import { usePresetsDatabase } from '../hooks/usePresetsDatabase'
import { useActivePresetStore } from '../stores/useActivePresetStore'
import type { PlannerFile } from '../services/api-types'

interface PresetSelectionOverlayProps {
  isOpen: boolean
  onClose: () => void
  onPresetSelected: (presetId: string) => void
  modeCategory: ModeCategory
  onPresetSelect?: (servers: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow') => void
  onPresetFolderSelect?: (folderPath?: string) => void
  setCurrentQuery?: (query: string) => void
}

export const PresetSelectionOverlay: React.FC<PresetSelectionOverlayProps> = ({
  isOpen,
  onClose,
  onPresetSelected,
  modeCategory,
  onPresetSelect,
  onPresetFolderSelect,
  setCurrentQuery
}) => {
  const { customPresets, addPreset } = usePresetsDatabase()
  const { setActivePresetQueryId } = useActivePresetStore()
  const [selectedPresetId, setSelectedPresetId] = useState<string | null>(null)
  const [isPresetModalOpen, setIsPresetModalOpen] = useState(false)

  // Get presets for the current mode category
  const categoryKey = modeCategory === 'deep-research' ? 'orchestrator' : 'workflow'
  const presets = customPresets.filter(preset => preset.agentMode === categoryKey)

  // Reset selection when overlay opens
  useEffect(() => {
    if (isOpen) {
      setSelectedPresetId(null)
    }
  }, [isOpen])

  const handlePresetSelect = (presetId: string) => {
    setSelectedPresetId(presetId)
  }

  const handleConfirm = () => {
    if (selectedPresetId && (modeCategory === 'deep-research' || modeCategory === 'workflow')) {
      // Find the selected preset
      const selectedPreset = presets.find(preset => preset.id === selectedPresetId)
      
      if (selectedPreset) {
        // Set the query in the chat input
        if (setCurrentQuery) {
          setCurrentQuery(selectedPreset.query)
        }
        
        // Call the preset select callback (same as PresetQueries)
        if (onPresetSelect) {
          if (selectedPreset.selectedServers && selectedPreset.selectedServers.length > 0) {
            onPresetSelect(selectedPreset.selectedServers, selectedPreset.agentMode)
          } else {
            onPresetSelect([], selectedPreset.agentMode)
          }
        }
        
        // Call the folder select callback (same as PresetQueries)
        if (onPresetFolderSelect) {
          onPresetFolderSelect(selectedPreset.selectedFolder?.filepath)
        }
        
        // Set active preset query ID in the dedicated store
        setActivePresetQueryId(modeCategory, selectedPresetId)
        
        // Call the original callback
        onPresetSelected(selectedPresetId)
        onClose()
      }
    }
  }

  const handleCreateNew = () => {
    setIsPresetModalOpen(true)
  }

  const handlePresetModalClose = () => {
    setIsPresetModalOpen(false)
  }

  const handlePresetSave = async (
    label: string, 
    query: string, 
    selectedServers?: string[], 
    _agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', 
    selectedFolder?: PlannerFile
  ) => {
    // Set the agent mode based on the mode category
    const presetAgentMode = modeCategory === 'deep-research' ? 'orchestrator' : 'workflow'
    
    try {
      // Create the preset and get the returned preset object directly
      const newPreset = await addPreset(label, query, selectedServers, presetAgentMode, selectedFolder)
      
      // Close the modal
      setIsPresetModalOpen(false)
      
      // Use the returned preset directly instead of searching for it
      setSelectedPresetId(newPreset.id)
      
      // Set the query in the chat input
      if (setCurrentQuery) {
        setCurrentQuery(newPreset.query)
      }
      
      // Call the preset select callback (same as PresetQueries)
      if (onPresetSelect) {
        if (newPreset.selectedServers && newPreset.selectedServers.length > 0) {
          onPresetSelect(newPreset.selectedServers, newPreset.agentMode)
        } else {
          onPresetSelect([], newPreset.agentMode)
        }
      }
      
      // Call the folder select callback (same as PresetQueries)
      if (onPresetFolderSelect) {
        onPresetFolderSelect(newPreset.selectedFolder?.filepath)
      }
      
          // Automatically confirm the selection
          if (modeCategory === 'deep-research' || modeCategory === 'workflow') {
            // Set active preset query ID in the dedicated store
            setActivePresetQueryId(modeCategory, newPreset.id)
            onPresetSelected(newPreset.id)
            onClose()
          }
    } catch (error) {
      console.error('Failed to create preset:', error)
      // You might want to show an error message to the user here
    }
  }

  if (!isOpen) return null

  const getModeIcon = () => {
    switch (modeCategory) {
      case 'deep-research':
        return <Search className="w-8 h-8 text-blue-600" />
      case 'workflow':
        return <Workflow className="w-8 h-8 text-blue-600" />
      default:
        return <Folder className="w-8 h-8 text-gray-400" />
    }
  }

  const getModeTitle = () => {
    switch (modeCategory) {
      case 'deep-research':
        return 'Deep Research Mode'
      case 'workflow':
        return 'Workflow Mode'
      default:
        return 'Select Mode'
    }
  }

  const getModeDescription = () => {
    switch (modeCategory) {
      case 'deep-research':
        return 'Select a research preset to organize your analysis projects'
      case 'workflow':
        return 'Select a workflow preset to organize your task execution'
      default:
        return 'Please select a preset to continue'
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="relative bg-white dark:bg-slate-800 rounded-xl border border-gray-200 dark:border-slate-700 p-8 max-w-2xl mx-4 shadow-xl">
        {/* Header */}
        <div className="text-center mb-6">
          <div className="flex items-center justify-center w-16 h-16 bg-blue-50 dark:bg-blue-900/20 rounded-xl mb-4 mx-auto">
            {getModeIcon()}
          </div>
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">
            {getModeTitle()}
          </h2>
          <p className="text-gray-600 dark:text-gray-400">
            {getModeDescription()}
          </p>
        </div>

        {/* Preset List */}
        <div className="mb-6">
          {presets.length === 0 ? (
            <div className="text-center py-8">
              <Folder className="w-12 h-12 text-gray-400 mx-auto mb-4" />
              <p className="text-gray-500 dark:text-gray-400 mb-4">
                No presets available yet
              </p>
              <button
                onClick={handleCreateNew}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors flex items-center gap-2 mx-auto"
              >
                <Plus className="w-4 h-4" />
                Create Your First Preset
              </button>
            </div>
          ) : (
            <div className="space-y-2 max-h-64 overflow-y-auto">
              {presets.map((preset) => (
                <button
                  key={preset.id}
                  onClick={() => handlePresetSelect(preset.id)}
                  className={`w-full text-left p-4 rounded-lg border transition-colors ${
                    selectedPresetId === preset.id
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                      : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <div className={`w-4 h-4 rounded-full border-2 flex items-center justify-center ${
                        selectedPresetId === preset.id
                          ? 'border-blue-500 bg-blue-500'
                          : 'border-gray-300 dark:border-gray-600'
                      }`}>
                        {selectedPresetId === preset.id && (
                          <Check className="w-2.5 h-2.5 text-white" />
                        )}
                      </div>
                      <div>
                        <div className="font-medium text-gray-900 dark:text-white">
                          {preset.label}
                        </div>
                        <div className="text-sm text-gray-600 dark:text-gray-400 mt-1">
                          {preset.query.length > 100 ? `${preset.query.substring(0, 100)}...` : preset.query}
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          {preset.selectedFolder ? `Folder: ${preset.selectedFolder.filepath}` : 'No folder selected'}
                        </div>
                      </div>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Action Buttons */}
        <div className="flex gap-3 justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
          >
            Cancel
          </button>
          {presets.length > 0 && (
            <button
              onClick={handleCreateNew}
              className="px-4 py-2 text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/20 rounded-lg transition-colors flex items-center gap-2"
            >
              <Plus className="w-4 h-4" />
              Create New
            </button>
          )}
          {selectedPresetId && (
            <button
              onClick={handleConfirm}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors"
            >
              Select Preset
            </button>
          )}
        </div>
      </div>

      {/* Preset Creation Modal */}
      <PresetModal
        isOpen={isPresetModalOpen}
        onClose={handlePresetModalClose}
        onSave={handlePresetSave}
        editingPreset={null}
        availableServers={[]}
        hideAgentModeSelection={true}
        fixedAgentMode={modeCategory === 'deep-research' ? 'orchestrator' : 'workflow'}
      />
    </div>
  )
}
