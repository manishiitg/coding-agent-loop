import { buildTranscriptItems, selectTerminalEvents } from '../../shared/session/transcript/terminalEventTranscript'
import { conversationToRestoredEvents } from '../../shared/session/restore'
import { buildCleanConversationItems } from './cleanConversation'
import { describe, expect, it } from 'vitest'
import { intermediateUpdateFromTranscriptChunk } from './transcriptChunkUpdates'
import type { PollingEvent } from '../../shared/session/types'

function chunk(inner: Record<string, unknown>, id = 'sess_streaming_chunk_1'): PollingEvent {
  return {
    id,
    type: 'streaming_chunk',
    timestamp: '2026-09-04T10:43:09.518Z',
    session_id: 'sess',
    data: { type: 'streaming_chunk', data: { chunk_index: 2, ...inner } },
  } as unknown as PollingEvent
}

describe('intermediateUpdateFromTranscriptChunk', () => {
  it('turns a whole-message transcript chunk into an intermediate reply row', () => {
    const update = intermediateUpdateFromTranscriptChunk(chunk({
      content: 'I’ll separate original posts from replies and count both by day.',
      source: 'transcript',
      is_delta: false,
      is_tool_call: false,
    }))
    expect(update).not.toBeNull()
    expect(update!.type).toBe('llm_generation_end')
    expect(update!.id).toBe('sess_streaming_chunk_1-update')
    const inner = (update!.data as { data: Record<string, unknown> }).data
    expect(inner.content).toBe('I’ll separate original posts from replies and count both by day.')
    expect(inner.restored_intermediate_update).toBe(true)
  })

  it('ignores token deltas, terminal frames, tool markers and empty chunks', () => {
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: 'I’l', source: 'transcript', is_delta: true }))).toBeNull()
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: 'screen', source: 'terminal', is_delta: false }))).toBeNull()
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: 'screen', source: 'transcript', is_delta: false, metadata: { kind: 'terminal', replace: true } }))).toBeNull()
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: 'exec', source: 'transcript', is_delta: false, is_tool_call: true }))).toBeNull()
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: '   ', source: 'transcript', is_delta: false }))).toBeNull()
    expect(intermediateUpdateFromTranscriptChunk(chunk({ content: 'hi' }))).toBeNull()
  })
})

// The shared renderer is used by both AgentWorks and product chat surfaces.
it('keeps both SSE narration rows across a follow-up and final completion', () => {
  const first = intermediateUpdateFromTranscriptChunk(chunk({ content: 'Checking which Simulator checks we cover.', source: 'transcript' }, 'first'))!
  const second = intermediateUpdateFromTranscriptChunk(chunk({ content: 'Checking the remaining Simulator points.', source: 'transcript' }, 'second'))!
  const followup = { id: 'steer', type: 'user_message', data: { data: { content: 'all 20 points' } } } as PollingEvent
  const final = { id: 'done', type: 'unified_completion', data: { data: { final_result: 'Here is the Simulator coverage.', status: 'completed' } } } as PollingEvent
  const visible = (events: PollingEvent[]) => buildTranscriptItems(events).flatMap(item => item.kind === 'event' ? [item.event.id] : [])
  expect(visible([first])).toContain('first-update')
  expect(visible([first, followup, second])).toEqual(['first-update', 'steer', 'second-update'])
  expect(visible([first, followup, second, final])).toEqual(['first-update', 'steer', 'second-update', 'done'])
})

describe('CLI narration across chat delivery paths', () => {
  const finalText = 'Fixed and validated all tabs.'
  const fixture = (): PollingEvent[] => [
    { id: 'user', type: 'user_message', data: { data: { content: 'Add a dedicated tab.' } } } as PollingEvent,
    chunk({ content: 'All the helpers exist. Adding the tab.', source: 'transcript' }, 'first'),
    { id: 'tool', type: 'tool_call_start', data: { data: { tool_name: 'api-bridge', tool_call_id: 'call' } } } as PollingEvent,
    chunk({ content: 'Now wiring the render function.', source: 'transcript' }, 'second'),
    chunk({ content: 'Now validating and previewing.', source: 'transcript' }, 'third'),
    chunk({ content: 'raw terminal screen', source: 'terminal' }, 'frame'),
    chunk({ content: 'partial', source: 'content', is_delta: true }, 'delta'),
    chunk({ content: finalText, source: 'transcript' }, 'answer-chunk'),
    { id: 'final', type: 'unified_completion', data: { data: { final_result: finalText, status: 'completed' } } } as PollingEvent,
  ].map((event, index) => ({ ...event, session_id: 'sess', event_index: index, timestamp: new Date(Date.UTC(2026, 8, 12, 12, 0, index)).toISOString() }))

  it.each(['claude-code', 'codex-cli', 'cursor-cli', 'muse-cli', 'pi-cli'])('renders raw %s transcript messages in order, with the final answer once', provider => {
    const events = fixture().map(event => ({ ...event, data: { ...event.data, provider } })) as PollingEvent[]
    const visible = buildTranscriptItems(selectTerminalEvents(events, null))
      .flatMap(item => item.kind === 'event' ? [item.event.id] : [])
    expect(visible).toEqual(['user', 'first-update', 'second-update', 'third-update', 'final'])
    expect(buildCleanConversationItems(events).map(item => item.content)).toEqual([
      'Add a dedicated tab.', 'All the helpers exist. Adding the tab.',
      'Now wiring the render function.', 'Now validating and previewing.', finalText,
    ])
  })

  it('restores narration from the saved UI trace when conversation history only has the final answer', () => {
    const events = conversationToRestoredEvents({
      session_id: 'sess',
      conversation_history: [
        { role: 'human', parts: [{ type: 'text', content: 'Add a dedicated tab.' }] },
        { role: 'ai', parts: [{ type: 'text', content: finalText }] },
      ],
      ui_events: JSON.parse(JSON.stringify(fixture())),
    })
    const transcript = buildTranscriptItems(selectTerminalEvents(events, null))
    const visible = transcript.flatMap(item => item.kind === 'event' ? [item.event.id] : [])
    expect(visible).toContain('first-update')
    expect(visible).toContain('second-update')
    expect(visible).toContain('third-update')
    expect(visible).not.toContain('answer-chunk-update')
    expect(visible).not.toContain('frame')
    expect(visible).not.toContain('delta')
    const rendered = transcript.flatMap(item => item.kind === 'event' && item.event.type !== 'conversation_resumed' ? [item.event] : [])
    expect(rendered.map(event => event.type)).toEqual([
      'user_message', 'llm_generation_end', 'llm_generation_end', 'llm_generation_end', 'unified_completion',
    ])
    expect(rendered.slice(1, -1).map(event => event.id)).toEqual(['first-update', 'second-update', 'third-update'])
  })

  it('keeps child narration out of main chat while preserving its ownership', () => {
    const child = { ...fixture()[1], execution_kind: 'delegated', execution_id: 'child' } as PollingEvent
    expect(intermediateUpdateFromTranscriptChunk(child)?.execution_id).toBe('child')
    expect(selectTerminalEvents([child], null)).toEqual([])
    expect(buildCleanConversationItems([child])).toEqual([])
  })
})
