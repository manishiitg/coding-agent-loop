import { StepNode } from './StepNode'
import { ConditionalNode } from './ConditionalNode'
import { TodoTaskNode } from './TodoTaskNode'
import { HumanInputNode } from './HumanInputNode'
import { EvaluationNode } from './EvaluationNode'
import { RoutingStepNode } from './RoutingStepNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'
import { WorkflowArtifactNode } from './WorkflowArtifactNode'

export { StepNode } from './StepNode'
export { ConditionalNode } from './ConditionalNode'
export { TodoTaskNode } from './TodoTaskNode'
export { HumanInputNode } from './HumanInputNode'
export { EvaluationNode } from './EvaluationNode'
export { RoutingStepNode } from './RoutingStepNode'
export { StartNode, EndNode } from './StartEndNodes'
export { VariablesNode } from './VariablesNode'
export { WorkflowArtifactNode } from './WorkflowArtifactNode'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  conditional: ConditionalNode,
  todo_task: TodoTaskNode,
  human_input: HumanInputNode,
  evaluation: EvaluationNode,
  routing: RoutingStepNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode,
  'workflow-artifact': WorkflowArtifactNode
} as const
