import { StepNode } from './StepNode'
import { ConditionalNode } from './ConditionalNode'
import { DecisionNode } from './DecisionNode'
import { OrchestratorNode } from './RoutingNode'
import { LoopNode } from './LoopNode'
import { HumanInputNode } from './HumanInputNode'
import { ValidationNode } from './ValidationNode'
import { LearningNode } from './LearningNode'
import { EvaluationNode } from './EvaluationNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'
import { ExecutionSettingsNode } from './ExecutionSettingsNode'

export { StepNode } from './StepNode'
export { ConditionalNode } from './ConditionalNode'
export { DecisionNode } from './DecisionNode'
export { OrchestratorNode } from './RoutingNode'
export { LoopNode } from './LoopNode'
export { HumanInputNode } from './HumanInputNode'
export { ValidationNode } from './ValidationNode'
export { LearningNode } from './LearningNode'
export { EvaluationNode } from './EvaluationNode'
export { StartNode, EndNode } from './StartEndNodes'
export { VariablesNode } from './VariablesNode'
export { ExecutionSettingsNode } from './ExecutionSettingsNode'
export { NodeConfigFooter } from './NodeConfigFooter'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  conditional: ConditionalNode,
  decision: DecisionNode,
  orchestrator: OrchestratorNode,
  human_input: HumanInputNode,
  loop: LoopNode,
  validation: ValidationNode,
  learning: LearningNode,
  evaluation: EvaluationNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode,
  'execution-settings': ExecutionSettingsNode
} as const
