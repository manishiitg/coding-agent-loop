import React, { useState, useEffect, useRef } from 'react';
import { ChevronDown, ChevronUp, Settings, Sparkles, Code2, Search } from 'lucide-react';
import { Button } from '../../ui/Button';
import LLMSelectionDropdown from '../../LLMSelectionDropdown';
import { ToolSelectionSection } from '../../ToolSelectionSection';
import { usePresetApplication } from '../../../stores/useGlobalPresetStore';
import type { TodoStepWithConfigs, AgentConfigs, AgentLLMConfig } from '../../../utils/stepConfigMatching';
import type { PresetLLMConfig } from '../../../services/api-types';
import type { LLMOption } from '../../../types/llm';
import { useLLMStore } from '../../../stores/useLLMStore';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../../ui/tooltip';
import { 
  HUMAN_TOOLS, 
  getToolsByCategory,
  getToolsByWorkspaceSubCategory,
} from '../../../utils/customToolNames';
import { PrerequisiteConfigPanel } from '../../workflow/canvas/PrerequisiteConfigPanel';

interface StepEditPanelProps {
  step: TodoStepWithConfigs;
  stepIndex: number;
  onSave: (updatedStep: TodoStepWithConfigs) => Promise<void>;
  onCancel: () => void;
  isSaving?: boolean;
  presetServers?: string[]; // Preset's selected servers (subset to show in UI)
  presetLLMConfig?: PresetLLMConfig | null; // Preset's LLM config with agent defaults
  presetUseCodeExecutionMode?: boolean; // Preset's code execution mode (default value for step)
  isExpanded?: boolean; // Controlled expanded state from parent
  onToggleExpanded?: (expanded: boolean) => void; // Callback when expansion state changes
  planSteps?: import('../../../utils/stepConfigMatching').PlanStep[]; // All plan steps (for prerequisite detection)
}

const MAX_TURNS_OPTIONS = [10, 25, 50, 75, 100] as const;

