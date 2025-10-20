import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Checkbox } from './ui/checkbox';
import { Folder, Plus, X, Settings } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig } from '../services/api-types';
import { useLLMStore } from '../stores/useLLMStore';
import LLMSelectionDropdown from './LLMSelectionDropdown';
import type { LLMOption } from '../types/llm';

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig) => void;
  editingPreset?: CustomPreset | null;
  availableServers?: string[];
  hideAgentModeSelection?: boolean;
  fixedAgentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
}

const PresetModal: React.FC<PresetModalProps> = React.memo(({
  isOpen,
  onClose,
  onSave,
  editingPreset,
  availableServers = [],
  hideAgentModeSelection = false,
  fixedAgentMode,
}) => {
  const [label, setLabel] = useState('');
  const [query, setQuery] = useState('');
  const [selectedServers, setSelectedServers] = useState<string[]>([]);
  const [agentMode, setAgentMode] = useState<'simple' | 'ReAct' | 'orchestrator' | 'workflow'>('ReAct');
  const [selectedFolder, setSelectedFolder] = useState<PlannerFile | null>(null);
  const [showFolderDialog, setShowFolderDialog] = useState(false);
  const [folderDialogPosition, setFolderDialogPosition] = useState({ top: 0, left: 0 });
  const [llmConfig, setLlmConfig] = useState<PresetLLMConfig | null>(null);

  // Store subscriptions - using selectors for stable references
  const primaryConfig = useLLMStore(state => state.primaryConfig);
  const availableLLMs = useLLMStore(state => state.availableLLMs);
  const getCurrentLLMOption = useLLMStore(state => state.getCurrentLLMOption);
  const refreshAvailableLLMs = useLLMStore(state => state.refreshAvailableLLMs);

  // Calculate effective agent mode that always honors fixedAgentMode when provided
  const effectiveAgentMode = fixedAgentMode || agentMode;

  // LLM selection handler - updates local preset LLM config
  const handleLLMSelect = useCallback((llm: LLMOption) => {
    setLlmConfig({
      provider: llm.provider as 'openrouter' | 'bedrock' | 'openai',
      model_id: llm.model
    });
  }, []);

  // Get current LLM option for display
  const currentLLMOption = useMemo(() => {
    if (llmConfig) {
      // Find the matching LLM option from available LLMs
      const matchingLLM = availableLLMs.find(llm => 
        llm.provider === llmConfig.provider && llm.model === llmConfig.model_id
      );
      return matchingLLM || null;
    }
    return getCurrentLLMOption();
  }, [llmConfig, availableLLMs, getCurrentLLMOption]);

  useEffect(() => {
    if (editingPreset) {
      setLabel(editingPreset.label);
      setQuery(editingPreset.query);
      setSelectedServers(editingPreset.selectedServers || []);
      setAgentMode(editingPreset.agentMode || 'ReAct');
      setSelectedFolder(editingPreset.selectedFolder || null);
      setLlmConfig(editingPreset.llmConfig || {
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      });
    } else {
      setLabel('');
      setQuery('');
      setSelectedServers([]);
      setAgentMode(fixedAgentMode || 'ReAct');
      setSelectedFolder(null);
      // Initialize LLM config from current primary config
      setLlmConfig({
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      });
    }
  }, [editingPreset, fixedAgentMode, primaryConfig]);

  const handleServerToggle = useCallback((server: string) => {
    setSelectedServers(prev => 
      prev.includes(server)
        ? prev.filter(s => s !== server)
        : [...prev, server]
    );
  }, []);

  const handleSelectFolders = useCallback((e: React.MouseEvent) => {
    const rect = e.currentTarget.getBoundingClientRect();
    setFolderDialogPosition({
      top: rect.bottom + window.scrollY,
      left: rect.left + window.scrollX
    });
    setShowFolderDialog(true);
  }, []);

  const handleFolderSelect = useCallback((folder: PlannerFile) => {
    setSelectedFolder(folder);
    setShowFolderDialog(false);
  }, []);

  const handleRemoveFolder = useCallback(() => {
    setSelectedFolder(null);
  }, []);

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault();
    if (label.trim() && query.trim()) {
      if ((effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder) {
        alert('Folder selection is required for Deep Search and workflow presets');
        return;
      }
      // Use the local LLM config (either from editing preset or user selection)
      onSave(label.trim(), query.trim(), selectedServers, effectiveAgentMode, selectedFolder || undefined, llmConfig || undefined);
      onClose();
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, llmConfig, onSave, onClose]);

  // Close modal on escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleEscape);
      return () => document.removeEventListener('keydown', handleEscape);
    }
  }, [isOpen, onClose]);

  // Memoized backdrop click handler
  const handleBackdropClick = useCallback((e: React.MouseEvent) => {
    // Only close if clicking on the backdrop, not on the card
    if (e.target === e.currentTarget) {
      onClose();
    }
  }, [onClose]);

  if (!isOpen) return null;

  return (
    <div 
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      onClick={handleBackdropClick}
    >
      <Card 
        className="w-full max-w-6xl mx-4 p-6 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex justify-between items-center mb-6">
          <h2 className="text-2xl font-semibold">
            {editingPreset ? 'Edit Preset' : 'Add New Preset'}
          </h2>
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              form="preset-form"
              variant="outline"
              size="sm"
              disabled={!label.trim() || !query.trim() || ((effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder)}
            >
              {editingPreset ? 'Update' : 'Save'} Preset
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onClose}
            >
              ✕
            </Button>
          </div>
        </div>

        <form id="preset-form" onSubmit={handleSubmit} className="space-y-6">
          {/* Two Column Layout */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Left Column - Preset Name and Query */}
            <div className="space-y-4">
              <div>
                <label htmlFor="preset-label" className="block text-sm font-medium mb-2">
                  Preset Name
                </label>
                <Input
                  id="preset-label"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  placeholder="Enter preset name..."
                  required
                />
              </div>

              <div>
                <label htmlFor="preset-query" className="block text-sm font-medium mb-2">
                  Query
                </label>
                <Textarea
                  id="preset-query"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Enter your query..."
                  rows={24}
                  required
                  className="resize-none"
                />
              </div>
            </div>

            {/* Right Column - Configuration Options */}
            <div className="space-y-4">
              {/* LLM Configuration */}
              <div>
                <label className="block text-sm font-medium mb-2 flex items-center gap-2">
                  <Settings className="w-4 h-4" />
                  LLM Configuration
                </label>
                <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                  <div className="space-y-3">
                    <div>
                      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
                        Select LLM for this preset
                      </label>
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={currentLLMOption}
                        onLLMSelect={handleLLMSelect}
                        onRefresh={refreshAvailableLLMs}
                        disabled={false}
                        inModal={true}
                        openDirection="down"
                      />
                    </div>
                    <div className="text-xs text-gray-500">
                      This preset will use the selected LLM configuration
                    </div>
                  </div>
                </div>
              </div>

              {/* Folder Selection */}
              <div>
                <label className="block text-sm font-medium mb-2">
                  Folder {effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow' ? '(Required)' : '(Optional)'} - Attach workspace folder to this preset
                </label>
                <div className="space-y-2">
                  {selectedFolder && (
                    <div className="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                      <div className="flex items-center gap-2">
                        <Folder className="w-4 h-4 text-blue-600" />
                        <span className="text-sm text-gray-900 dark:text-gray-100">{selectedFolder.filepath}</span>
                      </div>
                      <button
                        type="button"
                        onClick={handleRemoveFolder}
                        className="p-1 text-gray-500 hover:text-red-600 transition-colors"
                      >
                        <X className="w-4 h-4" />
                      </button>
                    </div>
                  )}
                  <button
                    type="button"
                    data-folder-button
                    onClick={handleSelectFolders}
                    className={`w-full p-3 border-2 border-dashed rounded-md transition-colors ${
                      (effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder
                        ? 'border-red-300 dark:border-red-600 text-red-500 dark:text-red-400 hover:border-red-500'
                        : 'border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-blue-500'
                    }`}
                  >
                    <div className="flex items-center justify-center gap-2">
                      <Plus className="w-4 h-4" />
                      <span>{selectedFolder ? 'Change Folder' : 'Select Folder'}</span>
                    </div>
                  </button>
                </div>
                {selectedFolder && (
                  <p className="text-xs text-gray-500 mt-1">
                    Selected: {selectedFolder.filepath}
                  </p>
                )}
                {(effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder && (
                  <p className="text-xs text-red-500 mt-1">
                    ⚠️ Folder selection is required for {effectiveAgentMode} presets
                  </p>
                )}
              </div>

              {/* Agent Mode Selection */}
              {!hideAgentModeSelection && (
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Agent Mode
                  </label>
                  <div className="grid grid-cols-2 gap-2">
                    {[
                      { value: 'simple', label: 'Simple', description: 'Ask simple questions' },
                      { value: 'ReAct', label: 'ReAct', description: 'Step-by-step reasoning' },
                      { value: 'orchestrator', label: 'Deep Search', description: 'Multi-step plans' },
                      { value: 'workflow', label: 'Workflow', description: 'Todo-list execution' }
                    ].map((mode) => (
                      <div key={mode.value} className="flex items-center space-x-2">
                        <input
                          type="radio"
                          id={`agent-mode-${mode.value}`}
                          name="agentMode"
                          value={mode.value}
                          checked={agentMode === mode.value}
                          onChange={(e) => setAgentMode(e.target.value as 'simple' | 'ReAct' | 'orchestrator' | 'workflow')}
                          className="w-4 h-4 text-blue-600 bg-gray-100 border-gray-300 focus:ring-blue-500"
                        />
                        <label
                          htmlFor={`agent-mode-${mode.value}`}
                          className="text-sm cursor-pointer flex-1"
                        >
                          <div className="font-medium">{mode.label}</div>
                          <div className="text-xs text-gray-500">{mode.description}</div>
                        </label>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {hideAgentModeSelection && fixedAgentMode && (
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Agent Mode
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                    <div className="flex items-center gap-2">
                      <div className="font-medium text-gray-900 dark:text-white">
                        {fixedAgentMode === 'simple' ? 'Simple' :
                         fixedAgentMode === 'ReAct' ? 'ReAct' :
                         fixedAgentMode === 'orchestrator' ? 'Deep Search' : 'Workflow'}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {fixedAgentMode === 'simple' ? 'Ask simple questions' :
                         fixedAgentMode === 'ReAct' ? 'Step-by-step reasoning' :
                         fixedAgentMode === 'orchestrator' ? 'Multi-step plans' : 'Todo-list execution'}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {availableServers.length > 0 && (
                <div>
                  <label className="block text-sm font-medium mb-2">
                    MCP Servers (Optional - Leave empty to use all servers)
                  </label>
                  <div className="grid grid-cols-2 gap-2 max-h-40 overflow-y-auto border rounded-md p-3">
                    {availableServers.map((server) => (
                      <div key={server} className="flex items-center space-x-2">
                        <Checkbox
                          id={`server-${server}`}
                          checked={selectedServers.includes(server)}
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
                  {selectedServers.length > 0 && (
                    <p className="text-xs text-gray-500 mt-1">
                      Selected: {selectedServers.join(', ')}
                    </p>
                  )}
                </div>
              )}
            </div>
          </div>
        </form>

        {/* Folder Selection Dialog */}
        <FolderSelectionDialog
          isOpen={showFolderDialog}
          onClose={() => setShowFolderDialog(false)}
          onSelectFolder={handleFolderSelect}
          searchQuery=""
          position={folderDialogPosition}
          agentMode={effectiveAgentMode}
        />
      </Card>
    </div>
  );
});

PresetModal.displayName = 'PresetModal';

export default PresetModal;