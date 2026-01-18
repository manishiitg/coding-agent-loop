import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Folder, Plus, X, Settings, Sparkles, Code2, Info, Search } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import { ToolSelectionSection } from './ToolSelectionSection';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig, AgentLLMConfig } from '../services/api-types';
import { useLLMStore } from '../stores/useLLMStore';
import { useModeStore } from '../stores/useModeStore';
import LLMSelectionDropdown from './LLMSelectionDropdown';
import type { LLMOption } from '../types/llm';

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, useToolSearchMode?: boolean) => void;
  editingPreset?: CustomPreset | null;
  availableServers?: string[];
  hideAgentModeSelection?: boolean;
  fixedAgentMode?: 'simple' | 'workflow';
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
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [agentMode, setAgentMode] = useState<'simple' | 'workflow'>('simple');
  const [selectedFolder, setSelectedFolder] = useState<PlannerFile | null>(null);
  const [showFolderDialog, setShowFolderDialog] = useState(false);
  const [folderDialogPosition, setFolderDialogPosition] = useState({ top: 0, left: 0 });
  const [llmConfig, setLlmConfig] = useState<PresetLLMConfig | null>(null);
  const [useCodeExecutionMode, setUseCodeExecutionMode] = useState(false);
  const [useToolSearchMode, setUseToolSearchMode] = useState(false);
  const [enableContextSummarization, setEnableContextSummarization] = useState(true);
  const [useKnowledgebase, setUseKnowledgebase] = useState(true); // Default true (enabled)
  // Agent-specific LLM configs (for workflow mode)
  const [executionLLM, setExecutionLLM] = useState<AgentLLMConfig | null>(null);
  const [validationLLM, setValidationLLM] = useState<AgentLLMConfig | null>(null);
  const [learningLLM, setLearningLLM] = useState<AgentLLMConfig | null>(null);
  const [phaseLLM, setPhaseLLM] = useState<AgentLLMConfig | null>(null);

  // Store subscriptions - using selectors for stable references
  const primaryConfig = useLLMStore(state => state.primaryConfig);
  const availableLLMs = useLLMStore(state => state.availableLLMs);
  const getCurrentLLMOption = useLLMStore(state => state.getCurrentLLMOption);
  const refreshAvailableLLMs = useLLMStore(state => state.refreshAvailableLLMs);
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore();

  // Calculate effective agent mode that always honors fixedAgentMode when provided
  // This ensures workflow presets only show Workflow/ folders in the folder selection dialog
  const effectiveAgentMode = fixedAgentMode || agentMode;

  // Helper to manage execution modes (mutually exclusive in UI for simplicity)
  const setExecutionMode = useCallback((mode: 'simple' | 'code' | 'search') => {
    if (mode === 'code') {
      setUseCodeExecutionMode(true);
      setUseToolSearchMode(false);
    } else if (mode === 'search') {
      setUseCodeExecutionMode(false);
      setUseToolSearchMode(true);
    } else {
      // simple
      setUseCodeExecutionMode(false);
      setUseToolSearchMode(false);
    }
  }, []);

  // LLM selection handler - updates local preset LLM config
  const handleLLMSelect = useCallback((llm: LLMOption) => {
    setLlmConfig({
      provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex',
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
      console.log('[PresetModal] Loading preset:', editingPreset);
      console.log('[PresetModal] Selected tools from preset:', editingPreset.selectedTools);
      setLabel(editingPreset.label);
      setQuery(editingPreset.query);
      setSelectedServers(editingPreset.selectedServers || []);
      setSelectedTools(editingPreset.selectedTools || []); // NEW
      setAgentMode(editingPreset.agentMode || 'simple');
      setSelectedFolder(editingPreset.selectedFolder || null);
      const presetLLM = editingPreset.llmConfig || {
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      };
      setLlmConfig(presetLLM);
      setUseCodeExecutionMode(editingPreset.useCodeExecutionMode || false);
      setUseToolSearchMode(editingPreset.useToolSearchMode || false);
      setEnableContextSummarization(editingPreset.enableContextSummarization !== undefined ? editingPreset.enableContextSummarization : true);
      setUseKnowledgebase(presetLLM.use_knowledgebase !== false); // Default true unless explicitly false
      // Load agent-specific configs if available
      setExecutionLLM(presetLLM.execution_llm || null);
      setValidationLLM(presetLLM.validation_llm || null);
      setLearningLLM(presetLLM.learning_llm || null);
      setPhaseLLM(presetLLM.phase_llm || null);
    } else {
      setLabel('');
      setQuery('');
      setSelectedServers([]);
      setSelectedTools([]); // NEW
      // Default to current mode if no fixedAgentMode is provided
      const defaultMode = fixedAgentMode || (selectedModeCategory ? (getAgentModeFromCategory(selectedModeCategory) as 'simple' | 'workflow') : 'simple');
      setAgentMode(defaultMode);
      setSelectedFolder(null);
      // Initialize LLM config from current primary config
      const defaultLLM = {
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      };
      setLlmConfig(defaultLLM);
      setUseCodeExecutionMode(false);
      setUseToolSearchMode(false);
      setEnableContextSummarization(true);
      setUseKnowledgebase(true); // Default true
      // Initialize agent-specific configs to null (will use legacy default)
      setExecutionLLM(null);
      setValidationLLM(null);
      setLearningLLM(null);
      setPhaseLLM(null);
    }
  }, [editingPreset, fixedAgentMode, primaryConfig, selectedModeCategory, getAgentModeFromCategory]);

  const handleSelectFolders = useCallback((e: React.MouseEvent) => {
    const rect = e.currentTarget.getBoundingClientRect();
    // Estimate dialog height (max-h-80 = 320px + some padding)
    const estimatedDialogHeight = 320;
    const spaceAbove = rect.top + window.scrollY;
    
    // Always try to position above the button so contents are visible
    // Fallback to below only if there's not enough space above
    const minSpaceNeeded = 200; // Minimum space needed above
    const shouldPositionAbove = spaceAbove >= minSpaceNeeded;
    
    setFolderDialogPosition({
      top: shouldPositionAbove 
        ? rect.top + window.scrollY - estimatedDialogHeight 
        : rect.bottom + window.scrollY,
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
      if (effectiveAgentMode === 'workflow' && !selectedFolder) {
        alert('Folder selection is required for workflow presets');
        return;
      }
      
      // Debug: Log what we're sending
      console.log('[PresetModal] Saving preset with:', {
        selectedServers,
        selectedTools,
        label,
        agentMode: effectiveAgentMode
      });
      
      // Build LLM config with agent-specific defaults for workflow mode
      // Save execution_llm, validation_llm, learning_llm, and phase_llm
      let finalLLMConfig: PresetLLMConfig | undefined = llmConfig || undefined;
      if (effectiveAgentMode === 'workflow') {
        // For workflow mode, always include all 4 agent configs
        // Use the displayed fallback value (from llmConfig) if user didn't explicitly select
        // This ensures the visual selection is saved even if user didn't explicitly click the dropdown
        const defaultAgentLLM: AgentLLMConfig | undefined = llmConfig?.provider && llmConfig?.model_id ? {
          provider: llmConfig.provider,
          model_id: llmConfig.model_id
        } : undefined;

        finalLLMConfig = {
          ...(llmConfig || {}),
          execution_llm: executionLLM || defaultAgentLLM,
          validation_llm: validationLLM || defaultAgentLLM,
          learning_llm: learningLLM || defaultAgentLLM,
          phase_llm: phaseLLM || defaultAgentLLM,
          use_knowledgebase: useKnowledgebase,
        };
      }
      console.log('[PRESET_MODAL] Agent LLM configs being saved:', {
        executionLLM: executionLLM,
        validationLLM: validationLLM,
        learningLLM: learningLLM,
        phaseLLM: phaseLLM,
        defaultAgentLLM: llmConfig?.provider && llmConfig?.model_id ? { provider: llmConfig.provider, model_id: llmConfig.model_id } : undefined,
        finalLLMConfig: finalLLMConfig,
      });
      console.log('[code_execution] [PRESET_MODAL] Saving preset with code execution mode:', {
        useCodeExecutionMode,
        useToolSearchMode,
        type: typeof useCodeExecutionMode,
        label: label.trim(),
        finalLLMConfig: finalLLMConfig ? 'defined' : 'undefined',
        selectedFolder: selectedFolder ? 'defined' : 'undefined'
      })
      
      console.log('[code_execution] [PRESET_MODAL] Calling onSave with all parameters:', {
        param1: label.trim(),
        param2: query.trim(),
        param3: selectedServers,
        param4: selectedTools,
        param5: effectiveAgentMode,
        param6: selectedFolder || undefined,
        param7: finalLLMConfig,
        param8: useCodeExecutionMode,
        param10: useToolSearchMode
      })
      
      // CRITICAL FIX: Always pass useCodeExecutionMode explicitly, even if it's undefined
      // JavaScript can drop trailing undefined parameters, so we ensure it's always a boolean
      const codeExecutionModeToPass = useCodeExecutionMode === undefined ? false : useCodeExecutionMode
      const toolSearchModeToPass = useToolSearchMode === undefined ? false : useToolSearchMode
      
      console.log('[code_execution] [PRESET_MODAL] Final onSave call - param8:', codeExecutionModeToPass, 'original:', useCodeExecutionMode)
      
      onSave(
        label.trim(), 
        query.trim(), 
        selectedServers, 
        selectedTools, 
        effectiveAgentMode, 
        selectedFolder || undefined, 
        finalLLMConfig, 
        codeExecutionModeToPass,  // Always pass explicit boolean, never undefined
        enableContextSummarization,
        toolSearchModeToPass // Always pass explicit boolean
      );
      onClose();
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, selectedTools, llmConfig, executionLLM, validationLLM, learningLLM, phaseLLM, useCodeExecutionMode, useToolSearchMode, enableContextSummarization, useKnowledgebase, onSave, onClose]);

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
              disabled={!label.trim() || !query.trim() || (effectiveAgentMode === 'workflow' && !selectedFolder)}
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
                    {effectiveAgentMode === 'workflow' ? (
                      <>
                        {/* Workflow mode: Show agent-specific LLM selections */}
                        <div>
                          <div className="flex items-center justify-between mb-2">
                            <div className="flex items-center gap-1.5">
                              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                                Execution Agent Default Model
                              </label>
                              <TooltipProvider>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <Info className="w-3.5 h-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                  </TooltipTrigger>
                                  <TooltipContent className="max-w-xs">
                                    <p className="text-xs">
                                      <strong>Execution Agents</strong> execute plan steps by calling MCP tools and performing actions. 
                                      They handle the actual work of your workflow (reading files, making API calls, running commands, etc.).
                                    </p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>
                            </div>
                            {/* Execution Mode Toggle - Only for Execution Agent */}
                            <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                              <TooltipProvider>
                                {/* Simple Mode */}
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      type="button"
                                      onClick={() => setExecutionMode('simple')}
                                      className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                        !useCodeExecutionMode && !useToolSearchMode
                                          ? 'agent-mode-selected'
                                          : 'agent-mode-unselected'
                                      }`}
                                    >
                                      <Sparkles className="w-3 h-3 inline mr-1" />
                                      Simple
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent>
                                    <p>Simple mode - Direct MCP tool access</p>
                                  </TooltipContent>
                                </Tooltip>
                                
                                {/* Code Execution Mode */}
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      type="button"
                                      onClick={() => setExecutionMode('code')}
                                      className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                        useCodeExecutionMode
                                          ? 'agent-mode-selected'
                                          : 'agent-mode-unselected'
                                      }`}
                                    >
                                      <Code2 className="w-3 h-3 inline mr-1" />
                                      Code Exec
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent>
                                    <p>Code Exec mode - MCP tools accessed via generated Go code</p>
                                  </TooltipContent>
                                </Tooltip>

                                {/* Tool Search Mode */}
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      type="button"
                                      onClick={() => setExecutionMode('search')}
                                      className={`px-2 py-1 text-xs font-medium transition-colors ${
                                        useToolSearchMode
                                          ? 'agent-mode-selected'
                                          : 'agent-mode-unselected'
                                      }`}
                                    >
                                      <Search className="w-3 h-3 inline mr-1" />
                                      Tool Search
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent>
                                    <p>Tool Search mode - Dynamic tool discovery. Selected tools become pre-discovered.</p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>
                            </div>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={executionLLM ? availableLLMs.find(llm => 
                              llm.provider === executionLLM.provider && llm.model === executionLLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setExecutionLLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Default model for execution agents (used when step config doesn't specify). 
                            Execution agents perform the actual work - calling tools, reading files, executing commands.
                          </div>
                        </div>
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Validation Agent Default Model
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3.5 h-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">
                                    <strong>Validation Agents</strong> check if step execution succeeded by evaluating success criteria. 
                                    They analyze execution results and determine if the step met its goals (Success/Partial/Failed).
                                  </p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={validationLLM ? availableLLMs.find(llm => 
                              llm.provider === validationLLM.provider && llm.model === validationLLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setValidationLLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Default model for validation agents (used when step config doesn't specify). 
                            Validation agents evaluate execution results and determine if success criteria were met.
                          </div>
                        </div>
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Learning Agent Default Model
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3.5 h-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">
                                    <strong>Learning Agents</strong> extract patterns from successful executions and save them to learnings files. 
                                    These patterns help future executions work better by capturing what worked and what didn't.
                                  </p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={learningLLM ? availableLLMs.find(llm => 
                              llm.provider === learningLLM.provider && llm.model === learningLLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setLearningLLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Default model for learning agents (used when step config doesn't specify). 
                            Learning agents analyze execution history and extract reusable patterns for future runs.
                          </div>
                        </div>
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Phase Agent Default Model
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3.5 h-3.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">
                                    <strong>Phase Agents</strong> handle workflow phases: Planning (creates execution plans), 
                                    Variable Extraction (extracts dynamic values), Anonymization (replaces values with placeholders), 
                                    Plan Improvement (analyzes execution for feedback), Plan Tool Optimization (optimizes tool selections), 
                                    Learning Consolidation (consolidates learning files), and Plan Learnings Alignment (aligns plans with learnings).
                                  </p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={phaseLLM ? availableLLMs.find(llm => 
                              llm.provider === phaseLLM.provider && llm.model === phaseLLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setPhaseLLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Default model for all phase agents (used when step config doesn't specify). 
                            Phase agents handle workflow phases: planning, variable extraction, anonymization, plan improvement, 
                            plan tool optimization, learning consolidation, and plan learnings alignment.
                          </div>
                        </div>
                        <div className="text-xs text-gray-500 pt-2 border-t border-gray-200 dark:border-gray-700">
                          Step-specific configs in step_config.json take priority over these defaults
                        </div>
                      </>
                    ) : (
                      <>
                        {/* Simple mode: Show single LLM selection */}
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
                      </>
                    )}
                  </div>
                </div>
              </div>

              {/* MCP Servers and Tools Selection */}
              {availableServers.length > 0 && (
                <ToolSelectionSection
                  availableServers={availableServers}
                  selectedServers={selectedServers}
                  selectedTools={selectedTools}
                  onServerChange={setSelectedServers}
                  onToolChange={setSelectedTools}
                />
              )}

              {/* Folder Selection */}
              <div>
                <label className="block text-sm font-medium mb-2">
                  Folder {effectiveAgentMode === 'workflow' ? '(Required)' : '(Optional)'} - Attach workspace folder to this preset
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
                      effectiveAgentMode === 'workflow' && !selectedFolder
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
                {effectiveAgentMode === 'workflow' && !selectedFolder && (
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
                      { value: 'workflow', label: 'Workflow', description: 'Todo-list execution' }
                    ].map((mode) => (
                      <div key={mode.value} className="flex items-center space-x-2">
                        <input
                          type="radio"
                          id={`agent-mode-${mode.value}`}
                          name="agentMode"
                          value={mode.value}
                          checked={agentMode === mode.value}
                          onChange={(e) => setAgentMode(e.target.value as 'simple' | 'workflow')}
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
                         'Workflow'}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {fixedAgentMode === 'simple' ? 'Ask simple questions' :
                         'Todo-list execution'}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Context Summarization Toggle */}
              <div>
                <label className="block text-sm font-medium mb-2">
                  Context Summarization
                </label>
                <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                  <div className="flex items-center justify-between">
                    <div className="flex-1">
                      <div className="text-sm font-medium text-gray-900 dark:text-white">
                        Enable Context Summarization
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                        Automatically summarize conversation history when token usage exceeds 80% of context window
                      </div>
                    </div>
                    <label className="relative inline-flex items-center cursor-pointer">
                      <input
                        type="checkbox"
                        checked={enableContextSummarization}
                        onChange={(e) => setEnableContextSummarization(e.target.checked)}
                        className="sr-only peer"
                      />
                      <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                    </label>
                  </div>
                </div>
              </div>

              {/* Knowledgebase Toggle (Workflow mode only) */}
              {effectiveAgentMode === 'workflow' && (
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Knowledgebase
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                    <div className="flex items-center justify-between">
                      <div className="flex-1">
                        <div className="text-sm font-medium text-gray-900 dark:text-white">
                          Enable Knowledgebase
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          Persistent folder shared across all runs for templates, reference data, and configurations
                        </div>
                      </div>
                      <label className="relative inline-flex items-center cursor-pointer">
                        <input
                          type="checkbox"
                          checked={useKnowledgebase}
                          onChange={(e) => setUseKnowledgebase(e.target.checked)}
                          className="sr-only peer"
                        />
                        <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                      </label>
                    </div>
                  </div>
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
          agentMode={effectiveAgentMode as 'simple' | 'workflow'}
        />
      </Card>
    </div>
  );
});

PresetModal.displayName = 'PresetModal';

export default PresetModal;