export const StepEditPanel: React.FC<StepEditPanelProps> = ({
  step,
  stepIndex,
  onSave,
  isSaving = false,
  presetServers = [],
  presetLLMConfig = null,
  presetUseCodeExecutionMode = false,
  isExpanded: controlledIsExpanded,
  onToggleExpanded,
  planSteps = [],
}) => {
  const { availableLLMs, getCurrentLLMOption } = useLLMStore();
  const { currentPresetTools } = usePresetApplication();
  
  // Use controlled state if provided, otherwise fall back to local state
  const [localIsExpanded, setLocalIsExpanded] = useState(false);
  const isExpanded = controlledIsExpanded !== undefined ? controlledIsExpanded : localIsExpanded;
  
  const handleToggleExpanded = () => {
    const newExpanded = !isExpanded;
    if (onToggleExpanded) {
      onToggleExpanded(newExpanded);
    } else {
      setLocalIsExpanded(newExpanded);
    }
  };

  // Initialize state from step's agent_configs
  // Ensure validation is enabled for loop steps (required to check loop conditions)
  // Ensure validation and learning are enabled for code execution mode
  const [agentConfigs, setAgentConfigs] = useState<AgentConfigs>(() => {
    const configs = step.agent_configs || {};
    console.log('[StepConfigDebug] Initializing agentConfigs from step:', {
      stepTitle: step.title,
      stepId: step.id,
      step_agent_configs: step.agent_configs,
      disable_learning: configs.disable_learning,
      disable_validation: configs.disable_validation,
      use_tool_search_mode: configs.use_tool_search_mode,
      configs,
    });
    
    // Determine effective code execution mode (step config > preset default)
    const effectiveCodeExecMode = configs.use_code_execution_mode !== undefined 
      ? configs.use_code_execution_mode 
      : presetUseCodeExecutionMode;
    
    const updatedConfigs = { ...configs };
    let needsUpdate = false;
    
    // Force enable validation for loop steps
    if (step.has_loop && configs.disable_validation) {
      updatedConfigs.disable_validation = false;
      needsUpdate = true;
    }
    
    // Force enable validation and learning for code execution mode
    if (effectiveCodeExecMode) {
      if (configs.disable_validation) {
        updatedConfigs.disable_validation = false;
        needsUpdate = true;
      }
      if (configs.disable_learning) {
        updatedConfigs.disable_learning = false;
        needsUpdate = true;
      }
    }
    
    return needsUpdate ? updatedConfigs : configs;
  });

  // Initialize step-level server/tool selection
  // If step config has selection, use it; otherwise use preset defaults for display
  const [selectedServers, setSelectedServers] = useState<string[]>(() => {
    // If step config has explicit selection, use it
    if (agentConfigs.selected_servers && agentConfigs.selected_servers.length > 0) {
      return agentConfigs.selected_servers;
    }
    // Otherwise, use preset defaults for display
    return presetServers;
  });
  const [selectedTools, setSelectedTools] = useState<string[]>(() => {
    // If step config has explicit selection, use it
    if (agentConfigs.selected_tools && agentConfigs.selected_tools.length > 0) {
      console.log('[StepConfigDebug] ⚠️ CRITICAL: Initializing selectedTools from agentConfigs:', {
        stepId: step.id,
        agentConfigs_selected_tools: agentConfigs.selected_tools,
        using: agentConfigs.selected_tools
      });
      return agentConfigs.selected_tools;
    }
    // Otherwise, use preset defaults for display
    console.log('[StepConfigDebug] ⚠️ CRITICAL: Initializing selectedTools from preset (no step config):', {
      stepId: step.id,
      step_agent_configs: step.agent_configs,
      agentConfigs_selected_tools: agentConfigs.selected_tools,
      currentPresetTools,
      using: currentPresetTools || []
    });
    return currentPresetTools || [];
  });

  // Check if NO_SERVERS is explicitly selected
  const hasNoServers = selectedServers.includes("NO_SERVERS");
  const actualSelectedServers = selectedServers.filter(s => s !== "NO_SERVERS");

  // Helper functions for format conversion (must be defined before useState that uses them)
  const formatToolEntry = (category: string, tool: string): string => {
    return `${category}:${tool}`;
  };

  // Convert old format (categories + tools) to new unified format
  const convertOldFormatToNew = (categories?: string[], tools?: string[]): string[] => {
    const result: string[] = [];
    
    // Convert categories to "category:*" format
    if (categories && categories.length > 0) {
      for (const category of categories) {
        result.push(formatToolEntry(category, '*'));
      }
    }
    
    // Convert specific tools - determine category for each tool
    if (tools && tools.length > 0) {
      const allWorkspaceTools = getToolsByCategory('workspace_tools');
      const allHumanTools = getToolsByCategory('human_tools');
      
      for (const toolName of tools) {
        if (allWorkspaceTools.includes(toolName)) {
          result.push(formatToolEntry('workspace_tools', toolName));
        } else if (allHumanTools.includes(toolName)) {
          result.push(formatToolEntry('human_tools', toolName));
        }
      }
    }
    
    return result;
  };

  // State for enabled custom tools in unified format: "category:tool" or "category:*"
  const [enabledCustomTools, setEnabledCustomTools] = useState<string[]>(() => {
    const configs = step.agent_configs || {};
    // Check if already in new format
    if (configs.enabled_custom_tools && configs.enabled_custom_tools.length > 0) {
      const firstEntry = configs.enabled_custom_tools[0];
      if (firstEntry.includes(':')) {
        return configs.enabled_custom_tools;
      }
    }
    // Convert from old format (backward compatibility)
    const oldCategories = configs.enabled_custom_tool_categories;
    const oldTools = configs.enabled_custom_tools;
    return convertOldFormatToNew(oldCategories, oldTools);
  });

  // State for expanded tool categories (to show individual tools)
  const [expandedToolCategories, setExpandedToolCategories] = useState<Set<string>>(new Set());
  // State for expanded workspace sub-categories (expanded by default to show tools)
  const [expandedWorkspaceSubCategories, setExpandedWorkspaceSubCategories] = useState<Set<string>>(
    new Set(['basic_workspace', 'advanced_workspace', 'plus_tools'])
  );

  // Track the step to detect when step changes (using title + index as stable identifier)
  // This ensures state resets properly when switching between different steps
  const stepIdentifier = `${step.title || ''}-${stepIndex}`;
  const prevStepIdentifierRef = useRef<string>(stepIdentifier);
  const prevAgentConfigsRef = useRef<string>(''); // Track previous agent_configs as JSON string

  // Sync state when step changes (different step identifier) OR when agent_configs changes (after save)
  // This ensures state updates both when switching steps and when the same step's config is updated
  useEffect(() => {
    const isDifferentStep = prevStepIdentifierRef.current !== stepIdentifier;
    const currentConfigs = step.agent_configs || {};
    const currentConfigsJson = JSON.stringify(currentConfigs);
    const configsChanged = prevAgentConfigsRef.current !== currentConfigsJson;
    
    // Sync if it's a different step OR if agent_configs has changed (e.g., after save)
    // We need to sync when agent_configs changes to reflect saved changes
    if (isDifferentStep || configsChanged) {
      // Critical log - always show this to track the issue
      console.log('[StepConfigDebug] ⚠️ CRITICAL: Syncing state from step config:', {
        stepId: step.id,
        stepTitle: step.title,
        currentConfigs_selected_tools: currentConfigs.selected_tools,
        currentConfigs_selected_servers: currentConfigs.selected_servers,
        step_agent_configs_selected_tools: step.agent_configs?.selected_tools,
        step_agent_configs_selected_servers: step.agent_configs?.selected_servers,
        fullStepAgentConfigs: step.agent_configs,
      });
      
      // Reset agentConfigs state from step's config
      // Force enable validation for loop steps
      // Force enable validation and learning for code execution mode
      const effectiveCodeExecMode = currentConfigs.use_code_execution_mode !== undefined 
        ? currentConfigs.use_code_execution_mode 
        : presetUseCodeExecutionMode;
      
      const newAgentConfigs: AgentConfigs = { ...currentConfigs };
      
      // Force enable validation for loop steps
      if (step.has_loop && currentConfigs.disable_validation) {
        newAgentConfigs.disable_validation = false;
      }
      
      // Force enable validation and learning for code execution mode
      if (effectiveCodeExecMode) {
        if (currentConfigs.disable_validation) {
          newAgentConfigs.disable_validation = false;
        }
        if (currentConfigs.disable_learning) {
          newAgentConfigs.disable_learning = false;
        }
      }
      
      setAgentConfigs(newAgentConfigs);
      
      // Update servers: use step config if available, otherwise preset defaults
      if (currentConfigs.selected_servers && currentConfigs.selected_servers.length > 0) {
        // Check if NO_SERVERS is in the config
        setSelectedServers(currentConfigs.selected_servers);
      } else {
        setSelectedServers(presetServers);
      }

      // Update tools: use step config if available, otherwise preset defaults
      if (currentConfigs.selected_tools && currentConfigs.selected_tools.length > 0) {
        console.log('[StepConfigDebug] ⚠️ CRITICAL: Setting selectedTools from step config:', {
          stepId: step.id,
          from: currentConfigs.selected_tools,
          settingTo: currentConfigs.selected_tools
        });
        setSelectedTools(currentConfigs.selected_tools);
      } else {
        console.log('[StepConfigDebug] ⚠️ CRITICAL: No step config tools, using preset:', {
          stepId: step.id,
          currentPresetTools,
          settingTo: currentPresetTools || []
        });
        setSelectedTools(currentPresetTools || []);
      }

      // Update enabled custom tools: convert from old format if needed
      if (currentConfigs.enabled_custom_tools && currentConfigs.enabled_custom_tools.length > 0) {
        const firstEntry = currentConfigs.enabled_custom_tools[0];
        if (firstEntry.includes(':')) {
          // Already in new format
          setEnabledCustomTools(currentConfigs.enabled_custom_tools);
        } else {
          // Convert from old format
          const oldCategories = currentConfigs.enabled_custom_tool_categories;
          const oldTools = currentConfigs.enabled_custom_tools;
          setEnabledCustomTools(convertOldFormatToNew(oldCategories, oldTools));
        }
      } else {
        // No tools specified - empty array (all tools enabled by default)
        setEnabledCustomTools([]);
      }

      // Reset expanded categories when step changes
      setExpandedToolCategories(new Set());
      setExpandedWorkspaceSubCategories(new Set(['basic_workspace', 'advanced_workspace', 'plus_tools']));

      // Update refs for next comparison
      prevStepIdentifierRef.current = stepIdentifier;
      prevAgentConfigsRef.current = currentConfigsJson;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stepIdentifier, step.agent_configs, presetServers, currentPresetTools]); // Include stepIdentifier and agent_configs to detect changes

  // Helper to convert AgentLLMConfig to LLMOption
  const llmConfigToOption = (config: AgentLLMConfig | undefined): LLMOption | null => {
    if (!config || !config.provider || !config.model_id) {
      return null;
    }
    const llm = availableLLMs.find(
      (l) => l.provider === config.provider && l.model === config.model_id
    );
    return llm || null;
  };

  // Helper to get preset default LLM for an agent type
  const getPresetDefaultLLM = (agentType: 'execution' | 'validation' | 'learning' | 'conditional'): LLMOption | null => {
    if (!presetLLMConfig) {
      return null;
    }
    let config: AgentLLMConfig | undefined;
    if (agentType === 'execution') {
      config = presetLLMConfig.execution_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider,
        model_id: presetLLMConfig.model_id
      } : undefined);
    } else if (agentType === 'validation') {
      config = presetLLMConfig.validation_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider,
        model_id: presetLLMConfig.model_id
      } : undefined);
    } else if (agentType === 'learning') {
      config = presetLLMConfig.learning_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider,
        model_id: presetLLMConfig.model_id
      } : undefined);
    } else if (agentType === 'conditional') {
      // Conditional LLM uses the same default as execution LLM (or preset default)
      config = presetLLMConfig.execution_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
        provider: presetLLMConfig.provider,
        model_id: presetLLMConfig.model_id
      } : undefined);
    }
    if (config) {
      return llmConfigToOption(config);
    }
    return null;
  };

  // Helper to convert LLMOption to AgentLLMConfig
  const optionToLLMConfig = (option: LLMOption | null): AgentLLMConfig | undefined => {
    if (!option) {
      return undefined;
    }
    return {
      provider: option.provider as 'openai' | 'bedrock' | 'openrouter' | 'vertex',
      model_id: option.model,
    };
  };

  // Helper functions for unified format: "category:tool" or "category:*"
  const parseToolEntry = (entry: string): { category: string; tool: string } | null => {
    // Split only on first colon to handle tool names that might contain colons
    const colonIndex = entry.indexOf(':');
    if (colonIndex === -1 || colonIndex === 0) return null;
    return { 
      category: entry.substring(0, colonIndex), 
      tool: entry.substring(colonIndex + 1) 
    };
  };

  const isCategoryEnabled = (category: string, enabledTools: string[]): boolean => {
    // Empty array means all tools enabled by default
    if (enabledTools.length === 0) return true;
    return enabledTools.includes(formatToolEntry(category, '*'));
  };

  const isToolEnabled = (category: string, toolName: string, enabledTools: string[]): boolean => {
    // Empty array means all tools enabled by default
    if (enabledTools.length === 0) return true;
    // Check if category is enabled (all tools)
    if (isCategoryEnabled(category, enabledTools)) return true;
    // Check if specific tool is enabled
    return enabledTools.includes(formatToolEntry(category, toolName));
  };

  const enableCategory = (category: string, enabledTools: string[]): string[] => {
    // Remove any specific tools from this category, add category:*
    const filtered = enabledTools.filter(entry => {
      const parsed = parseToolEntry(entry);
      return !parsed || parsed.category !== category;
    });
    return [...filtered, formatToolEntry(category, '*')];
  };

  const disableCategory = (category: string, enabledTools: string[]): string[] => {
    // If array is empty (default = all enabled), explicitly enable all other categories
    if (enabledTools.length === 0) {
      const allCategories = ['workspace_tools', 'human_tools'];
      const otherCategories = allCategories.filter(c => c !== category);
      const result: string[] = [];
      for (const otherCategory of otherCategories) {
        const otherCategoryTools = getToolsByCategory(otherCategory);
        result.push(...otherCategoryTools.map(t => formatToolEntry(otherCategory, t)));
      }
      return result;
    }
    // Remove category:* and all specific tools from this category
    return enabledTools.filter(entry => {
      const parsed = parseToolEntry(entry);
      return !parsed || parsed.category !== category;
    });
  };

  const enableTool = (category: string, toolName: string, enabledTools: string[]): string[] => {
    // If category is enabled, disable it first (switch to specific tools)
    let filtered = enabledTools;
    if (isCategoryEnabled(category, enabledTools)) {
      filtered = disableCategory(category, enabledTools);
      // Add all other tools from this category
      const allCategoryTools = getToolsByCategory(category);
      filtered = [...filtered, ...allCategoryTools.map(t => formatToolEntry(category, t))];
    }
    // Add this specific tool if not already present
    const toolEntry = formatToolEntry(category, toolName);
    if (!filtered.includes(toolEntry)) {
      filtered = [...filtered, toolEntry];
    }
    return filtered;
  };

  const disableTool = (category: string, toolName: string, enabledTools: string[]): string[] => {
    // If category is enabled, disable it and enable all other tools
    if (isCategoryEnabled(category, enabledTools)) {
      const allCategoryTools = getToolsByCategory(category);
      const otherTools = allCategoryTools.filter(t => t !== toolName);
      const filtered = enabledTools.filter(entry => {
        const parsed = parseToolEntry(entry);
        return !parsed || parsed.category !== category;
      });
      return [...filtered, ...otherTools.map(t => formatToolEntry(category, t))];
    }
    // Just remove this specific tool
    return enabledTools.filter(entry => entry !== formatToolEntry(category, toolName));
  };

  // Helper to check if a sub-category is enabled
  const isSubCategoryEnabled = (category: string, subCategoryTools: string[], enabledTools: string[]): boolean => {
    if (isCategoryEnabled(category, enabledTools)) return true;
    if (enabledTools.length === 0) return true; // Default: all enabled
    
    const enabledInSubCategory = subCategoryTools.filter(toolName => 
      isToolEnabled(category, toolName, enabledTools)
    );
    return enabledInSubCategory.length === subCategoryTools.length;
  };

  // Helper to enable/disable a sub-category
  const toggleSubCategory = (category: string, subCategoryTools: string[], enabled: boolean, enabledTools: string[]): string[] => {
    if (enabled) {
      // Enable sub-category - add all tools from this sub-category
      let result = enabledTools;
      
      // If category is enabled, disable it first
      if (isCategoryEnabled(category, enabledTools)) {
        result = disableCategory(category, enabledTools);
        // Add all other tools from the category
        const allCategoryTools = getToolsByCategory(category);
        result = [...result, ...allCategoryTools.map(t => formatToolEntry(category, t))];
      }
      
      // Add all tools from this sub-category
      for (const toolName of subCategoryTools) {
        const toolEntry = formatToolEntry(category, toolName);
        if (!result.includes(toolEntry)) {
          result = [...result, toolEntry];
        }
      }
      
      return result;
    } else {
      // Disable sub-category - remove all tools from this sub-category
      if (isCategoryEnabled(category, enabledTools)) {
        // Category is enabled - disable it and enable all other tools
        const allCategoryTools = getToolsByCategory(category);
        const otherTools = allCategoryTools.filter(t => !subCategoryTools.includes(t));
        const filtered = enabledTools.filter(entry => {
          const parsed = parseToolEntry(entry);
          return !parsed || parsed.category !== category;
        });
        return [...filtered, ...otherTools.map(t => formatToolEntry(category, t))];
      } else {
        // Just remove tools from this sub-category
        return enabledTools.filter(entry => {
          const parsed = parseToolEntry(entry);
          if (!parsed || parsed.category !== category) return true;
          return !subCategoryTools.includes(parsed.tool);
        });
      }
    }
  };

  // Update execution LLM
  const handleExecutionLLMSelect = (llm: LLMOption) => {
    setAgentConfigs((prev) => ({
      ...prev,
      execution_llm: optionToLLMConfig(llm),
    }));
  };

  // Update validation LLM
  const handleValidationLLMSelect = (llm: LLMOption) => {
    setAgentConfigs((prev) => ({
      ...prev,
      validation_llm: optionToLLMConfig(llm),
    }));
  };

  // Update learning LLM
  const handleLearningLLMSelect = (llm: LLMOption) => {
    setAgentConfigs((prev) => ({
      ...prev,
      learning_llm: optionToLLMConfig(llm),
    }));
  };

  // Update conditional LLM
  const handleConditionalLLMSelect = (llm: LLMOption) => {
    setAgentConfigs((prev) => ({
      ...prev,
      conditional_llm: optionToLLMConfig(llm),
    }));
  };

  // Update max turns (only for execution - validation/learning are fixed at 25)
  const handleMaxTurnsChange = (
    agentType: 'execution',
    value: number
  ) => {
    setAgentConfigs((prev) => {
      const key = `${agentType}_max_turns` as keyof AgentConfigs;
      return {
        ...prev,
        [key]: value,
      };
    });
  };

  // Update toggles
  const handleToggleChange = (key: keyof AgentConfigs, value: boolean) => {
    setAgentConfigs((prev) => ({
      ...prev,
      [key]: value,
    }));
  };

  // Handle save
  const handleSave = async () => {
    // Start with a clean config object - we'll explicitly set each field
    const finalConfigs: AgentConfigs = {
      ...agentConfigs,
    };
    
    // Force enable validation for loop steps (required to check loop conditions)
    if (step.has_loop) {
      finalConfigs.disable_validation = false;
    }

    // Handle server/tool selection:
    // - If NO_SERVERS is selected → save ["NO_SERVERS"] explicitly
    // - If user has selected servers/tools → save them
    // - If user has deselected all servers/tools → set to undefined (will use preset defaults)
    if (hasNoServers) {
      // Explicitly save NO_SERVERS to indicate no servers should be used
      finalConfigs.selected_servers = ["NO_SERVERS"];
      finalConfigs.selected_tools = []; // No tools when NO_SERVERS is selected
    } else if (selectedServers.length > 0) {
      finalConfigs.selected_servers = selectedServers;
    } else {
      // Explicitly set to undefined to remove any existing saved selection
      // This ensures the step falls back to preset defaults
      finalConfigs.selected_servers = undefined;
    }

    if (hasNoServers) {
      // No tools when NO_SERVERS is selected
      finalConfigs.selected_tools = [];
    } else if (selectedTools.length > 0) {
      finalConfigs.selected_tools = selectedTools;
    } else {
      // Explicitly set to undefined to remove any existing saved selection
      // This ensures the step falls back to preset defaults
      finalConfigs.selected_tools = undefined;
    }

    // Handle Tool Search Mode logic
    if (agentConfigs.use_tool_search_mode) {
      // Enable Tool Search Mode
      finalConfigs.use_tool_search_mode = true;
      
      // Map currently selected tools to pre-discovered tools
      if (selectedTools.length > 0) {
        // Extract tool names (remove "server:" prefix)
        const toolNames = selectedTools.map(t => {
          const parts = t.split(':');
          return parts.length > 1 ? parts[1] : t;
        }).filter(t => t !== '*'); // Exclude wildcards
        
        // Save as pre-discovered tools
        finalConfigs.pre_discovered_tools = toolNames;
        
        // Clear selected_tools to allow dynamic search (defaults to all tools from selected servers)
        // This effectively "unlocks" other tools for searching while keeping known ones ready
        finalConfigs.selected_tools = undefined;
      }
    } else {
      // Disable Tool Search Mode
      delete finalConfigs.use_tool_search_mode;
      delete finalConfigs.pre_discovered_tools;
    }

    // Handle custom tools in unified format: "category:tool" or "category:*"
    if (enabledCustomTools.length === 0) {
      // Empty array means all tools enabled (default behavior)
      finalConfigs.enabled_custom_tools = undefined;
    } else {
      finalConfigs.enabled_custom_tools = enabledCustomTools;
    }

    // Handle context offloading virtual tools: only save if explicitly set to false
    if (agentConfigs.enable_context_offloading === false) {
      finalConfigs.enable_context_offloading = false;
    } else {
      // Default to true (undefined means enabled for backward compatibility)
      finalConfigs.enable_context_offloading = undefined;
    }

    // Handle disable_learning: explicitly save false when enabled, true when disabled
    // nil/undefined = not set (default enabled), false = explicitly enabled, true = disabled
    // CRITICAL: When user unchecks checkbox (to enable learning), we MUST save false explicitly
    // to override any previous true value in JSON. JSON.stringify will include false values.
    if (agentConfigs.disable_learning === false) {
      // User explicitly enabled learning (unchecked the checkbox)
      // Save false to override any previous true value
      finalConfigs.disable_learning = false;
    } else if (agentConfigs.disable_learning === true) {
      // User explicitly disabled learning (checked the checkbox)
      finalConfigs.disable_learning = true;
    } else {
      // State is undefined - check if it was previously set in the step config
      // If it was never set before, keep undefined (default enabled)
      // If it was set before but now undefined, this is unexpected - keep undefined to reset to default
      const wasPreviouslySet = step.agent_configs?.disable_learning !== undefined;
      if (!wasPreviouslySet) {
        // Never set before - keep undefined (default enabled, field omitted from JSON)
        finalConfigs.disable_learning = undefined;
      } else {
        // Was set before but now undefined in state - reset to default (undefined)
        // This allows user to "reset" by clearing the value
        finalConfigs.disable_learning = undefined;
      }
    }

    // Handle lock_learnings: explicitly save true when locked, false when unlocked
    if (agentConfigs.lock_learnings === true) {
      finalConfigs.lock_learnings = true;
    } else if (agentConfigs.lock_learnings === false) {
      finalConfigs.lock_learnings = false;
    } else {
      // State is undefined - keep undefined (default unlocked, field omitted from JSON)
      finalConfigs.lock_learnings = undefined;
    }

    // Handle disable_validation: explicitly save false when enabled, true when disabled
    // Similar logic to disable_learning
    if (agentConfigs.disable_validation === false) {
      // User explicitly enabled validation (unchecked the checkbox)
      finalConfigs.disable_validation = false;
    } else if (agentConfigs.disable_validation === true) {
      // User explicitly disabled validation (checked the checkbox)
      finalConfigs.disable_validation = true;
    } else {
      // State is undefined - reset to default
      finalConfigs.disable_validation = undefined;
    }

    // Handle use_code_execution_mode: save if explicitly set by user
    // If undefined, step will use preset default (field will be omitted from JSON)
    if (agentConfigs.use_code_execution_mode !== undefined) {
      // User has explicitly set it - save the value
      finalConfigs.use_code_execution_mode = agentConfigs.use_code_execution_mode;
    } else {
      // Not explicitly set - delete the field so it uses preset default
      // JSON.stringify will omit undefined fields automatically
      delete finalConfigs.use_code_execution_mode;
    }

    // Debug logging to verify data being saved
    console.log('[StepEditPanel] Saving step config:', {
      stepTitle: step.title,
      selectedServers,
      selectedTools,
      agentConfigs_disable_learning: agentConfigs.disable_learning,
      agentConfigs_use_code_execution_mode: agentConfigs.use_code_execution_mode,
      finalConfigs_disable_learning: finalConfigs.disable_learning,
      finalConfigs_use_code_execution_mode: finalConfigs.use_code_execution_mode,
      finalConfigs: {
        selected_servers: finalConfigs.selected_servers,
        selected_tools: finalConfigs.selected_tools,
        disable_learning: finalConfigs.disable_learning,
        disable_validation: finalConfigs.disable_validation,
        use_code_execution_mode: finalConfigs.use_code_execution_mode,
        use_tool_search_mode: finalConfigs.use_tool_search_mode,
        pre_discovered_tools: finalConfigs.pre_discovered_tools
      },
    });

    const updatedStep: TodoStepWithConfigs = {
      ...step,
      agent_configs: finalConfigs,
    };
    await onSave(updatedStep);
    
    // Collapse the panel after saving
    if (onToggleExpanded) {
      onToggleExpanded(false);
    } else {
      setLocalIsExpanded(false);
    }
  };

  // Get current agent config summary (LLM settings only)
  const getAgentConfigSummary = () => {
    // Priority: step config > preset default > global default
    const execLLM = llmConfigToOption(agentConfigs.execution_llm) || getPresetDefaultLLM('execution') || getCurrentLLMOption();
    const valLLM = llmConfigToOption(agentConfigs.validation_llm) || getPresetDefaultLLM('validation') || getCurrentLLMOption();
    const learnLLM = llmConfigToOption(agentConfigs.learning_llm) || getPresetDefaultLLM('learning') || getCurrentLLMOption();
    
    // Get effective code execution mode (step config > preset default)
    const effectiveCodeExecMode = agentConfigs.use_code_execution_mode !== undefined 
      ? agentConfigs.use_code_execution_mode 
      : presetUseCodeExecutionMode;
    
    const parts = [];
    if (execLLM) {
      let codeExecLabel = 'Simple';
      if (effectiveCodeExecMode) codeExecLabel = 'Code Exec';
      if (agentConfigs.use_tool_search_mode) codeExecLabel = 'Tool Search';
      parts.push(`Exec: ${execLLM.label} (${codeExecLabel})`);
    }
    if (valLLM && agentConfigs.disable_validation === false) parts.push(`Val: ${valLLM.label}`);
    if (learnLLM && !agentConfigs.disable_learning) {
      const detailLevel = agentConfigs.learning_detail_level || 'exact';
      const detailLabel = detailLevel === 'exact' ? 'Exact' : 'General';
      parts.push(`Learn: ${learnLLM.label} (${detailLabel})`);
    }
    if (agentConfigs.disable_validation !== false) parts.push('Val: Disabled');
    if (agentConfigs.disable_learning) parts.push('Learn: Disabled');
    
    return parts.length > 0 ? parts.join(' • ') : 'Default config';
  };

  // Get MCP config summary with detailed information
  const getMCPConfigSummary = () => {
    if (hasNoServers) {
      return 'No servers (Pure LLM mode)';
    }
    
    if (selectedServers.length === 0) {
      // Using preset defaults
      return `Using preset defaults (${presetServers.length} servers)`;
    }
    
    const parts = [];
    
    // Server details
    if (selectedServers.length === 1) {
      parts.push(`Server: ${selectedServers[0]}`);
    } else {
      parts.push(`Servers: ${selectedServers.length} (${selectedServers.slice(0, 2).join(', ')}${selectedServers.length > 2 ? '...' : ''})`);
    }
    
    // Tool details
    if (agentConfigs.use_tool_search_mode) {
      parts.push('Tool Search');
      if (agentConfigs.pre_discovered_tools && agentConfigs.pre_discovered_tools.length > 0) {
        const tools = agentConfigs.pre_discovered_tools;
        if (tools.length <= 3) {
          // Show actual names for small lists
          parts.push(`Pre-discovered: ${tools.join(', ')}`);
        } else {
          // Show first 2 names + count for larger lists
          parts.push(`Pre-discovered: ${tools.slice(0, 2).join(', ')} +${tools.length - 2} more`);
        }
      }
    } else if (selectedTools.length > 0) {
      const allToolsServers = selectedTools.filter(t => t.endsWith(':*')).map(t => t.replace(':*', ''));
      const specificTools = selectedTools.filter(t => !t.endsWith(':*'));
      
      if (allToolsServers.length > 0) {
        if (allToolsServers.length === 1) {
          parts.push(`All tools from ${allToolsServers[0]}`);
        } else {
          parts.push(`All tools from ${allToolsServers.length} servers`);
        }
      }
      
      if (specificTools.length > 0) {
        if (specificTools.length === 1) {
          parts.push(`1 specific tool`);
        } else {
          parts.push(`${specificTools.length} specific tools`);
        }
      }
    } else {
      parts.push('No tools selected');
    }
    
    return parts.join(' • ');
  };

  // Get custom tools summary with detailed information (using unified format)
  const getCustomToolsSummary = () => {
    const allWorkspaceTools = getToolsByCategory('workspace_tools');
    const allHumanTools = getToolsByCategory('human_tools');
    
    // Check if no filtering (default: all enabled)
    if (enabledCustomTools.length === 0) {
      const defaultParts = ['All custom tools enabled (default)'];
      if (agentConfigs.enable_context_offloading === false) {
        defaultParts.push('Context offloading: disabled');
      } else {
        defaultParts.push('Context offloading: enabled');
      }
      return defaultParts.join(' • ');
    }
    
    const parts = [];
    
    // Parse enabled tools
    const workspaceCategoryEnabled = isCategoryEnabled('workspace_tools', enabledCustomTools);
    const humanCategoryEnabled = isCategoryEnabled('human_tools', enabledCustomTools);
    
    // Get specific tools enabled
    const workspaceSpecificTools: string[] = [];
    const humanSpecificTools: string[] = [];
    
    for (const entry of enabledCustomTools) {
      const parsed = parseToolEntry(entry);
      if (!parsed) continue;
      
      if (parsed.category === 'workspace_tools' && parsed.tool !== '*') {
        workspaceSpecificTools.push(parsed.tool);
      } else if (parsed.category === 'human_tools' && parsed.tool !== '*') {
        humanSpecificTools.push(parsed.tool);
      }
    }
    
    // Workspace tools summary
    if (workspaceCategoryEnabled) {
      parts.push('All workspace tools');
    } else if (workspaceSpecificTools.length > 0) {
      if (workspaceSpecificTools.length === allWorkspaceTools.length) {
        parts.push('All workspace tools');
      } else {
        // Show sub-category breakdown
        const basicTools = getToolsByWorkspaceSubCategory('basic_workspace');
        const advancedTools = getToolsByWorkspaceSubCategory('advanced_workspace');
        const plusTools = getToolsByWorkspaceSubCategory('plus_tools');
        
        const basicEnabled = workspaceSpecificTools.filter(t => basicTools.includes(t)).length;
        const advancedEnabled = workspaceSpecificTools.filter(t => advancedTools.includes(t)).length;
        const plusEnabled = workspaceSpecificTools.filter(t => plusTools.includes(t)).length;
        
        const subCategoryParts = [];
        if (basicEnabled > 0) {
          subCategoryParts.push(`${basicEnabled}/${basicTools.length} basic`);
        }
        if (advancedEnabled > 0) {
          subCategoryParts.push(`${advancedEnabled}/${advancedTools.length} advanced`);
        }
        if (plusEnabled > 0) {
          subCategoryParts.push(`${plusEnabled}/${plusTools.length} plus`);
        }
        
        if (subCategoryParts.length > 0) {
          parts.push(`${workspaceSpecificTools.length}/${allWorkspaceTools.length} workspace (${subCategoryParts.join(', ')})`);
        } else {
          parts.push(`${workspaceSpecificTools.length}/${allWorkspaceTools.length} workspace tools`);
        }
      }
    }
    
    // Human tools summary
    if (humanCategoryEnabled) {
      parts.push('All human tools');
    } else if (humanSpecificTools.length > 0) {
      if (humanSpecificTools.length === allHumanTools.length) {
        parts.push('All human tools');
      } else {
        parts.push(`${humanSpecificTools.length}/${allHumanTools.length} human tools`);
      }
    } else if (!workspaceCategoryEnabled && workspaceSpecificTools.length > 0) {
      // Workspace tools enabled but human tools disabled
      parts.push('0/1 human tools');
    }
    
    // Context offloading status
    if (agentConfigs.enable_context_offloading === false) {
      parts.push('Context offloading: disabled');
    } else {
      parts.push('Context offloading: enabled');
    }
    
    return parts.length > 0 ? parts.join(' • ') : 'No custom tools';
  };

  // State for human input step editing
  const [humanInputQuestion, setHumanInputQuestion] = useState(step.question || '')
  const [humanInputResponseType, setHumanInputResponseType] = useState(step.response_type || 'text')
  const [humanInputOptions, setHumanInputOptions] = useState<string[]>(step.options || [])
  const [humanInputVariableName, setHumanInputVariableName] = useState(step.variable_name || '')
  const [humanInputNextStepId, setHumanInputNextStepId] = useState(step.next_step_id || '')
  const [humanInputIfYesNextStepId, setHumanInputIfYesNextStepId] = useState(step.if_yes_next_step_id || '')
  const [humanInputIfNoNextStepId, setHumanInputIfNoNextStepId] = useState(step.if_no_next_step_id || '')
  const [humanInputOptionRoutes, setHumanInputOptionRoutes] = useState<Record<string, string>>(step.option_routes || {})

  // Update human input state when step changes
  useEffect(() => {
    if (step.has_human_input) {
      setHumanInputQuestion(step.question || '')
      setHumanInputResponseType(step.response_type || 'text')
      setHumanInputOptions(step.options || [])
      setHumanInputVariableName(step.variable_name || '')
      setHumanInputNextStepId(step.next_step_id || '')
      setHumanInputIfYesNextStepId(step.if_yes_next_step_id || '')
      setHumanInputIfNoNextStepId(step.if_no_next_step_id || '')
      setHumanInputOptionRoutes(step.option_routes || {})
    }
  }, [step.has_human_input, step.question, step.response_type, step.options, step.variable_name, step.next_step_id, step.if_yes_next_step_id, step.if_no_next_step_id, step.option_routes])

  return (
    <div className="mt-2 border-t border-gray-200 dark:border-gray-700 pt-2">
      {/* Human Input Step Configuration */}
      {step.has_human_input && (
        <div className="space-y-3 mb-4">
          <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
            Human Input Configuration
          </div>
          
          {/* Question */}
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              Question *
            </label>
            <textarea
              value={humanInputQuestion}
              onChange={(e) => setHumanInputQuestion(e.target.value)}
              placeholder="Enter the question to ask the user..."
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              rows={3}
            />
          </div>

          {/* Response Type */}
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              Response Type
            </label>
            <select
              value={humanInputResponseType}
              onChange={(e) => {
                setHumanInputResponseType(e.target.value)
                // Clear options if switching away from multiple_choice
                if (e.target.value !== 'multiple_choice') {
                  setHumanInputOptions([])
                  setHumanInputOptionRoutes({})
                }
              }}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            >
              <option value="text">Text</option>
              <option value="yesno">Yes/No</option>
              <option value="multiple_choice">Multiple Choice</option>
            </select>
          </div>

          {/* Options (for multiple_choice) */}
          {humanInputResponseType === 'multiple_choice' && (
            <div>
              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                Options
              </label>
              <div className="space-y-2">
                {humanInputOptions.map((option, index) => (
                  <div key={index} className="flex items-center gap-2">
                    <input
                      type="text"
                      value={option}
                      onChange={(e) => {
                        const newOptions = [...humanInputOptions]
                        newOptions[index] = e.target.value
                        setHumanInputOptions(newOptions)
                      }}
                      placeholder={`Option ${index + 1}`}
                      className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    />
                    <input
                      type="text"
                      value={humanInputOptionRoutes[String(index)] || humanInputOptionRoutes[option] || ''}
                      onChange={(e) => {
                        const newRoutes = { ...humanInputOptionRoutes }
                        newRoutes[String(index)] = e.target.value
                        setHumanInputOptionRoutes(newRoutes)
                      }}
                      placeholder="Next step ID"
                      className="w-32 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    />
                    <button
                      type="button"
                      onClick={() => {
                        const newOptions = humanInputOptions.filter((_, i) => i !== index)
                        setHumanInputOptions(newOptions)
                        const newRoutes = { ...humanInputOptionRoutes }
                        delete newRoutes[String(index)]
                        if (option) delete newRoutes[option]
                        setHumanInputOptionRoutes(newRoutes)
                      }}
                      className="px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 rounded"
                    >
                      Remove
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  onClick={() => setHumanInputOptions([...humanInputOptions, ''])}
                  className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400"
                >
                  + Add Option
                </button>
              </div>
            </div>
          )}

          {/* Variable Name */}
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              Variable Name (Optional)
            </label>
            <input
              type="text"
              value={humanInputVariableName}
              onChange={(e) => setHumanInputVariableName(e.target.value)}
              placeholder="Store response in variable..."
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
          </div>

          {/* Routing Configuration */}
          <div className="space-y-2">
            <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
              Routing
            </div>
            
            {humanInputResponseType === 'yesno' ? (
              <>
                <div>
                  <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                    If Yes → Next Step ID
                  </label>
                  <input
                    type="text"
                    value={humanInputIfYesNextStepId}
                    onChange={(e) => setHumanInputIfYesNextStepId(e.target.value)}
                    placeholder="step-id or 'end'"
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                    If No → Next Step ID
                  </label>
                  <input
                    type="text"
                    value={humanInputIfNoNextStepId}
                    onChange={(e) => setHumanInputIfNoNextStepId(e.target.value)}
                    placeholder="step-id or 'end'"
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                </div>
              </>
            ) : (
              <div>
                <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Next Step ID
                </label>
                <input
                  type="text"
                  value={humanInputNextStepId}
                  onChange={(e) => setHumanInputNextStepId(e.target.value)}
                  placeholder="step-id or 'end'"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                />
              </div>
            )}
          </div>

          {/* Save Button */}
          <div className="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-700">
            <Button
              variant="default"
              size="sm"
              onClick={async () => {
                const updatedStep: TodoStepWithConfigs = {
                  ...step,
                  question: humanInputQuestion,
                  response_type: humanInputResponseType,
                  options: humanInputResponseType === 'multiple_choice' ? humanInputOptions : undefined,
                  variable_name: humanInputVariableName || undefined,
                  next_step_id: humanInputNextStepId || undefined,
                  if_yes_next_step_id: humanInputResponseType === 'yesno' ? (humanInputIfYesNextStepId || undefined) : undefined,
                  if_no_next_step_id: humanInputResponseType === 'yesno' ? (humanInputIfNoNextStepId || undefined) : undefined,
                  option_routes: humanInputResponseType === 'multiple_choice' && Object.keys(humanInputOptionRoutes).length > 0 ? humanInputOptionRoutes : undefined,
                }
                await onSave(updatedStep)
              }}
              disabled={isSaving || !humanInputQuestion.trim()}
              className="text-xs h-7 px-3"
            >
              {isSaving ? 'Saving...' : 'Save'}
            </Button>
          </div>
        </div>
      )}

      {/* Agent Config Section - Hidden for human_input steps */}
      {!step.has_human_input && (
        <>
      {/* Compact Header - Always Visible */}
      <div 
        className="cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700/30 rounded px-2 py-1.5 -mx-2 transition-colors"
        onClick={handleToggleExpanded}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 flex-1 min-w-0">
            <Settings className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400 flex-shrink-0" />
            <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
              Agent Config
            </span>
            <span className="text-xs text-gray-500 dark:text-gray-500 truncate">
              {getAgentConfigSummary()}
            </span>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {isExpanded ? (
              <ChevronUp className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
            ) : (
              <ChevronDown className="w-3.5 h-3.5 text-gray-500 dark:text-gray-400" />
            )}
          </div>
        </div>
        {/* MCP Config on separate line */}
        <div className="flex items-center gap-2 mt-1 ml-6">
          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
            MCP Config:
          </span>
          <span className="text-xs text-gray-500 dark:text-gray-500">
            {getMCPConfigSummary()}
          </span>
        </div>
        {/* Custom Tools Config on separate line */}
        <div className="flex items-center gap-2 mt-1 ml-6">
          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
            Custom Tools:
          </span>
          <span className="text-xs text-gray-500 dark:text-gray-500">
            {getCustomToolsSummary()}
          </span>
        </div>
      </div>

      {/* Expanded Configuration Panel */}
      {isExpanded && (
        <div className="mt-2 p-3 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-700 rounded-lg">
          <div className="space-y-4">

            {/* Execution Agent Configuration */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                  Execution
                </div>
                {/* Code Execution Mode Toggle */}
                <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => {
                            console.log('[StepEditPanel] Setting mode to Simple');
                            setAgentConfigs((prev) => ({
                              ...prev,
                              use_code_execution_mode: false,
                              use_tool_search_mode: false,
                            }));
                          }}
                          className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                            !agentConfigs.use_tool_search_mode &&
                            (agentConfigs.use_code_execution_mode === false || 
                            (agentConfigs.use_code_execution_mode === undefined && !presetUseCodeExecutionMode))
                              ? 'agent-mode-selected rounded-l-md rounded-r-none'
                              : 'agent-mode-unselected rounded-none'
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
                    
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => {
                            console.log('[StepEditPanel] Setting mode to Code Exec');
                            setAgentConfigs((prev) => ({
                              ...prev,
                              use_code_execution_mode: true,
                              use_tool_search_mode: false,
                              // Auto-enable learning and validation when code exec is enabled
                              disable_learning: false,
                              disable_validation: false,
                              // Auto-set learning detail level to 'exact' for code exec mode
                              learning_detail_level: 'exact',
                            }));
                          }}
                          className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                            !agentConfigs.use_tool_search_mode &&
                            (agentConfigs.use_code_execution_mode === true ||
                            (agentConfigs.use_code_execution_mode === undefined && presetUseCodeExecutionMode))
                              ? 'agent-mode-selected rounded-none'
                              : 'agent-mode-unselected rounded-none'
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

                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => {
                            console.log('[StepEditPanel] Setting mode to Tool Search');
                            setAgentConfigs((prev) => ({
                              ...prev,
                              use_code_execution_mode: false,
                              use_tool_search_mode: true,
                            }));
                          }}
                          className={`px-2 py-1 text-xs font-medium transition-colors ${
                            agentConfigs.use_tool_search_mode === true
                              ? 'agent-mode-selected rounded-r-md rounded-l-none'
                              : 'agent-mode-unselected rounded-none'
                          }`}
                        >
                          <Search className="w-3 h-3 inline mr-1" />
                          Tool Search
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Tool Search mode - Dynamic tool discovery</p>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <div className="flex-1 min-w-0">
                  <LLMSelectionDropdown
                    availableLLMs={availableLLMs}
                    selectedLLM={llmConfigToOption(agentConfigs.execution_llm) || getPresetDefaultLLM('execution') || getCurrentLLMOption()}
                    onLLMSelect={handleExecutionLLMSelect}
                    inModal={false}
                    openDirection="down"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Max Turns:</label>
                  <select
                    value={agentConfigs.execution_max_turns || 100}
                    onChange={(e) => handleMaxTurnsChange('execution', parseInt(e.target.value))}
                    className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs focus:ring-2 focus:ring-blue-500 focus:border-blue-500 w-20"
                  >
                    {MAX_TURNS_OPTIONS.map((value) => (
                      <option key={value} value={value}>
                        {value}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            </div>

            {/* Divider */}
            <div className="border-t border-gray-200 dark:border-gray-700"></div>

            {/* Validation Agent Configuration */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                  Validation
                </div>
                {(() => {
                  const effectiveCodeExecMode = agentConfigs.use_code_execution_mode !== undefined 
                    ? agentConfigs.use_code_execution_mode 
                    : presetUseCodeExecutionMode;
                  const isDisabled = step.has_loop || effectiveCodeExecMode;
                  const tooltipText = step.has_loop 
                    ? "Validation cannot be disabled for loop steps - it's required to check loop conditions"
                    : effectiveCodeExecMode
                    ? "Validation is automatically enabled in Code Exec mode"
                    : undefined;
                  
                  return (
                    <label 
                      className={`flex items-center gap-1.5 ${isDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}`}
                      title={tooltipText}
                    >
                      <input
                        type="checkbox"
                        checked={agentConfigs.disable_validation !== false}
                        onChange={(e) => {
                          if (!isDisabled) {
                            handleToggleChange('disable_validation', e.target.checked);
                          }
                        }}
                        disabled={isDisabled}
                        className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">
                        Disable{step.has_loop && ' (Required for loops)'}
                      </span>
                    </label>
                  );
                })()}
              </div>
              {agentConfigs.disable_validation === false ? (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <div className="flex-1 min-w-0">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={llmConfigToOption(agentConfigs.validation_llm) || getPresetDefaultLLM('validation') || getCurrentLLMOption()}
                        onLLMSelect={handleValidationLLMSelect}
                        inModal={false}
                        openDirection="down"
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-2 pt-1">
                    <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Mode:</label>
                    <select
                      value={agentConfigs.llm_validation_mode || 'skip'}
                      onChange={(e) => {
                        const mode = e.target.value as 'auto' | 'always' | 'skip';
                        setAgentConfigs((prev) => ({
                          ...prev,
                          llm_validation_mode: mode,
                        }));
                      }}
                      className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs focus:ring-2 focus:ring-blue-500 focus:border-blue-500 flex-1"
                    >
                      <option value="auto">Auto (Validate initial runs)</option>
                      <option value="always">Always Validate</option>
                      <option value="skip">Skip if Pre-check Passes</option>
                    </select>
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help">ℹ️</span>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">
                          <p className="text-xs">
                            <strong>Auto:</strong> Runs LLM validation for the first 3 successful executions, then skips (assuming stability).
                            <br />
                            <strong>Always:</strong> Always runs LLM validation.
                            <br />
                            <strong>Skip:</strong> Skips LLM validation if code-based pre-validation passes.
                          </p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  </div>
                </div>
              ) : (
                <div className="text-xs text-gray-500 dark:text-gray-500 italic py-1">
                  Validation disabled - step will auto-approve
                </div>
              )}
            </div>

            {/* Divider */}
            <div className="border-t border-gray-200 dark:border-gray-700"></div>

            {/* Learning Agent Configuration */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                  Learning
                </div>
                {(() => {
                  const effectiveCodeExecMode = agentConfigs.use_code_execution_mode !== undefined 
                    ? agentConfigs.use_code_execution_mode 
                    : presetUseCodeExecutionMode;
                  const isDisabled = effectiveCodeExecMode;
                  const tooltipText = effectiveCodeExecMode
                    ? "Learning is automatically enabled in Code Exec mode"
                    : undefined;
                  
                  return (
                    <label className={`flex items-center gap-1.5 ${isDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}`} title={tooltipText}>
                      <input
                        type="checkbox"
                        checked={agentConfigs.disable_learning || false}
                        onChange={(e) => {
                          if (!isDisabled) {
                            handleToggleChange('disable_learning', e.target.checked);
                          }
                        }}
                        disabled={isDisabled}
                        className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">
                        Disable{effectiveCodeExecMode && ' (Auto-enabled)'}
                      </span>
                    </label>
                  );
                })()}
              </div>
              {!agentConfigs.disable_learning ? (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <div className="flex-1 min-w-0">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={llmConfigToOption(agentConfigs.learning_llm) || getPresetDefaultLLM('learning') || getCurrentLLMOption()}
                        onLLMSelect={handleLearningLLMSelect}
                        inModal={false}
                        openDirection="down"
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Detail Level:</label>
                    {(() => {
                      const effectiveCodeExecMode = agentConfigs.use_code_execution_mode !== undefined 
                        ? agentConfigs.use_code_execution_mode 
                        : presetUseCodeExecutionMode;
                      const isDisabled = effectiveCodeExecMode;
                      
                      return (
                        <select
                          value={agentConfigs.learning_detail_level || 'exact'}
                          onChange={(e) => {
                            if (!isDisabled) {
                              const value = e.target.value as 'exact' | 'general';
                              setAgentConfigs((prev): AgentConfigs => ({
                                ...prev,
                                learning_detail_level: value,
                              }));
                            }
                          }}
                          disabled={isDisabled}
                          title={isDisabled ? "Learning detail level is automatically set to 'exact' in Code Exec mode" : undefined}
                          className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs focus:ring-2 focus:ring-blue-500 focus:border-blue-500 flex-1 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <option value="general">General Patterns</option>
                          <option value="exact">Exact MCP Tools</option>
                        </select>
                      );
                    })()}
                  </div>
                  <div className="flex items-center gap-2 pt-1">
                    <label className="flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={agentConfigs.lock_learnings || false}
                        onChange={(e) => {
                          setAgentConfigs((prev): AgentConfigs => ({
                            ...prev,
                            lock_learnings: e.target.checked,
                          }));
                        }}
                        className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">
                        Lock Learnings
                      </span>
                    </label>
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 cursor-help">ℹ️</span>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">
                          <p className="text-xs">
                            Prevents learning agent from running but still uses existing learnings. Useful when learnings are stable and don't need updates.
                          </p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  </div>
                </div>
              ) : (
                <div className="text-xs text-gray-500 dark:text-gray-500 italic py-1">
                  Learning disabled for this step
                </div>
              )}
            </div>

            {/* Conditional Branching Configuration - Hidden for decision steps */}
            {!step.has_decision_step && (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-3 mt-3">
              <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide mb-2">
                Conditional Branching
              </div>
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`has-condition-${stepIndex}`}
                    checked={step.has_condition || false}
                    disabled={true}
                    className="w-4 h-4 text-purple-600 border-gray-300 rounded focus:ring-purple-500 disabled:opacity-50"
                  />
                  <label
                    htmlFor={`has-condition-${stepIndex}`}
                    className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer flex-1"
                  >
                    Enable Conditional Branching
                    <span className="text-gray-500 dark:text-gray-500 ml-1">
                      (Use planning tools to convert step to conditional)
                    </span>
                  </label>
                </div>
                
                {step.has_condition && (
                  <div className="ml-6 space-y-3 p-2 bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-700 rounded">
                    <div>
                      <div className="text-xs font-medium text-purple-700 dark:text-purple-400 mb-1">
                        Condition Question:
                      </div>
                      <div className="text-xs text-gray-700 dark:text-gray-300">
                        {step.condition_question || '(Not set)'}
                      </div>
                    </div>
                    
                    {/* Conditional Agent Configuration */}
                    <div className="space-y-2">
                      <div className="text-xs font-semibold text-purple-700 dark:text-purple-400 uppercase tracking-wide mb-2">
                        Conditional Agent
                      </div>
                      
                      {/* Conditional LLM Configuration */}
                      <div>
                        <div className="text-xs font-medium text-purple-700 dark:text-purple-400 mb-1">
                          Conditional LLM:
                        </div>
                        <div className="flex-1 min-w-0">
                          <LLMSelectionDropdown
                            availableLLMs={availableLLMs}
                            selectedLLM={llmConfigToOption(agentConfigs.conditional_llm) || getPresetDefaultLLM('conditional') || getCurrentLLMOption()}
                            onLLMSelect={handleConditionalLLMSelect}
                            inModal={false}
                            openDirection="down"
                          />
                        </div>
                        <div className="text-[10px] text-gray-500 dark:text-gray-500 mt-1">
                          LLM used to evaluate the condition. Defaults to execution LLM if not specified.
                        </div>
                      </div>
                      
                      {/* Conditional Agent Code Execution Mode Toggle */}
                      <div>
                        <div className="text-xs font-medium text-purple-700 dark:text-purple-400 mb-1">
                          Execution Mode:
                        </div>
                        <div className="flex items-center border border-gray-300 dark:border-gray-600 rounded-md overflow-hidden">
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  onClick={() => {
                                    console.log('[StepEditPanel] Setting conditional agent use_code_execution_mode to false');
                                    setAgentConfigs((prev) => ({
                                      ...prev,
                                      use_code_execution_mode: false,
                                    }));
                                  }}
                                  className={`px-2 py-1 text-xs font-medium transition-colors border-r border-gray-300 dark:border-gray-600 ${
                                    agentConfigs.use_code_execution_mode === false || 
                                    (agentConfigs.use_code_execution_mode === undefined && !presetUseCodeExecutionMode)
                                      ? 'agent-mode-selected rounded-l-md rounded-r-none'
                                      : 'agent-mode-unselected rounded-none'
                                  }`}
                                >
                                  <Sparkles className="w-3 h-3 inline mr-1" />
                                  Simple
                                </button>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>Simple mode - Direct MCP tool access for conditional agent</p>
                              </TooltipContent>
                            </Tooltip>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  onClick={() => {
                                    console.log('[StepEditPanel] Setting conditional agent use_code_execution_mode to true');
                                    setAgentConfigs((prev) => ({
                                      ...prev,
                                      use_code_execution_mode: true,
                                    }));
                                  }}
                                  className={`px-2 py-1 text-xs font-medium transition-colors ${
                                    agentConfigs.use_code_execution_mode === true ||
                                    (agentConfigs.use_code_execution_mode === undefined && presetUseCodeExecutionMode)
                                      ? 'agent-mode-selected rounded-r-md rounded-l-none'
                                      : 'agent-mode-unselected rounded-none'
                                  }`}
                                >
                                  <Code2 className="w-3 h-3 inline mr-1" />
                                  Code Exec
                                </button>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>Code Exec mode - MCP tools accessed via generated Go code for conditional agent</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        </div>
                        <div className="text-[10px] text-gray-500 dark:text-gray-500 mt-1">
                          Execution mode for the conditional agent. Controls how MCP tools are accessed.
                        </div>
                      </div>
                    </div>
                    
                    {step.condition_context && (
                      <>
                        <div className="text-xs font-medium text-purple-700 dark:text-purple-400 mt-2">
                          Condition Context:
                        </div>
                        <div className="text-xs text-gray-600 dark:text-gray-400">
                          {step.condition_context}
                        </div>
                      </>
                    )}
                    
                    {step.if_true_steps && step.if_true_steps.length > 0 && (
                      <div className="mt-2">
                        <div className="text-xs font-medium text-green-700 dark:text-green-400">
                          ✅ If True Branch: {step.if_true_steps.length} step(s)
                        </div>
                      </div>
                    )}
                    
                    {step.if_false_steps && step.if_false_steps.length > 0 && (
                      <div className="mt-2">
                        <div className="text-xs font-medium text-red-700 dark:text-red-400">
                          ❌ If False Branch: {step.if_false_steps.length} step(s)
                        </div>
                      </div>
                    )}
                    
                    {step.condition_result !== undefined && (
                      <div className={`mt-2 p-1 rounded text-xs ${step.condition_result ? 'bg-green-50 dark:bg-green-900/20' : 'bg-red-50 dark:bg-red-900/20'}`}>
                        <div className="font-medium">
                          {step.condition_result ? '✅ Decision: TRUE' : '❌ Decision: FALSE'}
                        </div>
                        {step.condition_reason && (
                          <div className="text-gray-600 dark:text-gray-400 mt-1 italic text-xs">
                            {step.condition_reason}
                          </div>
                        )}
                      </div>
                    )}
                    
                    <div className="text-xs text-gray-500 dark:text-gray-500 mt-2 italic">
                      Note: Use planning agent tools (convert_step_to_conditional, add_branch_steps, etc.) to manage conditional steps and branches. All planning tools now use step IDs (from the step's id field) instead of titles for identification.
                    </div>
                  </div>
                )}
              </div>
            </div>
            )}

            {/* Loop Configuration (only shown if has_loop is true) */}
            {step.has_loop && (
              <>
                <div className="border-t border-gray-200 dark:border-gray-700"></div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`learning-after-loop-${stepIndex}`}
                    checked={agentConfigs.learning_after_loop_iteration !== undefined ? agentConfigs.learning_after_loop_iteration : (step.has_loop ? true : false)}
                    onChange={(e) =>
                      handleToggleChange('learning_after_loop_iteration', e.target.checked)
                    }
                    className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                  />
                  <label
                    htmlFor={`learning-after-loop-${stepIndex}`}
                    className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer"
                  >
                    Run Learning After Each Loop Iteration
                  </label>
                </div>
              </>
            )}

            {/* MCP Servers and Tools Selection (Step Level) */}
            {presetServers.length > 0 && (
              <>
                <div className="border-t border-gray-200 dark:border-gray-700"></div>
                <div className="space-y-2">
                  <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    MCP Servers & Tools (Step Level)
                  </div>
                  <div className="text-xs text-gray-500 dark:text-gray-500 italic">
                    Filter preset's selected servers/tools further for this step
                  </div>
                  
                  {/* NO_SERVERS Option */}
                  <div className="flex items-center gap-2 p-2 bg-gray-100 dark:bg-gray-800/50 rounded border border-gray-200 dark:border-gray-700">
                    <input
                      type="checkbox"
                      id={`no-servers-${stepIndex}`}
                      checked={hasNoServers}
                      onChange={(e) => {
                        if (e.target.checked) {
                          // Set NO_SERVERS and clear all other selections
                          setSelectedServers(["NO_SERVERS"]);
                          setSelectedTools([]);
                        } else {
                          // Remove NO_SERVERS and use preset defaults
                          setSelectedServers(presetServers);
                          setSelectedTools(currentPresetTools || []);
                        }
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <label
                      htmlFor={`no-servers-${stepIndex}`}
                      className="text-xs text-gray-700 dark:text-gray-300 cursor-pointer flex-1"
                    >
                      <span className="font-medium">No MCP Servers</span>
                      <span className="text-gray-500 dark:text-gray-500 ml-1">
                        (Pure LLM mode - no tools available)
                      </span>
                    </label>
                  </div>

                  {/* Server/Tool Selection (disabled when NO_SERVERS is selected) */}
                  {!hasNoServers && (
                    <ToolSelectionSection
                      availableServers={presetServers}
                      selectedServers={actualSelectedServers}
                      selectedTools={selectedTools}
                      onServerChange={(servers) => {
                        // Ensure NO_SERVERS is not included
                        setSelectedServers(servers.filter(s => s !== "NO_SERVERS"));
                      }}
                      onToolChange={setSelectedTools}
                      stepId={step.id}
                    />
                  )}

                  {/* Pre-discovered Tools Display - show when Tool Search Mode is enabled */}
                  {agentConfigs.use_tool_search_mode && agentConfigs.pre_discovered_tools && agentConfigs.pre_discovered_tools.length > 0 && (
                    <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700">
                      <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1.5 flex items-center gap-1">
                        <Sparkles className="w-3 h-3" />
                        Pre-discovered Tools ({agentConfigs.pre_discovered_tools.length})
                      </div>
                      <div className="flex flex-wrap gap-1">
                        {agentConfigs.pre_discovered_tools.map((tool, idx) => (
                          <span
                            key={idx}
                            className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200"
                          >
                            {tool}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}

            {/* Custom Tool Categories and Individual Tools Selection */}
            <div className="border-t border-gray-200 dark:border-gray-700"></div>
            <div className="space-y-3">
              <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                Custom Tools
              </div>
              <div className="text-xs text-gray-500 dark:text-gray-500 italic">
                Select categories (enables all tools) or individual tools. By default, all tools are enabled.
              </div>
              
              {/* Workspace Tools Category */}
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="flex items-center gap-2 cursor-pointer flex-1">
                    <input
                      type="checkbox"
                      checked={(() => {
                        const allWorkspaceTools = getToolsByCategory('workspace_tools');
                        const categoryEnabled = isCategoryEnabled('workspace_tools', enabledCustomTools);
                        
                        if (categoryEnabled) return true;
                        
                        // Check if all workspace tools are enabled individually
                        const workspaceSpecificTools = enabledCustomTools
                          .map(entry => parseToolEntry(entry))
                          .filter(parsed => parsed && parsed.category === 'workspace_tools' && parsed.tool !== '*')
                          .map(parsed => parsed!.tool);
                        
                        // Checked if all tools are enabled (either via category:* or all individual tools)
                        return workspaceSpecificTools.length === allWorkspaceTools.length;
                      })()}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setEnabledCustomTools(prev => enableCategory('workspace_tools', prev));
                        } else {
                          setEnabledCustomTools(prev => disableCategory('workspace_tools', prev));
                        }
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Workspace Tools</span>
                    <span className="text-xs text-gray-500 dark:text-gray-500">
                      {(() => {
                        const allWorkspaceTools = getToolsByCategory('workspace_tools');
                        const categoryEnabled = isCategoryEnabled('workspace_tools', enabledCustomTools);
                        
                        let enabledCount = 0;
                        if (categoryEnabled || enabledCustomTools.length === 0) {
                          enabledCount = allWorkspaceTools.length;
                        } else {
                          const workspaceSpecificTools = enabledCustomTools
                            .map(entry => parseToolEntry(entry))
                            .filter(parsed => parsed && parsed.category === 'workspace_tools' && parsed.tool !== '*')
                            .map(parsed => parsed!.tool);
                          enabledCount = workspaceSpecificTools.length;
                        }
                        
                        return `(${enabledCount}/${allWorkspaceTools.length} tools)`;
                      })()}
                    </span>
                  </label>
                  <button
                    type="button"
                    onClick={() => {
                      const newExpanded = new Set(expandedToolCategories);
                      if (newExpanded.has('workspace_tools')) {
                        newExpanded.delete('workspace_tools');
                      } else {
                        newExpanded.add('workspace_tools');
                      }
                      setExpandedToolCategories(newExpanded);
                    }}
                    className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                  >
                    {expandedToolCategories.has('workspace_tools') ? 'Hide' : 'Show'} tools
                  </button>
                </div>
                {expandedToolCategories.has('workspace_tools') && (
                  <div className="ml-6 space-y-3 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                    {/* Workspace Tools Sub-categories (FRONTEND ONLY - for easy grouping/toggling) */}
                    {/* Backend only receives individual tool names in enabled_custom_tools */}
                    {/* Basic Workspace Tools Sub-category */}
                    {(() => {
                      const subCategoryName = 'basic_workspace';
                      const subCategoryTools = getToolsByWorkspaceSubCategory(subCategoryName);
                      const isSubCategoryChecked = isSubCategoryEnabled('workspace_tools', subCategoryTools, enabledCustomTools);
                      const enabledInSubCategory = subCategoryTools.filter(toolName => 
                        isToolEnabled('workspace_tools', toolName, enabledCustomTools)
                      );
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  setEnabledCustomTools(prev => 
                                    toggleSubCategory('workspace_tools', subCategoryTools, e.target.checked, prev)
                                  );
                                }}
                                className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                              />
                              <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Basic Workspace</span>
                              <span className="text-xs text-gray-500 dark:text-gray-500">
                                ({enabledInSubCategory.length}/{subCategoryTools.length})
                              </span>
                            </label>
                            <button
                              type="button"
                              onClick={() => {
                                const newExpanded = new Set(expandedWorkspaceSubCategories);
                                if (newExpanded.has(subCategoryName)) {
                                  newExpanded.delete(subCategoryName);
                                } else {
                                  newExpanded.add(subCategoryName);
                                }
                                setExpandedWorkspaceSubCategories(newExpanded);
                              }}
                              className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                            >
                              {expandedWorkspaceSubCategories.has(subCategoryName) ? 'Hide' : 'Show'} tools
                            </button>
                          </div>
                          {expandedWorkspaceSubCategories.has(subCategoryName) && (
                            <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                              {subCategoryTools.map((toolName) => {
                                const toolIsEnabled = isToolEnabled('workspace_tools', toolName, enabledCustomTools);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={toolIsEnabled}
                                      onChange={(e) => {
                                        if (e.target.checked) {
                                          setEnabledCustomTools(prev => enableTool('workspace_tools', toolName, prev));
                                        } else {
                                          setEnabledCustomTools(prev => disableTool('workspace_tools', toolName, prev));
                                        }
                                      }}
                                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                                    />
                                    <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                                  </label>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })()}
                    
                    {/* Advanced Workspace Tools Sub-category */}
                    {(() => {
                      const subCategoryName = 'advanced_workspace';
                      const subCategoryTools = getToolsByWorkspaceSubCategory(subCategoryName);
                      const isSubCategoryChecked = isSubCategoryEnabled('workspace_tools', subCategoryTools, enabledCustomTools);
                      const enabledInSubCategory = subCategoryTools.filter(toolName => 
                        isToolEnabled('workspace_tools', toolName, enabledCustomTools)
                      );
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  setEnabledCustomTools(prev => 
                                    toggleSubCategory('workspace_tools', subCategoryTools, e.target.checked, prev)
                                  );
                                }}
                                className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                              />
                              <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Advanced Workspace</span>
                              <span className="text-xs text-gray-500 dark:text-gray-500">
                                ({enabledInSubCategory.length}/{subCategoryTools.length})
                              </span>
                            </label>
                            <button
                              type="button"
                              onClick={() => {
                                const newExpanded = new Set(expandedWorkspaceSubCategories);
                                if (newExpanded.has(subCategoryName)) {
                                  newExpanded.delete(subCategoryName);
                                } else {
                                  newExpanded.add(subCategoryName);
                                }
                                setExpandedWorkspaceSubCategories(newExpanded);
                              }}
                              className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                            >
                              {expandedWorkspaceSubCategories.has(subCategoryName) ? 'Hide' : 'Show'} tools
                            </button>
                          </div>
                          {expandedWorkspaceSubCategories.has(subCategoryName) && (
                            <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                              {subCategoryTools.map((toolName) => {
                                const toolIsEnabled = isToolEnabled('workspace_tools', toolName, enabledCustomTools);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={toolIsEnabled}
                                      onChange={(e) => {
                                        if (e.target.checked) {
                                          setEnabledCustomTools(prev => enableTool('workspace_tools', toolName, prev));
                                        } else {
                                          setEnabledCustomTools(prev => disableTool('workspace_tools', toolName, prev));
                                        }
                                      }}
                                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                                    />
                                    <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                                  </label>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })()}
                    
                    {/* Plus Tools Sub-category */}
                    {(() => {
                      const subCategoryName = 'plus_tools';
                      const subCategoryTools = getToolsByWorkspaceSubCategory(subCategoryName);
                      const isSubCategoryChecked = isSubCategoryEnabled('workspace_tools', subCategoryTools, enabledCustomTools);
                      const enabledInSubCategory = subCategoryTools.filter(toolName => 
                        isToolEnabled('workspace_tools', toolName, enabledCustomTools)
                      );
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  setEnabledCustomTools(prev => 
                                    toggleSubCategory('workspace_tools', subCategoryTools, e.target.checked, prev)
                                  );
                                }}
                                className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                              />
                              <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Plus Tools</span>
                              <span className="text-xs text-gray-500 dark:text-gray-500">
                                ({enabledInSubCategory.length}/{subCategoryTools.length})
                              </span>
                            </label>
                            <button
                              type="button"
                              onClick={() => {
                                const newExpanded = new Set(expandedWorkspaceSubCategories);
                                if (newExpanded.has(subCategoryName)) {
                                  newExpanded.delete(subCategoryName);
                                } else {
                                  newExpanded.add(subCategoryName);
                                }
                                setExpandedWorkspaceSubCategories(newExpanded);
                              }}
                              className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                            >
                              {expandedWorkspaceSubCategories.has(subCategoryName) ? 'Hide' : 'Show'} tools
                            </button>
                          </div>
                          {expandedWorkspaceSubCategories.has(subCategoryName) && (
                            <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                              {subCategoryTools.map((toolName) => {
                                const toolIsEnabled = isToolEnabled('workspace_tools', toolName, enabledCustomTools);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={toolIsEnabled}
                                      onChange={(e) => {
                                        if (e.target.checked) {
                                          setEnabledCustomTools(prev => enableTool('workspace_tools', toolName, prev));
                                        } else {
                                          setEnabledCustomTools(prev => disableTool('workspace_tools', toolName, prev));
                                        }
                                      }}
                                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                                    />
                                    <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                                  </label>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })()}
                  </div>
                )}
              </div>

              {/* Human Tools Category */}
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="flex items-center gap-2 cursor-pointer flex-1">
                    <input
                      type="checkbox"
                      checked={(() => {
                        const allHumanTools = getToolsByCategory('human_tools');
                        const categoryEnabled = isCategoryEnabled('human_tools', enabledCustomTools);
                        
                        if (categoryEnabled) return true;
                        
                        // Check if all human tools are enabled individually
                        const humanSpecificTools = enabledCustomTools
                          .map(entry => parseToolEntry(entry))
                          .filter(parsed => parsed && parsed.category === 'human_tools' && parsed.tool !== '*')
                          .map(parsed => parsed!.tool);
                        
                        // Checked if all tools are enabled (either via category:* or all individual tools)
                        return humanSpecificTools.length === allHumanTools.length;
                      })()}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setEnabledCustomTools(prev => enableCategory('human_tools', prev));
                        } else {
                          setEnabledCustomTools(prev => disableCategory('human_tools', prev));
                        }
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Human Tools</span>
                    <span className="text-xs text-gray-500 dark:text-gray-500">
                      {(() => {
                        const allHumanTools = getToolsByCategory('human_tools');
                        const categoryEnabled = isCategoryEnabled('human_tools', enabledCustomTools);
                        
                        let enabledCount = 0;
                        if (categoryEnabled || enabledCustomTools.length === 0) {
                          enabledCount = allHumanTools.length;
                        } else {
                          const humanSpecificTools = enabledCustomTools
                            .map(entry => parseToolEntry(entry))
                            .filter(parsed => parsed && parsed.category === 'human_tools' && parsed.tool !== '*')
                            .map(parsed => parsed!.tool);
                          enabledCount = humanSpecificTools.length;
                        }
                        
                        return `(${enabledCount}/${allHumanTools.length} tools)`;
                      })()}
                    </span>
                  </label>
                  <button
                    type="button"
                    onClick={() => {
                      const newExpanded = new Set(expandedToolCategories);
                      if (newExpanded.has('human_tools')) {
                        newExpanded.delete('human_tools');
                      } else {
                        newExpanded.add('human_tools');
                      }
                      setExpandedToolCategories(newExpanded);
                    }}
                    className="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                  >
                    {expandedToolCategories.has('human_tools') ? 'Hide' : 'Show'} tools
                  </button>
                </div>
                {expandedToolCategories.has('human_tools') && (
                  <div className="ml-6 space-y-1.5 pl-2 border-l-2 border-gray-200 dark:border-gray-700">
                    {HUMAN_TOOLS.map((toolName) => {
                      const toolIsEnabled = isToolEnabled('human_tools', toolName, enabledCustomTools);
                      
                      return (
                        <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={toolIsEnabled}
                            onChange={(e) => {
                              if (e.target.checked) {
                                setEnabledCustomTools(prev => enableTool('human_tools', toolName, prev));
                              } else {
                                setEnabledCustomTools(prev => disableTool('human_tools', toolName, prev));
                              }
                            }}
                            className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                          />
                          <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                        </label>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            {/* Context Offloading Virtual Tools Toggle */}
            <div className="border-t border-gray-200 dark:border-gray-700"></div>
            <div className="space-y-2">
              <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                Context Offloading Virtual Tools
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id={`large-output-${stepIndex}`}
                  checked={agentConfigs.enable_context_offloading !== false}
                  onChange={(e) => {
                    setAgentConfigs((prev) => ({
                      ...prev,
                      enable_context_offloading: e.target.checked,
                    }));
                  }}
                  className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                />
                <label
                  htmlFor={`large-output-${stepIndex}`}
                  className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer flex-1"
                >
                  Enable Context Offloading Virtual Tools
                  <span className="text-gray-500 dark:text-gray-500 ml-1">
                    (read_large_output, search_large_output, query_large_output)
                  </span>
                </label>
              </div>
            </div>

            {/* Prerequisite Detection Configuration - Hidden for decision steps and conditional steps */}
            {!step.has_decision_step && !step.has_condition && (
            <PrerequisiteConfigPanel
              agentConfigs={agentConfigs}
              onUpdate={(updatedConfigs) => {
                setAgentConfigs(updatedConfigs);
              }}
              planSteps={planSteps}
              currentStepIndex={stepIndex}
            />
            )}

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-700">
              <Button
                variant="default"
                size="sm"
                onClick={handleSave}
                disabled={isSaving}
                className="text-xs h-7 px-3"
              >
                {isSaving ? 'Saving...' : 'Save'}
              </Button>
            </div>
          </div>
        </div>
      )}
        </>
      )}
    </div>
  );
};

