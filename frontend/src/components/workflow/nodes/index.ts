import { StepNode } from './StepNode'
import { TodoTaskNode } from './TodoTaskNode'
import { HumanInputNode } from './HumanInputNode'
import { EvaluationNode } from './EvaluationNode'
import { RoutingStepNode } from './RoutingStepNode'
import { MessageSequenceNode } from './MessageSequenceNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'
import { WorkflowArtifactNode } from './WorkflowArtifactNode'

export { StepNode } from './StepNode'
export { TodoTaskNode } from './TodoTaskNode'
export { HumanInputNode } from './HumanInputNode'
export { EvaluationNode } from './EvaluationNode'
export { RoutingStepNode } from './RoutingStepNode'
export { MessageSequenceNode } from './MessageSequenceNode'
export { StartNode, EndNode } from './StartEndNodes'
export { VariablesNode } from './VariablesNode'
export { WorkflowArtifactNode } from './WorkflowArtifactNode'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  todo_task: TodoTaskNode,
  human_input: HumanInputNode,
  evaluation: EvaluationNode,
  routing: RoutingStepNode,
  message_sequence: MessageSequenceNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode,
  'workflow-artifact': WorkflowArtifactNode
} as const
