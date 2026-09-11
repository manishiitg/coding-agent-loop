import { describe, expect, it } from 'vitest'
import { buildTranscriptItems } from '../terminalEventTranscript'
import { buildCleanConversationItems } from '../cleanConversation'
import type { PollingEvent } from '../../services/api-types'

const event = (id: string, type: string, data: Record<string, unknown>) => ({ id, type, data: { data } } as PollingEvent)
const update = (id: string, thinking: string) => event(id, 'conversation_thinking', { thinking, metadata: { presentation: 'assistant_update' } })

describe('Muse assistant updates', () => {
  it('keeps updates separate from thinking and the final answer, including restored JSON', () => {
    const events = JSON.parse(JSON.stringify([
      update('u1', 'Checking the file.'),
      event('r1', 'conversation_thinking', { thinking: 'Reasoning from another provider.' }),
      update('u2', 'Found the issue.'),
      event('final', 'llm_generation_end', { content: 'Fixed.' }),
    ]))
    const transcript = buildTranscriptItems(events)
    expect(transcript.map(item => item.kind === 'thinking' ? item.assistantUpdate : false)).toEqual([true, false, true, false])
    const clean = buildCleanConversationItems(events)
    expect(clean.map(item => item.role)).toEqual(['assistant', 'reasoning', 'assistant', 'assistant'])
    expect(clean.map(item => item.content)).toEqual(['Checking the file.', 'Reasoning from another provider.', 'Found the issue.', 'Fixed.'])
  })
  it('does not append an intermediate delta to the previous final answer', () => {
    const items = buildCleanConversationItems([
      event('final', 'llm_generation_end', { content: 'Previous answer.' }),
      event('u', 'conversation_thinking', { thinking: 'Checking again.', is_delta: true, metadata: { presentation: 'assistant_update' } }),
    ])
    expect(items.map(item => item.content)).toEqual(['Previous answer.', 'Checking again.'])
  })
})
