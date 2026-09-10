// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import { TerminalEventTranscript } from './TerminalEventTranscript'

vi.mock('react-virtuoso', () => ({
  Virtuoso: ({ data, firstItemIndex, itemContent }: { data: unknown[]; firstItemIndex: number; itemContent: (index: number, item: unknown) => React.ReactNode }) => (
    <div>{data.map((item, index) => <div key={index}>{itemContent(firstItemIndex + index, item)}</div>)}</div>
  ),
}))
vi.mock('./events/EventDispatcher', () => ({ EventDispatcher: () => null }))
vi.mock('./ui/MarkdownRenderer', () => ({ ConversationMarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div> }))

const cleanups: Array<() => void> = []
afterEach(() => { cleanups.splice(0).forEach(cleanup => cleanup()); vi.unstubAllGlobals() })

function event(id: string, type: string, data: Record<string, unknown>): PollingEvent {
  return { id, type, timestamp: '2026-09-10T00:00:00Z', data: { type, data } } as PollingEvent
}
const failedTurn = [
  event('user', 'user_message', { content: 'Make a video' }),
  event('failed', 'conversation_error', { error: 'Provider temporarily unavailable' }),
]

async function mount(events: PollingEvent[], retry?: () => Promise<void>) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  await act(async () => { root.render(<TerminalEventTranscript events={events} terminal={null} onRetryLastMessage={retry} />) })
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}

describe('shared transcript failure retry', () => {
  it('distinguishes terminal delivery from processing the message', async () => {
    const host = await mount([event('user', 'user_message', {
      content: 'Check the browser', metadata: { delivery_status: 'sent_to_cli' },
    })])
    expect(host.textContent).toContain('Submitted to agent · may be queued')
    expect(host.textContent).not.toContain('Message sent')
  })

  it('retries the latest failed turn once while acknowledgement is pending', async () => {
    let finish!: () => void
    const retry = vi.fn(() => new Promise<void>(resolve => { finish = resolve }))
    const host = await mount(failedTurn, retry)
    const button = Array.from(host.querySelectorAll('button')).find(button => button.textContent === 'Retry message')!
    expect(button).toBeDefined()
    await act(async () => { button.click() })
    expect(button.disabled).toBe(true)
    await act(async () => { button.click() })
    expect(retry).toHaveBeenCalledTimes(1)
    await act(async () => { finish() })
    expect(button.disabled).toBe(false)
  })

  it('does not retry an older failure after the user continues', async () => {
    const host = await mount([...failedTurn, event('next-user', 'user_message', { content: 'Use a different approach' })], vi.fn())
    expect(host.textContent).not.toContain('Retry message')
  })

  it('does not offer retry when the chat cannot accept it', async () => {
    const host = await mount(failedTurn)
    expect(host.textContent).not.toContain('Retry message')
  })
})
