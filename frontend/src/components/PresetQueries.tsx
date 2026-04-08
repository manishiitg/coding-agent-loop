import React, { useState, useEffect, useCallback } from 'react';
import { Button } from './ui/Button';
import { usePresetManagement, usePresetApplication } from '../stores/useGlobalPresetStore';
import PresetModal from './PresetModal';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig } from '../services/api-types';
import { useModeStore } from '../stores/useModeStore';
import { useAppStore } from '../stores/useAppStore';

interface PresetQueriesProps {
  setCurrentQuery: (query: string) => void;
  isStreaming: boolean;
  availableServers?: string[];
  onPresetSelect?: (servers: string[], agentMode?: 'simple' | 'workflow') => void;
  onPresetFolderSelect?: (folderPath?: string) => void;
  triggerAddPreset?: boolean;
  onAddPresetTriggered?: () => void;
  onPresetAdded?: () => void;
}

  const PresetQueries: React.FC<PresetQueriesProps> = ({ 
    setCurrentQuery, 
    isStreaming, 
    availableServers = [],
    onPresetSelect,
    onPresetFolderSelect,
    triggerAddPreset,
    onAddPresetTriggered,
    onPresetAdded,
  }) => {
  const { selectedModeCategory } = useModeStore();
  const { setWorkspaceMinimized } = useAppStore();
  
  const {
    workflowPresets,
    loading,
    error,
    savePreset,
    refreshPresets,
  } = usePresetManagement();
  
  const { getPresetsForMode, applyPreset } = usePresetApplication();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null);

  const handlePresetClick = (query: string | undefined, selectedServers?: string[], presetQueryId?: string, agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile) => {
    // Find the preset object in workflow presets
    const preset = workflowPresets.find(p => p.id === presetQueryId)
    
    if (preset) {
      // Guard against null/undefined selectedModeCategory and provide safe default
      const safeModeCategory = selectedModeCategory && 
        ['multi-agent', 'workflow'].includes(selectedModeCategory)
        ? selectedModeCategory as 'multi-agent' | 'workflow'
        : 'multi-agent' // Safe default for initial setup or invalid values
      
      // Use the global store's applyPreset method for consistency
      const result = applyPreset(preset, safeModeCategory)
      
      if (result.success) {
        // Also call the legacy callbacks for backward compatibility
        // Only set query if it exists
        if (query) {
          setCurrentQuery(query);
        }
        if (selectedServers && selectedServers.length > 0) {
          onPresetSelect?.(selectedServers, agentMode);
        } else {
          onPresetSelect?.([], agentMode);
        }
        onPresetFolderSelect?.(selectedFolder?.filepath);
      } else {
        console.error('Failed to apply preset:', result.error)
      }
    } else {
      console.error('Preset not found:', presetQueryId)
    }
  };

  const handleAddPreset = useCallback(() => {
    setEditingPreset(null);
    setIsModalOpen(true);
    setWorkspaceMinimized(true);
  }, [setWorkspaceMinimized]);

  // Handle trigger from parent component
  useEffect(() => {
    if (triggerAddPreset) {
      handleAddPreset();
      onAddPresetTriggered?.();
    }
  }, [triggerAddPreset, onAddPresetTriggered, handleAddPreset]);


  const handleEditPreset = (preset: CustomPreset) => {
    setEditingPreset(preset);
    setIsModalOpen(true);
    setWorkspaceMinimized(true);
  };

  // Memoized callback for closing the modal
  const handleCloseModal = useCallback(() => {
    setIsModalOpen(false);
  }, []);

  const handleSavePreset = async (label: string, query: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, enableBrowserAccess?: boolean, selectedSecrets?: string[], selectedGlobalSecretNames?: string[] | null, camofoxHeaded?: boolean, browserMode?: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth') => {
    console.log('[code_execution] [PresetQueries] handleSavePreset called with:', {
      label,
      editingPreset: editingPreset?.id,
      useCodeExecutionMode,
      selectedSkills,
      type: typeof useCodeExecutionMode,
      enableBrowserAccess
    })

    try {
      // Use consolidated savePreset function - pass id if editing, undefined if creating
      await savePreset(
        label,
        query,
        selectedServers,
        selectedTools,
        selectedSkills, // Skill folder names for workflow
        agentMode,
        selectedFolder,
        llmConfig,
        useCodeExecutionMode,
        editingPreset?.id, // Pass id if editing, undefined if creating
        enableContextSummarization,
        enableBrowserAccess,
        undefined, // enableContextEditing
        selectedSecrets,
        selectedGlobalSecretNames,
        camofoxHeaded,
        browserMode
      );

      // Call the callback to refresh workflow presets when a preset is saved
      setTimeout(() => {
        onPresetAdded?.();
      }, 100);
      
      setIsModalOpen(false);
      setEditingPreset(null);
    } catch (error) {
      console.error('[code_execution] [PresetQueries] Failed to save preset:', error);
    }
  };

  return (
    <div className="flex-shrink-0 mb-4">
      {/* Loading and Error States */}
      {loading && (
        <div className="text-xs text-gray-500 dark:text-gray-400 text-center py-2">
          Loading presets...
        </div>
      )}
      
      {error && (
        <div className="text-xs text-red-500 dark:text-red-400 text-center py-2">
          {error}
          <button 
            onClick={refreshPresets}
            className="ml-2 text-blue-500 hover:text-blue-700 underline"
          >
            Retry
          </button>
        </div>
      )}

      <div className="flex flex-wrap gap-3">
        {/* Workflow Presets */}
        {(selectedModeCategory
          ? getPresetsForMode(selectedModeCategory)
          : [])
          .map((preset) => (
          <div key={preset.id} className="relative group">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={isStreaming}
              onClick={() => handlePresetClick(preset.query, preset.selectedServers, preset.id, preset.agentMode, preset.selectedFolder)}
              className="pr-12"
            >
              <div className="flex items-center gap-2">
                <span>{preset.label}</span>
                {preset.agentMode && (
                  <span className="text-xs bg-purple-100 text-purple-800 px-1 rounded">
                    {preset.agentMode}
                  </span>
                )}
                {preset.selectedServers && preset.selectedServers.length > 0 && (
                  <span className="text-xs bg-green-100 text-green-800 px-1 rounded">
                    {preset.selectedServers.length}
                  </span>
                )}
              </div>
            </Button>
            {/* Edit Button */}
            <div className="absolute right-1 top-1/2 transform -translate-y-1/2 flex gap-1">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  handleEditPreset(preset as CustomPreset);
                }}
                className="w-4 h-4 flex items-center justify-center text-xs hover:bg-gray-200 rounded"
                title="Edit preset"
              >
                ✏️
              </button>
            </div>
          </div>
        ))}

      </div>

      {/* Preset Modal */}
      <PresetModal
        isOpen={isModalOpen}
        onClose={handleCloseModal}
        onSave={handleSavePreset}
        editingPreset={editingPreset}
        availableServers={availableServers}
        agentMode={selectedModeCategory === 'workflow' ? 'workflow' : 'simple'}
      />

    </div>
  );
};

export default PresetQueries; 
