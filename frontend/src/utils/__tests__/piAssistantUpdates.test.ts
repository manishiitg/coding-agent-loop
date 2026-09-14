import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../../services/api-types'
import { buildCleanConversationItems } from '../cleanConversation'
import { buildTranscriptItems } from '../terminalEventTranscript'

const event = (id: string, type: string, data: Record<string, unknown>) => ({ id, type, data: { data } } as PollingEvent)

describe('Pi assistant updates', () => {
  it('renders stored Pi progress as an assistant update instead of reasoning', () => {
    const events = [
      event('pi-progress', 'conversation_thinking', {
        thinking: 'Checking the latest webhook delivery.',
        metadata: { provider: 'pi-cli' },
      }),
      event('final', 'llm_generation_end', { content: 'The webhook is arriving.' }),
    ]

    const transcript = buildTranscriptItems(events)
    expect(transcript.map(item => item.kind === 'thinking' ? item.assistantUpdate : false)).toEqual([true, false])

    const clean = buildCleanConversationItems(events)
    expect(clean.map(item => item.role)).toEqual(['assistant', 'assistant'])
    expect(clean.map(item => item.content)).toEqual([
      'Checking the latest webhook delivery.',
      'The webhook is arriving.',
    ])
  })

  it('keeps non-Pi reasoning in the Thinking presentation', () => {
    const transcript = buildTranscriptItems([
      event('reasoning', 'conversation_thinking', {
        thinking: 'Private reasoning from another provider.',
        metadata: { provider: 'other-cli' },
      }),
    ])

    expect(transcript).toHaveLength(1)
    expect(transcript[0]?.kind).toBe('thinking')
    expect(transcript[0]?.kind === 'thinking' && transcript[0].assistantUpdate).toBe(false)
  })
})
