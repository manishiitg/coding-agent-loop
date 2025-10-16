import React, { useState, useEffect, useCallback } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Checkbox } from './ui/checkbox';
import { Folder, Plus, X } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile } from '../services/api-types';

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', selectedFolder?: PlannerFile) => void;
  editingPreset?: CustomPreset | null;
  availableServers?: string[];
  hideAgentModeSelection?: boolean;
  fixedAgentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
}

const PresetModal: React.FC<PresetModalProps> = ({
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

  // Calculate effective agent mode that always honors fixedAgentMode when provided
  const effectiveAgentMode = fixedAgentMode || agentMode;

  useEffect(() => {
    if (editingPreset) {
      setLabel(editingPreset.label);
      setQuery(editingPreset.query);
      setSelectedServers(editingPreset.selectedServers || []);
      setAgentMode(editingPreset.agentMode || 'ReAct');
      setSelectedFolder(editingPreset.selectedFolder || null);
    } else {
      setLabel('');
      setQuery('');
      setSelectedServers([]);
      setAgentMode(fixedAgentMode || 'ReAct');
      setSelectedFolder(null);
    }
  }, [editingPreset, fixedAgentMode]);

  const handleServerToggle = (server: string) => {
    setSelectedServers(prev => 
      prev.includes(server)
        ? prev.filter(s => s !== server)
        : [...prev, server]
    );
  };

  const handleSelectFolders = useCallback(() => {
    // Calculate position for folder dialog
    const button = document.querySelector('[data-folder-button]') as HTMLElement
    if (button) {
      const rect = button.getBoundingClientRect()
      setFolderDialogPosition({
        top: rect.bottom + window.scrollY + 10,
        left: rect.left + window.scrollX
      })
    }
    setShowFolderDialog(true)
  }, [])

  const handleFolderSelect = useCallback((folder: PlannerFile) => {
    // For single folder selection, replace the current folder
    setSelectedFolder(folder)
    setShowFolderDialog(false)
  }, [])

  const handleRemoveFolder = useCallback(() => {
    setSelectedFolder(null)
  }, [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (label.trim() && query.trim()) {
      // For Deep Search and workflow modes, folder selection is mandatory
      if ((effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder) {
        alert('Folder selection is required for Deep Search and workflow presets');
        return;
      }
      
      // Saving preset
      onSave(label.trim(), query.trim(), selectedServers, effectiveAgentMode, selectedFolder || undefined);
      onClose();
    }
  };

  // Keyboard shortcuts
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
      }
      // Enter key is handled by the form's onSubmit
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <Card className="w-full max-w-2xl mx-4 p-6 max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-semibold">
            {editingPreset ? 'Edit Preset' : 'Add New Preset'}
          </h2>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onClose}
          >
            ✕
          </Button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
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

          {/* Folder Selection */}
          <div>
            <label className="block text-sm font-medium mb-2">
              Folder {agentMode === 'orchestrator' || agentMode === 'workflow' ? '(Required)' : '(Optional)'} - Attach workspace folder to this preset
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
                  (agentMode === 'orchestrator' || agentMode === 'workflow') && !selectedFolder
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
            {(agentMode === 'orchestrator' || agentMode === 'workflow') && !selectedFolder && (
              <p className="text-xs text-red-500 mt-1">
                ⚠️ Folder selection is required for {agentMode} presets
              </p>
            )}
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
              rows={8}
              required
            />
          </div>

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

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!label.trim() || !query.trim() || ((effectiveAgentMode === 'orchestrator' || effectiveAgentMode === 'workflow') && !selectedFolder)}
            >
              {editingPreset ? 'Update' : 'Save'} Preset
            </Button>
          </div>
        </form>
      </Card>

      {/* Folder Selection Dialog */}
      <FolderSelectionDialog
        isOpen={showFolderDialog}
        onClose={() => setShowFolderDialog(false)}
        onSelectFolder={handleFolderSelect}
        searchQuery=""
        position={folderDialogPosition}
        agentMode={effectiveAgentMode}
      />
    </div>
  );
};

export default PresetModal; 