import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Textarea } from './ui/Textarea';
import { Card } from './ui/Card';
import { CheckCircle2, ChevronDown, Folder, KeyRound, Loader2, Plus, Settings, SlidersHorizontal, Trash2, X } from 'lucide-react';
import { FolderSelectionDialog } from './FolderSelectionDialog';
import { ToolSelectionSection } from './ToolSelectionSection';
import { SkillSelectionSection } from './skills/SkillSelectionSection';
import { SecretSelectionSection } from './secrets/SecretSelectionSection';
import ConfirmationDialog from './ui/ConfirmationDialog';
import type { CustomPreset } from '../types/preset';
import type { PlannerFile, PresetLLMConfig, AgentLLMConfig, AgentLLMFallback, LLMProvider } from '../services/api-types';
import { useLLMStore } from '../stores/useLLMStore';
import { useModeStore } from '../stores/useModeStore';
import { agentApi } from '../services/api';
import LLMSelectionDropdown from './LLMSelectionDropdown';
import LLMRoleSelector from './LLMRoleSelector';
import WorkflowLLMTierPreview from './WorkflowLLMTierPreview';
import type { LLMOption } from '../types/llm';
import ModalPortal from './ui/ModalPortal';
import { getWorkflowLLMOptions, getWorkflowLLMTierDefaults, getWorkflowProviderOptions } from '../utils/workflowLLMTierDefaults';
import { llmOptionMatchesRef, llmOptionsKey } from '../utils/llmConfigDisplay';
import { mergeCdpPorts } from '../utils/cdpSetup';
import { secretsApi } from '../api/secrets';
import { useChatStore } from '../stores/useChatStore';
import BrowserAutomationSettings from './BrowserAutomationSettings';

type WorkflowLLMRoleKey = 'tier1' | 'tier2' | 'tier3' | 'builder' | 'maintenance' | 'pulse';

type WorkflowLLMRoleRow = {
  key: WorkflowLLMRoleKey;
  label: string;
  description: string;
  value: AgentLLMConfig | null;
  defaultValue: AgentLLMConfig | null;
  onSelect: (llm: LLMOption) => void;
  onReset: () => void;
  fallbacks?: AgentLLMFallback[];
  setFallbacks?: React.Dispatch<React.SetStateAction<AgentLLMFallback[]>>;
};

function agentLLMUsesProvider(config: AgentLLMConfig | null | undefined, provider: string): boolean {
  return config?.provider === provider || Boolean(config?.fallbacks?.some(fallback => fallback.provider === provider));
}

function sameAgentLLM(left: AgentLLMConfig | null | undefined, right: AgentLLMConfig | null | undefined): boolean {
  if (!left || !right) return left === right;
  return left.provider === right.provider
    && left.model_id === right.model_id
    && llmOptionsKey(left.options) === llmOptionsKey(right.options);
}

