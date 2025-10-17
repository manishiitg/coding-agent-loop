import React, { useState, useEffect } from 'react';
import { Button } from './ui/Button';
import { usePresetManagement, usePresetApplication } from '../stores/useGlobalPresetStore';
import PresetModal from './PresetModal';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile } from '../services/api-types';
import { Checkbox } from './ui/checkbox';
import { Card } from './ui/Card';
import { useModeStore } from '../stores/useModeStore';

interface PresetQueriesProps {
  setCurrentQuery: (query: string) => void;
  isStreaming: boolean;
  availableServers?: string[];
  onPresetSelect?: (servers: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow') => void;
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
  
  const {
    customPresets,
    predefinedPresets,
    predefinedServerSelections,
    loading,
    error,
    addPreset,
    updatePreset,
    deletePreset,
    updatePredefinedServerSelection,
    refreshPresets,
  } = usePresetManagement();
  
  const { getPresetsForMode, applyPreset } = usePresetApplication();

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null);
  const [isServerSelectionModalOpen, setIsServerSelectionModalOpen] = useState(false);
  const [currentPredefinedPresetId, setCurrentPredefinedPresetId] = useState<string>('');
  const [tempSelectedServers, setTempSelectedServers] = useState<string[]>([]);

  const handlePresetClick = (query: string, selectedServers?: string[], presetQueryId?: string, agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => {
    // Find the preset object to pass to applyPreset
    const preset = [...customPresets, ...predefinedPresets].find(p => p.id === presetQueryId)
    
    if (preset) {
      // Guard against null/undefined selectedModeCategory and provide safe default
      const safeModeCategory = selectedModeCategory && 
        ['chat', 'deep-research', 'workflow'].includes(selectedModeCategory) 
        ? selectedModeCategory as 'chat' | 'deep-research' | 'workflow'
        : 'chat' // Safe default for initial setup or invalid values
      
      // Use the global store's applyPreset method for consistency
      const result = applyPreset(preset, safeModeCategory)
      
      if (result.success) {
        // Also call the legacy callbacks for backward compatibility
        setCurrentQuery(query);
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

  const handleAddPreset = () => {
    setEditingPreset(null);
    setIsModalOpen(true);
  };

  // Handle trigger from parent component
  useEffect(() => {
    if (triggerAddPreset) {
      handleAddPreset();
      onAddPresetTriggered?.();
    }
  }, [triggerAddPreset, onAddPresetTriggered]);


  const handleEditPreset = (preset: CustomPreset) => {
    setEditingPreset(preset);
    setIsModalOpen(true);
  };

  const handleDeletePreset = async (id: string) => {
    if (confirm('Are you sure you want to delete this preset?')) {
      await deletePreset(id);
      // Call the callback to refresh workflow presets when a preset is deleted
      setTimeout(() => {
        onPresetAdded?.();
      }, 100);
    }
  };

  const handleSavePreset = async (label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => {
    if (editingPreset) {
      await updatePreset(editingPreset.id, label, query, selectedServers, agentMode, selectedFolder);
      // Call the callback to refresh workflow presets when a preset is updated
      setTimeout(() => {
        onPresetAdded?.();
      }, 100);
    } else {
      await addPreset(label, query, selectedServers, agentMode, selectedFolder);
      // Add a small delay to ensure the preset is fully processed
      setTimeout(() => {
        onPresetAdded?.();
      }, 100);
    }
  };

  const handlePredefinedServerSelection = (presetId: string, selectedServers: string[]) => {
    updatePredefinedServerSelection(presetId, selectedServers);
  };

  const handleOpenServerSelection = (presetId: string) => {
    setCurrentPredefinedPresetId(presetId);
    setTempSelectedServers(predefinedServerSelections[presetId] || []);
    setIsServerSelectionModalOpen(true);
  };

  const handleSaveServerSelection = () => {
    handlePredefinedServerSelection(currentPredefinedPresetId, tempSelectedServers);
    setIsServerSelectionModalOpen(false);
  };

  const handleServerToggle = (server: string) => {
    setTempSelectedServers(prev => 
      prev.includes(server)
        ? prev.filter(s => s !== server)
        : [...prev, server]
    );
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
        {/* Predefined Presets - Filtered by current mode */}
        {(selectedModeCategory
          ? getPresetsForMode(selectedModeCategory)
          : [])
          .filter(preset => predefinedPresets.some(pp => pp.id === preset.id))
          .map((preset) => {
          const selectedServers = predefinedServerSelections[preset.id] || [];
          return (
            <div key={preset.id} className="relative group">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={isStreaming}
                onClick={() => handlePresetClick(preset.query, selectedServers, preset.id, preset.agentMode, preset.selectedFolder)}
                className="pr-10"
              >
                <div className="flex items-center gap-2">
                  <span>{preset.label}</span>
                  {preset.agentMode && (
                    <span className="text-xs bg-purple-100 text-purple-800 px-1 rounded">
                      {preset.agentMode}
                    </span>
                  )}
                  {selectedServers.length > 0 && (
                    <span className="text-xs bg-blue-100 text-blue-800 px-1 rounded">
                      {selectedServers.length}
                    </span>
                  )}
                </div>
              </Button>
              {/* Server Selection Button as sibling overlay */}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  handleOpenServerSelection(preset.id);
                }}
                className="absolute right-1 top-1/2 transform -translate-y-1/2 w-4 h-4 flex items-center justify-center text-xs hover:bg-gray-200 rounded"
                title={selectedServers.length > 0 
                  ? `Selected servers: ${selectedServers.join(', ')}` 
                  : 'Click to select servers'
                }
              >
                {selectedServers.length > 0 ? '🔧' : '⚙️'}
              </button>
            </div>
          );
        })}

        {/* Custom Presets - Filtered by current mode */}
        {(selectedModeCategory
          ? getPresetsForMode(selectedModeCategory)
          : [])
          .filter(preset => customPresets.some(cp => cp.id === preset.id))
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
            {/* Edit/Delete Buttons as sibling overlay */}
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
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  handleDeletePreset(preset.id);
                }}
                className="w-4 h-4 flex items-center justify-center text-xs text-red-600 hover:text-red-700 hover:bg-red-100 rounded"
                title="Delete preset"
              >
                🗑️
              </button>
            </div>
          </div>
        ))}

