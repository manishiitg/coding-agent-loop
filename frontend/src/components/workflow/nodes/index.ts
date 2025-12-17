import { StepNode } from './StepNode'
import { ConditionalNode } from './ConditionalNode'
import { DecisionNode } from './DecisionNode'
import { RoutingNode } from './RoutingNode'
import { LoopNode } from './LoopNode'
import { ValidationNode } from './ValidationNode'
import { LearningNode } from './LearningNode'
import { EvaluationNode } from './EvaluationNode'
import { StartNode, EndNode } from './StartEndNodes'
import { VariablesNode } from './VariablesNode'

export { StepNode } from './StepNode'
export { ConditionalNode } from './ConditionalNode'
export { DecisionNode } from './DecisionNode'
export { RoutingNode } from './RoutingNode'
export { LoopNode } from './LoopNode'
export { ValidationNode } from './ValidationNode'
export { LearningNode } from './LearningNode'
export { EvaluationNode } from './EvaluationNode'
export { StartNode, EndNode } from './StartEndNodes'
export { VariablesNode } from './VariablesNode'
export { NodeConfigFooter } from './NodeConfigFooter'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  conditional: ConditionalNode,
  decision: DecisionNode,
  routing: RoutingNode,
  loop: LoopNode,
  validation: ValidationNode,
  learning: LearningNode,
  evaluation: EvaluationNode,
  start: StartNode,
  end: EndNode,
  variables: VariablesNode
} as const
