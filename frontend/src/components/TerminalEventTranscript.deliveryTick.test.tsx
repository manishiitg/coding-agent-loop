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

function userRow(id: string, content: string, metadata?: Record<string, unknown>): PollingEvent {
  return {
    id, type: 'user_message', timestamp: '2026-09-20T09:55:47+05:30',
    data: { data: { content, ...(metadata ? { metadata } : {}) } },
  } as PollingEvent
}

async function mount(events: PollingEvent[], onResendMessage?: (msg: string) => void) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  await act(async () => { root.render(<TerminalEventTranscript events={events} terminal={null} onResendMessage={onResendMessage} />) })
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}

describe('transcript live-input delivery ticks', () => {
  it('renders the durable tick next to a confirmed steered row', async () => {
    const host = await mount([userRow('user-message-steer', 'just testing', {
      source: 'coding_agent_live_input',
      delivery_status: 'sent_to_cli',
      provider: 'muse-cli',
      message_id: 'steer-message-1',
      confirmation: 'confirmed',
      latency_ms: 1225,
    })])
    const tick = host.querySelector('[data-testid="delivery-tick"]')
    expect(tick?.querySelector('svg.lucide-check-check')).not.toBeNull()
    expect(tick?.getAttribute('data-state')).toBe('confirmed')
  })

  it('renders the fast tick for a sent but not yet confirmed row', async () => {
    const host = await mount([userRow('user-message-steer', 'just testing', {
      source: 'coding_agent_live_input',
      delivery_status: 'sent_to_cli',
      provider: 'muse-cli',
      message_id: 'steer-message-1',
      confirmation: 'fast',
    })])
    expect(host.querySelector('[data-testid="delivery-tick"] svg.lucide-check')).not.toBeNull()
  })

  it('renders no tick on a plain query row', async () => {
    const host = await mount([userRow('user-message-plain', 'ok')])
    expect(host.querySelector('[data-testid="delivery-tick"]')).toBeNull()
  })

  it('says a failed message was not delivered and resends it on click', async () => {
    const resend = vi.fn()
    const failed = { source: 'coding_agent_live_input', delivery_status: 'sent_to_cli', provider: 'claude-code', message_id: 'steer-message-2', confirmation: 'failed' }
    const host = await mount([userRow('user-message-lost', 'another option is that', failed)], resend)
    const notice = host.querySelector('[data-testid="delivery-failed-resend"]')
    expect(notice?.textContent).toContain('Not delivered')
    await act(async () => { notice!.querySelector('button')!.click() })
    expect(resend).toHaveBeenCalledWith('another option is that')
    expect(host.querySelector('[data-testid="delivery-failed-resend"]')).toBeNull()
    expect(host.textContent).toContain('Resent')
  })

  it('shows no resend for a confirmed message', async () => {
    const host = await mount([userRow('user-message-ok', 'fine', { source: 'coding_agent_live_input', delivery_status: 'sent_to_cli', provider: 'claude-code', message_id: 'm', confirmation: 'confirmed' })], vi.fn())
    expect(host.querySelector('[data-testid="delivery-failed-resend"]')).toBeNull()
  })
})
