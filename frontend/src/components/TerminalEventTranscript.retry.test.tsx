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
  it('renders Muse progress as an assistant response without a Thinking disclosure', async () => {
    const host = await mount([event('muse-update', 'conversation_thinking', {
      thinking: 'Checking the supplied files.', metadata: { presentation: 'assistant_update' },
    })])
    expect(host.querySelector('[data-testid="terminal-assistant-update"]')?.textContent).toBe('Checking the supplied files.')
    expect(host.querySelector('[data-testid="terminal-clear-thinking-batch-toggle"]')).toBeNull()
  })

  it.each(['sending', 'sent_to_cli', 'queued_for_injection', 'next_turn_started'])('shows only the timestamp for delivery status %s', async (status) => {
    const host = await mount([event('user', 'user_message', {
      content: 'Check the browser', metadata: { delivery_status: status },
    })])
    expect(host.textContent).toBe(`Check the browser${new Date('2026-09-10T00:00:00Z').toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`)
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

  it('shows the Muse reset time and upgrade link', async () => {
    const raw = `all LLMs failed (primary + 0 fallbacks): muse-cli/muse-spark-1.3-contributor [quota_exhausted]: Usage limit reached · /upgrade
(https://accountscenter.meta.com/muse_code/?ep=xgrade) for increased limits, or
wait for usage to reset at Sep 14 at 5:30 AM`
    const host = await mount([
      event('user', 'user_message', { content: 'Run it' }),
      event('failed', 'agent_error', { error: raw, provider: 'muse-cli', code: 'quota_exhausted' }),
    ])

    expect(host.textContent).toContain('Muse usage limit reached')
    expect(host.textContent).toContain('Retry after Sep 14 at 5:30 AM.')
    const upgrade = host.querySelector<HTMLAnchorElement>('a[href="https://accountscenter.meta.com/muse_code/?ep=xgrade"]')
    expect(upgrade?.textContent).toBe('Upgrade Muse')
    expect(upgrade?.target).toBe('_blank')
  })
})
