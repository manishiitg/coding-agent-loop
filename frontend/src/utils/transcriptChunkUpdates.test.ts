import { buildTranscriptItems } from '../../shared/session/transcript/terminalEventTranscript'
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