interface PresetModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (label: string, query: string, selectedServers?: string[], selectedTools?: string[], selectedSkills?: string[], agentMode?: 'multi-agent' | 'workflow', selectedFolder?: PlannerFile, llmConfig?: PresetLLMConfig, useCodeExecutionMode?: boolean, selectedSecrets?: string[], selectedGlobalSecretNames?: string[] | null, browserMode?: 'none' | 'auto' | 'headless' | 'cdp', cdpPorts?: number[]) => boolean | void | Promise<boolean | void>;
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
  const [browserMode, setBrowserModeState] = useState<'none' | 'auto' | 'headless' | 'cdp'>('auto');
  const enableBrowserAccess = browserMode === 'auto' || browserMode === 'headless' || browserMode === 'cdp';
  const [cdpPort, setCdpPort] = useState(9222);
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null);
  const [cdpError, setCdpError] = useState<string | null>(null);
  const [cdpChecking, setCdpChecking] = useState(false);
  const [showDeleteWorkflowConfirm, setShowDeleteWorkflowConfirm] = useState(false);
  const [deletingWorkflow, setDeletingWorkflow] = useState(false);
  const setBrowserMode = useCallback((mode: 'none' | 'auto' | 'headless' | 'cdp') => {
    setBrowserModeState(mode)
  }, [])

  const [builderLLM, setBuilderLLM] = useState<AgentLLMConfig | null>(null);
  const [maintenanceLLM, setMaintenanceLLM] = useState<AgentLLMConfig | null>(null);
  const [pulseLLM, setPulseLLM] = useState<AgentLLMConfig | null>(null);
  const [tier1LLM, setTier1LLM] = useState<AgentLLMConfig | null>(null);
  const [tier2LLM, setTier2LLM] = useState<AgentLLMConfig | null>(null);
  const [tier3LLM, setTier3LLM] = useState<AgentLLMConfig | null>(null);
  const [tier1Fallbacks, setTier1Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [tier2Fallbacks, setTier2Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [tier3Fallbacks, setTier3Fallbacks] = useState<AgentLLMFallback[]>([]);
  const [showWorkflowLLMAdvanced, setShowWorkflowLLMAdvanced] = useState(false);
  const [expandedWorkflowLLMRole, setExpandedWorkflowLLMRole] = useState<WorkflowLLMRoleKey | null>(null);
  const [claudeCodeToken, setClaudeCodeToken] = useState('');
  const [claudeCredentialConfigured, setClaudeCredentialConfigured] = useState(false);
  const [isLoadingClaudeCredential, setIsLoadingClaudeCredential] = useState(false);
  const [isDeletingClaudeCredential, setIsDeletingClaudeCredential] = useState(false);
  const [isSavingPreset, setIsSavingPreset] = useState(false);

  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore();
  const primaryConfig = useLLMStore(state => state.primaryConfig);
  const availableLLMs = useLLMStore(state => state.availableLLMs);
  const providerManifest = useLLMStore(state => state.providerManifest);
  const providerManifestLoaded = useLLMStore(state => state.providerManifestLoaded);
  const loadProviderManifest = useLLMStore(state => state.loadProviderManifest);
  const getCurrentLLMOption = useLLMStore(state => state.getCurrentLLMOption);
  const loadDefaultsFromBackend = useLLMStore(state => state.loadDefaultsFromBackend);

  const effectiveAgentMode = useMemo(() => {
    if (fixedAgentMode) return fixedAgentMode;
    if (propAgentMode) return propAgentMode as 'multi-agent' | 'workflow';
    return internalAgentMode;
  }, [fixedAgentMode, propAgentMode, internalAgentMode]);

  const workflowLLMOptions = useMemo(
    () => getWorkflowLLMOptions(availableLLMs, providerManifest),
    [availableLLMs, providerManifest]
  );
  const providerProfileOptions = useMemo(
    () => getWorkflowProviderOptions(providerManifest),
    [providerManifest]
  );

  useEffect(() => {
    if (isOpen && effectiveAgentMode === 'workflow' && !providerManifestLoaded) {
      loadProviderManifest();
    }
  }, [effectiveAgentMode, isOpen, loadProviderManifest, providerManifestLoaded]);

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
    setCdpError(null);
    try {
      const result = await agentApi.checkCdpPort(port);
      setCdpConnected(result.connected);
      setCdpError(result.connected ? null : result.error || null);
    } catch {
      setCdpConnected(false);
      setCdpError('Unable to check the CDP port.');
    } finally {
      setCdpChecking(false);
    }
  }, []);

  // Auto-check CDP for both automatic and required-CDP modes.
  useEffect(() => {
    if ((browserMode !== 'auto' && browserMode !== 'cdp') || !enableBrowserAccess) {
      setCdpConnected(null);
      setCdpError(null);
      return;
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort);
    }, 500); // debounce
    return () => clearTimeout(timer);
  }, [browserMode, cdpPort, enableBrowserAccess, checkCdpConnection]);

  const hasLLMOptions = (options?: Record<string, unknown>) => Boolean(options && Object.keys(options).length > 0);
  const toAgentLLMConfig = useCallback((llm: LLMOption): AgentLLMConfig => ({
    ...(llm.id ? { published_llm_id: llm.id } : {}),
    provider: llm.provider as AgentLLMConfig['provider'],
    model_id: llm.model,
    ...(hasLLMOptions(llm.options) ? { options: llm.options } : {}),
  }), []);
  const toAgentLLMFallback = useCallback((llm: LLMOption): AgentLLMFallback => {
    const config = toAgentLLMConfig(llm);
    return {
      ...(config.published_llm_id ? { published_llm_id: config.published_llm_id } : {}),
      provider: config.provider,
      model_id: config.model_id,
      ...(hasLLMOptions(config.options) ? { options: config.options } : {}),
    };
  }, [toAgentLLMConfig]);
  const findLLMOptionForConfig = useCallback((config?: AgentLLMConfig | null): LLMOption | null => {
    if (!config?.provider || !config?.model_id) return null;
    if (config.published_llm_id) {
      const byID = workflowLLMOptions.find(llm => llm.id === config.published_llm_id);
      if (byID) return byID;
    }
    return workflowLLMOptions.find(llm => llmOptionMatchesRef(llm, config)) || null;
  }, [workflowLLMOptions]);
  const llmConfigKey = (llm: { provider?: string; model_id?: string; published_llm_id?: string; options?: Record<string, unknown> }) =>
    llm.published_llm_id ? `id:${llm.published_llm_id}` : `model:${llm.provider}/${llm.model_id}/${llmOptionsKey(llm.options)}`;
  const llmOptionKey = (llm: LLMOption) =>
    llm.id ? `id:${llm.id}` : `model:${llm.provider}/${llm.model}/${llmOptionsKey(llm.options)}`;

  // Non-workflow presets still use the same explicit role contract.
  const handleLLMSelect = useCallback((llm: LLMOption) => {
    const selected = toAgentLLMConfig(llm);
    setLlmConfig({
      schema_version: 2,
      mode: 'explicit',
      builder_llm: selected,
      maintenance_llm: selected,
      pulse_llm: selected,
      tiered_config: { tier_1: selected, tier_2: selected, tier_3: selected },
    });
  }, [toAgentLLMConfig]);

  // Get current LLM option for display
  const currentLLMOption = useMemo(() => {
    if (llmConfig?.mode === 'provider_profile' && llmConfig.provider) {
      return providerProfileOptions.find(option => option.provider === llmConfig.provider) ?? null;
    }
    if (!llmConfig?.mode && llmConfig?.provider) {
      const legacyProfile = providerProfileOptions.find(option => option.provider === llmConfig.provider);
      if (legacyProfile) return legacyProfile;
    }
    if (llmConfig?.builder_llm) {
      return findLLMOptionForConfig(llmConfig.builder_llm);
    }
    return getCurrentLLMOption();
  }, [llmConfig, findLLMOptionForConfig, getCurrentLLMOption, providerProfileOptions]);

  const defaultAgentLLM = useMemo<AgentLLMConfig | null>(() => {
    if (llmConfig?.builder_llm) {
      return llmConfig.builder_llm;
    }
    if (primaryConfig.provider && primaryConfig.model_id) {
      return {
        provider: primaryConfig.provider as AgentLLMConfig['provider'],
        model_id: primaryConfig.model_id,
        options: primaryConfig.options
      };
    }
    return null;
  }, [llmConfig, primaryConfig]);

  const workflowDefaultTierLLMs = useMemo(() => {
    return currentLLMOption ? getWorkflowLLMTierDefaults(currentLLMOption, providerManifest) : null;
  }, [currentLLMOption, providerManifest]);

  const effectiveTier1LLM = useMemo<AgentLLMConfig | null>(() => tier1LLM || workflowDefaultTierLLMs?.tier1 || defaultAgentLLM, [tier1LLM, workflowDefaultTierLLMs, defaultAgentLLM]);
  const effectiveTier2LLM = useMemo<AgentLLMConfig | null>(() => tier2LLM || workflowDefaultTierLLMs?.tier2 || defaultAgentLLM, [tier2LLM, workflowDefaultTierLLMs, defaultAgentLLM]);
  const effectiveTier3LLM = useMemo<AgentLLMConfig | null>(() => tier3LLM || workflowDefaultTierLLMs?.tier3 || defaultAgentLLM, [tier3LLM, workflowDefaultTierLLMs, defaultAgentLLM]);
  const effectiveBuilderLLM = useMemo<AgentLLMConfig | null>(() => builderLLM || workflowDefaultTierLLMs?.builder || effectiveTier1LLM || defaultAgentLLM, [builderLLM, workflowDefaultTierLLMs, effectiveTier1LLM, defaultAgentLLM]);
  const effectiveMaintenanceLLM = useMemo<AgentLLMConfig | null>(() => maintenanceLLM || workflowDefaultTierLLMs?.maintenance || effectiveTier1LLM || defaultAgentLLM, [maintenanceLLM, workflowDefaultTierLLMs, effectiveTier1LLM, defaultAgentLLM]);
  const effectivePulseLLM = useMemo<AgentLLMConfig | null>(() => pulseLLM || workflowDefaultTierLLMs?.pulse || defaultAgentLLM, [pulseLLM, workflowDefaultTierLLMs, defaultAgentLLM]);
  const selectedWorkflowLLMOption = useMemo(() => {
    if (llmConfig && currentLLMOption) return currentLLMOption;
    const selected = effectiveBuilderLLM || effectiveTier1LLM || defaultAgentLLM;
    if (!selected) return currentLLMOption;
    return findLLMOptionForConfig(selected) || currentLLMOption;
  }, [currentLLMOption, defaultAgentLLM, effectiveBuilderLLM, effectiveTier1LLM, findLLMOptionForConfig, llmConfig]);

  const formatAgentLLMConfig = useCallback((config?: AgentLLMConfig | null) => {
    if (!config?.provider || !config?.model_id) return 'Not resolved';
    return `${config.provider}/${config.model_id}`;
  }, []);

  // Draft create paths can collide with existing workflows, so only load scoped secrets for persisted workflows.
  const workflowSecretPath = editingPreset ? selectedFolder?.filepath : undefined;
  const workflowCredentialPath = editingPreset ? selectedFolder?.filepath : undefined;

  const usesClaudeCode = useMemo(() => {
    return selectedWorkflowLLMOption?.provider === 'claude-code'
      || [effectiveTier1LLM, effectiveTier2LLM, effectiveTier3LLM, effectiveBuilderLLM, effectiveMaintenanceLLM, effectivePulseLLM]
        .some(config => agentLLMUsesProvider(config, 'claude-code'))
      || [...tier1Fallbacks, ...tier2Fallbacks, ...tier3Fallbacks].some(fallback => fallback.provider === 'claude-code');
  }, [effectiveBuilderLLM, effectiveMaintenanceLLM, effectivePulseLLM, effectiveTier1LLM, effectiveTier2LLM, effectiveTier3LLM, selectedWorkflowLLMOption?.provider, tier1Fallbacks, tier2Fallbacks, tier3Fallbacks]);

  const hasAdvancedWorkflowLLMConfig = useCallback((presetLLM?: PresetLLMConfig | null) => {
    return presetLLM?.mode === 'explicit';
  }, []);

  const handleSharedWorkflowLLMSelect = useCallback((llm: LLMOption) => {
    const defaults = getWorkflowLLMTierDefaults(llm, providerManifest);
    setLlmConfig({ schema_version: 2, mode: 'provider_profile', provider: llm.provider as LLMProvider });
    setTier1LLM(defaults.tier1);
    setTier2LLM(defaults.tier2);
    setTier3LLM(defaults.tier3);
    setBuilderLLM(defaults.builder);
    setMaintenanceLLM(defaults.maintenance);
    setPulseLLM(defaults.pulse);
    setTier1Fallbacks([]);
    setTier2Fallbacks([]);
    setTier3Fallbacks([]);
    setExpandedWorkflowLLMRole(null);
  }, [providerManifest]);

  const useManagedWorkflowLLMDefaults = useCallback(() => {
    const provider = selectedWorkflowLLMOption?.provider || currentLLMOption?.provider;
    if (provider) {
      setLlmConfig({ schema_version: 2, mode: 'provider_profile', provider: provider as LLMProvider });
    }
    setShowWorkflowLLMAdvanced(false);
    setExpandedWorkflowLLMRole(null);
    setBuilderLLM(null);
    setMaintenanceLLM(null);
    setPulseLLM(null);
    setTier1LLM(null);
    setTier2LLM(null);
    setTier3LLM(null);
    setTier1Fallbacks([]);
    setTier2Fallbacks([]);
    setTier3Fallbacks([]);
  }, [currentLLMOption?.provider, selectedWorkflowLLMOption?.provider]);

  useEffect(() => {
    if (!isOpen) return;
    setExpandedWorkflowLLMRole(null);
    setClaudeCodeToken('');
  }, [editingPreset?.id, isOpen]);

  useEffect(() => {
    if (!isOpen || effectiveAgentMode !== 'workflow' || !workflowCredentialPath) {
      setClaudeCredentialConfigured(false);
      setIsLoadingClaudeCredential(false);
      return;
    }
    let cancelled = false;
    setIsLoadingClaudeCredential(true);
    void secretsApi.getWorkflowClaudeCodeCredentialStatus(workflowCredentialPath)
      .then(status => {
        if (!cancelled) setClaudeCredentialConfigured(status.configured);
      })
      .catch(() => {
        if (!cancelled) setClaudeCredentialConfigured(false);
      })
      .finally(() => {
        if (!cancelled) setIsLoadingClaudeCredential(false);
      });
    return () => { cancelled = true; };
  }, [effectiveAgentMode, isOpen, workflowCredentialPath]);

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
        schema_version: 2,
        mode: 'provider_profile',
        provider: primaryConfig.provider as PresetLLMConfig['provider'],
      };
      setLlmConfig(presetLLM);
      // Load browser mode: prefer explicit browserMode, fall back to legacy derivation
      if (editingPreset.browserMode) {
        setBrowserModeState(editingPreset.browserMode);
      } else {
        // Legacy fallback for presets saved before browserMode was added
        if (editingPreset.enableBrowserAccess) {
          setBrowserModeState('headless');
        } else {
          setBrowserModeState('none');
        }
      }
      setCdpPort(editingPreset.cdpPorts?.[0] || 9222);
      // Load agent-specific configs if available
      setBuilderLLM(presetLLM.builder_llm || null);
      setMaintenanceLLM(presetLLM.maintenance_llm || null);
      setPulseLLM(presetLLM.pulse_llm || null);
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
        schema_version: 2,
        mode: 'provider_profile',
        provider: primaryConfig.provider as PresetLLMConfig['provider'],
      };
      setLlmConfig(defaultLLM);
      setBrowserModeState('auto'); // Prefer connected CDP, otherwise headless
      setCdpPort(9222);
      // Initialize agent-specific configs to null (will use legacy default)
      setBuilderLLM(null);
      setMaintenanceLLM(null);
      setPulseLLM(null);
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

  const handleDeleteClaudeCredential = useCallback(async () => {
    const workspacePath = selectedFolder?.filepath;
    if (!workspacePath) return;
    setIsDeletingClaudeCredential(true);
    try {
      await secretsApi.deleteWorkflowClaudeCodeCredential(workspacePath);
      setClaudeCredentialConfigured(false);
      setClaudeCodeToken('');
      useChatStore.getState().addToast('Workflow Claude Code token removed; saved Claude login will be used.', 'success');
    } catch (error) {
      const detail = error instanceof Error ? error.message : 'Unknown error';
      useChatStore.getState().addToast(`Failed to remove Claude Code token: ${detail}`, 'error');
    } finally {
      setIsDeletingClaudeCredential(false);
    }
  }, [selectedFolder?.filepath]);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    const isQueryRequired = effectiveAgentMode !== 'workflow';
    if (!label.trim() || (isQueryRequired && !query.trim())) return;
    if (effectiveAgentMode === 'workflow' && !selectedFolder) {
      alert('Folder selection is required for workflow presets');
      return;
    }

    setIsSavingPreset(true);
    try {
      
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
        const workflowBaseLLMConfig = { ...((llmConfig || {}) as PresetLLMConfig & { execution_llm?: unknown; learning_llm?: unknown }) };
        delete workflowBaseLLMConfig.execution_llm;
        delete workflowBaseLLMConfig.learning_llm;
        const withFallbacks = (llm: AgentLLMConfig, fallbacks: AgentLLMFallback[]): AgentLLMConfig => ({
          ...llm,
          ...(fallbacks.length > 0 ? { fallbacks } : {}),
        });
        const explicitTieredConfig = effectiveTier1LLM && effectiveTier2LLM && effectiveTier3LLM ? {
          tier_1: withFallbacks(effectiveTier1LLM, tier1Fallbacks),
          tier_2: withFallbacks(effectiveTier2LLM, tier2Fallbacks),
          tier_3: withFallbacks(effectiveTier3LLM, tier3Fallbacks),
        } : undefined;

        if (!showWorkflowLLMAdvanced) {
          if (!workflowBaseLLMConfig.provider) {
            alert('Select a coding agent provider');
            return;
          }
          finalLLMConfig = {
            ...workflowBaseLLMConfig,
            schema_version: 2,
            mode: 'provider_profile',
            builder_llm: undefined,
            maintenance_llm: undefined,
            pulse_llm: undefined,
            chief_of_staff_llm: undefined,
            tiered_config: undefined,
          };
        } else {
          if (!effectiveBuilderLLM || !effectiveMaintenanceLLM || !effectivePulseLLM || !explicitTieredConfig) {
            alert('Builder, Maintenance, Pulse, and all three execution tiers are required');
            return;
          }
          finalLLMConfig = {
            ...workflowBaseLLMConfig,
            schema_version: 2,
            mode: 'explicit',
            provider: undefined,
            builder_llm: effectiveBuilderLLM,
            maintenance_llm: effectiveMaintenanceLLM,
            pulse_llm: effectivePulseLLM,
            tiered_config: explicitTieredConfig,
          };
        }
      }
      console.log('[PRESET_MODAL] Agent LLM configs being saved:', {
        builderLLM: builderLLM,
        effectiveBuilderLLM: effectiveBuilderLLM || undefined,
        maintenanceLLM: maintenanceLLM,
        effectiveMaintenanceLLM: effectiveMaintenanceLLM || undefined,
        pulseLLM: pulseLLM,
        effectivePulseLLM: effectivePulseLLM || undefined,
        defaultAgentLLM: defaultAgentLLM || undefined,
        effectiveTier1LLM: effectiveTier1LLM || undefined,
        effectiveTier2LLM: effectiveTier2LLM || undefined,
        effectiveTier3LLM: effectiveTier3LLM || undefined,
        finalLLMConfig: finalLLMConfig,
      });
      const cdpPorts = browserMode === 'auto' || browserMode === 'cdp'
        ? mergeCdpPorts(cdpPort, editingPreset?.cdpPorts)
        : [];
      const saved = await onSave(
        label.trim(),
        effectiveAgentMode === 'workflow' ? '' : query.trim(),
        selectedServers,
        selectedTools,
        selectedSkills, // Skill folder names for workflow
        effectiveAgentMode,
        selectedFolder || undefined,
        finalLLMConfig,
        false, // useCodeExecutionMode — backend determines mode from browser selection
        selectedSecrets, // Secret names for workflow injection
        selectedGlobalSecrets, // Per-preset global secret selection (null=all)
        browserMode, // Browser mode: none|auto|headless|cdp
        cdpPorts
      );
      if (saved === false) return;
      if (effectiveAgentMode === 'workflow' && usesClaudeCode && claudeCodeToken.trim() && selectedFolder?.filepath) {
        await secretsApi.storeWorkflowClaudeCodeCredential(selectedFolder.filepath, claudeCodeToken.trim());
        setClaudeCredentialConfigured(true);
        setClaudeCodeToken('');
        useChatStore.getState().addToast('Workflow Claude Code token saved.', 'success');
      }
      onClose();
    } catch (error) {
      const serverDetail = (error as { response?: { data?: unknown } })?.response?.data;
      const detail = typeof serverDetail === 'string' && serverDetail.trim() !== ''
        ? serverDetail.trim()
        : error instanceof Error ? error.message : 'Unknown error';
      useChatStore.getState().addToast(`Failed to save automation: ${detail}`, 'error');
    } finally {
      setIsSavingPreset(false);
    }
  }, [label, query, effectiveAgentMode, selectedFolder, selectedServers, selectedTools, selectedSkills, selectedSecrets, selectedGlobalSecrets, llmConfig, builderLLM, effectiveBuilderLLM, maintenanceLLM, effectiveMaintenanceLLM, pulseLLM, effectivePulseLLM, browserMode, cdpPort, editingPreset?.cdpPorts, tier1Fallbacks, tier2Fallbacks, tier3Fallbacks, onSave, onClose, defaultAgentLLM, effectiveTier1LLM, effectiveTier2LLM, effectiveTier3LLM, showWorkflowLLMAdvanced, usesClaudeCode, claudeCodeToken]);

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

  const executionLLMRows: WorkflowLLMRoleRow[] = [
    {
      key: 'tier1', label: 'High reasoning', description: 'First runs and complex execution.',
      value: effectiveTier1LLM, defaultValue: workflowDefaultTierLLMs?.tier1 || defaultAgentLLM,
      onSelect: llm => setTier1LLM(toAgentLLMConfig(llm)),
      onReset: () => { setTier1LLM(null); setTier1Fallbacks([]); },
      fallbacks: tier1Fallbacks, setFallbacks: setTier1Fallbacks,
    },
    {
      key: 'tier2', label: 'Medium reasoning', description: 'Execution after useful learnings exist.',
      value: effectiveTier2LLM, defaultValue: workflowDefaultTierLLMs?.tier2 || defaultAgentLLM,
      onSelect: llm => setTier2LLM(toAgentLLMConfig(llm)),
      onReset: () => { setTier2LLM(null); setTier2Fallbacks([]); },
      fallbacks: tier2Fallbacks, setFallbacks: setTier2Fallbacks,
    },
    {
      key: 'tier3', label: 'Low reasoning', description: 'Validation and mature learned tasks.',
      value: effectiveTier3LLM, defaultValue: workflowDefaultTierLLMs?.tier3 || defaultAgentLLM,
      onSelect: llm => setTier3LLM(toAgentLLMConfig(llm)),
      onReset: () => { setTier3LLM(null); setTier3Fallbacks([]); },
      fallbacks: tier3Fallbacks, setFallbacks: setTier3Fallbacks,
    },
  ];
  const supportLLMRows: WorkflowLLMRoleRow[] = [
    {
      key: 'builder', label: 'Builder', description: 'Chat, planning, evaluation design, and coordination.',
      value: effectiveBuilderLLM, defaultValue: workflowDefaultTierLLMs?.builder || workflowDefaultTierLLMs?.tier1 || defaultAgentLLM,
      onSelect: llm => setBuilderLLM(toAgentLLMConfig(llm)), onReset: () => setBuilderLLM(null),
    },
    {
      key: 'maintenance', label: 'Maintenance', description: 'Harden, Goal Advisor, and deeper health reviews.',
      value: effectiveMaintenanceLLM, defaultValue: workflowDefaultTierLLMs?.maintenance || workflowDefaultTierLLMs?.tier1 || defaultAgentLLM,
      onSelect: llm => setMaintenanceLLM(toAgentLLMConfig(llm)), onReset: () => setMaintenanceLLM(null),
    },
    {
      key: 'pulse', label: 'Pulse', description: 'Scheduled post-run QA and routine coordination.',
      value: effectivePulseLLM, defaultValue: workflowDefaultTierLLMs?.pulse || defaultAgentLLM,
      onSelect: llm => setPulseLLM(toAgentLLMConfig(llm)), onReset: () => setPulseLLM(null),
    },
  ];
  const allWorkflowLLMRows = [...executionLLMRows, ...supportLLMRows];
  const isWorkflowLLMRoleCustomized = (row: WorkflowLLMRoleRow) => !sameAgentLLM(row.value, row.defaultValue) || Boolean(row.fallbacks?.length);
  const customizedWorkflowLLMRoleCount = allWorkflowLLMRows.filter(isWorkflowLLMRoleCustomized).length;

  const renderWorkflowLLMRoleRow = (row: WorkflowLLMRoleRow) => {
    const expanded = expandedWorkflowLLMRole === row.key;
    const customized = isWorkflowLLMRoleCustomized(row);
    const fallbacks = row.fallbacks ?? [];
    const setFallbacks = row.setFallbacks;
    return (
      <div key={row.key} className="border-t border-gray-200 first:border-t-0 dark:border-gray-700">
        <button
          type="button"
          onClick={() => setExpandedWorkflowLLMRole(expanded ? null : row.key)}
          className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors hover:bg-gray-50 dark:hover:bg-gray-800/70"
          aria-expanded={expanded}
        >
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-gray-800 dark:text-gray-100">{row.label}</span>
              <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium ${customized
                ? 'bg-blue-100 text-blue-700 dark:bg-blue-950/60 dark:text-blue-300'
                : 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400'
              }`}>
                {customized ? 'Customized' : 'Provider default'}
              </span>
            </div>
            <div className="mt-0.5 truncate text-[11px] text-gray-500 dark:text-gray-400">{row.description}</div>
          </div>
          <div className="min-w-0 max-w-[45%] text-right">
            <div className="truncate font-mono text-[11px] text-gray-700 dark:text-gray-200" title={formatAgentLLMConfig(row.value)}>
              {formatAgentLLMConfig(row.value)}
            </div>
            {fallbacks.length > 0 && <div className="text-[10px] text-gray-400">{fallbacks.length} fallback{fallbacks.length === 1 ? '' : 's'}</div>}
          </div>
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-gray-400 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
        {expanded && (
          <div className="space-y-3 border-t border-gray-100 bg-white px-3 py-3 dark:border-gray-700 dark:bg-gray-900/40">
            <LLMRoleSelector
              availableLLMs={workflowLLMOptions}
              value={row.value}
              onLLMSelect={row.onSelect}
              disabled={false}
            />
            {customized && (
              <button type="button" onClick={row.onReset} className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400">
                Reset this role to provider default
              </button>
            )}
            {setFallbacks && (
              <details className="rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 dark:border-gray-700 dark:bg-gray-800/60">
                <summary className="cursor-pointer text-xs font-medium text-gray-600 dark:text-gray-300">
                  Fallbacks{fallbacks.length > 0 ? ` (${fallbacks.length})` : ''}
                </summary>
                <div className="mt-2 space-y-2">
                  {fallbacks.map((fallback, index) => (
                    <span key={`${row.key}-fallback-${index}`} className="mr-1 inline-flex items-center gap-1 rounded-full bg-white px-2 py-0.5 text-xs dark:bg-gray-700">
                      {fallback.provider}/{fallback.model_id.split('/').pop()}
                      <button type="button" onClick={() => setFallbacks(previous => previous.filter((_, itemIndex) => itemIndex !== index))} className="text-gray-400 hover:text-red-500" aria-label={`Remove ${row.label} fallback`}>
                        <X className="h-3 w-3" />
                      </button>
                    </span>
                  ))}
                  <LLMSelectionDropdown
                    availableLLMs={workflowLLMOptions.filter(llm => {
                      const key = llmOptionKey(llm);
                      return !(row.value && llmConfigKey(row.value) === key) && !fallbacks.some(fallback => llmConfigKey(fallback) === key);
                    })}
                    selectedLLM={null}
                    onLLMSelect={llm => setFallbacks(previous => [...previous, toAgentLLMFallback(llm)])}
                    onRefresh={loadDefaultsFromBackend}
                    disabled={false}
                    inModal={true}
                    openDirection="down"
                    placeholder="+ Add fallback"
                  />
                </div>
              </details>
            )}
          </div>
        )}
      </div>
    );
  };

  if (!isOpen) return null;

  return (
    <ModalPortal>
    <div 
      className={`fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 backdrop-blur-sm ${effectiveAgentMode === 'workflow' ? 'p-0 sm:p-3' : 'p-2 sm:p-4'}`}
      onClick={handleBackdropClick}
    >
      <Card
        className={`flex w-full flex-col overflow-hidden p-0 ${effectiveAgentMode === 'workflow'
          ? 'h-[100dvh] max-h-[100dvh] max-w-none rounded-none sm:h-[calc(100dvh-1.5rem)] sm:max-h-[calc(100dvh-1.5rem)] sm:w-[calc(100vw-1.5rem)] sm:rounded-xl'
          : 'max-h-[calc(100dvh-1rem)] max-w-6xl sm:max-h-[90vh]'
        }`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-shrink-0 flex-col gap-3 border-b border-border px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6 sm:py-4">
          <h2 className="min-w-0 text-lg font-semibold text-foreground sm:text-2xl">
            {effectiveAgentMode === 'workflow'
              ? (editingPreset ? 'Edit Automation' : 'Add Automation')
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
                Delete Automation
              </Button>
            )}
            <Button
              type="submit"
              form="preset-form"
              variant="outline"
              size="sm"
              disabled={isSavingPreset || !label.trim() || (effectiveAgentMode !== 'workflow' && !query.trim()) || (effectiveAgentMode === 'workflow' && !selectedFolder)}
            >
              {isSavingPreset && <Loader2 className="mr-1 h-4 w-4 animate-spin" />}
              {editingPreset ? 'Update' : 'Save'} {effectiveAgentMode === 'workflow' ? 'Automation' : 'Preset'}
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
                    Automation Name
                  </label>
                  <Input
                    id="preset-label"
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                    placeholder="Enter automation name..."
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
                          Automation provider
                        </label>
                        <LLMSelectionDropdown
                          availableLLMs={providerProfileOptions}
                          selectedLLM={selectedWorkflowLLMOption}
                          onLLMSelect={handleSharedWorkflowLLMSelect}
                          onRefresh={loadDefaultsFromBackend}
                          disabled={false}
                          inModal={true}
                          openDirection="down"
                          title="Select automation provider"
                          placeholder="Select a coding agent"
                        />
                        <WorkflowLLMTierPreview selectedLLM={selectedWorkflowLLMOption} providerManifest={providerManifest} />
                        <div className="text-xs text-gray-500 mt-2">
                          The coding agent provider manages Builder, Maintenance, Pulse, and execution-tier defaults.
                          Those defaults update with the app as models change.
                        </div>
                        <button
                          type="button"
                          onClick={() => setShowWorkflowLLMAdvanced(true)}
                          className="mt-3 inline-flex items-center gap-1.5 text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                        >
                          <SlidersHorizontal className="w-3.5 h-3.5" />
                          Advanced automation LLM setup
                        </button>
                      </div>
                    )}

                    {showWorkflowLLMAdvanced && (
                      <>
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <div className="text-xs font-medium text-gray-800 dark:text-gray-100">Customize only what you need</div>
                            <div className="mt-0.5 text-[11px] leading-snug text-gray-500 dark:text-gray-400">
                              All roles keep the coding-agent defaults until you change one. Open a row to edit it.
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={useManagedWorkflowLLMDefaults}
                            className="shrink-0 text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                          >
                            Use managed defaults
                          </button>
                        </div>
                        <div className="space-y-3">
                          <div>
                            <div className="mb-1.5 flex items-center justify-between px-1">
                              <span className="text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Execution</span>
                              <span className="text-[10px] text-gray-400">The workflow chooses the tier automatically</span>
                            </div>
                            <div className="overflow-hidden rounded-md border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900/40">
                              {executionLLMRows.map(renderWorkflowLLMRoleRow)}
                            </div>
                          </div>
                          <div>
                            <div className="mb-1.5 px-1 text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Workflow agents</div>
                            <div className="overflow-hidden rounded-md border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900/40">
                              {supportLLMRows.map(renderWorkflowLLMRoleRow)}
                            </div>
                          </div>
                          <div className="text-[11px] text-gray-500 dark:text-gray-400">
                            {customizedWorkflowLLMRoleCount === 0
                              ? 'No custom roles yet. Everything follows the current provider defaults.'
                              : `${customizedWorkflowLLMRoleCount} customized role${customizedWorkflowLLMRoleCount === 1 ? '' : 's'}; all other roles use provider defaults.`}
                          </div>
                        </div>
                      </>
                    )}
                  </div>
                </div>

                {usesClaudeCode && (
                  <div>
                    <label className="mb-2 flex items-center gap-2 text-sm font-medium">
                      <KeyRound className="h-4 w-4" />
                      Claude Code login for this automation
                    </label>
                    <div className="rounded-md border border-violet-200 bg-violet-50/60 p-3 dark:border-violet-800 dark:bg-violet-950/20">
                      <div className="flex items-start justify-between gap-3">
                        <div className="text-xs leading-relaxed text-gray-600 dark:text-gray-300">
                          Optional. Paste a long-lived token from <code className="font-mono">claude setup-token</code>. It is private to this user and workflow. If empty, Claude Code uses its saved login.
                        </div>
                        {isLoadingClaudeCredential ? (
                          <Loader2 className="h-4 w-4 shrink-0 animate-spin text-gray-400" />
                        ) : claudeCredentialConfigured ? (
                          <span className="inline-flex shrink-0 items-center gap-1 text-xs text-emerald-700 dark:text-emerald-400">
                            <CheckCircle2 className="h-3.5 w-3.5" /> Configured
                          </span>
                        ) : null}
                      </div>
                      <input
                        type="password"
                        autoComplete="off"
                        value={claudeCodeToken}
                        onChange={event => setClaudeCodeToken(event.target.value)}
                        placeholder={claudeCredentialConfigured ? 'Paste a replacement token' : 'Paste Claude Code token'}
                        className="mt-3 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-mono text-gray-900 outline-none focus:border-violet-500 dark:border-slate-600 dark:bg-slate-900 dark:text-gray-100"
                      />
                      <div className="mt-2 flex items-center justify-between gap-3">
                        <span className="text-[11px] text-gray-500 dark:text-gray-400">Terminal Anthropic API keys are not used for this Claude Code session.</span>
                        {claudeCredentialConfigured && (
                          <button
                            type="button"
                            onClick={handleDeleteClaudeCredential}
                            disabled={isDeletingClaudeCredential || isSavingPreset}
                            className="inline-flex shrink-0 items-center gap-1 text-xs text-red-600 hover:text-red-700 disabled:opacity-50 dark:text-red-400"
                          >
                            {isDeletingClaudeCredential ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                            Remove token
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                )}
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
                          Reset to automation name
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

                <BrowserAutomationSettings
                  browserMode={browserMode}
                  onBrowserModeChange={setBrowserMode}
                  cdpPort={cdpPort}
                  onCdpPortChange={setCdpPort}
                  cdpConnected={cdpConnected}
                  cdpError={cdpError}
                  cdpChecking={cdpChecking}
                  onCheckCdpConnection={checkCdpConnection}
                />
                {/* Agent Mode Display (read-only for workflow) */}
                {hideAgentModeSelection && fixedAgentMode && (
                  <div className="p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-gray-600 dark:text-gray-400">Mode:</span>
                      <span className="text-sm font-medium text-gray-900 dark:text-white">Automation</span>
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
                        { value: 'workflow', label: 'Automation', description: 'Todo-list execution' }
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
          title="Delete Automation"
          message={
            editingPreset?.selectedFolder?.filepath
              ? `Delete automation "${editingPreset.label}" and permanently remove the folder \`${editingPreset.selectedFolder.filepath}\`? This cannot be undone.`
              : `Delete automation "${editingPreset?.label || ''}"? This cannot be undone.`
          }
          confirmText="Delete Automation"
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
