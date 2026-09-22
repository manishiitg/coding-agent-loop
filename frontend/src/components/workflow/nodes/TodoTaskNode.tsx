import { memo } from 'react'
import type { TodoTaskNodeData, MessageSequenceNodeData } from '../hooks/usePlanToFlow'
import { legacyTodoMessagesAsSequenceItems, isTodoTaskStep } from '../../../utils/stepConfigMatching'
import { MessageSequenceNode } from './MessageSequenceNode'

interface TodoTaskNodeProps {
  data: TodoTaskNodeData
  selected?: boolean
}

// Compatibility adapter for stale saved canvas data. New Plan Design graphs no
// longer create `todo_task` nodes; both former orchestrators and authored
// message sequences are emitted as the single `message_sequence` Agent type.
export const TodoTaskNode = memo(({ data, selected }: TodoTaskNodeProps) => {
  const step = data.step
  const agentData: MessageSequenceNodeData = {
    ...data,
    description: step.description,
    items: isTodoTaskStep(step) ? legacyTodoMessagesAsSequenceItems(step) : []
  }

  return <MessageSequenceNode data={agentData} selected={selected} />
})

TodoTaskNode.displayName = 'TodoTaskNode'
export default TodoTaskNode
