import { StepNode } from './StepNode'
import { ConditionalNode } from './ConditionalNode'
import { LoopNode } from './LoopNode'
import { ValidationNode } from './ValidationNode'
import { LearningNode } from './LearningNode'
import { StartNode, EndNode } from './StartEndNodes'

export { StepNode } from './StepNode'
export { ConditionalNode } from './ConditionalNode'
export { LoopNode } from './LoopNode'
export { ValidationNode } from './ValidationNode'
export { LearningNode } from './LearningNode'
export { StartNode, EndNode } from './StartEndNodes'
export { NodeConfigFooter } from './NodeConfigFooter'

// Node types map for React Flow
export const nodeTypes = {
  step: StepNode,
  conditional: ConditionalNode,
  loop: LoopNode,
  validation: ValidationNode,
  learning: LearningNode,
  start: StartNode,
  end: EndNode
} as const
