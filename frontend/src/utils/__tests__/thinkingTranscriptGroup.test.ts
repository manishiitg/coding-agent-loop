import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../../services/api-types'
import { buildTranscriptItems } from '../terminalEventTranscript'

function mk(id: string, type: string, data: Record<string, unknown>): PollingEvent {
  return {
    id, type, session_id: 's1', execution_kind: 'main_agent', execution_id: 'main:s1',
    timestamp: '2026-09-03T00:00:00Z', data: { data },
  } as unknown as PollingEvent
}

describe('thinking transcript grouping', () => {
  it('folds consecutive thinking events into one block and keeps the answer separate', () => {
    const items = buildTranscriptItems([
      mk('u1', 'user_message', { content: 'hi' }),
      mk('t1', 'conversation_thinking', { thinking: 'Reading soul.md first.', turn: 1 }),
      mk('t2', 'conversation_thinking', { thinking: 'Checking runs/.', turn: 1 }),
      mk('a1', 'llm_generation_end', { content: 'Hello. Here is the status.' }),
    ])
    expect(items.map(item => item.kind)).toEqual(['event', 'thinking', 'event'])
    const thinking = items[1] as Extract<ReturnType<typeof buildTranscriptItems>[number], { kind: 'thinking' }>
    expect(thinking.key).toBe('t1')
    expect(thinking.events.map(e => e.id)).toEqual(['t1', 't2'])
    expect(thinking.text).toBe('Reading soul.md first.\n\nChecking runs/.')
  })

  it('drops thinking events that carry no text', () => {
    const items = buildTranscriptItems([
      mk('t1', 'conversation_thinking', { thinking: '   ', turn: 1 }),
      mk('a1', 'llm_generation_end', { content: 'Hello.' }),
    ])
    expect(items.map(item => item.kind)).toEqual(['event'])
  })

  it('shows an explicit empty thinking activity only while it is newest', () => {
    const active = mk('t1', 'conversation_thinking', {
      thinking: '', turn: 1, metadata: { thinking_active: true },
    })
    const liveItems = buildTranscriptItems([mk('u1', 'user_message', { content: 'hi' }), active])
    expect(liveItems.map(item => item.kind)).toEqual(['event', 'thinking'])
    expect(liveItems[1]?.kind === 'thinking' && liveItems[1].text).toBe('')

    const completedItems = buildTranscriptItems([
      mk('u1', 'user_message', { content: 'hi' }),
      active,
      mk('a1', 'llm_generation_end', { content: 'Done.' }),
    ])
    expect(completedItems.map(item => item.kind)).toEqual(['event', 'event'])
  })

  it('does not let stale empty thinking markers split one tool batch', () => {
    const emptyThinking = (id: string) => mk(id, 'conversation_thinking', {
      thinking: '', turn: 1, metadata: { thinking_active: true },
    })
    const items = buildTranscriptItems([
      mk('t1-start', 'tool_call_start', { tool_call_id: 'call-1', tool_name: 'read_file' }),
      mk('t1-end', 'tool_call_end', { tool_call_id: 'call-1', tool_name: 'read_file' }),
      emptyThinking('thinking-1'),
      mk('t2-start', 'tool_call_start', { tool_call_id: 'call-2', tool_name: 'read_file' }),
      mk('t2-end', 'tool_call_end', { tool_call_id: 'call-2', tool_name: 'read_file' }),
      emptyThinking('thinking-2'),
      mk('answer', 'llm_generation_end', { content: 'Done.' }),
    ])

    expect(items.map(item => item.kind)).toEqual(['tools', 'event'])
    expect(items[0]?.kind === 'tools' && items[0].events).toHaveLength(4)
  })
})
