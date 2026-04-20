import type { TodoStep } from '../generated/event-types';
import type { LLMProvider } from '../services/api-types';

// AgentLLMConfig represents LLM configuration for an agent
export interface AgentLLMConfig {
  provider?: LLMProvider;
  model_id?: string;
}

// AgentConfigs represents per-agent configuration for a step
export interface AgentConfigs {
  execution_llm?: AgentLLMConfig;
  learning_llm?: AgentLLMConfig;
  conditional_llm?: AgentLLMConfig;
  execution_max_turns?: number;
  validation_max_turns?: number;
  learning_max_turns?: number;
  orchestration_max_iterations?: number;
  lock_learnings?: boolean;
  lock_code?: boolean;                        // Freeze main.py against LLM rewrites; independent of lock_learnings
  // DEPRECATED: backend removed these fields (replaced by learnings_access).
  // Kept on the interface only so legacy node-display code compiles; the backend
  // never reads them. New frontend code should consult learnings_access directly.
  disable_learning?: boolean;
  learning_detail_level?: string;
  learning_after_loop_iteration?: boolean;
  keep_learning_full?: boolean;
  // Primary gate for learnings_/_global/ access. "read" (default when unset) —
  // step sees global SKILL.md in its prompt; "read-write" — also contributes
  // (requires non-empty learning_objective); "none" — no read, no write.
  // Mirrors knowledgebase_access. Auto-inferred to "read-write" on save when
  // learning_objective is non-empty and this field is unset (backend migration).
  learnings_access?: 'read' | 'read-write' | 'none';
  learning_objective?: string;                // Extraction instruction for the post-step learning agent; required when learnings_access="read-write"
  selected_servers?: string[];
  selected_tools?: string[];
  enabled_custom_tools?: string[];
  enabled_custom_tool_categories?: string[];  // Legacy format kept for transitional frontend compatibility
  enabled_skills?: string[];
  enable_context_offloading?: boolean;
  use_code_execution_mode?: boolean;
  use_tool_search_mode?: boolean;             // Legacy field still cleaned up by older editor flows
  pre_discovered_tools?: string[];            // Legacy field still cleaned up by older editor flows
  todo_task_orchestrator_tier?: number;       // 1/2/3 - tier for orchestrator agent in tiered mode
  orchestrator_llm?: AgentLLMConfig;          // Direct LLM override for orchestrator (works in both tiered and manual modes)
  sub_agent_llm?: AgentLLMConfig;             // Direct LLM override for ALL sub-agents spawned by this step (works in both tiered and manual modes)
  disable_parallel_tool_execution?: boolean;  // Disable parallel tool execution (default: enabled)
  disable_tier_optimization?: boolean;        // If true, execution/conditional agents always use Tier 1 (high reasoning)
  global_skill_objective?: string;            // Objective for the global skill — what domain knowledge should it capture
  knowledgebase_access?: 'read' | 'write' | 'read-write' | 'none';
  knowledgebase_contribution?: string;
  optimized?: boolean;                        // UI/reporting flag set alongside lock_learnings after auto-lock; cleared on auto-unlock
  description_reviewed?: boolean;
  review_notes?: string;
  declared_execution_mode?: string;           // "learn_code" | "code_exec" | "tool_calling" — authoring hint; resolution in backend
  declared_execution_mode_reason?: string;
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

export interface StepSharing {
  orchestrator_ids?: string[];
}

// Common fields shared by all step types
interface CommonStepFields {
  id: string;                        // Stable step ID (required, always provided by backend)
  title: string;
  description?: string;
  success_criteria?: string;
  context_dependencies?: string[];
  context_output?: string | string[];
  validation_schema?: ValidationSchema; // Optional structured validation schema for step outputs
  agent_configs?: AgentConfigs;       // Merged from step_config.json
  shared_with?: StepSharing;
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

// Todo task step (orchestrator with todo list management + predefined routes + generic agent)
// Fields from the former inner todo_task_step are now flattened onto this level:
//   description, success_criteria, context_dependencies, context_output, validation_schema
// These are inherited from CommonStepFields already.
export interface TodoTaskPlanStep extends CommonStepFields {
  type: 'todo_task';
  todo_task_step?: PlanStep;                // DEPRECATED: kept for backwards compat with old plan.json
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

// Routing route for routing steps
export interface RoutingRoute {
  route_id: string;
  route_name: string;
  condition: string;
  next_step_id: string;
}

// Routing step (N-way LLM-based routing)
export interface RoutingPlanStep extends CommonStepFields {
  type: 'routing';
  routing_question: string;           // Question to evaluate for route selection
  routes: RoutingRoute[];             // Available routes (min 2)
  default_route_id?: string;          // Optional fallback route_id
  selected_route_id?: string;         // runtime: stores selected route
}

// Discriminated union type for all step types
export type PlanStep = RegularPlanStep | ConditionalPlanStep | HumanInputPlanStep | TodoTaskPlanStep | RoutingPlanStep;

// PlanRoutingRoute represents a possible route/sub-agent for planning
export interface PlanRoutingRoute {
  route_id: string;                   // Unique ID for this route
  route_name: string;                 // Human-readable name
  condition: string;                  // Condition description
  sub_agent_step?: PlanStep;           // The sub-agent step to execute
  orphan_step_ref?: string;           // Optional reference to a reusable orphan step in the same plan
  context_to_pass?: string;           // Optional: specific context to pass to sub-agent
}

// PlanningResponse interface for plan.json
export interface PlanningResponse {
  steps: PlanStep[];
  orphan_steps?: PlanStep[];
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

export function isHumanInputStep(step: PlanStep): step is HumanInputPlanStep {
  return step.type === 'human_input';
}

export function isTodoTaskStep(step: PlanStep): step is TodoTaskPlanStep {
  return step.type === 'todo_task';
}

export function isRoutingStep(step: PlanStep): step is RoutingPlanStep {
  return step.type === 'routing';
}
