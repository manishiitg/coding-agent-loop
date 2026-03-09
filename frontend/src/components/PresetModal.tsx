import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Folder, Plus, X, Settings, Sparkles, Code2, Info, Search, Download } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import { ToolSelectionSection } from './ToolSelectionSection';
import { SkillSelectionSection } from './skills/SkillSelectionSection';
import { SecretSelectionSection } from './secrets/SecretSelectionSection';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig, AgentLLMConfig } from '../services/api-types';
import { useLLMStore } from '../stores/useLLMStore';
import { useModeStore } from '../stores/useModeStore';
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore';
import { useMCPStore } from '../stores/useMCPStore';
import { agentApi, getApiBaseUrl } from '../services/api';
import LLMSelectionDropdown from './LLMSelectionDropdown';
import type { LLMOption } from '../types/llm';

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'simple' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, useToolSearchMode?: boolean, enableBrowserAccess?: boolean, selectedSecrets?: string[], selectedGlobalSecretNames?: string[] | null, camofoxHeaded?: boolean) => void;
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
  const [selectedSecrets, setSelectedSecrets] = useState<string[]>([]);
  // Per-preset global secret selection (null = all selected, [] = none, [...] = specific)
  const [selectedGlobalSecrets, setSelectedGlobalSecrets] = useState<string[] | null>(null);
  const [internalAgentMode, setInternalAgentMode] = useState<'simple' | 'workflow'>('simple');
  const [selectedFolder, setSelectedFolder] = useState<PlannerFile | null>(null);
  const [showFolderDialog, setShowFolderDialog] = useState(false);
  const [folderDialogPosition, setFolderDialogPosition] = useState({ top: 0, left: 0 });
  const [llmConfig, setLlmConfig] = useState<PresetLLMConfig | null>(null);
  const [useCodeExecutionMode, setUseCodeExecutionMode] = useState(false);
  const [useToolSearchMode, setUseToolSearchMode] = useState(false);
  const enableContextSummarization = true;
  const [useKnowledgebase, setUseKnowledgebase] = useState(true);
  const [browserMode, setBrowserModeState] = useState<'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'>('none');
  const [camofoxHeaded, setCamofoxHeaded] = useState(true);
  const enableBrowserAccess = browserMode === 'headless' || browserMode === 'cdp';
  const [useCdp, setUseCdp] = useState(false);
  const [cdpPort, setCdpPort] = useState(9222);
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null);
  const [cdpChecking, setCdpChecking] = useState(false);
  const [gwsChecking, setGwsChecking] = useState(false);
  const [gwsAuthStatus, setGwsAuthStatus] = useState<{
    configured?: boolean; auth_method?: string; token_valid?: boolean; token_error?: string;
    enabled_api_count?: number; scope_count?: number; error?: string;
  } | null>(null);
  const [gwsSyncing, setGwsSyncing] = useState(false);
  const [gwsSyncResult, setGwsSyncResult] = useState<{
    synced?: number; failed?: { name: string; error: string }[]; error?: string;
  } | null>(null);
  const isLocalMode = useCapabilitiesStore(state => state.capabilities?.local_mode ?? false);
  const toolList = useMCPStore(state => state.toolList);

  // Playwright MCP availability: check if 'playwright' server exists in toolList
  const playwrightServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'playwright')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])

  // Camofox MCP availability: check if 'camofox' server exists in toolList
  const camofoxServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'camofox')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])

  // Browser mode setter that also syncs selectedServers
  const setBrowserMode = useCallback((mode: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth') => {
    setBrowserModeState(mode)
    setSelectedServers(prev => {
      const cleaned = prev.filter(s => s !== 'playwright' && s !== 'camofox')
      if (mode === 'playwright') return [...cleaned, 'playwright']
      if (mode === 'stealth') return [...cleaned, 'camofox']
      return cleaned
    })
    // Reset CDP when switching away
    if (mode !== 'cdp') {
      setUseCdp(false)
    } else {
      setUseCdp(true)
    }
  }, [])

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

  // CDP connection check
  const checkCdpConnection = useCallback(async (port: number) => {
    setCdpChecking(true);
    setCdpConnected(null);
    try {
      const result = await agentApi.checkCdpPort(port);
      setCdpConnected(result.connected);
    } catch {
      setCdpConnected(false);
    } finally {
      setCdpChecking(false);
    }
  }, []);

  // Auto-check CDP connection when CDP is enabled or port changes
  useEffect(() => {
    if (!useCdp || !enableBrowserAccess) {
      setCdpConnected(null);
      return;
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort);
    }, 500); // debounce
    return () => clearTimeout(timer);
  }, [useCdp, cdpPort, enableBrowserAccess, checkCdpConnection]);

  const syncGWSSkills = useCallback(async () => {
    setGwsSyncing(true);
    setGwsSyncResult(null);
    try {
      const result = await agentApi.syncGWSSkills();
      setGwsSyncResult(result);
    } catch {
      setGwsSyncResult({ error: 'Failed to connect to backend' });
    } finally {
      setGwsSyncing(false);
    }
  }, []);

  const checkGWSAuth = useCallback(async () => {
    setGwsChecking(true);
    setGwsAuthStatus(null);
    try {
      const result = await agentApi.checkGWSAuthStatus();
      setGwsAuthStatus(result);
    } catch {
      setGwsAuthStatus({ configured: false, error: 'Failed to connect to backend' });
    } finally {
      setGwsChecking(false);
    }
  }, []);

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
      setSelectedSecrets(editingPreset.selectedSecrets || []);
      setSelectedGlobalSecrets(editingPreset.selectedGlobalSecretNames ?? null);
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
      // Derive browser mode from preset data
      const presetServers = editingPreset.selectedServers || [];
      if (presetServers.includes('camofox')) {
        setBrowserModeState('stealth');
      } else if (presetServers.includes('playwright')) {
        setBrowserModeState('playwright');
      } else if (editingPreset.enableBrowserAccess && editingPreset.selectedServers?.includes('cdp')) {
        setBrowserModeState('cdp');
        setUseCdp(true);
      } else if (editingPreset.enableBrowserAccess) {
        // Check if CDP was enabled (useCdp was stored via enableBrowserAccess + cdp detection)
        setBrowserModeState('headless');
      } else {
        setBrowserModeState('none');
      }
      setCamofoxHeaded(editingPreset.camofoxHeaded !== false); // Default true
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
      setSelectedSecrets([]);
      setSelectedGlobalSecrets(null); // null = all global secrets selected by default
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
      setBrowserModeState('none'); // Default no browser
      setCamofoxHeaded(true); // Default headed
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
          phase_llm: phaseLLM || (llmAllocationMode === 'tiered' && tier1LLM ? tier1LLM : defaultAgentLLM),
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
        enableBrowserAccess, // Browser automation access
        selectedSecrets, // Secret IDs for injection
        selectedGlobalSecrets, // Per-preset global secret selection (null=all)
        browserMode === 'stealth' ? camofoxHeaded : undefined // Camofox headed mode
      );
      onClose();
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, selectedTools, selectedSkills, selectedSecrets, selectedGlobalSecrets, llmConfig, executionLLM, validationLLM, learningLLM, phaseLLM, useCodeExecutionMode, useToolSearchMode, useKnowledgebase, enableBrowserAccess, browserMode, camofoxHeaded, llmAllocationMode, tier1LLM, tier2LLM, tier3LLM, onSave, onClose, enableContextSummarization]);

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
            {effectiveAgentMode === 'workflow'
              ? (editingPreset ? 'Edit Workflow' : 'Add Workflow')
              : (editingPreset ? 'Edit Preset' : 'Add New Preset')}
          </h2>
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              form="preset-form"
              variant="outline"
              size="sm"
              disabled={!label.trim() || (effectiveAgentMode !== 'workflow' && !query.trim()) || (effectiveAgentMode === 'workflow' && !selectedFolder)}
            >
              {editingPreset ? 'Update' : 'Save'} {effectiveAgentMode === 'workflow' ? 'Workflow' : 'Preset'}
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
              {/* Left Column - Workflow Name and LLM Configuration */}
              <div className="space-y-4">
                {/* Workflow Name */}
                <div>
                  <label htmlFor="preset-label" className="block text-sm font-medium mb-2">
                    Workflow Name
                  </label>
                  <Input
                    id="preset-label"
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                    placeholder="Enter workflow name..."
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
                                  <p className="text-xs">Used for first-time execution (no learnings yet) and initial learning extraction.</p>
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
                        {/* Phase Agent - also available in tiered mode */}
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
                                  <p className="text-xs">Independent LLM for all workflow phases: planning, variable extraction, evaluation design, anonymization, plan improvement, learning consolidation, and debugging. This is separate from the tiered execution/learning/validation assignments.</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={phaseLLM ? availableLLMs.find(llm =>
                              llm.provider === phaseLLM.provider && llm.model === phaseLLM.model_id
                            ) || null : (tier1LLM ? availableLLMs.find(llm =>
                              llm.provider === tier1LLM.provider && llm.model === tier1LLM.model_id
                            ) || null : currentLLMOption)}
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
                            Used for planning, evaluation design, anonymization, plan improvement, and debugging phases. Defaults to Tier 1 if not set.
                          </div>
                        </div>
                        {/* Info panel */}
                        <div className="text-xs text-gray-500 pt-2 border-t border-gray-200 dark:border-gray-700 space-y-1">
                          <div className="font-medium text-gray-600 dark:text-gray-400">Auto-selection rules:</div>
                          <div>Execution: Tier 1 → Tier 2 (after first learning)</div>
                          <div>Learning: Tier 2 → Tier 3 (after 2+ runs)</div>
                          <div>Validation: Always Tier 3</div>
                          <div>Phase Agent: Independent — always uses the configured Phase LLM above</div>
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
                              <p className="text-xs">Executes each plan step by calling MCP tools, reading files, and performing actions. This is the main workhorse that carries out the plan.</p>
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
                              <p className="text-xs">Evaluates whether each step succeeded by checking the execution output against defined success criteria. Can be a lighter model since it only judges results.</p>
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
                              <p className="text-xs">Extracts reusable patterns and insights from execution results to improve future runs. Also handles plan improvement, tool optimization, and learning consolidation.</p>
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
                              <p className="text-xs">Independent LLM for all workflow phases: planning, variable extraction, evaluation design, anonymization, plan improvement, learning consolidation, and debugging. This is separate from the execution/validation/learning agent LLMs.</p>
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
                        Used for planning, evaluation design, anonymization, plan improvement, and debugging phases.
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
                        <p className="text-xs mt-1 text-red-400">Required for workflows</p>
                      </button>
                    )}
                  </div>
                </div>

                {/* MCP Server Selection */}
                {availableServers.length > 0 ? (
                  <ToolSelectionSection
                    availableServers={availableServers}
                    selectedServers={selectedServers}
                    selectedTools={selectedTools}
                    onServerChange={setSelectedServers}
                    onToolChange={setSelectedTools}
                    agentMode={effectiveAgentMode}
                  />
                ) : (
                  <div className="space-y-2">
                    <label className="block text-sm font-medium text-gray-900 dark:text-gray-100">
                      MCP Server Selection
                    </label>
                    <div className="p-3 border border-gray-200 dark:border-gray-700 rounded-md text-xs text-gray-500 dark:text-gray-400">
                      No MCP servers configured. Add servers in the MCP settings sidebar.
                    </div>
                  </div>
                )}

                {/* Skills Selection - Workflow mode only */}
                {effectiveAgentMode === 'workflow' && (
                  <SkillSelectionSection
                    selectedSkills={selectedSkills}
                    onSkillChange={setSelectedSkills}
                  />
                )}

                {/* Secrets Selection - Workflow mode only */}
                {effectiveAgentMode === 'workflow' && (
                  <SecretSelectionSection
                    selectedSecrets={selectedSecrets}
                    onSecretChange={setSelectedSecrets}
                    selectedGlobalSecrets={selectedGlobalSecrets}
                    onGlobalSecretChange={setSelectedGlobalSecrets}
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

                {/* Browser Automation Mode Selector */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Browser Automation
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-900/60 border border-gray-200 dark:border-gray-700 rounded-lg space-y-3">
                    {/* Mode selection cards */}
                    <div className="space-y-2">
                      {/* None */}
                      <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                        browserMode === 'none'
                          ? 'border-gray-400 dark:border-gray-500 bg-gray-100 dark:bg-gray-800/60'
                          : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40'
                      }`}>
                        <input type="radio" name="presetBrowserMode" checked={browserMode === 'none'} onChange={() => setBrowserMode('none')} className="mt-0.5 w-4 h-4 accent-gray-500" />
                        <div>
                          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">None</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">No browser access for this workflow</div>
                        </div>
                      </label>

                      {/* Headless */}
                      <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                        browserMode === 'headless'
                          ? 'border-blue-500 dark:border-blue-500 bg-blue-50 dark:bg-blue-950/40'
                          : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40'
                      }`}>
                        <input type="radio" name="presetBrowserMode" checked={browserMode === 'headless'} onChange={() => setBrowserMode('headless')} className="mt-0.5 w-4 h-4 text-blue-500 accent-blue-500" />
                        <div>
                          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Headless Browser</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            Agent controls a headless Chromium inside Docker. You won&apos;t see the browser window.
                          </div>
                        </div>
                      </label>

                      {/* CDP (local mode only) */}
                      {isLocalMode && (
                        <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                          browserMode === 'cdp'
                            ? 'border-green-500 dark:border-green-500 bg-green-50 dark:bg-green-950/40'
                            : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40'
                        }`}>
                          <input type="radio" name="presetBrowserMode" checked={browserMode === 'cdp'} onChange={() => setBrowserMode('cdp')} className="mt-0.5 w-4 h-4 text-green-500 accent-green-500" />
                          <div className="flex-1">
                            <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Connect to Local Chrome (CDP)</div>
                            <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                              Agent connects to your real Chrome browser so you can watch it navigate in real-time.
                            </div>
                          </div>
                        </label>
                      )}

                      {/* Playwright MCP */}
                      <label className={`flex items-start gap-3 p-3 rounded-lg border transition-colors ${
                        playwrightServerStatus === 'not_found'
                          ? 'border-gray-200 dark:border-gray-700 opacity-50 cursor-not-allowed'
                          : browserMode === 'playwright'
                            ? 'border-purple-500 dark:border-purple-500 bg-purple-50 dark:bg-purple-950/40 cursor-pointer'
                            : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40 cursor-pointer'
                      }`}>
                        <input
                          type="radio"
                          name="presetBrowserMode"
                          checked={browserMode === 'playwright'}
                          onChange={() => setBrowserMode('playwright')}
                          disabled={playwrightServerStatus === 'not_found'}
                          className="mt-0.5 w-4 h-4 text-purple-500 accent-purple-500"
                        />
                        <div className="flex-1">
                          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Playwright MCP</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            Opens a new visible browser window per session. Uses Playwright MCP server.
                          </div>
                          {playwrightServerStatus === 'not_found' && (
                            <div className="text-xs text-red-500 dark:text-red-400 mt-1.5 flex items-center gap-1">
                              <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                              &quot;playwright&quot; server not found in MCP config &mdash; add it in MCP Settings
                            </div>
                          )}
                          {playwrightServerStatus === 'error' && (
                            <div className="text-xs text-amber-500 dark:text-amber-400 mt-1.5 flex items-center gap-1">
                              <span className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                              Playwright server has errors &mdash; check MCP Settings
                            </div>
                          )}
                        </div>
                      </label>

                      {/* Stealth (Camofox) */}
                      <label className={`flex items-start gap-3 p-3 rounded-lg border transition-colors ${
                        camofoxServerStatus === 'not_found'
                          ? 'border-gray-200 dark:border-gray-700 opacity-50 cursor-not-allowed'
                          : browserMode === 'stealth'
                            ? 'border-orange-500 dark:border-orange-500 bg-orange-50 dark:bg-orange-950/40 cursor-pointer'
                            : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40 cursor-pointer'
                      }`}>
                        <input
                          type="radio"
                          name="presetBrowserMode"
                          checked={browserMode === 'stealth'}
                          onChange={() => setBrowserMode('stealth')}
                          disabled={camofoxServerStatus === 'not_found'}
                          className="mt-0.5 w-4 h-4 text-orange-500 accent-orange-500"
                        />
                        <div className="flex-1">
                          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Stealth Browser (Camofox)</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            Anti-detect Firefox for sites that block bots. Headed mode with session persistence.
                          </div>
                          {camofoxServerStatus === 'not_found' && (
                            <div className="text-xs text-red-500 dark:text-red-400 mt-1.5 flex items-center gap-1">
                              <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                              &quot;camofox&quot; server not found in MCP config &mdash; add it in MCP Settings
                            </div>
                          )}
                          {camofoxServerStatus === 'error' && (
                            <div className="text-xs text-amber-500 dark:text-amber-400 mt-1.5 flex items-center gap-1">
                              <span className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                              Camofox MCP server has errors &mdash; check MCP Settings
                            </div>
                          )}
                        </div>
                      </label>
                    </div>

                    {/* Camofox configuration sub-panel */}
                    {browserMode === 'stealth' && (
                      <div className="p-3 rounded-lg bg-gray-100 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700">
                        <label className="flex items-center gap-2.5 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={camofoxHeaded}
                            onChange={(e) => setCamofoxHeaded(e.target.checked)}
                            className="w-4 h-4 rounded accent-orange-500"
                          />
                          <div>
                            <div className="text-sm text-gray-900 dark:text-gray-200">Show browser window</div>
                            <div className="text-xs text-gray-500 dark:text-gray-500">
                              {camofoxHeaded
                                ? 'Visible Firefox window \u2014 watch the agent navigate in real-time'
                                : 'Headless mode \u2014 browser runs in background (faster, no window)'}
                            </div>
                          </div>
                        </label>
                      </div>
                    )}

                    {/* CDP configuration sub-panel */}
                    {browserMode === 'cdp' && (
                      <div className="space-y-3 p-3 rounded-lg bg-gray-100 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700">
                        <div className="flex items-center gap-3">
                          <label className="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap">CDP Port:</label>
                          <input
                            type="number"
                            value={cdpPort}
                            onChange={(e) => setCdpPort(parseInt(e.target.value) || 9222)}
                            className="w-24 px-2.5 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:border-green-500 focus:outline-none"
                            min={1}
                            max={65535}
                          />
                          <button
                            type="button"
                            onClick={() => checkCdpConnection(cdpPort)}
                            disabled={cdpChecking}
                            className="px-3 py-1.5 text-xs font-medium bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600 rounded-md text-gray-700 dark:text-gray-200 disabled:opacity-50 transition-colors"
                          >
                            {cdpChecking ? 'Checking...' : 'Check Connection'}
                          </button>
                        </div>
                        {typeof navigator !== 'undefined' && navigator.platform?.includes('Mac') && (
                          <div className="rounded-lg bg-white dark:bg-gray-900/80 border border-gray-300 dark:border-gray-600 p-3 space-y-2">
                            <p className="text-xs font-medium text-gray-700 dark:text-gray-300">macOS: Easy setup</p>
                            <a
                              href={`${getApiBaseUrl()}/api/downloads/chrome-cdp-macOS.zip`}
                              download="Chrome-CDP-macOS.zip"
                              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium bg-green-600 hover:bg-green-500 text-white rounded-md transition-colors"
                            >
                              <Download className="w-3.5 h-3.5" />
                              Download Chrome CDP launcher
                            </a>
                            <ol className="text-xs text-gray-500 dark:text-gray-400 list-decimal list-inside space-y-0.5">
                              <li>Double-click the zip to unzip.</li>
                              <li>Move &quot;Chrome CDP.app&quot; to Applications, then open it from Spotlight or LaunchPad.</li>
                              <li>Click Check Connection above to verify.</li>
                            </ol>
                            <p className="text-xs text-amber-600 dark:text-amber-400/90 mt-1.5">
                              If macOS says &quot;package is damaged&quot;, run in Terminal: <code className="bg-gray-100 dark:bg-gray-950 px-1 rounded font-mono text-[11px]">xattr -c &apos;~/Downloads/Chrome CDP.app&apos;</code> or right-click the app → Open.
                            </p>
                          </div>
                        )}
                        <div className="flex items-start gap-2">
                          {cdpChecking ? (
                            <>
                              <div className="w-3 h-3 rounded-full bg-yellow-400 animate-pulse mt-0.5 flex-shrink-0" />
                              <span className="text-sm text-yellow-600 dark:text-yellow-400">Checking connection to port {cdpPort}...</span>
                            </>
                          ) : cdpConnected === true ? (
                            <>
                              <div className="w-3 h-3 rounded-full bg-green-500 mt-0.5 flex-shrink-0" />
                              <span className="text-sm text-green-600 dark:text-green-400">Connected! Chrome is reachable on port {cdpPort}.</span>
                            </>
                          ) : cdpConnected === false ? (
                            <>
                              <div className="w-3 h-3 rounded-full bg-red-500 mt-0.5 flex-shrink-0" />
                              <div className="text-sm">
                                <span className="text-red-600 dark:text-red-400">Chrome is not reachable on port {cdpPort}.</span>
                                <div className="mt-2 text-xs text-gray-500 dark:text-gray-400 space-y-1">
                                  <p className="font-medium text-gray-700 dark:text-gray-300">To enable CDP, launch Chrome with:</p>
                                  <code className="block bg-gray-200 dark:bg-gray-950 px-2 py-1.5 rounded text-xs font-mono break-all text-green-700 dark:text-green-400 border border-gray-300 dark:border-gray-700">
                                    {typeof navigator !== 'undefined' && navigator.platform?.includes('Mac')
                                      ? `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=${cdpPort}`
                                      : `google-chrome --remote-debugging-port=${cdpPort}`}
                                  </code>
                                  <p className="text-gray-500">Close all Chrome windows first, then run the command above.</p>
                                </div>
                              </div>
                            </>
                          ) : (
                            <span className="text-xs text-gray-500">Click &quot;Check Connection&quot; to verify Chrome is reachable.</span>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                </div>

                {/* Google Workspace Section */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Google Workspace
                  </label>
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md space-y-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2 flex-1">
                        <svg viewBox="0 0 24 24" className="w-5 h-5 flex-shrink-0" fill="none">
                          <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
                          <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
                          <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l3.66-2.84z" fill="#FBBC05"/>
                          <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
                        </svg>
                        <div>
                          <div className="text-sm font-medium text-gray-900 dark:text-white">Enable Google Workspace</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">Access Drive, Gmail, Calendar, Docs, Sheets, and Slides tools</div>
                        </div>
                      </div>
                      <label className="relative inline-flex items-center cursor-pointer ml-3">
                        <input
                          type="checkbox"
                          checked={selectedServers.includes('gws')}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setSelectedServers(prev => prev.includes('gws') ? prev : [...prev, 'gws']);
                            } else {
                              setSelectedServers(prev => prev.filter(s => s !== 'gws'));
                            }
                          }}
                          className="sr-only peer hidden"
                        />
                        <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                      </label>
                    </div>

                    <details className="group">
                      <summary className="cursor-pointer text-xs font-medium text-blue-600 dark:text-blue-400 hover:underline select-none list-none flex items-center gap-1">
                        <span className="group-open:rotate-90 transition-transform inline-block">▶</span>
                        Setup Instructions
                      </summary>
                      <div className="mt-2 space-y-2 text-xs text-gray-600 dark:text-gray-400">
                        <ol className="list-decimal list-inside space-y-1 pl-1">
                          <li>Install: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded font-mono text-[11px]">npm install -g @googleworkspace/cli</code></li>
                          <li>Create a Google Cloud project, enable Workspace APIs, and download your OAuth2 client secret JSON</li>
                          <li>Authenticate: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded font-mono text-[11px]">gws auth login --client-secret /path/to/client_secret.json</code></li>
                          <li>Authorize in the browser when prompted</li>
                          <li>Verify below with &quot;Check Auth Status&quot;</li>
                        </ol>
                        <p className="text-amber-600 dark:text-amber-400 mt-1">
                          Tip: each gws service exposes 10–80 tools. The default config uses the core 6 services.
                        </p>
                      </div>
                    </details>

                    <div className="flex items-center gap-2 flex-wrap">
                      <button
                        type="button"
                        onClick={checkGWSAuth}
                        disabled={gwsChecking}
                        className="px-2 py-1 text-xs bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600 rounded text-gray-700 dark:text-gray-300 disabled:opacity-50"
                      >
                        {gwsChecking ? 'Checking...' : 'Check Auth Status'}
                      </button>
                      <button
                        type="button"
                        onClick={syncGWSSkills}
                        disabled={gwsSyncing}
                        className="px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900/40 hover:bg-blue-200 dark:hover:bg-blue-900/60 rounded text-blue-700 dark:text-blue-300 disabled:opacity-50"
                      >
                        {gwsSyncing ? 'Syncing...' : 'Sync Skills from GitHub'}
                      </button>
                      {gwsAuthStatus && (
                        gwsAuthStatus.configured && gwsAuthStatus.token_valid !== false ? (
                          <div className="flex items-center gap-1.5">
                            <div className="w-2 h-2 rounded-full bg-green-500 flex-shrink-0" />
                            <span className="text-xs text-green-600 dark:text-green-400">
                              Auth OK · {gwsAuthStatus.enabled_api_count ?? 0} APIs · {gwsAuthStatus.scope_count ?? 0} scopes
                              {gwsAuthStatus.auth_method ? ` (${gwsAuthStatus.auth_method})` : ''}
                            </span>
                          </div>
                        ) : (
                          <div className="flex items-center gap-1.5">
                            <div className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                            <span className="text-xs text-red-600 dark:text-red-400">
                              {gwsAuthStatus.token_valid === false
                                ? `Token invalid — run gws auth login${gwsAuthStatus.token_error ? ` (${gwsAuthStatus.token_error})` : ''}`
                                : (gwsAuthStatus.error ?? 'Not configured — run gws auth login')}
                            </span>
                          </div>
                        )
                      )}
                      {gwsSyncResult && (
                        gwsSyncResult.error ? (
                          <span className="text-xs text-red-600 dark:text-red-400">{gwsSyncResult.error}</span>
                        ) : (
                          <span className="text-xs text-green-600 dark:text-green-400">
                            Synced {gwsSyncResult.synced} skill{gwsSyncResult.synced !== 1 ? 's' : ''}
                            {gwsSyncResult.failed && gwsSyncResult.failed.length > 0
                              ? ` · ${gwsSyncResult.failed.length} failed`
                              : ''}
                          </span>
                        )
                      )}
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
                {availableServers.length > 0 ? (
                  <ToolSelectionSection
                    availableServers={availableServers}
                    selectedServers={selectedServers}
                    selectedTools={selectedTools}
                    onServerChange={setSelectedServers}
                    onToolChange={setSelectedTools}
                    agentMode={effectiveAgentMode}
                  />
                ) : (
                  <div className="space-y-2">
                    <label className="block text-sm font-medium text-gray-900 dark:text-gray-100">
                      MCP Server Selection
                    </label>
                    <div className="p-3 border border-gray-200 dark:border-gray-700 rounded-md text-xs text-gray-500 dark:text-gray-400">
                      No MCP servers configured. Add servers in the MCP settings sidebar.
                    </div>
                  </div>
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
                            checked={internalAgentMode === mode.value}
                            onChange={(e) => setInternalAgentMode(e.target.value as 'simple' | 'workflow')}
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