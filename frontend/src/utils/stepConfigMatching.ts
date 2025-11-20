import type { TodoStep } from '../generated/events-bridge';

// AgentLLMConfig represents LLM configuration for an agent
export interface AgentLLMConfig {
  provider?: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic';
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
  enabled_custom_tools?: string[]; // Format: "category:tool" or "category:*" (e.g., "workspace_tools:*", "human_tools:human_feedback")
  enable_large_output_virtual_tools?: boolean;
}

// Extended TodoStep with agent_configs
export interface TodoStepWithConfigs {
  id?: string;                        // Stable step ID (from backend, always present for new steps)
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
  // Conditional branching fields
  has_condition?: boolean;
  condition_question?: string;
  condition_context?: string;
  if_true_steps?: TodoStepWithConfigs[];
  if_false_steps?: TodoStepWithConfigs[];
  condition_result?: boolean;
  condition_reason?: string;
  agent_configs?: AgentConfigs;
}

// StepConfig represents a single step's configuration in step_config.json
export interface StepConfig {
  id: string;                 // Stable step ID (generated from title) - required identifier
  title?: string;             // Step title (optional, for reference/display only)
  agent_configs?: AgentConfigs;
}

// StepConfigFile represents the entire step_config.json file structure
export interface StepConfigFile {
  steps: StepConfig[];
}


/**
 * Matches new plan steps with existing configs by ID only
 * Returns a map of step index -> matched AgentConfigs
 */
export function matchStepConfigs(
  newSteps: TodoStep[] | TodoStepWithConfigs[],
  oldConfigs: StepConfigFile
): Map<number, AgentConfigs> {
  const result = new Map<number, AgentConfigs>();

  // Create lookup map: ID -> config
  const idConfigMap = new Map<string, AgentConfigs>();
  
  for (const stepConfig of oldConfigs.steps) {
    if (stepConfig.agent_configs !== undefined && stepConfig.id) {
      idConfigMap.set(stepConfig.id, stepConfig.agent_configs);
    }
  }

  // Match new steps to old configs by ID only
  // Steps always have IDs from backend - throw error if missing
  for (let i = 0; i < newSteps.length; i++) {
    const step = newSteps[i];
    
    // Match by ID - step.id is required (always provided by backend)
    const stepId = (step as TodoStepWithConfigs).id;
    if (!stepId) {
      throw new Error(`Step at index ${i} is missing required ID field. Step title: "${step.title || 'unknown'}"`);
    }
    
    const config = idConfigMap.get(stepId);
    if (config) {
      result.set(i, config);
    }
    // If not found, step won't have config (use defaults)
  }

  return result;
}

// PlanStep interface for plan.json (matches backend PlanStep structure)
export interface PlanStep {
  id: string;                        // Stable step ID (required, always provided by backend)
  title: string;
  description?: string;
  success_criteria?: string;
  why_this_step?: string;
  context_dependencies?: string[];
  context_output?: string | string[];
  learning_files_to_reference?: string[];
  has_loop?: boolean;
  loop_condition?: string;
  max_iterations?: number;
  loop_description?: string;
  has_condition?: boolean;
  condition_question?: string;
  condition_context?: string;
  if_true_steps?: PlanStep[];
  if_false_steps?: PlanStep[];
  condition_result?: boolean;
  condition_reason?: string;
  [key: string]: unknown;              // Allow other fields for flexibility
}

// PlanningResponse interface for plan.json
export interface PlanningResponse {
  steps: PlanStep[];
  run_mode?: string;
  [key: string]: unknown;             // Allow other fields for flexibility
}


