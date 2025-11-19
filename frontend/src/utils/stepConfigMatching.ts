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
 * Ensures all steps in step_config.json have IDs
 * Generates IDs from step titles if not already present
 */
export function addIDsToStepConfigs(stepConfigFile: StepConfigFile): void {
  for (const stepConfig of stepConfigFile.steps) {
    if (!stepConfig.id && stepConfig.title) {
      stepConfig.id = generateStepId(stepConfig.title);
    }
  }
}

/**
 * Generates a stable ID from a step title
 * Uses a simple hash-like approach to create a consistent ID
 */
export function generateStepId(title: string): string {
  if (!title) {
    return '';
  }
  
  // Create a URL-friendly slug from the title
  const slug = title
    .toLowerCase()
    .trim()
    .replace(/[^\w\s-]/g, '') // Remove special characters
    .replace(/\s+/g, '-')      // Replace spaces with hyphens
    .replace(/-+/g, '-')       // Replace multiple hyphens with single
    .replace(/^-|-$/g, '');    // Remove leading/trailing hyphens
  
  // Add a simple hash to ensure uniqueness (first 8 chars of a hash)
  // This helps avoid collisions while keeping IDs readable
  let hash = 0;
  for (let i = 0; i < title.length; i++) {
    const char = title.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash; // Convert to 32-bit integer
  }
  const hashStr = Math.abs(hash).toString(36).substring(0, 8);
  
  return `${slug}-${hashStr}`;
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
  // Use step.id directly if available (from backend), otherwise generate from title
  for (let i = 0; i < newSteps.length; i++) {
    const step = newSteps[i];
    
    // Match by ID - use step.id directly if available
    // Check if step has id property (TodoStepWithConfigs has id, TodoStep might not)
    const stepId = (step as TodoStepWithConfigs).id;
    if (stepId) {
      const config = idConfigMap.get(stepId);
      if (config) {
        result.set(i, config);
      }
    } else if (step.title) {
      // Fallback: generate ID from title (should not happen if backend always provides IDs)
      const generatedId = generateStepId(step.title);
      const config = idConfigMap.get(generatedId);
      if (config) {
        result.set(i, config);
      }
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

/**
 * @deprecated This function is no longer needed - backend always provides IDs
 * Kept for backward compatibility with old plan.json files that might not have IDs
 * Recursively adds IDs to all steps in a plan if not already present
 */
export function addIDsToPlanSteps(steps: PlanStep[]): void {
  for (let i = 0; i < steps.length; i++) {
    // Add ID to top-level step if not already present
    if (!steps[i].id && steps[i].title) {
      steps[i].id = generateStepId(steps[i].title);
    }

    // Recursively add IDs to branch steps
    const ifTrueSteps = steps[i].if_true_steps;
    if (ifTrueSteps && ifTrueSteps.length > 0 && steps[i].title) {
      addIDsToBranchSteps(ifTrueSteps, steps[i].title, 'true');
    }
    const ifFalseSteps = steps[i].if_false_steps;
    if (ifFalseSteps && ifFalseSteps.length > 0 && steps[i].title) {
      addIDsToBranchSteps(ifFalseSteps, steps[i].title, 'false');
    }
  }
}

/**
 * @deprecated This function is no longer needed - backend always provides IDs
 * Recursively adds IDs to branch steps if not already present
 */
function addIDsToBranchSteps(steps: PlanStep[], parentTitle: string, branchType: 'true' | 'false'): void {
  for (let i = 0; i < steps.length; i++) {
    // Generate ID: parent-title + branch-type + nested-index + branch-title
    if (!steps[i].id && steps[i].title && parentTitle) {
      const idInput = `${parentTitle}-${branchType}-${i}-${steps[i].title}`;
      steps[i].id = generateStepId(idInput);
    }

    // Recursively add IDs to nested branch steps
    const ifTrueSteps = steps[i].if_true_steps;
    if (ifTrueSteps && ifTrueSteps.length > 0 && steps[i].title) {
      addIDsToBranchSteps(ifTrueSteps, steps[i].title, 'true');
    }
    const ifFalseSteps = steps[i].if_false_steps;
    if (ifFalseSteps && ifFalseSteps.length > 0 && steps[i].title) {
      addIDsToBranchSteps(ifFalseSteps, steps[i].title, 'false');
    }
  }
}

