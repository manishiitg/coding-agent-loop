import type { TodoStep } from '../generated/events-bridge';

// AgentLLMConfig represents LLM configuration for an agent
export interface AgentLLMConfig {
  provider?: 'openai' | 'bedrock' | 'openrouter' | 'vertex';
  model_id?: string;
}

// AgentConfigs represents per-agent configuration for a step
export interface AgentConfigs {
  execution_llm?: AgentLLMConfig;
  validation_llm?: AgentLLMConfig;
  learning_llm?: AgentLLMConfig;
  execution_max_turns?: number;
  validation_max_turns?: number;
  learning_max_turns?: number;
  disable_validation?: boolean;
  disable_learning?: boolean;
  learning_after_loop_iteration?: boolean;
  learning_detail_level?: 'exact' | 'general' | 'none';
  selected_servers?: string[];
  selected_tools?: string[];
  enabled_custom_tool_categories?: string[];
  enabled_custom_tools?: string[];
  enable_large_output_virtual_tools?: boolean;
}

// Extended TodoStep with agent_configs
export interface TodoStepWithConfigs {
  title?: string;
  description?: string;
  success_criteria?: string;
  why_this_step?: string;
  context_dependencies?: string[];
  context_output?: string;
  success_patterns?: string[];
  failure_patterns?: string[];
  has_loop?: boolean;
  loop_condition?: string;
  max_iterations?: number;
  loop_description?: string;
  agent_configs?: AgentConfigs;
}

// StepConfig represents a single step's configuration in step_config.json
export interface StepConfig {
  index: number;              // Step index (0-based) - primary key for matching
  title?: string;             // Step title (optional, for reference/display only)
  agent_configs?: AgentConfigs;
}

// StepConfigFile represents the entire step_config.json file structure
export interface StepConfigFile {
  steps: StepConfig[];
}

/**
 * Matches new plan steps with existing configs by step index
 * Returns a map of step index -> matched AgentConfigs
 */
export function matchStepConfigs(
  newSteps: TodoStep[] | TodoStepWithConfigs[],
  oldConfigs: StepConfigFile
): Map<number, AgentConfigs> {
  const result = new Map<number, AgentConfigs>();

  // Create a lookup map from old config indices to configs
  const oldConfigMap = new Map<number, AgentConfigs>();
  for (const stepConfig of oldConfigs.steps) {
    if (stepConfig.agent_configs !== undefined) {
      oldConfigMap.set(stepConfig.index, stepConfig.agent_configs);
    }
  }

  // Match new steps to old configs by index (0-based)
  for (let i = 0; i < newSteps.length; i++) {
    const config = oldConfigMap.get(i);
    if (config) {
      result.set(i, config);
    }
    // If not found, step won't have config (use defaults)
  }

  return result;
}

