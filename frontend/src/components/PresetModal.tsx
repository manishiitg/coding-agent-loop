import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Folder, Plus, X, Settings, Sparkles, Code2, Info, Search } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import { ToolSelectionSection } from './ToolSelectionSection';
import { SkillSelectionSection } from './skills/SkillSelectionSection';
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
  onSave: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, useToolSearchMode?: boolean, enableBrowserAccess?: boolean) => void;
  editingPreset?: CustomPreset | null;
  availableServers?: string[];
  hideAgentModeSelection?: boolean;
  fixedAgentMode?: 'simple' | 'workflow';
  agentMode: string;
}

const PresetModal: React.FC<PresetModalProps> = React.memo(({
  isOpen,
  onClose,
  onSave,
  editingPreset,
  availableServers = [],
  hideAgentModeSelection = false,
  fixedAgentMode,
  agentMode: propAgentMode,
}) => {
  const [label, setLabel] = useState('');
  const [query, setQuery] = useState('');
  const [selectedServers, setSelectedServers] = useState<string[]>([]);
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [internalAgentMode, setInternalAgentMode] = useState<'simple' | 'workflow'>('simple');
  const [selectedFolder, setSelectedFolder] = useState<PlannerFile | null>(null);
  const [showFolderDialog, setShowFolderDialog] = useState(false);
  const [folderDialogPosition, setFolderDialogPosition] = useState({ top: 0, left: 0 });
  const [llmConfig, setLlmConfig] = useState<PresetLLMConfig | null>(null);
  const [useCodeExecutionMode, setUseCodeExecutionMode] = useState(false);
  const [useToolSearchMode, setUseToolSearchMode] = useState(false);
  const enableContextSummarization = true;
  const [useKnowledgebase, setUseKnowledgebase] = useState(true);
  const [enableBrowserAccess, setEnableBrowserAccess] = useState(false);
  const [executionLLM, setExecutionLLM] = useState<AgentLLMConfig | null>(null);
  const [validationLLM, setValidationLLM] = useState<AgentLLMConfig | null>(null);
  const [learningLLM, setLearningLLM] = useState<AgentLLMConfig | null>(null);
  const [phaseLLM, setPhaseLLM] = useState<AgentLLMConfig | null>(null);
  const [llmAllocationMode, setLlmAllocationMode] = useState<'manual' | 'tiered'>('manual');
  const [tier1LLM, setTier1LLM] = useState<AgentLLMConfig | null>(null);
  const [tier2LLM, setTier2LLM] = useState<AgentLLMConfig | null>(null);
  const [tier3LLM, setTier3LLM] = useState<AgentLLMConfig | null>(null);

  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore();
  const primaryConfig = useLLMStore(state => state.primaryConfig);
  const availableLLMs = useLLMStore(state => state.availableLLMs);
  const getCurrentLLMOption = useLLMStore(state => state.getCurrentLLMOption);
  const refreshAvailableLLMs = useLLMStore(state => state.refreshAvailableLLMs);

  const effectiveAgentMode = useMemo(() => {
    if (fixedAgentMode) return fixedAgentMode;
    if (propAgentMode) return propAgentMode as 'simple' | 'workflow';
    return internalAgentMode;
  }, [fixedAgentMode, propAgentMode, internalAgentMode]);

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
      provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
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
      console.log('[PresetModal] Selected skills from preset:', editingPreset.selectedSkills);
      setLabel(editingPreset.label);
      setQuery(editingPreset.query || '');
      setSelectedServers(editingPreset.selectedServers || []);
      setSelectedTools(editingPreset.selectedTools || []); // NEW
      setSelectedSkills(editingPreset.selectedSkills || []);
      setInternalAgentMode(editingPreset.agentMode || 'workflow'); // Default to workflow
      setSelectedFolder(editingPreset.selectedFolder || null);
      const presetLLM = editingPreset.llmConfig || {
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      };
      setLlmConfig(presetLLM);
      setUseCodeExecutionMode(editingPreset.useCodeExecutionMode || false);
      // For workflow presets, default to true if not explicitly set
      setUseToolSearchMode(editingPreset.useToolSearchMode !== undefined ? editingPreset.useToolSearchMode : true); // Default true for workflow
      setUseKnowledgebase(presetLLM.use_knowledgebase !== false); // Default true unless explicitly false
      setEnableBrowserAccess(editingPreset?.enableBrowserAccess ?? false); // Default false unless explicitly true
      // Load agent-specific configs if available
      setExecutionLLM(presetLLM.execution_llm || null);
      setValidationLLM(presetLLM.validation_llm || null);
      setLearningLLM(presetLLM.learning_llm || null);
      setPhaseLLM(presetLLM.phase_llm || null);
      // Load tiered LLM allocation config
      setLlmAllocationMode(presetLLM.llm_allocation_mode || 'manual');
      setTier1LLM(presetLLM.tiered_config?.tier_1 || null);
      setTier2LLM(presetLLM.tiered_config?.tier_2 || null);
      setTier3LLM(presetLLM.tiered_config?.tier_3 || null);
    } else {
      setLabel('');
      setQuery('');
      setSelectedServers([]);
      setSelectedTools([]); // NEW
      setSelectedSkills([]);
      // Default to workflow mode as chat presets are disabled
      const defaultMode = 'workflow';
      setInternalAgentMode(defaultMode);
      setSelectedFolder(null);
      // Initialize LLM config from current primary config
      const defaultLLM = {
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id
      };
      setLlmConfig(defaultLLM);
      setUseCodeExecutionMode(false);
      // Default tool search mode to true for workflow presets
      setUseToolSearchMode(true);
      setUseKnowledgebase(true); // Default true
      setEnableBrowserAccess(false); // Default false
      // Initialize agent-specific configs to null (will use legacy default)
      setExecutionLLM(null);
      setValidationLLM(null);
      setLearningLLM(null);
      setPhaseLLM(null);
      // Initialize tiered config
      setLlmAllocationMode('manual');
      setTier1LLM(null);
      setTier2LLM(null);
      setTier3LLM(null);
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
    const isQueryRequired = effectiveAgentMode !== 'workflow';
    if (label.trim() && (!isQueryRequired || query.trim())) {
      if (effectiveAgentMode === 'workflow' && !selectedFolder) {
        alert('Folder selection is required for workflow presets');
        return;
      }
      
      // Debug: Log what we're sending
      console.log('[PresetModal] Saving preset with:', {
        selectedServers,
        selectedTools,
        selectedSkills,
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
          llm_allocation_mode: llmAllocationMode,
          ...(llmAllocationMode === 'tiered' && tier1LLM && tier2LLM && tier3LLM ? {
            tiered_config: {
              tier_1: tier1LLM,
              tier_2: tier2LLM,
              tier_3: tier3LLM,
            }
          } : {}),
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
        effectiveAgentMode === 'workflow' ? '' : query.trim(),
        selectedServers,
        selectedTools,
        selectedSkills, // Skill folder names for workflow
        effectiveAgentMode,
        selectedFolder || undefined,
        finalLLMConfig,
        codeExecutionModeToPass,  // Always pass explicit boolean, never undefined
        enableContextSummarization,
        toolSearchModeToPass, // Always pass explicit boolean
        enableBrowserAccess // Browser automation access
      );
      onClose();
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, selectedTools, selectedSkills, llmConfig, executionLLM, validationLLM, learningLLM, phaseLLM, useCodeExecutionMode, useToolSearchMode, useKnowledgebase, enableBrowserAccess, llmAllocationMode, tier1LLM, tier2LLM, tier3LLM, onSave, onClose]);

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
              disabled={!label.trim() || (effectiveAgentMode !== 'workflow' && !query.trim()) || (effectiveAgentMode === 'workflow' && !selectedFolder)}
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
          {/* Two Column Layout for both modes */}
          {effectiveAgentMode === 'workflow' ? (
            /* Workflow Mode: Two Column Layout with LLM Config on Left */
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              {/* Left Column - Preset Name and LLM Configuration */}
              <div className="space-y-4">
                {/* Preset Name */}
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

                {/* LLM Configuration - in place of Query */}
                <div>
                  <label className="block text-sm font-medium mb-2 flex items-center gap-2">
                    <Settings className="w-4 h-4" />
                    Agent LLM Configuration
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md space-y-4">
                    {/* Execution Mode Selection */}
                    <div className="mb-4">
                      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
                        Execution Mode
                      </label>
                      <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                type="button"
                                onClick={() => setExecutionMode('simple')}
                                className={`flex-1 px-3 py-2 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                  !useCodeExecutionMode && !useToolSearchMode ? 'agent-mode-selected' : 'agent-mode-unselected'
                                }`}
                              >
                                <Sparkles className="w-3.5 h-3.5 inline mr-1.5" />
                                Simple
                              </button>
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="font-medium">Simple Mode</p>
                              <p className="text-xs mt-1">Direct MCP tool access. Agent calls tools directly without code generation.</p>
                            </TooltipContent>
                          </Tooltip>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                type="button"
                                onClick={() => setExecutionMode('code')}
                                className={`flex-1 px-3 py-2 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                  useCodeExecutionMode ? 'agent-mode-selected' : 'agent-mode-unselected'
                                }`}
                              >
                                <Code2 className="w-3.5 h-3.5 inline mr-1.5" />
                                Code Exec
                              </button>
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="font-medium">Code Execution Mode</p>
                              <p className="text-xs mt-1">MCP tools accessed via generated Go code. Better for complex multi-tool workflows.</p>
                            </TooltipContent>
                          </Tooltip>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                type="button"
                                onClick={() => setExecutionMode('search')}
                                className={`flex-1 px-3 py-2 text-xs font-medium transition-colors ${
                                  useToolSearchMode ? 'agent-mode-selected' : 'agent-mode-unselected'
                                }`}
                              >
                                <Search className="w-3.5 h-3.5 inline mr-1.5" />
                                Tool Search
                              </button>
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="font-medium">Tool Search Mode</p>
                              <p className="text-xs mt-1">Dynamic tool discovery. Agent searches for tools as needed. Selected tools become pre-discovered.</p>
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      </div>
                      <div className="text-xs text-gray-500 mt-1">
                        {!useCodeExecutionMode && !useToolSearchMode && 'Simple: Direct MCP tool access'}
                        {useCodeExecutionMode && 'Code Exec: Tools accessed via generated Go code'}
                        {useToolSearchMode && 'Tool Search: Dynamic tool discovery as needed'}
                      </div>
                    </div>

                    {/* LLM Allocation Mode Toggle */}
                    <div className="mb-4">
                      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
                        LLM Allocation Mode
                      </label>
                      <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                        <button
                          type="button"
                          onClick={() => setLlmAllocationMode('manual')}
                          className={`flex-1 px-3 py-2 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                            llmAllocationMode === 'manual' ? 'agent-mode-selected' : 'agent-mode-unselected'
                          }`}
                        >
                          Fixed Models
                        </button>
                        <button
                          type="button"
                          onClick={() => setLlmAllocationMode('tiered')}
                          className={`flex-1 px-3 py-2 text-xs font-medium transition-colors ${
                            llmAllocationMode === 'tiered' ? 'agent-mode-selected' : 'agent-mode-unselected'
                          }`}
                        >
                          Tiered Auto
                        </button>
                      </div>
                      <div className="text-xs text-gray-500 mt-1">
                        {llmAllocationMode === 'manual' && 'Manual: Configure each agent type separately'}
                        {llmAllocationMode === 'tiered' && 'Auto: System selects tier based on learning maturity'}
                      </div>
                    </div>

                    {llmAllocationMode === 'tiered' ? (
                      <>
                        {/* Tier 1 - High Reasoning */}
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Tier 1 - High Reasoning
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">Used for first-time execution, initial learning, phase agents, and new conditional evaluations.</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={tier1LLM ? availableLLMs.find(llm =>
                              llm.provider === tier1LLM.provider && llm.model === tier1LLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setTier1LLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Most capable model for complex first-time tasks.
                          </div>
                        </div>
                        {/* Tier 2 - Medium Reasoning */}
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Tier 2 - Medium Reasoning
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">Used for execution with existing learnings and learning refinement.</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={tier2LLM ? availableLLMs.find(llm =>
                              llm.provider === tier2LLM.provider && llm.model === tier2LLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setTier2LLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Balanced model for tasks with existing learnings.
                          </div>
                        </div>
                        {/* Tier 3 - Low Reasoning */}
                        <div>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              Tier 3 - Low Reasoning
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">Used for validation (always) and mature learning refinement (2+ runs).</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={tier3LLM ? availableLLMs.find(llm =>
                              llm.provider === tier3LLM.provider && llm.model === tier3LLM.model_id
                            ) || null : currentLLMOption}
                            onLLMSelect={(llm) => setTier3LLM({
                              provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                              model_id: llm.model
                            })}
                            onRefresh={refreshAvailableLLMs}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="text-xs text-gray-500 mt-1">
                            Cost-efficient model for validation and mature learnings.
                          </div>
                        </div>
                        {/* Info panel */}
                        <div className="text-xs text-gray-500 pt-2 border-t border-gray-200 dark:border-gray-700 space-y-1">
                          <div className="font-medium text-gray-600 dark:text-gray-400">Auto-selection rules:</div>
                          <div>Execution: Tier 1 → Tier 2 (after first learning)</div>
                          <div>Learning: Tier 1 → Tier 2 → Tier 3 (progressive)</div>
                          <div>Validation: Always Tier 3</div>
                          <div>Phase agents: Always Tier 1</div>
                          <div className="text-yellow-600 dark:text-yellow-400 mt-1">Temp LLM overrides and per-step LLM configs are disabled in tiered mode</div>
                        </div>
                      </>
                    ) : (
                      <>
                    {/* Execution Agent */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-2">
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                          Execution Agent
                        </label>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="text-xs">Executes plan steps by calling MCP tools and performing actions.</p>
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      </div>
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={executionLLM ? availableLLMs.find(llm =>
                          llm.provider === executionLLM.provider && llm.model === executionLLM.model_id
                        ) || null : currentLLMOption}
                        onLLMSelect={(llm) => setExecutionLLM({
                          provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                          model_id: llm.model
                        })}
                        onRefresh={refreshAvailableLLMs}
                        disabled={false}
                        inModal={true}
                        openDirection="down"
                      />
                      <div className="text-xs text-gray-500 mt-1">
                        Performs the actual work - calling tools, reading files, executing commands.
                      </div>
                    </div>
                    {/* Validation Agent */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-2">
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                          Validation Agent
                        </label>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="text-xs">Checks if step execution succeeded by evaluating success criteria.</p>
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
                          provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                          model_id: llm.model
                        })}
                        onRefresh={refreshAvailableLLMs}
                        disabled={false}
                        inModal={true}
                        openDirection="down"
                      />
                      <div className="text-xs text-gray-500 mt-1">
                        Evaluates execution results and determines if success criteria were met.
                      </div>
                    </div>
                    {/* Learning Agent */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-2">
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                          Learning Agent
                        </label>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="text-xs">Extracts patterns from successful executions for future runs.</p>
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
                          provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                          model_id: llm.model
                        })}
                        onRefresh={refreshAvailableLLMs}
                        disabled={false}
                        inModal={true}
                        openDirection="down"
                      />
                      <div className="text-xs text-gray-500 mt-1">
                        Analyzes execution history and extracts reusable patterns.
                      </div>
                    </div>
                    {/* Phase Agent */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-2">
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                          Phase Agent
                        </label>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent className="max-w-xs">
                              <p className="text-xs">Handles planning, variable extraction, and other workflow phases.</p>
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
                          provider: llm.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                          model_id: llm.model
                        })}
                        onRefresh={refreshAvailableLLMs}
                        disabled={false}
                        inModal={true}
                        openDirection="down"
                      />
                      <div className="text-xs text-gray-500 mt-1">
                        Handles planning, variable extraction, anonymization, and other phases.
                      </div>
                    </div>
                    <div className="text-xs text-gray-500 pt-2 border-t border-gray-200 dark:border-gray-700">
                      Step-specific configs in step_config.json take priority over these defaults
                    </div>
                      </>
                    )}
                  </div>
                </div>
              </div>

              {/* Right Column - Folder, Tools, and Options */}
              <div className="space-y-4">
                {/* Folder Selection - Required for workflow */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Workflow Folder <span className="text-red-500">*</span>
                  </label>
                  <div className="space-y-2">
                    {selectedFolder && (
                      <div className="flex items-center justify-between p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-md">
                        <div className="flex items-center gap-2">
                          <Folder className="w-5 h-5 text-blue-600" />
                          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">{selectedFolder.filepath}</span>
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
                    {!selectedFolder && (
                      <button
                        type="button"
                        data-folder-button
                        onClick={handleSelectFolders}
                        className="w-full p-4 border-2 border-dashed border-red-300 dark:border-red-600 text-red-500 dark:text-red-400 hover:border-red-500 rounded-md transition-colors"
                      >
                        <div className="flex items-center justify-center gap-2">
                          <Folder className="w-5 h-5" />
                          <span className="font-medium">Select Workflow Folder</span>
                        </div>
                        <p className="text-xs mt-1 text-red-400">Required for workflow presets</p>
                      </button>
                    )}
                  </div>
                </div>

                {/* MCP Server Selection */}
                {availableServers.length > 0 && (
                  <ToolSelectionSection
                    availableServers={availableServers}
                    selectedServers={selectedServers}
                    selectedTools={selectedTools}
                    onServerChange={setSelectedServers}
                    onToolChange={setSelectedTools}
                    agentMode={effectiveAgentMode}
                  />
                )}

                {/* Skills Selection - Workflow mode only */}
                {effectiveAgentMode === 'workflow' && (
                  <SkillSelectionSection
                    selectedSkills={selectedSkills}
                    onSkillChange={setSelectedSkills}
                  />
                )}

                {/* Knowledgebase Toggle */}
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
                          Persistent folder shared across all runs for templates and configs
                        </div>
                      </div>
                      <label className="relative inline-flex items-center cursor-pointer ml-3">
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

                {/* Browser Automation Toggle */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Browser Automation
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                    <div className="flex items-center justify-between">
                      <div className="flex-1">
                        <div className="text-sm font-medium text-gray-900 dark:text-white">
                          Enable Browser Access
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          Allow agent to control browser for web automation and testing
                        </div>
                      </div>
                      <label className="relative inline-flex items-center cursor-pointer ml-3">
                        <input
                          type="checkbox"
                          checked={enableBrowserAccess}
                          onChange={(e) => setEnableBrowserAccess(e.target.checked)}
                          className="sr-only peer"
                        />
                        <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                      </label>
                    </div>
                  </div>
                </div>

                {/* Agent Mode Display (read-only for workflow) */}
                {hideAgentModeSelection && fixedAgentMode && (
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-gray-600 dark:text-gray-400">Mode:</span>
                      <span className="text-sm font-medium text-gray-900 dark:text-white">Workflow</span>
                      <span className="text-xs text-gray-500 dark:text-gray-400">- Todo-list execution</span>
                    </div>
                  </div>
                )}
              </div>
            </div>
          ) : (
            /* Simple/Chat Mode: Two Column Layout */
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

                {/* Folder Selection (Optional for simple mode) */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Folder (Optional) - Attach workspace folder to this preset
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
                      className="w-full p-3 border-2 border-dashed border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-blue-500 rounded-md transition-colors"
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
                        <div className="font-medium text-gray-900 dark:text-white">Simple</div>
                        <div className="text-xs text-gray-500 dark:text-gray-400">Ask simple questions</div>
                      </div>
                    </div>
                  </div>
                )}

              </div>
            </div>
          )}
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