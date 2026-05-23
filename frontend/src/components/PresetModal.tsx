import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { Folder, Plus, X, Settings, Sparkles, Code2, Info, Search, Download, Trash2, SlidersHorizontal } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import { ToolSelectionSection } from './ToolSelectionSection';
import { SkillSelectionSection } from './skills/SkillSelectionSection';
import { SecretSelectionSection } from './secrets/SecretSelectionSection';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip';
import ConfirmationDialog from './ui/ConfirmationDialog';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig, AgentLLMConfig, AgentLLMFallback, LLMProvider } from '../services/api-types';
import { useLLMStore } from '../stores/useLLMStore';
import { useModeStore } from '../stores/useModeStore';
import { useMCPStore } from '../stores/useMCPStore';
import { agentApi, getApiBaseUrl } from '../services/api';
import LLMSelectionDropdown from './LLMSelectionDropdown';
import type { LLMOption } from '../types/llm';
import ModalPortal from './ui/ModalPortal';

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'multi-agent' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, enableContextSummarization?: boolean, enableBrowserAccess?: boolean, selectedSecrets?: string[], selectedGlobalSecretNames?: string[] | null, browserMode?: 'none' | 'headless' | 'cdp' | 'playwright') => void;
  editingPreset?: CustomPreset | null;
  availableServers?: string[];
  hideAgentModeSelection?: boolean;
  fixedAgentMode?: 'multi-agent' | 'workflow';
  agentMode: string;
  onDeleteWorkflow?: (preset: CustomPreset) => Promise<void>;
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
  onDeleteWorkflow,
}) => {
  const [label, setLabel] = useState('');
  const [query, setQuery] = useState('');
  const [selectedServers, setSelectedServers] = useState<string[]>([]);
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedSecrets, setSelectedSecrets] = useState<string[]>([]);
  // Per-preset global secret selection (null = all selected, [] = none, [...] = specific)
  const [selectedGlobalSecrets, setSelectedGlobalSecrets] = useState<string[] | null>([]);
  const [internalAgentMode, setInternalAgentMode] = useState<'multi-agent' | 'workflow'>('multi-agent');
  const [selectedFolder, setSelectedFolder] = useState<PlannerFile | null>(null);
  const [workflowFolderEdited, setWorkflowFolderEdited] = useState(false);
  const [showFolderDialog, setShowFolderDialog] = useState(false);
  const [folderDialogPosition, setFolderDialogPosition] = useState({ top: 0, left: 0 });
  const [llmConfig, setLlmConfig] = useState<PresetLLMConfig | null>(null);
  const enableContextSummarization = true;
  const [useKnowledgebase, setUseKnowledgebase] = useState(true);
  const [browserMode, setBrowserModeState] = useState<'none' | 'headless' | 'cdp' | 'playwright'>('none');
  const enableBrowserAccess = browserMode === 'headless' || browserMode === 'cdp';
  const [useCdp, setUseCdp] = useState(false);
  const [cdpPort, setCdpPort] = useState(9222);
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null);
  const [cdpChecking, setCdpChecking] = useState(false);
  const [showDeleteWorkflowConfirm, setShowDeleteWorkflowConfirm] = useState(false);
  const [deletingWorkflow, setDeletingWorkflow] = useState(false);
  const toolList = useMCPStore(state => state.toolList);

  // Playwright MCP availability: check if 'playwright' server exists in toolList
  const playwrightServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'playwright')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])

  // Browser mode setter that also syncs selectedServers
  const setBrowserMode = useCallback((mode: 'none' | 'headless' | 'cdp' | 'playwright') => {
    setBrowserModeState(mode)
    setSelectedServers(prev => {
      const cleaned = prev.filter(s => s !== 'playwright')
      if (mode === 'playwright') return [...cleaned, 'playwright']
      return cleaned
    })
    // Reset CDP when switching away
    if (mode !== 'cdp') {
      setUseCdp(false)
    } else {
      setUseCdp(true)
    }
  }, [])

  const [learningLLM, setLearningLLM] = useState<AgentLLMConfig | null>(null);
  const [phaseLLM, setPhaseLLM] = useState<AgentLLMConfig | null>(null);
  const [tier1LLM, setTier1LLM] = useState<AgentLLMConfig | null>(null);
  const [tier2LLM, setTier2LLM] = useState<AgentLLMConfig | null>(null);
  const [tier3LLM, setTier3LLM] = useState<AgentLLMConfig | null>(null);
  const [tier1Fallbacks, setTier1Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [tier2Fallbacks, setTier2Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [tier3Fallbacks, setTier3Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [showWorkflowLLMAdvanced, setShowWorkflowLLMAdvanced] = useState(false);

  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore();
  const primaryConfig = useLLMStore(state => state.primaryConfig);
  const availableLLMs = useLLMStore(state => state.availableLLMs);
  const getCurrentLLMOption = useLLMStore(state => state.getCurrentLLMOption);
  const loadDefaultsFromBackend = useLLMStore(state => state.loadDefaultsFromBackend);

  const effectiveAgentMode = useMemo(() => {
    if (fixedAgentMode) return fixedAgentMode;
    if (propAgentMode) return propAgentMode as 'multi-agent' | 'workflow';
    return internalAgentMode;
  }, [fixedAgentMode, propAgentMode, internalAgentMode]);

  const sanitizeWorkflowFolderName = useCallback((value: string): string => {
    const sanitized = value
      .normalize('NFKD')
      .replace(/[\u0300-\u036f]/g, '')
      .toLowerCase()
      .replace(/[^a-z0-9]/g, '')

    return sanitized || 'workflow'
  }, []);

  const makeWorkflowFolder = useCallback((folderName: string): PlannerFile => ({
    filepath: `Workflow/${folderName}`,
    type: 'folder'
  }), []);

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

  // LLM selection handler - updates local preset LLM config
  const handleLLMSelect = useCallback((llm: LLMOption) => {
    setLlmConfig({
      provider: llm.provider as LLMProvider,
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

  const defaultAgentLLM = useMemo<AgentLLMConfig | null>(() => {
    if (llmConfig?.provider && llmConfig?.model_id) {
      return {
        provider: llmConfig.provider,
        model_id: llmConfig.model_id
      };
    }
    if (primaryConfig.provider && primaryConfig.model_id) {
      return {
        provider: primaryConfig.provider as AgentLLMConfig['provider'],
        model_id: primaryConfig.model_id
      };
    }
    return null;
  }, [llmConfig, primaryConfig]);

  const effectiveTier1LLM = useMemo<AgentLLMConfig | null>(() => tier1LLM || defaultAgentLLM, [tier1LLM, defaultAgentLLM]);
  const effectiveTier2LLM = useMemo<AgentLLMConfig | null>(() => tier2LLM || defaultAgentLLM, [tier2LLM, defaultAgentLLM]);
  const effectiveTier3LLM = useMemo<AgentLLMConfig | null>(() => tier3LLM || defaultAgentLLM, [tier3LLM, defaultAgentLLM]);
  // Draft create paths can collide with existing workflows, so only load scoped secrets for persisted workflows.
  const workflowSecretPath = editingPreset ? selectedFolder?.filepath : undefined;

  const hasAdvancedWorkflowLLMConfig = useCallback((presetLLM?: PresetLLMConfig | null) => {
    const t1 = presetLLM?.tiered_config?.tier_1;
    const t2 = presetLLM?.tiered_config?.tier_2;
    const t3 = presetLLM?.tiered_config?.tier_3;
    const phase = presetLLM?.phase_llm;
    const configured = [t1, t2, t3, phase].filter(Boolean);
    const key = (cfg?: AgentLLMConfig | null) => cfg?.provider && cfg?.model_id ? `${cfg.provider}/${cfg.model_id}` : '';
    if (configured.some(cfg => (cfg?.fallbacks ?? []).length > 0)) return true;
    return new Set(configured.map(key).filter(Boolean)).size > 1;
  }, []);

  const handleSharedWorkflowLLMSelect = useCallback((llm: LLMOption) => {
    const selected: AgentLLMConfig = {
      provider: llm.provider as AgentLLMConfig['provider'],
      model_id: llm.model
    };
    setLlmConfig({
      provider: llm.provider as LLMProvider,
      model_id: llm.model
    });
    setTier1LLM(selected);
    setTier2LLM(selected);
    setTier3LLM(selected);
    setPhaseLLM(selected);
    setTier1Fallbacks([]);
    setTier2Fallbacks([]);
    setTier3Fallbacks([]);
  }, []);

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
      setWorkflowFolderEdited(true);
      const presetLLM: PresetLLMConfig = editingPreset.llmConfig || {
        provider: primaryConfig.provider as PresetLLMConfig['provider'],
        model_id: primaryConfig.model_id
      };
      setLlmConfig(presetLLM);
      setUseKnowledgebase(presetLLM.use_knowledgebase !== false); // Default true unless explicitly false
      // Load browser mode: prefer explicit browserMode, fall back to legacy derivation
      if (editingPreset.browserMode && editingPreset.browserMode !== 'none') {
        setBrowserModeState(editingPreset.browserMode);
        if (editingPreset.browserMode === 'cdp') setUseCdp(true);
      } else {
        // Legacy fallback for presets saved before browserMode was added
        const presetServers = editingPreset.selectedServers || [];
        if (presetServers.includes('playwright')) {
          setBrowserModeState('playwright');
        } else if (editingPreset.enableBrowserAccess) {
          setBrowserModeState('headless');
        } else {
          setBrowserModeState('none');
        }
      }
      // Load agent-specific configs if available
      setLearningLLM(presetLLM.learning_llm || null);
      setPhaseLLM(presetLLM.phase_llm || null);
      // Load tiered LLM allocation config
      setTier1LLM(presetLLM.tiered_config?.tier_1 || null);
      setTier2LLM(presetLLM.tiered_config?.tier_2 || null);
      setTier3LLM(presetLLM.tiered_config?.tier_3 || null);
      setTier1Fallbacks(presetLLM.tiered_config?.tier_1?.fallbacks || []);
      setTier2Fallbacks(presetLLM.tiered_config?.tier_2?.fallbacks || []);
      setTier3Fallbacks(presetLLM.tiered_config?.tier_3?.fallbacks || []);
      setShowWorkflowLLMAdvanced(hasAdvancedWorkflowLLMConfig(presetLLM));
    } else {
      setLabel('');
      setQuery('');
      setSelectedServers([]);
      setSelectedTools([]); // NEW
      setSelectedSkills([]);
      setSelectedSecrets([]);
      setSelectedGlobalSecrets([]);
      // Default to workflow mode as chat presets are disabled
      const defaultMode = 'workflow';
      setInternalAgentMode(defaultMode);
      setSelectedFolder(makeWorkflowFolder(sanitizeWorkflowFolderName('')));
      setWorkflowFolderEdited(false);
      // Initialize LLM config from current primary config
      const defaultLLM: PresetLLMConfig = {
        provider: primaryConfig.provider as PresetLLMConfig['provider'],
        model_id: primaryConfig.model_id
      };
      setLlmConfig(defaultLLM);
      setUseKnowledgebase(true); // Default true
      setBrowserModeState('none'); // Default no browser
      // Initialize agent-specific configs to null (will use legacy default)
      setLearningLLM(null);
      setPhaseLLM(null);
      // Initialize tiered config
      setTier1LLM(null);
      setTier2LLM(null);
      setTier3LLM(null);
      setTier1Fallbacks([]);
      setTier2Fallbacks([]);
      setTier3Fallbacks([]);
      setShowWorkflowLLMAdvanced(false);
    }
  }, [editingPreset, fixedAgentMode, primaryConfig, selectedModeCategory, getAgentModeFromCategory, makeWorkflowFolder, sanitizeWorkflowFolderName, hasAdvancedWorkflowLLMConfig]);

  useEffect(() => {
    if (editingPreset || effectiveAgentMode !== 'workflow' || workflowFolderEdited) {
      return;
    }

    setSelectedFolder(makeWorkflowFolder(sanitizeWorkflowFolderName(label)));
  }, [editingPreset, effectiveAgentMode, label, makeWorkflowFolder, sanitizeWorkflowFolderName, workflowFolderEdited]);

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
    setWorkflowFolderEdited(true);
    setShowFolderDialog(false);
  }, []);

  const handleRemoveFolder = useCallback(() => {
    setSelectedFolder(null);
    setWorkflowFolderEdited(true);
  }, []);

  const handleWorkflowFolderNameChange = useCallback((value: string) => {
    setWorkflowFolderEdited(true);
    const folderName = sanitizeWorkflowFolderName(value);
    setSelectedFolder(makeWorkflowFolder(folderName));
  }, [makeWorkflowFolder, sanitizeWorkflowFolderName]);

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
      
      // Build LLM config with workflow-level defaults
      // execution_llm is step-only and is not persisted at the workflow level.
      let finalLLMConfig: PresetLLMConfig | undefined = llmConfig || undefined;
      if (effectiveAgentMode === 'workflow') {
        // For workflow mode, keep tiered defaults plus phase/learning settings.
        // Use the displayed fallback value (from llmConfig) if user didn't explicitly select
        // This ensures the visual selection is saved even if user didn't explicitly click the dropdown
        const defaultWorkflowLLM = defaultAgentLLM || undefined;
        const workflowBaseLLMConfig = { ...((llmConfig || {}) as PresetLLMConfig & { execution_llm?: unknown }) };
        delete workflowBaseLLMConfig.execution_llm;

        finalLLMConfig = {
          ...workflowBaseLLMConfig,
          learning_llm: learningLLM || defaultWorkflowLLM,
          phase_llm: phaseLLM || effectiveTier1LLM || defaultWorkflowLLM,
          use_knowledgebase: useKnowledgebase,
          llm_allocation_mode: 'tiered' as const,
          ...(effectiveTier1LLM && effectiveTier2LLM && effectiveTier3LLM ? {
            tiered_config: {
              tier_1: { provider: effectiveTier1LLM.provider, model_id: effectiveTier1LLM.model_id, ...(tier1Fallbacks.length > 0 ? { fallbacks: tier1Fallbacks } : {}) },
              tier_2: { provider: effectiveTier2LLM.provider, model_id: effectiveTier2LLM.model_id, ...(tier2Fallbacks.length > 0 ? { fallbacks: tier2Fallbacks } : {}) },
              tier_3: { provider: effectiveTier3LLM.provider, model_id: effectiveTier3LLM.model_id, ...(tier3Fallbacks.length > 0 ? { fallbacks: tier3Fallbacks } : {}) },
            }
          } : {}),
        };
      }
      console.log('[PRESET_MODAL] Agent LLM configs being saved:', {
        learningLLM: learningLLM,
        phaseLLM: phaseLLM,
        defaultAgentLLM: defaultAgentLLM || undefined,
        effectiveTier1LLM: effectiveTier1LLM || undefined,
        effectiveTier2LLM: effectiveTier2LLM || undefined,
        effectiveTier3LLM: effectiveTier3LLM || undefined,
        finalLLMConfig: finalLLMConfig,
      });
      onSave(
        label.trim(),
        effectiveAgentMode === 'workflow' ? '' : query.trim(),
        selectedServers,
        selectedTools,
        selectedSkills, // Skill folder names for workflow
        effectiveAgentMode,
        selectedFolder || undefined,
        finalLLMConfig,
        false, // useCodeExecutionMode — backend determines mode from browser selection
        enableContextSummarization,
        enableBrowserAccess, // Browser automation access
        selectedSecrets, // Secret names for workflow injection
        selectedGlobalSecrets, // Per-preset global secret selection (null=all)
        browserMode // Browser mode: none|headless|cdp|playwright
      );
      onClose();
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, selectedTools, selectedSkills, selectedSecrets, selectedGlobalSecrets, llmConfig, learningLLM, phaseLLM, useKnowledgebase, enableBrowserAccess, browserMode, tier1Fallbacks, tier2Fallbacks, tier3Fallbacks, onSave, onClose, enableContextSummarization, defaultAgentLLM, effectiveTier1LLM, effectiveTier2LLM, effectiveTier3LLM]);

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

  const handleDeleteWorkflowConfirm = useCallback(async () => {
    if (!editingPreset || !onDeleteWorkflow) return;
    setDeletingWorkflow(true);
    try {
      await onDeleteWorkflow(editingPreset);
      setShowDeleteWorkflowConfirm(false);
    } finally {
      setDeletingWorkflow(false);
    }
  }, [editingPreset, onDeleteWorkflow]);

  if (!isOpen) return null;

  return (
    <ModalPortal>
    <div 
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4"
      onClick={handleBackdropClick}
    >
      <Card
        className="flex max-h-[calc(100dvh-1rem)] w-full max-w-6xl flex-col overflow-hidden p-0 sm:max-h-[90vh]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-shrink-0 flex-col gap-3 border-b border-border px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6 sm:py-4">
          <h2 className="min-w-0 text-lg font-semibold text-foreground sm:text-2xl">
            {effectiveAgentMode === 'workflow'
              ? (editingPreset ? 'Edit Workflow' : 'Add Workflow')
              : (editingPreset ? 'Edit Preset' : 'Add New Preset')}
          </h2>
          <div className="flex flex-wrap items-center gap-2 sm:justify-end">
            {editingPreset && effectiveAgentMode === 'workflow' && onDeleteWorkflow && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShowDeleteWorkflowConfirm(true)}
                className="border-red-200 text-red-600 hover:bg-red-50 hover:text-red-700 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/30"
              >
                <Trash2 className="mr-1 h-4 w-4" />
                Delete Workflow
              </Button>
            )}
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
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
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
                    {!showWorkflowLLMAdvanced && (
                      <div>
                        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
                          Workflow model
                        </label>
                        <LLMSelectionDropdown
                          availableLLMs={availableLLMs}
                          selectedLLM={(() => {
                            const selected = phaseLLM || effectiveTier1LLM || defaultAgentLLM;
                            if (!selected) return currentLLMOption;
                            return availableLLMs.find(llm =>
                              llm.provider === selected.provider && llm.model === selected.model_id
                            ) || currentLLMOption;
                          })()}
                          onLLMSelect={handleSharedWorkflowLLMSelect}
                          onRefresh={loadDefaultsFromBackend}
                          disabled={false}
                          inModal={true}
                          openDirection="down"
                        />
                        <div className="text-xs text-gray-500 mt-2">
                          Used for execution, learning, validation, planning, and debugging.
                        </div>
                        <button
                          type="button"
                          onClick={() => setShowWorkflowLLMAdvanced(true)}
                          className="mt-3 inline-flex items-center gap-1.5 text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                        >
                          <SlidersHorizontal className="w-3.5 h-3.5" />
                          Advanced workflow LLM setup
                        </button>
                      </div>
                    )}

                    {showWorkflowLLMAdvanced && (
                      <>
                        <div className="flex justify-end">
                          <button
                            type="button"
                            onClick={() => setShowWorkflowLLMAdvanced(false)}
                            className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                          >
                            Use simple setup
                          </button>
                        </div>
                    {[
                          { label: 'Tier 1 - High Reasoning', tooltip: 'Used for first-time execution (no learnings yet) and initial learning extraction.', desc: 'Most capable model for complex first-time tasks.', llm: tier1LLM, setLLM: setTier1LLM, fallbacks: tier1Fallbacks, setFallbacks: setTier1Fallbacks, num: 1 },
                          { label: 'Tier 2 - Medium Reasoning', tooltip: 'Used for execution with existing learnings and learning refinement.', desc: 'Balanced model for tasks with existing learnings.', llm: tier2LLM, setLLM: setTier2LLM, fallbacks: tier2Fallbacks, setFallbacks: setTier2Fallbacks, num: 2 },
                          { label: 'Tier 3 - Low Reasoning', tooltip: 'Used for validation (always) and mature learning refinement (2+ runs).', desc: 'Cost-efficient model for validation and mature learnings.', llm: tier3LLM, setLLM: setTier3LLM, fallbacks: tier3Fallbacks, setFallbacks: setTier3Fallbacks, num: 3 },
                        ].map((tier) => (
                        <div key={tier.num}>
                          <div className="flex items-center gap-1.5 mb-2">
                            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">
                              {tier.label}
                            </label>
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Info className="w-3 h-3 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help" />
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  <p className="text-xs">{tier.tooltip}</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={(() => {
                              const effectiveTierLLM =
                                tier.num === 1 ? effectiveTier1LLM :
                                tier.num === 2 ? effectiveTier2LLM :
                                effectiveTier3LLM;
                              if (!effectiveTierLLM) return currentLLMOption;
                              return availableLLMs.find(llm =>
                                llm.provider === effectiveTierLLM.provider && llm.model === effectiveTierLLM.model_id
                              ) || null;
                            })()}
                            onLLMSelect={(llm) => tier.setLLM({
                              provider: llm.provider as AgentLLMConfig['provider'],
                              model_id: llm.model
                            })}
                            onRefresh={loadDefaultsFromBackend}
                            disabled={false}
                            inModal={true}
                            openDirection="down"
                          />
                          <div className="mt-1.5">
                            {tier.fallbacks.map((fb, i) => (
                              <span key={`t${tier.num}-fb-${i}`} className="inline-flex items-center gap-1 mr-1 mb-1 px-2 py-0.5 bg-gray-100 dark:bg-gray-700 text-xs rounded-full">
                                {fb.provider}/{fb.model_id.split('/').pop()}
                                <button type="button" onClick={() => tier.setFallbacks(prev => prev.filter((_, idx) => idx !== i))} className="text-gray-400 hover:text-red-500">
                                  <X className="w-3 h-3" />
                                </button>
                              </span>
                            ))}
                            <LLMSelectionDropdown
                              availableLLMs={availableLLMs.filter(llm => {
                                const effectiveTierLLM =
                                  tier.num === 1 ? effectiveTier1LLM :
                                  tier.num === 2 ? effectiveTier2LLM :
                                  effectiveTier3LLM;
                                return !(
                                  effectiveTierLLM &&
                                  llm.provider === effectiveTierLLM.provider &&
                                  llm.model === effectiveTierLLM.model_id
                                ) && !tier.fallbacks.some(fb => fb.provider === llm.provider && fb.model_id === llm.model);
                              })}
                              selectedLLM={null}
                              onLLMSelect={(llm) => tier.setFallbacks(prev => [...prev, { provider: llm.provider, model_id: llm.model }])}
                              onRefresh={loadDefaultsFromBackend}
                              disabled={false}
                              inModal={true}
                              openDirection="down"
                              placeholder="+ Add fallback"
                            />
                          </div>
                          <div className="text-xs text-gray-500 mt-1">
                            {tier.desc}
                          </div>
                        </div>
                        ))}
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
                                  <p className="text-xs">Independent LLM for all workflow phases: planning, variable extraction, evaluation design, anonymization, plan improvement, learning consolidation, and debugging. This is separate from the tiered execution/learning/validation assignments.</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={phaseLLM ? availableLLMs.find(llm =>
                              llm.provider === phaseLLM.provider && llm.model === phaseLLM.model_id
                            ) || null : (effectiveTier1LLM ? availableLLMs.find(llm =>
                              llm.provider === effectiveTier1LLM.provider && llm.model === effectiveTier1LLM.model_id
                            ) || null : currentLLMOption)}
                            onLLMSelect={(llm) => setPhaseLLM({
                              provider: llm.provider as LLMProvider,
                              model_id: llm.model
                            })}
                            onRefresh={loadDefaultsFromBackend}
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
                    <div className="flex overflow-hidden rounded-md border border-gray-300 bg-white focus-within:ring-2 focus-within:ring-blue-500 dark:border-gray-600 dark:bg-gray-700">
                      <div className="flex flex-shrink-0 items-center gap-2 border-r border-gray-200 bg-gray-50 px-3 text-sm text-gray-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-400">
                        <Folder className="h-4 w-4" />
                        Workflow/
                      </div>
                      <input
                        type="text"
                        value={(selectedFolder?.filepath || 'Workflow/').replace(/^Workflow\//i, '')}
                        onChange={(e) => handleWorkflowFolderNameChange(e.target.value)}
                        disabled={!!editingPreset}
                        className="min-w-0 flex-1 bg-transparent px-3 py-2 text-sm text-gray-900 outline-none disabled:cursor-not-allowed disabled:opacity-60 dark:text-gray-100"
                        aria-label="Workflow folder name"
                        required
                      />
                    </div>
                    <div className="flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
                      <span className="min-w-0 truncate">
                        {editingPreset
                          ? 'Folder path cannot be changed while editing.'
                          : `Default folder: ${selectedFolder?.filepath || 'Workflow/workflow'}`}
                      </span>
                      {!editingPreset && (
                        <button
                          type="button"
                          data-folder-button
                          onClick={handleSelectFolders}
                          className="flex-shrink-0 text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                        >
                          Choose existing
                        </button>
                      )}
                    </div>
                    {!editingPreset && selectedFolder && workflowFolderEdited && (
                      <div className="flex justify-end">
                        <button
                          type="button"
                          onClick={() => {
                            setWorkflowFolderEdited(false);
                            setSelectedFolder(makeWorkflowFolder(sanitizeWorkflowFolderName(label)));
                          }}
                          className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                        >
                          Reset to workflow name
                        </button>
                      </div>
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
                    workflowPath={workflowSecretPath}
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

                      {/* CDP */}
                      <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                        browserMode === 'cdp'
                          ? 'border-green-500 dark:border-green-500 bg-green-50 dark:bg-green-950/40'
                          : 'border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800/40'
                      }`}>
                        <input type="radio" name="presetBrowserMode" checked={browserMode === 'cdp'} onChange={() => setBrowserMode('cdp')} className="mt-0.5 w-4 h-4 text-green-500 accent-green-500" />
                        <div className="flex-1">
                          <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Connect to Local Chrome (CDP)</div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            Agent connects to your real Chrome browser. Chrome may come to the foreground while it works.
                          </div>
                        </div>
                      </label>

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

                    </div>

                    {/* CDP configuration sub-panel */}
                    {browserMode === 'cdp' && (
                      <div className="p-3 rounded-lg bg-gray-100 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700">
                        <div className="mb-3 rounded-md border border-amber-300 dark:border-amber-700/60 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                          CDP drives visible Chrome and can steal keyboard focus. Use headless mode for background runs, or use a dedicated automation Chrome/profile/port for schedules.
                        </div>
                        <div className="flex flex-col gap-4 items-stretch xl:flex-row">
                          {/* Left: port + status */}
                          <div className="flex-1 space-y-3">
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
                                  <span className="text-sm text-red-600 dark:text-red-400">Chrome is not reachable on port {cdpPort}.</span>
                                </>
                              ) : (
                                <span className="text-xs text-gray-500">Click &quot;Check Connection&quot; to verify Chrome is reachable.</span>
                              )}
                            </div>
                          </div>

                          {/* Right: instructions */}
                          <div className="w-full flex-shrink-0 rounded-lg bg-white dark:bg-gray-900/80 border border-gray-300 dark:border-gray-600 p-2.5 space-y-1.5 flex flex-col xl:w-64">
                            <p className="text-xs font-medium text-gray-700 dark:text-gray-300">Launch Chrome with CDP</p>
                            {typeof navigator !== 'undefined' && navigator.platform?.includes('Mac') && (
                              <div className="space-y-1">
                                <a
                                  href={`${getApiBaseUrl()}/api/downloads/chrome-cdp-macOS.zip`}
                                  download="Chrome-CDP-macOS.zip"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  onClick={(e) => e.stopPropagation()}
                                  className="inline-flex items-center gap-1.5 px-2 py-1 text-xs font-medium bg-green-600 hover:bg-green-500 text-white rounded-md transition-colors"
                                >
                                  <Download className="w-3 h-3" />
                                  Download Chrome CDP.app (macOS)
                                </a>
                                <ol className="text-xs text-gray-500 dark:text-gray-400 list-decimal list-inside space-y-0.5">
                                  <li>Double-click the zip to unzip.</li>
                                  <li>Drag <strong className="text-gray-700 dark:text-gray-300">Chrome CDP.app</strong> to <strong className="text-gray-700 dark:text-gray-300">Applications</strong>.</li>
                                  <li>Open from Spotlight (⌘+Space) or Launchpad.</li>
                                </ol>
                                <p className="text-xs text-gray-500 dark:text-gray-400">If macOS says &quot;damaged&quot;, run in Terminal:</p>
                                <code className="block bg-gray-200 dark:bg-gray-950 px-2 py-1 rounded text-[10px] font-mono text-amber-600 dark:text-amber-400 border border-gray-300 dark:border-gray-700">
                                  xattr -c /Applications/Chrome\ CDP.app
                                </code>
                                <p className="text-xs text-gray-400 dark:text-gray-600">then open it again, or right-click → Open.</p>
                              </div>
                            )}
                            <p className="text-xs text-gray-500 dark:text-gray-400">Or run in Terminal (close all Chrome windows first):</p>
                            <code className="block bg-gray-200 dark:bg-gray-950 px-2 py-1.5 rounded text-[11px] font-mono break-all text-green-700 dark:text-green-400 border border-gray-300 dark:border-gray-700">
                              {typeof navigator !== 'undefined' && navigator.platform?.includes('Mac')
                                ? `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=${cdpPort}`
                                : `google-chrome --remote-debugging-port=${cdpPort}`}
                            </code>
                          </div>
                        </div>
                      </div>
                    )}
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
                          onRefresh={loadDefaultsFromBackend}
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
                        <div className="flex min-w-0 items-center gap-2">
                          <Folder className="w-4 h-4 text-blue-600" />
                          <span className="truncate text-sm text-gray-900 dark:text-gray-100">{selectedFolder.filepath}</span>
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
                            onChange={(e) => setInternalAgentMode(e.target.value as 'multi-agent' | 'workflow')}
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
          agentMode={effectiveAgentMode as 'multi-agent' | 'workflow'}
        />
        <ConfirmationDialog
          isOpen={showDeleteWorkflowConfirm}
          onClose={() => !deletingWorkflow && setShowDeleteWorkflowConfirm(false)}
          onConfirm={handleDeleteWorkflowConfirm}
          title="Delete Workflow"
          message={
            editingPreset?.selectedFolder?.filepath
              ? `Delete workflow "${editingPreset.label}" and permanently remove the folder \`${editingPreset.selectedFolder.filepath}\`? This cannot be undone.`
              : `Delete workflow "${editingPreset?.label || ''}"? This cannot be undone.`
          }
          confirmText="Delete Workflow"
          type="danger"
          isLoading={deletingWorkflow}
        />
        </div>
      </Card>
    </div>
    </ModalPortal>
  );
});

PresetModal.displayName = 'PresetModal';

export default PresetModal;
