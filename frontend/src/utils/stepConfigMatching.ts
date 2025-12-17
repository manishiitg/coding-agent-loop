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
  conditional_llm?: AgentLLMConfig; // Step-specific conditional LLM for conditional step evaluation
  execution_max_turns?: number;
  validation_max_turns?: number;
  learning_max_turns?: number;
  disable_validation?: boolean;
  disable_learning?: boolean;
  lock_learnings?: boolean; // If true, prevents learning agent from running but still uses existing learnings
  learning_after_loop_iteration?: boolean;
  learning_detail_level?: 'exact' | 'general';
  selected_servers?: string[];
  selected_tools?: string[];
  enabled_custom_tools?: string[]; // Format: "category:tool" or "category:*" (e.g., "workspace_tools:*", "human_tools:human_feedback")
  enable_large_output_virtual_tools?: boolean;
  use_code_execution_mode?: boolean; // Step-level code execution mode override (undefined = use preset default)
  enable_prerequisite_detection?: boolean; // Enable prerequisite failure detection for this step (default: false)
  prerequisite_rules?: PrerequisiteRule[]; // Array of prerequisite rules, each with one step dependency and one description
}

// PrerequisiteRule represents a single prerequisite rule with one step dependency and one description
export interface PrerequisiteRule {
  depends_on_step: string; // Step ID this rule depends on
  description: string; // User description of when to detect prerequisite failures for this specific step (e.g., "if login session is missing or expired, go back to step 0")
}

// Extended TodoStep with agent_configs
export interface TodoStepWithConfigs {
  id?: string;                        // Stable step ID (from backend, always present for new steps)
  title?: string;
  description?: string;
  success_criteria?: string;
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
  if_true_next_step_id?: string;
  if_false_next_step_id?: string;
  condition_result?: boolean;
  condition_reason?: string;
  // Decision step fields (execute step, evaluate output, route based on result)
  has_decision_step?: boolean;
  decision_step?: TodoStepWithConfigs;
  decision_evaluation_question?: string;
  decision_result?: boolean;
  decision_reason?: string;
  agent_configs?: AgentConfigs;
}

// StepConfig represents a single step's configuration in step_config.json
export interface StepConfig {
  id: string;                 // Stable step ID (generated from title) - required identifier
  title?: string;             // Step title (optional, for reference/display only)
  agent_configs?: AgentConfigs;
}

/**
 * Matches new plan steps with existing configs by ID only
 * Returns a map of step index -> matched AgentConfigs
 */
export function matchStepConfigs(
  newSteps: TodoStep[] | TodoStepWithConfigs[],
  oldConfigs: StepConfig[]
): Map<number, AgentConfigs> {
  const result = new Map<number, AgentConfigs>();

  // Create lookup map: ID -> config
  const idConfigMap = new Map<string, AgentConfigs>();
  
  for (const stepConfig of oldConfigs) {
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
  context_dependencies?: string[];
  context_output?: string | string[];
  has_loop?: boolean;
  loop_condition?: string;
  max_iterations?: number;
  loop_description?: string;
  has_condition?: boolean;
  condition_question?: string;
  condition_context?: string;
  if_true_steps?: PlanStep[];
  if_false_steps?: PlanStep[];
  if_true_next_step_id?: string;      // ID of step to connect to after true branch completes (or "end" to end workflow)
  if_false_next_step_id?: string;     // ID of step to connect to after false branch completes (or "end" to end workflow)
  condition_result?: boolean;
  condition_reason?: string;
  // Decision step fields (execute step, evaluate output, route based on result)
  has_decision_step?: boolean;
  decision_step?: PlanStep;           // The single step to execute
  decision_evaluation_question?: string; // Question to evaluate step output
  decision_result?: boolean;          // runtime: stores evaluation result
  decision_reason?: string;           // runtime: stores evaluation reasoning
  // Orchestration step fields (orchestrator with multiple sub-agents)
  has_orchestration_step?: boolean;
  orchestration_step?: PlanStep;            // The main orchestrator step to execute
  orchestration_routes?: PlanRoutingRoute[]; // Array of possible routes with conditions
  next_step_id?: string;              // ID of step after orchestration completes (or "end")
  agent_configs?: AgentConfigs;       // Merged from step_config.json
  [key: string]: unknown;              // Allow other fields for flexibility
}

// PlanRoutingRoute represents a possible route/sub-agent for planning
export interface PlanRoutingRoute {
  route_id: string;                   // Unique ID for this route
  route_name: string;                 // Human-readable name
  condition: string;                  // Condition description
  sub_agent_step: PlanStep;            // The sub-agent step to execute
  context_to_pass?: string;           // Optional: specific context to pass to sub-agent
}

// PlanningResponse interface for plan.json
export interface PlanningResponse {
  steps: PlanStep[];
  run_mode?: string;
  [key: string]: unknown;             // Allow other fields for flexibility
}


