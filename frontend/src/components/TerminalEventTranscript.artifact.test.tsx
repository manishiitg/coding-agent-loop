// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PollingEvent } from '../services/api-types'

const getChatArtifact = vi.hoisted(() => vi.fn())
vi.mock('../services/api', () => ({ agentApi: { getChatArtifact }, getApiBaseUrl: () => '', getAuthToken: () => null }))
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({ data, firstItemIndex, itemContent }: { data: unknown[]; firstItemIndex: number; itemContent: (index: number, item: unknown) => React.ReactNode }) => (
    <div>{data.map((item, index) => <div key={index}>{itemContent(firstItemIndex + index, item)}</div>)}</div>
  ),
}))
vi.mock('./events/EventDispatcher', () => ({ EventDispatcher: () => null }))
vi.mock('./ui/MarkdownRenderer', () => ({ ConversationMarkdownRenderer: ({ content }: { content: string }) => <div data-testid="md">{content}</div> }))

import { TerminalEventTranscript } from './TerminalEventTranscript'

const cleanups: Array<() => void> = []
afterEach(() => { cleanups.splice(0).forEach(cleanup => cleanup()); vi.unstubAllGlobals(); getChatArtifact.mockReset() })

function event(id: string, type: string, data: Record<string, unknown>): PollingEvent {
  return { id, type, session_id: 'chat-1', timestamp: '2026-09-23T00:00:00Z', data: { type, data } } as unknown as PollingEvent
}

async function mount(events: PollingEvent[]) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  await act(async () => { root.render(<TerminalEventTranscript events={events} terminal={null} />) })
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}

describe('summarized chat rows', () => {
  it('shows the summary and loads the full response on demand', async () => {
    getChatArtifact.mockResolvedValue({ id: 'answer', type: 'unified_completion', data: { data: { final_result: 'FULL ANSWER TEXT' } } })
    const host = await mount([
      event('user', 'user_message', { content: 'Summarize everything' }),
      event('answer', 'unified_completion', { final_result: 'SUMMARY…', truncated: true, artifact_id: 'a'.repeat(32), original_size_bytes: 200_000 }),
    ])
    expect(host.textContent).toContain('SUMMARY…')
    const button = host.querySelector('[data-testid="chat-artifact-show-full"] button') as HTMLButtonElement
    expect(button).not.toBeNull()
    await act(async () => { button.click() })
    expect(getChatArtifact).toHaveBeenCalledWith('chat-1', 'a'.repeat(32))
    expect(host.textContent).toContain('FULL ANSWER TEXT')
    expect(host.querySelector('[data-testid="chat-artifact-show-full"]')).toBeNull()
  })

  it('renders ordinary rows without a show-full control', async () => {
    const host = await mount([
      event('user', 'user_message', { content: 'hi' }),
      event('answer', 'unified_completion', { final_result: 'hello' }),
    ])
    expect(host.querySelector('[data-testid="chat-artifact-show-full"]')).toBeNull()
  })
})
