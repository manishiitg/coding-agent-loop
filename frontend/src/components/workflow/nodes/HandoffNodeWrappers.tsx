import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { StepNode } from './StepNode'
import { TodoTaskNode } from './TodoTaskNode'
import { HumanInputNode } from './HumanInputNode'
import { RoutingStepNode } from './RoutingStepNode'
import { MessageSequenceNode } from './MessageSequenceNode'
import { CrewNode } from './CrewNode'
import type {
  StepNodeData,
  TodoTaskNodeData,
  HumanInputNodeData,
  RoutingStepNodeData,
  MessageSequenceNodeData,
  CrewStepNodeData,
} from '../hooks/usePlanToFlow'

// Route-to-route branches are laid out as lateral handoffs. These invisible
// handles let the edge enter and leave through the sides while all regular
// workflow edges continue using the existing top/bottom handles.
const HandoffTarget = () => (
  <Handle
    type="target"
    position={Position.Left}
    id="handoff"
    className="!h-2 !w-2 !border-0 !bg-transparent !opacity-0"
    style={{ left: '-4px', top: '50%' }}
  />
)

export const HandoffStepNode = memo((props: NodeProps) => (
  <>
    <StepNode data={props.data as StepNodeData} selected={props.selected} />
    <HandoffTarget />
  </>
))

export const HandoffTodoTaskNode = memo((props: NodeProps) => (
  <>
    <TodoTaskNode data={props.data as TodoTaskNodeData} selected={props.selected} />
    <HandoffTarget />
  </>
))

export const HandoffHumanInputNode = memo((props: NodeProps) => (
  <>
    <HumanInputNode data={props.data as HumanInputNodeData} selected={props.selected} />
    <HandoffTarget />
  </>
))

export const HandoffMessageSequenceNode = memo((props: NodeProps) => (
  <>
    <MessageSequenceNode data={props.data as MessageSequenceNodeData} selected={props.selected} />
    <HandoffTarget />
  </>
))

export const HandoffCrewNode = memo((props: NodeProps) => (
  <>
    <CrewNode data={props.data as CrewStepNodeData} selected={props.selected} />
    <HandoffTarget />
  </>
))

export const HandoffRoutingNode = memo((props: NodeProps) => {
  const data = props.data as RoutingStepNodeData

  return (
    <>
      <RoutingStepNode data={data} selected={props.selected} />
      <HandoffTarget />
      {data.routes?.map(route => (
        <Handle
          key={route.route_id}
          type="source"
          position={Position.Right}
          id={`handoff-${route.route_id}`}
          className="!h-2 !w-2 !border-0 !bg-transparent !opacity-0"
          style={{ right: '-4px', top: '50%' }}
        />
      ))}
    </>
  )
})

HandoffStepNode.displayName = 'HandoffStepNode'
HandoffTodoTaskNode.displayName = 'HandoffTodoTaskNode'
HandoffHumanInputNode.displayName = 'HandoffHumanInputNode'
HandoffMessageSequenceNode.displayName = 'HandoffMessageSequenceNode'
HandoffCrewNode.displayName = 'HandoffCrewNode'
HandoffRoutingNode.displayName = 'HandoffRoutingNode'
