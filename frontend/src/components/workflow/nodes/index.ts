import { StepNode } from './StepNode'
import { ConditionalNode } from './ConditionalNode'
import { DecisionNode } from './DecisionNode'
import { OrchestratorNode } from './RoutingNode'
import { TodoTaskNode } from './TodoTaskNode'
import { LoopNode } from './LoopNode'
import { HumanInputNode } from './HumanInputNode'
import { EvaluationNode } from './EvaluationNode'
import { RoutingStepNode } from './RoutingStepNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'
import { ExecutionSettingsNode } from './ExecutionSettingsNode'

export { StepNode } from './StepNode'
export { ConditionalNode } from './ConditionalNode'
export { DecisionNode } from './DecisionNode'
export { OrchestratorNode } from './RoutingNode'
export { TodoTaskNode } from './TodoTaskNode'
export { LoopNode } from './LoopNode'
export { HumanInputNode } from './HumanInputNode'
export { EvaluationNode } from './EvaluationNode'
export { RoutingStepNode } from './RoutingStepNode'
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
  todo_task: TodoTaskNode,
  human_input: HumanInputNode,
  loop: LoopNode,
  evaluation: EvaluationNode,
  routing: RoutingStepNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode,
  'execution-settings': ExecutionSettingsNode
} as const