      </div>

      {/* Preset Modal */}
      <PresetModal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        onSave={handleSavePreset}
        editingPreset={editingPreset}
        availableServers={availableServers}
      />

      {/* Server Selection Modal for Predefined Presets */}
      {isServerSelectionModalOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <Card className="w-full max-w-md mx-4 p-6">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-semibold">
                Select Servers for "{predefinedPresets.find(p => p.id === currentPredefinedPresetId)?.label || 'Unknown Preset'}"
              </h2>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsServerSelectionModalOpen(false)}
              >
                ✕
              </Button>
            </div>

            <div className="space-y-4">
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Choose which MCP servers to use with this preset. Leave empty to use all available servers.
              </p>

              <div className="grid grid-cols-1 gap-2 max-h-60 overflow-y-auto border rounded-md p-3">
                {availableServers.map((server) => (
                  <div key={server} className="flex items-center space-x-2">
                    <Checkbox
                      id={`server-${server}`}
                      checked={tempSelectedServers.includes(server)}
                      onCheckedChange={() => handleServerToggle(server)}
                    />
                    <label
                      htmlFor={`server-${server}`}
                      className="text-sm cursor-pointer"
                    >
                      {server}
                    </label>
                  </div>
                ))}
              </div>

              {tempSelectedServers.length > 0 && (
                <p className="text-xs text-gray-500">
                  Selected: {tempSelectedServers.join(', ')}
                </p>
              )}

              <div className="flex justify-end space-x-2 pt-4">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setIsServerSelectionModalOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  type="button"
                  onClick={handleSaveServerSelection}
                >
                  Save Selection
                </Button>
              </div>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
};

export default PresetQueries; 