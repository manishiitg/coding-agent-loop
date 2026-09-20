import { StepNode } from './StepNode'
import { TodoTaskNode } from './TodoTaskNode'
import { HumanInputNode } from './HumanInputNode'
import { RoutingStepNode } from './RoutingStepNode'
import { MessageSequenceNode } from './MessageSequenceNode'
import { CrewNode } from './CrewNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'
import { WorkflowArtifactNode } from './WorkflowArtifactNode'

export { StepNode } from './StepNode'
export { TodoTaskNode } from './TodoTaskNode'
export { HumanInputNode } from './HumanInputNode'
export { RoutingStepNode } from './RoutingStepNode'
export { MessageSequenceNode } from './MessageSequenceNode'
export { CrewNode } from './CrewNode'
export { StartNode, EndNode } from './StartEndNodes'
export { VariablesNode } from './VariablesNode'
export { WorkflowArtifactNode } from './WorkflowArtifactNode'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  todo_task: TodoTaskNode,
  human_input: HumanInputNode,
  routing: RoutingStepNode,
  // Branch shares the mechanics but RoutingStepNode renders its own branch
  // icon and label, keeping the semantic distinction visible on the canvas.
  branch: RoutingStepNode,
  message_sequence: MessageSequenceNode,
  crew: CrewNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode,
  'workflow-artifact': WorkflowArtifactNode
} as const
