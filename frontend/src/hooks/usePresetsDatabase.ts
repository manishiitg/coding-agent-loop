import { useState, useEffect, useCallback } from 'react';
import { agentApi } from '../services/api';
import type { PresetQuery, CreatePresetQueryRequest, UpdatePresetQueryRequest, PlannerFile } from '../services/api-types';

export interface CustomPreset {
  id: string;
  label: string;
  query: string;
  createdAt: number;
  selectedServers?: string[];
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
  selectedFolder?: PlannerFile; // Single folder
}

export interface PredefinedPreset {
  label: string;
  query: string;
  selectedServers?: string[];
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
}

// Helper function to convert PresetQuery to CustomPreset
const convertToCustomPreset = (preset: PresetQuery): CustomPreset => {
  let selectedServers: string[] = [];
  let selectedFolder: PlannerFile | undefined;
  
  try {
    if (preset.selected_servers) {
      selectedServers = JSON.parse(preset.selected_servers);
    }
  } catch (error) {
    console.error('[PRESET] Error parsing selected servers:', error);
  }

  // Convert single folder path to PlannerFile
  if (preset.selected_folder) {
    selectedFolder = {
      filepath: preset.selected_folder,
      content: '', // Empty content for folders
      last_modified: '', // Empty for folders
      type: 'folder' as const,
      children: []
    };
  }

  return {
    id: preset.id,
    label: preset.label,
    query: preset.query,
    createdAt: new Date(preset.created_at).getTime(),
    selectedServers,
    selectedFolder,
    agentMode: preset.agent_mode as 'simple' | 'ReAct' | 'orchestrator' | 'workflow' | undefined,
  };
};


export const usePresetsDatabase = () => {
  const [customPresets, setCustomPresets] = useState<CustomPreset[]>([]);
  const [predefinedServerSelections, setPredefinedServerSelections] = useState<Record<string, string[]>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load presets from database
  const loadPresets = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await agentApi.getPresetQueries(100, 0); // Load up to 100 presets
      
      const customPresets = (response.presets || [])
        .filter(preset => !preset.is_predefined)
        .map(convertToCustomPreset);
      
      setCustomPresets(customPresets);
    } catch (err) {
      console.error('[PRESET] Error loading presets:', err);
      setError(err instanceof Error ? err.message : 'Failed to load presets');
    } finally {
      setLoading(false);
    }
  }, []);

  // Load predefined presets from database
  const [predefinedPresets, setPredefinedPresets] = useState<CustomPreset[]>([]);
  
  const loadPredefinedPresets = useCallback(async () => {
    try {
      const response = await agentApi.getPresetQueries(100, 0);
      const predefinedPresets = (response.presets || [])
        .filter(preset => preset.is_predefined)
        .map(convertToCustomPreset);
      setPredefinedPresets(predefinedPresets);
    } catch (err) {
      console.error('[USE_PRESETS_DATABASE] Error loading predefined presets:', err);
    }
  }, []);

  // Load predefined presets on mount
  useEffect(() => {
    loadPredefinedPresets();
  }, [loadPredefinedPresets]);

  // Load presets on mount
  useEffect(() => {
    loadPresets();
  }, [loadPresets]);

  // Load predefined server selections from localStorage (for now)
  useEffect(() => {
    const loadServerSelections = () => {
      try {
        const stored = localStorage.getItem('mcp-agent-predefined-servers');
        if (stored) {
          const selections = JSON.parse(stored);
          setPredefinedServerSelections(selections);
        }
      } catch (error) {
        console.error('[USE_PRESETS_DATABASE] Error loading server selections:', error);
      }
    };
    loadServerSelections();
  }, []);

  // Save server selections to localStorage (for now)
  useEffect(() => {
    localStorage.setItem('mcp-agent-predefined-servers', JSON.stringify(predefinedServerSelections));
  }, [predefinedServerSelections]);

  const addPreset = async (label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile): Promise<CustomPreset> => {
    // Adding preset
    setLoading(true);
    setError(null);
    
    try {
      // Convert single PlannerFile to string for API
      const folderPath = selectedFolder?.filepath || '';
      
      const request: CreatePresetQueryRequest = {
        label,
        query,
        selected_servers: selectedServers,
        selected_folder: folderPath,
        agent_mode: agentMode,
        is_predefined: false,
      };
      
      const newPreset = await agentApi.createPresetQuery(request);
      const customPreset = convertToCustomPreset(newPreset);
      
      setCustomPresets(prev => [...prev, customPreset]);
      return customPreset;
    } catch (err) {
      console.error('[USE_PRESETS_DATABASE] Error adding preset:', err);
      setError(err instanceof Error ? err.message : 'Failed to add preset');
      throw err;
    } finally {
      setLoading(false);
    }
  };

  const updatePreset = async (id: string, label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => {
    // Updating preset
    setLoading(true);
    setError(null);
    
    try {
      // Convert single PlannerFile to string for API
      const folderPath = selectedFolder?.filepath || '';
      
      const request: UpdatePresetQueryRequest = {
        label,
        query,
        selected_servers: selectedServers,
        selected_folder: folderPath,
        agent_mode: agentMode,
      };
      
      const updatedPreset = await agentApi.updatePresetQuery(id, request);
      const customPreset = convertToCustomPreset(updatedPreset);
      
      setCustomPresets(prev => 
        prev.map(preset => 
          preset.id === id ? customPreset : preset
        )
      );
    } catch (err) {
      console.error('[USE_PRESETS_DATABASE] Error updating preset:', err);
      setError(err instanceof Error ? err.message : 'Failed to update preset');
    } finally {
      setLoading(false);
    }
  };

  const deletePreset = async (id: string) => {
    // Deleting preset
    setLoading(true);
    setError(null);
    
    try {
      await agentApi.deletePresetQuery(id);
      setCustomPresets(prev => prev.filter(preset => preset.id !== id));
    } catch (err) {
      console.error('[USE_PRESETS_DATABASE] Error deleting preset:', err);
      setError(err instanceof Error ? err.message : 'Failed to delete preset');
    } finally {
      setLoading(false);
    }
  };

  const updatePredefinedServerSelection = (presetLabel: string, selectedServers: string[]) => {
    // Updating server selection
    setPredefinedServerSelections(prev => ({
      ...prev,
      [presetLabel]: selectedServers,
    }));
  };

  const refreshPresets = () => {
    loadPresets();
  };

  return {
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
  };
};
