import type { TodoStep, AgentConfigs } from '../generated/events-bridge';

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
  newSteps: TodoStep[],
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

