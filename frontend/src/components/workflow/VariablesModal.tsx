import React, { useState, useEffect } from 'react';
import { X, Save, Pencil } from 'lucide-react';
import { agentApi } from '../../services/api';
import { MarkdownRenderer } from '../ui/MarkdownRenderer';

interface Variable {
  name: string;
  value: string;
  description: string;
}

interface VariablesModalProps {
  isOpen: boolean;
  onClose: () => void;
  variables: Variable[];
  templatedObjective: string;
  workspacePath: string;
  inline?: boolean; // If true, render without fixed overlay (for sidebar use)
  onExtractAgain?: () => void;
  onUpdateVariables?: () => void;
}

export const VariablesModal: React.FC<VariablesModalProps> = ({
  isOpen,
  onClose,
  variables: initialVariables,
  templatedObjective: initialTemplatedObjective,
  workspacePath,
  inline = false,
  onExtractAgain: _onExtractAgain,
  onUpdateVariables: _onUpdateVariables,
}) => {
  const [variables, setVariables] = useState<Variable[]>(initialVariables);
  const [templatedObjective, setTemplatedObjective] = useState(initialTemplatedObjective);
  const [isSaving, setIsSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isObjectiveEditing, setIsObjectiveEditing] = useState(false);
  const [editingDescriptions, setEditingDescriptions] = useState<Record<number, boolean>>({});

  // Update state when props change (e.g., when new VariablesExtractedEvent is received)
  useEffect(() => {
    setVariables(initialVariables);
    setTemplatedObjective(initialTemplatedObjective);
  }, [initialVariables, initialTemplatedObjective]);

  if (!isOpen) return null;

  const handleVariableChange = (index: number, field: 'name' | 'value' | 'description', value: string) => {
    const updated = [...variables];
    updated[index] = { ...updated[index], [field]: value };
    setVariables(updated);
    setSaveError(null);
  };

  const handleSave = async () => {
    setIsSaving(true);
    setSaveError(null);

    try {
      // Validate that all variable names in objective match existing variables
      const variableNames = variables.map(v => v.name);
      const objectivePlaceholders = templatedObjective.match(/\{\{(\w+)\}\}/g) || [];
      const placeholderNames = objectivePlaceholders.map(p => p.replace(/\{\{|\}\}/g, ''));
      
      // Check if all placeholders have corresponding variables
      const missingVariables = placeholderNames.filter(name => !variableNames.includes(name));
      if (missingVariables.length > 0) {
        setSaveError(`Objective contains variables that don't exist: ${missingVariables.join(', ')}`);
        setIsSaving(false);
        return;
      }

      // Construct variables.json content
      const variablesPath = `${workspacePath}/variables/variables.json`;
      const variablesManifest = {
        objective: templatedObjective,
        variables: variables,
        extraction_date: new Date().toISOString(),
      };

      const content = JSON.stringify(variablesManifest, null, 2);

      // Save to backend
      const response = await agentApi.updatePlannerFile(
        variablesPath,
        content,
        'Updated variables and templated objective'
      );

      if (!response.success) {
        throw new Error(response.message || 'Failed to save variables');
      }

      // Exit edit mode and close modal on success
      setIsObjectiveEditing(false);
      setEditingDescriptions({});
      onClose();
    } catch (error) {
      console.error('Failed to save variables:', error);
      setSaveError(error instanceof Error ? error.message : 'Failed to save variables');
    } finally {
      setIsSaving(false);
    }
  };

  const content = (
    <div className={`${inline ? 'h-full' : 'bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-[95vw] max-h-[95vh]'} flex flex-col`}>
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
            Edit Variables
          </h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          <div className="grid grid-cols-2 gap-6 h-full">
            {/* Left 50%: Templated Objective */}
            <div className="flex flex-col h-full">
              <div className="flex items-center justify-between mb-2 flex-shrink-0">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                  Templated Objective
                </label>
                {!isObjectiveEditing && (
                  <button
                    onClick={() => setIsObjectiveEditing(true)}
                    className="flex items-center gap-1 px-2 py-1 text-xs text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
                    title="Edit objective"
                  >
                    <Pencil className="w-3 h-3" />
                    Edit
                  </button>
                )}
              </div>
              <div className="flex-1 flex flex-col min-h-0" style={{ height: 'calc(100% - 40px)' }}>
                {isObjectiveEditing ? (
                  <div className="flex flex-col h-full">
                    <textarea
                      value={templatedObjective}
                      onChange={(e) => {
                        setTemplatedObjective(e.target.value);
                        setSaveError(null);
                      }}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 font-mono text-sm resize-none"
                      placeholder="Enter objective with {{VARIABLE}} placeholders"
                      style={{ height: 'calc(100% - 30px)' }}
                    />
                    <p className="mt-2 text-xs text-gray-500 dark:text-gray-400 flex-shrink-0">
                      Use {'{{'}VARIABLE_NAME{'}}'} syntax for variable placeholders
                    </p>
                  </div>
                ) : (
                  <div className="flex flex-col h-full border border-gray-200 dark:border-gray-700 rounded-md bg-gray-50 dark:bg-gray-900/50 p-4 overflow-y-auto">
                    <div className="h-full overflow-y-auto">
                      <MarkdownRenderer 
                        content={templatedObjective || '*No content*'} 
                        className="text-sm"
                      />
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* Right 50%: Variables List */}
            <div className="flex flex-col">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Variables ({variables.length})
              </label>
              <div className="flex-1 overflow-y-auto space-y-3 pr-2">
                {variables.map((variable, index) => (
                  <div
                    key={index}
                    className="border border-gray-200 dark:border-gray-700 rounded-md p-4 bg-gray-50 dark:bg-gray-900/50"
                  >
                    <div className="grid grid-cols-1 gap-3">
                      {/* Variable Name */}
                      <div>
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                          Name
                        </label>
                        <input
                          type="text"
                          value={variable.name}
                          readOnly
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 text-sm font-mono cursor-not-allowed"
                          placeholder="VARIABLE_NAME"
                        />
                      </div>

                      {/* Variable Value */}
                      <div>
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                          Value
                        </label>
                        <input
                          type="text"
                          value={variable.value}
                          onChange={(e) => handleVariableChange(index, 'value', e.target.value)}
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm"
                          placeholder="Variable value"
                        />
                      </div>

                      {/* Variable Description */}
                      <div>
                        <div className="flex items-center justify-between mb-1">
                          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                            Description
                          </label>
                          {!editingDescriptions[index] && (
                            <button
                              onClick={() => setEditingDescriptions({ ...editingDescriptions, [index]: true })}
                              className="flex items-center gap-1 px-1.5 py-0.5 text-xs text-gray-500 dark:text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
                              title="Edit description"
                            >
                              <Pencil className="w-3 h-3" />
                            </button>
                          )}
                        </div>
                        {editingDescriptions[index] ? (
                          <textarea
                            value={variable.description}
                            onChange={(e) => handleVariableChange(index, 'description', e.target.value)}
                            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm min-h-[80px]"
                            rows={4}
                            placeholder="Variable description"
                          />
                        ) : (
                          <div className="p-2 border border-gray-200 dark:border-gray-700 rounded-md bg-gray-50 dark:bg-gray-900/30 min-h-[80px]">
                            {variable.description ? (
                              <MarkdownRenderer 
                                content={variable.description} 
                                className="text-xs"
                              />
                            ) : (
                              <p className="text-xs text-gray-400 dark:text-gray-500 italic">No description</p>
                            )}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Error Message */}
          {saveError && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3">
              <p className="text-sm text-red-700 dark:text-red-300">{saveError}</p>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center end gap-3 p-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={() => {
              setIsObjectiveEditing(false);
              setEditingDescriptions({});
              onClose();
            }}
            className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
            disabled={isSaving}
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={isSaving}
            className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 rounded-md transition-colors"
          >
            {isSaving ? (
              <>
                <div className="w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                Saving...
              </>
            ) : (
              <>
                <Save className="w-4 h-4" />
                Save
              </>
            )}
          </button>
        </div>
      </div>
  );

  if (inline) {
    return content;
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 dark:bg-black/70">
      {content}
    </div>
  );
};

