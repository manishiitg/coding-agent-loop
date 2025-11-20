import React, { useState, useEffect, useRef } from 'react';
import { ChevronDown, ChevronUp, Settings } from 'lucide-react';
import { Button } from '../../ui/Button';
import LLMSelectionDropdown from '../../LLMSelectionDropdown';
import { ToolSelectionSection } from '../../ToolSelectionSection';
import { usePresetApplication } from '../../../stores/useGlobalPresetStore';
import type { TodoStepWithConfigs, AgentConfigs, AgentLLMConfig } from '../../../utils/stepConfigMatching';
import type { LLMOption } from '../../../types/llm';
import { useLLMStore } from '../../../stores/useLLMStore';
import { 
  HUMAN_TOOLS, 
  getToolsByCategory,
  getToolsByWorkspaceSubCategory,
} from '../../../utils/customToolNames';

interface StepEditPanelProps {
  step: TodoStepWithConfigs;
  stepIndex: number;
  onSave: (updatedStep: TodoStepWithConfigs) => Promise<void>;
  onCancel: () => void;
  isSaving?: boolean;
  presetServers?: string[]; // Preset's selected servers (subset to show in UI)
  isExpanded?: boolean; // Controlled expanded state from parent
  onToggleExpanded?: (expanded: boolean) => void; // Callback when expansion state changes
}

const MAX_TURNS_OPTIONS = [10, 25, 50, 75, 100] as const;

