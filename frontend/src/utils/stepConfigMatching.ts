import type { TodoStep } from '../generated/event-types';

// AgentLLMConfig represents LLM configuration for an agent
export interface AgentLLMConfig {
  provider?: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic' | 'azure';
  model_id?: string;
}

// AgentConfigs represents per-agent configuration for a step
export interface AgentConfigs {
  execution_llm?: AgentLLMConfig;
  validation_llm?: AgentLLMConfig;
  learning_llm?: AgentLLMConfig;
  conditional_llm?: AgentLLMConfig;
  execution_max_turns?: number;
  validation_max_turns?: number;
  learning_max_turns?: number;
  orchestration_max_iterations?: number;
  disable_validation?: boolean;
  llm_validation_mode?: string; // "skip" (default), "auto", or "always". Controls LLM validation behavior when pre-validation passes.
  disable_learning?: boolean;
  lock_learnings?: boolean;
  learning_after_loop_iteration?: boolean;
  learning_detail_level?: string;
  selected_servers?: string[];
  selected_tools?: string[];
  enabled_custom_tool_categories?: string[];
  enabled_custom_tools?: string[];
  enable_context_offloading?: boolean;
  use_code_execution_mode?: boolean;
  use_tool_search_mode?: boolean;
  pre_discovered_tools?: string[];
  enable_prerequisite_detection?: boolean;
  prerequisite_rules?: PrerequisiteRule[];
  keep_learning_full?: boolean;
  disable_temp_llm?: boolean;
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
  // Orchestration step fields (orchestrator with multiple sub-agents)
  has_orchestration_step?: boolean;
  orchestration_step?: TodoStepWithConfigs;
  orchestration_routes?: Array<{
    route_id: string;
    route_name: string;
    condition: string;
    sub_agent_step: TodoStepWithConfigs;
    context_to_pass?: string;
  }>;
  next_step_id?: string;
  // Human input step fields (asks question to human and blocks for input)
  has_human_input?: boolean;
  question?: string;
  variable_name?: string;
  response_type?: string; // "text" (default), "yesno", "multiple_choice"
  options?: string[];
  if_yes_next_step_id?: string; // For yesno type when response is "yes"
  if_no_next_step_id?: string; // For yesno type when response is "no"
  option_routes?: Record<string, string>; // For multiple_choice type - maps option index or value to next_step_id
  agent_configs?: AgentConfigs;
  validation_schema?: ValidationSchema; // Optional structured validation schema for step outputs
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

// Validation schema types
export interface ValidationSchema {
  files?: FileValidationRule[];
}

export interface FileValidationRule {
  file_name: string;
  must_exist: boolean;
  json_checks?: JSONValidationCheck[];
}

export interface JSONValidationCheck {
  path: string;                      // JSONPath, e.g., "$.status", "$.databases[0].name"
  must_exist: boolean;               // Key/path must exist
  value_type?: string;                // "string", "number", "boolean", "array", "object"
  min_length?: number;               // For arrays/strings
  max_length?: number;               // For arrays/strings
  pattern?: string;                   // Regex for format validation
  min_value?: number;                // For numbers
  max_value?: number;                // For numbers
  consistency_check?: ConsistencyRule; // Compare with other fields
}

export interface ConsistencyRule {
  type: string;                      // "equals", "greater_than", "less_than", "array_length", "in_array"
  compare_with_path: string;         // JSONPath to compare with
}

// Common fields shared by all step types
interface CommonStepFields {
  id: string;                        // Stable step ID (required, always provided by backend)
  title: string;
  description?: string;
  success_criteria?: string;
  context_dependencies?: string[];
  context_output?: string | string[];
  enable_prerequisite_detection?: boolean;
  prerequisite_rules?: PrerequisiteRule[];
  validation_schema?: ValidationSchema; // Optional structured validation schema for step outputs
  agent_configs?: AgentConfigs;       // Merged from step_config.json
}

// Regular step (may have loops)
export interface RegularPlanStep extends CommonStepFields {
  type: 'regular';
  has_loop?: boolean;
  loop_condition?: string;
  max_iterations?: number;
  loop_description?: string;
}

// Conditional step with branches
export interface ConditionalPlanStep extends CommonStepFields {
  type: 'conditional';
  condition_question?: string;
  condition_context?: string;
  if_true_steps?: PlanStep[];
  if_false_steps?: PlanStep[];
  if_true_next_step_id?: string;      // ID of step to connect to after true branch completes (or "end" to end workflow)
  if_false_next_step_id?: string;     // ID of step to connect to after false branch completes (or "end" to end workflow)
  condition_result?: boolean;
  condition_reason?: string;
}

// Decision step (execute step, evaluate output, route based on result)
export interface DecisionPlanStep extends CommonStepFields {
  type: 'decision';
  decision_step?: PlanStep;           // The single step to execute
  decision_evaluation_question?: string; // Question to evaluate step output
  if_true_next_step_id?: string;      // ID of step to connect to if decision is true (or "end")
  if_false_next_step_id?: string;     // ID of step to connect to if decision is false (or "end")
  decision_result?: boolean;          // runtime: stores evaluation result
  decision_reason?: string;           // runtime: stores evaluation reasoning
}

// Orchestration step (orchestrator with multiple sub-agents)
export interface OrchestrationPlanStep extends CommonStepFields {
  type: 'orchestration';
  orchestration_step?: PlanStep;            // The main orchestrator step to execute
  orchestration_routes?: PlanRoutingRoute[]; // Array of possible routes with conditions
  next_step_id?: string;              // ID of step after orchestration completes (or "end")
}

// Todo task step (orchestrator with todo list management + predefined routes + generic agent)
export interface TodoTaskPlanStep extends CommonStepFields {
  type: 'todo_task';
  todo_task_step?: PlanStep;                // The main orchestrator step metadata
  predefined_routes?: PlanRoutingRoute[];   // Predefined sub-agents with learning/prevalidation
  enable_generic_agent?: boolean;           // Allow generic execution agent (no learning/prevalidation)
  next_step_id?: string;                    // ID of step after todo task completes (or "end")
}

// Human input step (asks question to human and blocks for input)
export interface HumanInputPlanStep extends CommonStepFields {
  type: 'human_input';
  question: string;                      // Required: question to ask human
  variable_name?: string;                // Optional: store response in variable
  response_type?: string;                // "text" (default), "yesno", "multiple_choice"
  options?: string[];                    // For multiple_choice type
  next_step_id?: string;                 // Default: where to go after response (or "end") - used if conditional routing not specified
  if_yes_next_step_id?: string;         // Optional: for yesno type when response is "yes"
  if_no_next_step_id?: string;          // Optional: for yesno type when response is "no"
  option_routes?: Record<string, string>; // Optional: for multiple_choice type - maps option index (as string "0", "1", etc.) or option value to next_step_id
}

// Discriminated union type for all step types
export type PlanStep = RegularPlanStep | ConditionalPlanStep | DecisionPlanStep | OrchestrationPlanStep | HumanInputPlanStep | TodoTaskPlanStep;

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

// Type guard functions for discriminated union
export function isRegularStep(step: PlanStep): step is RegularPlanStep {
  return step.type === 'regular';
}

export function isConditionalStep(step: PlanStep): step is ConditionalPlanStep {
  return step.type === 'conditional';
}

export function isDecisionStep(step: PlanStep): step is DecisionPlanStep {
  return step.type === 'decision';
}

export function isOrchestrationStep(step: PlanStep): step is OrchestrationPlanStep {
  return step.type === 'orchestration';
}

export function isHumanInputStep(step: PlanStep): step is HumanInputPlanStep {
  return step.type === 'human_input';
}

export function isTodoTaskStep(step: PlanStep): step is TodoTaskPlanStep {
  return step.type === 'todo_task';
}