export const StepEditPanel: React.FC<StepEditPanelProps> = ({
  step,
  stepIndex,
  onSave,
  isSaving = false,
  presetServers = [],
  isExpanded: controlledIsExpanded,
  onToggleExpanded,
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
  const [agentConfigs, setAgentConfigs] = useState<AgentConfigs>(() => {
    const configs = step.agent_configs || {};
    // Force enable validation for loop steps
    if (step.has_loop && configs.disable_validation) {
      return {
        ...configs,
        disable_validation: false,
      };
    }
    return configs;
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
      return agentConfigs.selected_tools;
    }
    // Otherwise, use preset defaults for display
    return currentPresetTools || [];
  });

  // Check if NO_SERVERS is explicitly selected
  const hasNoServers = selectedServers.includes("NO_SERVERS");
  const actualSelectedServers = selectedServers.filter(s => s !== "NO_SERVERS");

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

  // Sync state when step changes (different step identifier)
  // This prevents infinite loops by only syncing when we switch to a different step
  useEffect(() => {
    const isDifferentStep = prevStepIdentifierRef.current !== stepIdentifier;
    
    if (isDifferentStep) {
      const currentConfigs = step.agent_configs || {};
      
      // Reset agentConfigs state from step's config
      // Force enable validation for loop steps
      const newAgentConfigs: AgentConfigs = step.has_loop && currentConfigs.disable_validation
        ? { ...currentConfigs, disable_validation: false }
        : currentConfigs;
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
        setSelectedTools(currentConfigs.selected_tools);
      } else {
        setSelectedTools(currentPresetTools || []);
      }

      // Reset expanded categories when step changes
      setExpandedToolCategories(new Set());
      setExpandedWorkspaceSubCategories(new Set(['basic_workspace', 'advanced_workspace', 'plus_tools']));

      // Update ref for next comparison
      prevStepIdentifierRef.current = stepIdentifier;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stepIdentifier, step.agent_configs, presetServers, currentPresetTools]); // Include stepIdentifier and relevant dependencies

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

  // Update max turns
  const handleMaxTurnsChange = (
    agentType: 'execution' | 'validation' | 'learning',
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
    // Ensure validation is not disabled for loop steps
    const finalConfigs: AgentConfigs = {
      ...agentConfigs,
      // Force enable validation for loop steps (required to check loop conditions)
      disable_validation: step.has_loop ? false : agentConfigs.disable_validation,
    };

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

    // Handle custom tool categories and specific tools
    // If both are empty/undefined, all tools are enabled (default behavior)
    if (agentConfigs.enabled_custom_tool_categories && agentConfigs.enabled_custom_tool_categories.length === 0) {
      finalConfigs.enabled_custom_tool_categories = undefined;
    } else if (agentConfigs.enabled_custom_tool_categories && agentConfigs.enabled_custom_tool_categories.length > 0) {
      finalConfigs.enabled_custom_tool_categories = agentConfigs.enabled_custom_tool_categories;
    }

    // Handle specific tools: if empty array, set to undefined (use categories or all tools)
    if (agentConfigs.enabled_custom_tools && agentConfigs.enabled_custom_tools.length === 0) {
      finalConfigs.enabled_custom_tools = undefined;
    } else if (agentConfigs.enabled_custom_tools && agentConfigs.enabled_custom_tools.length > 0) {
      finalConfigs.enabled_custom_tools = agentConfigs.enabled_custom_tools;
    }

    // Handle large output virtual tools: only save if explicitly set to false
    if (agentConfigs.enable_large_output_virtual_tools === false) {
      finalConfigs.enable_large_output_virtual_tools = false;
    } else {
      // Default to true (undefined means enabled for backward compatibility)
      finalConfigs.enable_large_output_virtual_tools = undefined;
    }

    // Debug logging to verify data being saved
    console.log('[StepEditPanel] Saving step config:', {
      stepTitle: step.title,
      selectedServers,
      selectedTools,
      finalConfigs: {
        selected_servers: finalConfigs.selected_servers,
        selected_tools: finalConfigs.selected_tools,
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
    const execLLM = llmConfigToOption(agentConfigs.execution_llm) || getCurrentLLMOption();
    const valLLM = llmConfigToOption(agentConfigs.validation_llm) || getCurrentLLMOption();
    const learnLLM = llmConfigToOption(agentConfigs.learning_llm) || getCurrentLLMOption();
    
    const parts = [];
    if (execLLM) parts.push(`Exec: ${execLLM.label}`);
    if (valLLM && !agentConfigs.disable_validation) parts.push(`Val: ${valLLM.label}`);
    if (learnLLM && !agentConfigs.disable_learning) {
      const detailLevel = agentConfigs.learning_detail_level || 'general';
      const detailLabel = detailLevel === 'exact' ? 'Exact' : detailLevel === 'none' ? 'None' : 'General';
      parts.push(`Learn: ${learnLLM.label} (${detailLabel})`);
    }
    if (agentConfigs.disable_validation) parts.push('Val: Disabled');
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
    if (selectedTools.length > 0) {
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

  // Get custom tools summary with detailed information
  const getCustomToolsSummary = () => {
    const categories = agentConfigs.enabled_custom_tool_categories || [];
    const tools = agentConfigs.enabled_custom_tools || [];
    const allWorkspaceTools = getToolsByCategory('workspace_tools');
    const allHumanTools = getToolsByCategory('human_tools');
    
    // Check if no filtering (default: all enabled)
    if (categories.length === 0 && tools.length === 0) {
      // Show default status with human tools and large output
      const defaultParts = ['All custom tools enabled (default)'];
      if (agentConfigs.enable_large_output_virtual_tools === false) {
        defaultParts.push('Large output: disabled');
      } else {
        defaultParts.push('Large output: enabled');
      }
      return defaultParts.join(' • ');
    }
    
    const parts = [];
    
    // Category details
    if (categories.length > 0) {
      if (categories.length === 1) {
        const categoryName = categories[0] === 'workspace_tools' ? 'Workspace' : 'Human';
        parts.push(`${categoryName} Tools`);
      } else {
        parts.push(`${categories.length} categories`);
      }
    }
    
    // Individual tool details (only if not using categories)
    if (tools.length > 0 && categories.length === 0) {
      const workspaceToolsInList = tools.filter(t => allWorkspaceTools.includes(t));
      const humanToolsInList = tools.filter(t => allHumanTools.includes(t));
      
      if (workspaceToolsInList.length > 0 && humanToolsInList.length > 0) {
        // Mixed tools from both categories
        parts.push(`${tools.length} tools (${workspaceToolsInList.length} workspace, ${humanToolsInList.length} human)`);
      } else if (workspaceToolsInList.length > 0) {
        // Only workspace tools - show sub-category breakdown
        if (workspaceToolsInList.length === allWorkspaceTools.length) {
          parts.push('All workspace tools');
        } else {
          // Show sub-category breakdown
          const basicTools = getToolsByWorkspaceSubCategory('basic_workspace');
          const advancedTools = getToolsByWorkspaceSubCategory('advanced_workspace');
          const plusTools = getToolsByWorkspaceSubCategory('plus_tools');
          
          const basicEnabled = workspaceToolsInList.filter(t => basicTools.includes(t)).length;
          const advancedEnabled = workspaceToolsInList.filter(t => advancedTools.includes(t)).length;
          const plusEnabled = workspaceToolsInList.filter(t => plusTools.includes(t)).length;
          
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
            parts.push(`${workspaceToolsInList.length}/${allWorkspaceTools.length} workspace (${subCategoryParts.join(', ')})`);
          } else {
            parts.push(`${workspaceToolsInList.length}/${allWorkspaceTools.length} workspace tools`);
          }
        }
      } else if (humanToolsInList.length > 0) {
        // Only human tools
        if (humanToolsInList.length === allHumanTools.length) {
          parts.push('All human tools');
        } else {
          parts.push(`${humanToolsInList.length}/${allHumanTools.length} human tools`);
        }
      }
    } else if (tools.length > 0 && categories.length > 0) {
      // Some categories + some individual tools (mixed mode)
      const workspaceToolsInList = tools.filter(t => allWorkspaceTools.includes(t));
      const humanToolsInList = tools.filter(t => allHumanTools.includes(t));
      if (workspaceToolsInList.length > 0 || humanToolsInList.length > 0) {
        parts.push(`+ ${tools.length} individual tools`);
      }
    }
    
    // Show human tools status if not already included
    if (categories.length > 0 && categories.includes('human_tools')) {
      // Human tools category is enabled
      if (!parts.some(p => p.includes('human'))) {
        parts.push('All human tools');
      }
    } else if (tools.length > 0) {
      const humanToolsInList = tools.filter(t => allHumanTools.includes(t));
      if (humanToolsInList.length > 0 && !parts.some(p => p.includes('human'))) {
        if (humanToolsInList.length === allHumanTools.length) {
          parts.push('All human tools');
        } else {
          parts.push(`${humanToolsInList.length}/${allHumanTools.length} human tools`);
        }
      } else if (humanToolsInList.length === 0 && categories.length === 0 && tools.length > 0) {
        // Workspace tools enabled but human tools disabled
        parts.push('0/1 human tools');
      }
    } else if (categories.length === 0 && tools.length === 0) {
      // Default state - all enabled, so human tools are enabled
      // Already handled at the top
    } else if (categories.length > 0 && !categories.includes('human_tools')) {
      // Categories enabled but human_tools not in the list
      parts.push('0/1 human tools');
    }
    
    // Large output tools status
    if (agentConfigs.enable_large_output_virtual_tools === false) {
      parts.push('Large output: disabled');
    } else {
      // Only show if explicitly enabled (not default)
      // Default is enabled, so we don't need to show it
      // But we can show it if it's explicitly set to true for clarity
      // Actually, let's always show it for clarity
      parts.push('Large output: enabled');
    }
    
    return parts.length > 0 ? parts.join(' • ') : 'No custom tools';
  };

  return (
    <div className="mt-2 border-t border-gray-200 dark:border-gray-700 pt-2">
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
              <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                Execution
              </div>
              <div className="flex items-center gap-2">
                <div className="flex-1 min-w-0">
                  <LLMSelectionDropdown
                    availableLLMs={availableLLMs}
                    selectedLLM={llmConfigToOption(agentConfigs.execution_llm) || getCurrentLLMOption()}
                    onLLMSelect={handleExecutionLLMSelect}
                    inModal={false}
                    openDirection="down"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Max Turns:</label>
                  <select
                    value={agentConfigs.execution_max_turns || 25}
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
                <label 
                  className={`flex items-center gap-1.5 ${step.has_loop ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}`}
                  title={step.has_loop ? "Validation cannot be disabled for loop steps - it's required to check loop conditions" : undefined}
                >
                  <input
                    type="checkbox"
                    checked={agentConfigs.disable_validation || false}
                    onChange={(e) => {
                      if (!step.has_loop) {
                        handleToggleChange('disable_validation', e.target.checked);
                      }
                    }}
                    disabled={step.has_loop}
                    className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                  />
                  <span className="text-xs text-gray-600 dark:text-gray-400">
                    Disable{step.has_loop && ' (Required for loops)'}
                  </span>
                </label>
              </div>
              {!agentConfigs.disable_validation ? (
                <div className="flex items-center gap-2">
                  <div className="flex-1 min-w-0">
                    <LLMSelectionDropdown
                      availableLLMs={availableLLMs}
                      selectedLLM={llmConfigToOption(agentConfigs.validation_llm) || getCurrentLLMOption()}
                      onLLMSelect={handleValidationLLMSelect}
                      inModal={false}
                      openDirection="down"
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Max Turns:</label>
                    <select
                      value={agentConfigs.validation_max_turns || 25}
                      onChange={(e) => handleMaxTurnsChange('validation', parseInt(e.target.value))}
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
                <label className="flex items-center gap-1.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={agentConfigs.disable_learning || false}
                    onChange={(e) => handleToggleChange('disable_learning', e.target.checked)}
                    className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                  />
                  <span className="text-xs text-gray-600 dark:text-gray-400">Disable</span>
                </label>
              </div>
              {!agentConfigs.disable_learning ? (
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <div className="flex-1 min-w-0">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={llmConfigToOption(agentConfigs.learning_llm) || getCurrentLLMOption()}
                        onLLMSelect={handleLearningLLMSelect}
                        inModal={false}
                        openDirection="down"
                      />
                    </div>
                    <div className="flex items-center gap-2">
                      <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Max Turns:</label>
                      <select
                        value={agentConfigs.learning_max_turns || 25}
                        onChange={(e) => handleMaxTurnsChange('learning', parseInt(e.target.value))}
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
                  <div className="flex items-center gap-2">
                    <label className="text-xs text-gray-600 dark:text-gray-400 whitespace-nowrap">Detail Level:</label>
                    <select
                      value={agentConfigs.learning_detail_level || 'general'}
                      onChange={(e) => {
                        const value = e.target.value as 'exact' | 'general' | 'none';
                        setAgentConfigs((prev): AgentConfigs => ({
                          ...prev,
                          learning_detail_level: value,
                        }));
                      }}
                      className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs focus:ring-2 focus:ring-blue-500 focus:border-blue-500 flex-1"
                    >
                      <option value="exact">Exact MCP Tools</option>
                      <option value="general">General Patterns</option>
                      <option value="none">No Learnings Required</option>
                    </select>
                  </div>
                </div>
              ) : (
                <div className="text-xs text-gray-500 dark:text-gray-500 italic py-1">
                  Learning disabled for this step
                </div>
              )}
            </div>

            {/* Loop Configuration (only shown if has_loop is true) */}
            {step.has_loop && (
              <>
                <div className="border-t border-gray-200 dark:border-gray-700"></div>
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id={`learning-after-loop-${stepIndex}`}
                    checked={agentConfigs.learning_after_loop_iteration || false}
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
                    />
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
                        const categories = agentConfigs.enabled_custom_tool_categories || [];
                        const tools = agentConfigs.enabled_custom_tools || [];
                        const allWorkspaceTools = getToolsByCategory('workspace_tools');
                        
                        // Category is checked if:
                        // 1. Explicitly in categories list, OR
                        // 2. All tools from this category are in enabled_custom_tools, OR
                        // 3. No filtering at all (default: all enabled), OR
                        // 4. At least one tool from this category is enabled
                        let checked = false;
                        if (categories.includes('workspace_tools')) {
                          checked = true;
                        } else if (tools.length > 0 && allWorkspaceTools.every(tool => tools.includes(tool))) {
                          checked = true; // All tools enabled individually
                        } else if (categories.length === 0 && tools.length === 0) {
                          checked = true; // Default: all enabled
                        } else if (tools.length > 0) {
                          // Check if any tools from this category are enabled
                          const enabledInCategory = tools.filter((t: string) => allWorkspaceTools.includes(t));
                          if (enabledInCategory.length > 0) {
                            checked = true; // At least one tool from this category is enabled
                          }
                        }
                        
                        return checked;
                      })()}
                      onChange={(e) => {
                        console.log('[CHECKBOX_DEBUG] Workspace Tools category:', e.target.checked ? 'ENABLING' : 'DISABLING');
                        
                        setAgentConfigs((prev) => {
                          const current = prev.enabled_custom_tool_categories || [];
                          const currentTools = prev.enabled_custom_tools || [];
                          const workspaceTools = getToolsByCategory('workspace_tools');
                          
                          if (e.target.checked) {
                            // Enable category - add to categories, remove workspace tools from specific tools
                            const newCategories = current.includes('workspace_tools') ? current : [...current, 'workspace_tools'];
                            const newTools = currentTools.filter(t => !workspaceTools.includes(t));
                            
                            return {
                              ...prev,
                              enabled_custom_tool_categories: newCategories,
                              enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                            };
                          } else {
                            // Disable category
                            const isDefaultState = current.length === 0 && currentTools.length === 0;
                            
                            if (isDefaultState) {
                              // Coming from default state - explicitly disable workspace_tools by enabling only human_tools
                              return {
                                ...prev,
                                enabled_custom_tool_categories: ['human_tools'],
                                enabled_custom_tools: undefined,
                              };
                            } else {
                              // Remove category from list
                              const newCategories = current.filter(cat => cat !== 'workspace_tools');
                              // Remove all workspace tools from enabled_custom_tools
                              const newTools = currentTools.filter(t => !workspaceTools.includes(t));
                              
                              return {
                                ...prev,
                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                              };
                            }
                          }
                        });
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Workspace Tools</span>
                    <span className="text-xs text-gray-500 dark:text-gray-500">
                      {(() => {
                        const categories = agentConfigs.enabled_custom_tool_categories || [];
                        const tools = agentConfigs.enabled_custom_tools || [];
                        const allWorkspaceTools = getToolsByCategory('workspace_tools');
                        
                        // Calculate enabled count
                        let enabledCount = 0;
                        if (categories.includes('workspace_tools')) {
                          // Category enabled = all tools enabled
                          enabledCount = allWorkspaceTools.length;
                        } else if (categories.length === 0 && tools.length === 0) {
                          // Default state = all enabled
                          enabledCount = allWorkspaceTools.length;
                        } else if (tools.length > 0) {
                          // Count how many workspace tools are in the enabled list
                          enabledCount = tools.filter((t: string) => allWorkspaceTools.includes(t)).length;
                        } else {
                          // No workspace tools enabled
                          enabledCount = 0;
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
                      const categories = agentConfigs.enabled_custom_tool_categories || [];
                      const tools = agentConfigs.enabled_custom_tools || [];
                      const isCategoryEnabled = categories.includes('workspace_tools');
                      const enabledInSubCategory = subCategoryTools.filter((t: string) => 
                        isCategoryEnabled || (tools.length > 0 ? tools.includes(t) : categories.length === 0 && tools.length === 0)
                      );
                      const isSubCategoryChecked = isCategoryEnabled || enabledInSubCategory.length === subCategoryTools.length || 
                        (categories.length === 0 && tools.length === 0);
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  const wantsEnabled = e.target.checked;
                                  setAgentConfigs((prev: typeof agentConfigs) => {
                                    const current = prev.enabled_custom_tools || [];
                                    const currentCategories = prev.enabled_custom_tool_categories || [];
                                    const allCategoryTools = getToolsByCategory('workspace_tools');
                                    const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                    const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                    
                                    if (wantsEnabled) {
                                      if (categoryIsEnabled || isDefaultState) {
                                        // Already enabled via category or default
                                        return prev;
                                      }
                                      // Add all tools from this sub-category
                                      const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                      const newTools = [...new Set([...subCategoryTools, ...current.filter((t: string) => !subCategoryTools.includes(t)), ...toolsFromOtherCategories])];
                                      return {
                                        ...prev,
                                        enabled_custom_tools: newTools,
                                      };
                                    } else {
                                      if (categoryIsEnabled) {
                                        // Remove category, add all other workspace tools except this sub-category
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tool_categories: currentCategories.filter((cat: string) => cat !== 'workspace_tools'),
                                          enabled_custom_tools: [...otherWorkspaceTools, ...toolsFromOtherCategories],
                                        };
                                      } else if (isDefaultState) {
                                        // Enable all workspace tools except this sub-category
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: otherWorkspaceTools,
                                        };
                                      } else {
                                        // Remove all tools from this sub-category
                                        const newTools = current.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                                        };
                                      }
                                    }
                                  });
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
                                const isToolEnabled = isCategoryEnabled || (tools.length > 0 ? tools.includes(toolName) : categories.length === 0 && tools.length === 0);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={isToolEnabled}
                                      onChange={(e) => {
                                        const wantsEnabled = e.target.checked;
                                        setAgentConfigs((prev: typeof agentConfigs) => {
                                          const current = prev.enabled_custom_tools || [];
                                          const currentCategories = prev.enabled_custom_tool_categories || [];
                                          const allCategoryTools = getToolsByCategory('workspace_tools');
                                          const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                          const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                          
                                          if (wantsEnabled) {
                                            if (categoryIsEnabled || isDefaultState) {
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...allCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (!current.includes(toolName)) {
                                              return {
                                                ...prev,
                                                enabled_custom_tools: [...current, toolName],
                                              };
                                            }
                                          } else {
                                            if (categoryIsEnabled) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...otherCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (isDefaultState) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: otherCategoryTools,
                                              };
                                            } else {
                                              const finalTools = current.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: finalTools.length > 0 ? finalTools : undefined,
                                              };
                                            }
                                          }
                                          return prev;
                                        });
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
                      const categories = agentConfigs.enabled_custom_tool_categories || [];
                      const tools = agentConfigs.enabled_custom_tools || [];
                      const isCategoryEnabled = categories.includes('workspace_tools');
                      const enabledInSubCategory = subCategoryTools.filter((t: string) => 
                        isCategoryEnabled || (tools.length > 0 ? tools.includes(t) : categories.length === 0 && tools.length === 0)
                      );
                      const isSubCategoryChecked = isCategoryEnabled || enabledInSubCategory.length === subCategoryTools.length || 
                        (categories.length === 0 && tools.length === 0);
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  const wantsEnabled = e.target.checked;
                                  setAgentConfigs((prev: typeof agentConfigs) => {
                                    const current = prev.enabled_custom_tools || [];
                                    const currentCategories = prev.enabled_custom_tool_categories || [];
                                    const allCategoryTools = getToolsByCategory('workspace_tools');
                                    const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                    const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                    
                                    if (wantsEnabled) {
                                      if (categoryIsEnabled || isDefaultState) {
                                        return prev;
                                      }
                                      const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                      const newTools = [...new Set([...subCategoryTools, ...current.filter((t: string) => !subCategoryTools.includes(t)), ...toolsFromOtherCategories])];
                                      return {
                                        ...prev,
                                        enabled_custom_tools: newTools,
                                      };
                                    } else {
                                      if (categoryIsEnabled) {
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tool_categories: currentCategories.filter((cat: string) => cat !== 'workspace_tools'),
                                          enabled_custom_tools: [...otherWorkspaceTools, ...toolsFromOtherCategories],
                                        };
                                      } else if (isDefaultState) {
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: otherWorkspaceTools,
                                        };
                                      } else {
                                        const newTools = current.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                                        };
                                      }
                                    }
                                  });
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
                                const isToolEnabled = isCategoryEnabled || (tools.length > 0 ? tools.includes(toolName) : categories.length === 0 && tools.length === 0);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={isToolEnabled}
                                      onChange={(e) => {
                                        const wantsEnabled = e.target.checked;
                                        setAgentConfigs((prev: typeof agentConfigs) => {
                                          const current = prev.enabled_custom_tools || [];
                                          const currentCategories = prev.enabled_custom_tool_categories || [];
                                          const allCategoryTools = getToolsByCategory('workspace_tools');
                                          const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                          const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                          
                                          if (wantsEnabled) {
                                            if (categoryIsEnabled || isDefaultState) {
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...allCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (!current.includes(toolName)) {
                                              return {
                                                ...prev,
                                                enabled_custom_tools: [...current, toolName],
                                              };
                                            }
                                          } else {
                                            if (categoryIsEnabled) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...otherCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (isDefaultState) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: otherCategoryTools,
                                              };
                                            } else {
                                              const finalTools = current.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: finalTools.length > 0 ? finalTools : undefined,
                                              };
                                            }
                                          }
                                          return prev;
                                        });
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
                      const categories = agentConfigs.enabled_custom_tool_categories || [];
                      const tools = agentConfigs.enabled_custom_tools || [];
                      const isCategoryEnabled = categories.includes('workspace_tools');
                      const enabledInSubCategory = subCategoryTools.filter((t: string) => 
                        isCategoryEnabled || (tools.length > 0 ? tools.includes(t) : categories.length === 0 && tools.length === 0)
                      );
                      const isSubCategoryChecked = isCategoryEnabled || enabledInSubCategory.length === subCategoryTools.length || 
                        (categories.length === 0 && tools.length === 0);
                      
                      return (
                        <div key={subCategoryName} className="space-y-1.5">
                          <div className="flex items-center justify-between">
                            <label className="flex items-center gap-2 cursor-pointer flex-1">
                              <input
                                type="checkbox"
                                checked={isSubCategoryChecked}
                                onChange={(e) => {
                                  const wantsEnabled = e.target.checked;
                                  setAgentConfigs((prev: typeof agentConfigs) => {
                                    const current = prev.enabled_custom_tools || [];
                                    const currentCategories = prev.enabled_custom_tool_categories || [];
                                    const allCategoryTools = getToolsByCategory('workspace_tools');
                                    const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                    const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                    
                                    if (wantsEnabled) {
                                      if (categoryIsEnabled || isDefaultState) {
                                        return prev;
                                      }
                                      const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                      const newTools = [...new Set([...subCategoryTools, ...current.filter((t: string) => !subCategoryTools.includes(t)), ...toolsFromOtherCategories])];
                                      return {
                                        ...prev,
                                        enabled_custom_tools: newTools,
                                      };
                                    } else {
                                      if (categoryIsEnabled) {
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tool_categories: currentCategories.filter((cat: string) => cat !== 'workspace_tools'),
                                          enabled_custom_tools: [...otherWorkspaceTools, ...toolsFromOtherCategories],
                                        };
                                      } else if (isDefaultState) {
                                        const otherWorkspaceTools = allCategoryTools.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: otherWorkspaceTools,
                                        };
                                      } else {
                                        const newTools = current.filter((t: string) => !subCategoryTools.includes(t));
                                        return {
                                          ...prev,
                                          enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                                        };
                                      }
                                    }
                                  });
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
                                const isToolEnabled = isCategoryEnabled || (tools.length > 0 ? tools.includes(toolName) : categories.length === 0 && tools.length === 0);
                                return (
                                  <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                                    <input
                                      type="checkbox"
                                      checked={isToolEnabled}
                                      onChange={(e) => {
                                        const wantsEnabled = e.target.checked;
                                        setAgentConfigs((prev: typeof agentConfigs) => {
                                          const current = prev.enabled_custom_tools || [];
                                          const currentCategories = prev.enabled_custom_tool_categories || [];
                                          const allCategoryTools = getToolsByCategory('workspace_tools');
                                          const categoryIsEnabled = currentCategories.includes('workspace_tools');
                                          const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                          
                                          if (wantsEnabled) {
                                            if (categoryIsEnabled || isDefaultState) {
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...allCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (!current.includes(toolName)) {
                                              return {
                                                ...prev,
                                                enabled_custom_tools: [...current, toolName],
                                              };
                                            }
                                          } else {
                                            if (categoryIsEnabled) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              const newCategories = currentCategories.filter((cat: string) => cat !== 'workspace_tools');
                                              const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                              return {
                                                ...prev,
                                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                                enabled_custom_tools: [...otherCategoryTools, ...toolsFromOtherCategories],
                                              };
                                            } else if (isDefaultState) {
                                              const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: otherCategoryTools,
                                              };
                                            } else {
                                              const finalTools = current.filter((t: string) => t !== toolName);
                                              return {
                                                ...prev,
                                                enabled_custom_tools: finalTools.length > 0 ? finalTools : undefined,
                                              };
                                            }
                                          }
                                          return prev;
                                        });
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
                        const categories = agentConfigs.enabled_custom_tool_categories || [];
                        const tools = agentConfigs.enabled_custom_tools || [];
                        const allHumanTools = getToolsByCategory('human_tools');
                        
                        // Category is checked if:
                        // 1. Explicitly in categories list, OR
                        // 2. All tools from this category are in enabled_custom_tools, OR
                        // 3. No filtering at all (default: all enabled), OR
                        // 4. At least one tool from this category is enabled
                        let checked = false;
                        if (categories.includes('human_tools')) {
                          checked = true;
                        } else if (tools.length > 0 && allHumanTools.every(tool => tools.includes(tool))) {
                          checked = true; // All tools enabled individually
                        } else if (categories.length === 0 && tools.length === 0) {
                          checked = true; // Default: all enabled
                        } else if (tools.length > 0) {
                          // Check if any tools from this category are enabled
                          const enabledInCategory = tools.filter((t: string) => allHumanTools.includes(t));
                          if (enabledInCategory.length > 0) {
                            checked = true; // At least one tool from this category is enabled
                          }
                        }
                        
                        return checked;
                      })()}
                      onChange={(e) => {
                        console.log('[CHECKBOX_DEBUG] Human Tools category:', e.target.checked ? 'ENABLING' : 'DISABLING');
                        
                        setAgentConfigs((prev) => {
                          const current = prev.enabled_custom_tool_categories || [];
                          const currentTools = prev.enabled_custom_tools || [];
                          const humanTools = getToolsByCategory('human_tools');
                          
                          if (e.target.checked) {
                            // Enable category - add to categories, remove human tools from specific tools
                            const newCategories = current.includes('human_tools') ? current : [...current, 'human_tools'];
                            const newTools = currentTools.filter(t => !humanTools.includes(t));
                            
                            return {
                              ...prev,
                              enabled_custom_tool_categories: newCategories,
                              enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                            };
                          } else {
                            // Disable category
                            const isDefaultState = current.length === 0 && currentTools.length === 0;
                            
                            if (isDefaultState) {
                              // Coming from default state - explicitly disable human_tools by enabling only workspace_tools
                              return {
                                ...prev,
                                enabled_custom_tool_categories: ['workspace_tools'],
                                enabled_custom_tools: undefined,
                              };
                            } else {
                              // Remove category from list
                              const newCategories = current.filter(cat => cat !== 'human_tools');
                              // Remove all human tools from enabled_custom_tools
                              const newTools = currentTools.filter(t => !humanTools.includes(t));
                              
                              return {
                                ...prev,
                                enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                enabled_custom_tools: newTools.length > 0 ? newTools : undefined,
                              };
                            }
                          }
                        });
                      }}
                      className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                    />
                    <span className="text-xs font-medium text-gray-700 dark:text-gray-300">Human Tools</span>
                    <span className="text-xs text-gray-500 dark:text-gray-500">
                      {(() => {
                        const categories = agentConfigs.enabled_custom_tool_categories || [];
                        const tools = agentConfigs.enabled_custom_tools || [];
                        const allHumanTools = getToolsByCategory('human_tools');
                        
                        // Calculate enabled count
                        let enabledCount = 0;
                        if (categories.includes('human_tools')) {
                          // Category enabled = all tools enabled
                          enabledCount = allHumanTools.length;
                        } else if (categories.length === 0 && tools.length === 0) {
                          // Default state = all enabled
                          enabledCount = allHumanTools.length;
                        } else if (tools.length > 0) {
                          // Count how many human tools are in the enabled list
                          enabledCount = tools.filter((t: string) => allHumanTools.includes(t)).length;
                        } else {
                          // No human tools enabled
                          enabledCount = 0;
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
                      const categories = agentConfigs.enabled_custom_tool_categories || [];
                      const tools = agentConfigs.enabled_custom_tools || [];
                      const isCategoryEnabled = categories.includes('human_tools');
                      const isToolEnabled = tools.includes(toolName);
                      const hasAnyFiltering = categories.length > 0 || tools.length > 0;
                      
                      // Tool is enabled if:
                      // 1. Category is enabled (all tools in category), OR
                      // 2. Tool is specifically in enabled_custom_tools, OR
                      // 3. No filtering at all (default: all enabled)
                      // IMPORTANT: If tools array exists and has items, we're in explicit mode - only show checked if tool is in the list
                      const isEnabled = isCategoryEnabled || (hasAnyFiltering ? isToolEnabled : true);
                      
                      return (
                        <label key={toolName} className="flex items-center gap-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={isEnabled}
                            onChange={(e) => {
                              const wantsEnabled = e.target.checked;
                              console.log('[CHECKBOX_DEBUG] Human tool:', toolName, wantsEnabled ? 'ENABLING' : 'DISABLING');
                              
                              setAgentConfigs((prev: typeof agentConfigs) => {
                                const current = prev.enabled_custom_tools || [];
                                const currentCategories = prev.enabled_custom_tool_categories || [];
                                const allCategoryTools = getToolsByCategory('human_tools');
                                const categoryIsEnabled = currentCategories.includes('human_tools');
                                const isDefaultState = currentCategories.length === 0 && current.length === 0;
                                
                                if (wantsEnabled) {
                                  // User wants to enable this tool
                                  if (categoryIsEnabled) {
                                    // Category is enabled - switch to individual tool mode with all tools enabled
                                    // Remove category, add all tools from this category
                                    const newCategories = currentCategories.filter((cat: string) => cat !== 'human_tools');
                                    const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                    
                                    return {
                                      ...prev,
                                      enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                      enabled_custom_tools: [...allCategoryTools, ...toolsFromOtherCategories],
                                    };
                                  } else if (isDefaultState) {
                                    // Default state - checkbox was already checked, user clicked to "re-check" it
                                    // This shouldn't normally happen, but if it does, just ensure it's explicitly enabled
                                    return {
                                      ...prev,
                                      enabled_custom_tools: allCategoryTools,
                                    };
                                  } else {
                                    // Category not enabled, not default - just add this tool
                                    if (!current.includes(toolName)) {
                                      return {
                                        ...prev,
                                        enabled_custom_tools: [...current, toolName],
                                      };
                                    }
                                  }
                                } else {
                                  // User wants to disable this tool
                                  if (categoryIsEnabled) {
                                    // Category is enabled - switch to individual tool mode, exclude this tool
                                    // Remove category, add all other tools from this category
                                    const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                    const newCategories = currentCategories.filter((cat: string) => cat !== 'human_tools');
                                    const toolsFromOtherCategories = current.filter((t: string) => !allCategoryTools.includes(t));
                                    
                                    return {
                                      ...prev,
                                      enabled_custom_tool_categories: newCategories.length > 0 ? newCategories : undefined,
                                      enabled_custom_tools: [...otherCategoryTools, ...toolsFromOtherCategories],
                                    };
                                  } else if (isDefaultState) {
                                    // Default state - user clicked to disable this tool
                                    // Switch to explicit mode: enable all other tools from this category (exclude this one)
                                    // This is correct - we're disabling the clicked tool and keeping all others enabled
                                    const otherCategoryTools = allCategoryTools.filter((t: string) => t !== toolName);
                                    console.log('[CHECKBOX_DEBUG] Disabling from default - keeping enabled:', otherCategoryTools);
                                    return {
                                      ...prev,
                                      enabled_custom_tools: otherCategoryTools,
                                    };
                                  } else {
                                    // Category not enabled, not default - remove this tool from current list
                                    // Simply filter out the tool being disabled from the current list
                                    const finalTools = current.filter((t: string) => t !== toolName);
                                    
                                    console.log('[CHECKBOX_DEBUG] Removing tool from explicit list:', {
                                      removed: toolName,
                                      before: current.length,
                                      after: finalTools.length,
                                      finalTools,
                                    });
                                    
                                    return {
                                      ...prev,
                                      enabled_custom_tools: finalTools.length > 0 ? finalTools : undefined,
                                    };
                                  }
                                }
                                return prev; // No change
                              });
                            }}
                            className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                          />
                          <span className="text-xs text-gray-600 dark:text-gray-400">{toolName}</span>
                        </label>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            {/* Large Output Virtual Tools Toggle */}
            <div className="border-t border-gray-200 dark:border-gray-700"></div>
            <div className="space-y-2">
              <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                Large Output Virtual Tools
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id={`large-output-${stepIndex}`}
                  checked={agentConfigs.enable_large_output_virtual_tools !== false}
                  onChange={(e) => {
                    setAgentConfigs((prev) => ({
                      ...prev,
                      enable_large_output_virtual_tools: e.target.checked,
                    }));
                  }}
                  className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                />
                <label
                  htmlFor={`large-output-${stepIndex}`}
                  className="text-xs text-gray-600 dark:text-gray-400 cursor-pointer flex-1"
                >
                  Enable Large Output Virtual Tools
                  <span className="text-gray-500 dark:text-gray-500 ml-1">
                    (read_large_output, search_large_output, query_large_output)
                  </span>
                </label>
              </div>
            </div>

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
    </div>
  );
};

