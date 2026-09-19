// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { PreviousChatHistoryPanel } from './PreviousChatHistoryPanel'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import { workflowWebhooksApi } from '../api/workflowWebhooks'
import type { ScheduledJob, ScheduledJobRun } from '../services/api-types'
import { scheduleRunSlotLabel } from '../utils/scheduleRunSlot'

vi.mock('../services/api', () => ({ agentApi: {
  listChatHistorySessions: vi.fn(),
  getChatHistoryConversation: vi.fn(),
} }))
vi.mock('../api/scheduler', () => ({ schedulerApi: { listJobs: vi.fn(), getJobRuns: vi.fn() } }))
vi.mock('../api/workflowWebhooks', () => ({ workflowWebhooksApi: { getPayload: vi.fn() } }))
vi.mock('../stores/useChatStore', () => {
  const state = { addToast: vi.fn() }
  return { useChatStore: (select: (value: typeof state) => unknown) => select(state) }
})
vi.mock('./ui/MarkdownRenderer', () => ({ ConversationMarkdownRenderer: () => null }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const hook = { id: 'hook', name: 'PR reviews', schedule_type: 'webhook', workspace_path: 'Workflow/test' } as ScheduledJob
const cron = { id: 'cron', name: 'Daily audit', schedule_type: 'cron', workspace_path: 'Workflow/test' } as ScheduledJob
const webhookRun: ScheduledJobRun = {
  id: 'delivery-run', job_id: hook.id, trigger_source: 'webhook', session_id: 'schedule-webhook--hook_123',
  started_at: '2026-09-12T12:00:00Z', scheduled_for: '2026-09-12T12:00:00Z', status: 'running', run_folder: 'iteration-0',
  webhook: { trigger_name: 'PR reviews', delivery_id: 'delivery-123', event: 'pull_request', received_at: '2026-09-12T12:00:00Z' },
}
const cronRun: ScheduledJobRun = { id: 'cron-run', job_id: cron.id, trigger_source: 'cron', session_id: 'schedule-cron--cron_123', started_at: '2026-09-12T11:00:00Z', status: 'success' }
const cleanups: (() => void)[] = []
beforeEach(() => {
  vi.mocked(agentApi.listChatHistorySessions).mockResolvedValue({ sessions: [] })
  vi.mocked(agentApi.getChatHistoryConversation).mockResolvedValue({ session_id: 'old-chat', conversation_history: [] })
  vi.mocked(schedulerApi.listJobs).mockResolvedValue({ jobs: [hook, cron], total: 2, limit: 100, offset: 0 })
  vi.mocked(schedulerApi.getJobRuns).mockImplementation(async id => ({ runs: id === hook.id ? [webhookRun] : [cronRun], total: 1, limit: 30, offset: 0 }))
  vi.mocked(workflowWebhooksApi.getPayload).mockResolvedValue({ raw_payload: '{"action":"opened","number":42}' })
})
afterEach(() => { cleanups.splice(0).forEach(clean => clean()); vi.useRealTimers(); vi.clearAllMocks() })
async function mount(compact = false) {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  const onSelect = vi.fn()
  await act(async () => root.render(<PreviousChatHistoryPanel workspacePath="Workflow/test" compact={compact} onSelectSession={onSelect} />))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return { host, onSelect }
}

async function mountDeliveryHistory() {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel
      workspacePath="Workflow/test"
      title="Delivery history"
      emptyText="No webhook deliveries recorded yet."
      runOnly="webhook"
      runEntityType="product"
      readOnly
      showAll
      onSelectSession={vi.fn()}
    />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}
async function select(host: HTMLElement, label: string) {
  const button = host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)
  expect(button).not.toBeNull()
  await act(async () => button!.click())
}
it('separates webhook and time-triggered runs and opens runs outside the chat-history page', async () => {
  const { host, onSelect } = await mount()
  await select(host, 'Webhooks')
  expect(host.textContent).toContain('PR reviews')
  expect(host.textContent).toContain('pull_request')
  expect(host.textContent).toContain('delivery-123')
  expect(host.textContent).toContain('iteration-0')
  expect(host.textContent).not.toContain('Daily audit')
  expect(host.textContent).not.toContain('Scheduled slot')
  const open = [...host.querySelectorAll('button')].find(button => button.textContent === 'Open')!
  await act(async () => open.click())
  expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ session_id: webhookRun.session_id }))
  await select(host, 'Schedules')
  expect(host.textContent).toContain('Daily audit')
  expect(host.textContent).not.toContain('PR reviews')
})
it('keeps compact filters accessible and shows webhook setup guidance', async () => {
  vi.mocked(schedulerApi.listJobs).mockResolvedValue({ jobs: [cron], total: 1, limit: 100, offset: 0 })
  const { host } = await mount(true)
  await select(host, 'Webhooks')
  expect(host.textContent).toContain('workflow builder chat')
  expect(host.querySelector('button[aria-label="Webhooks"]')?.getAttribute('aria-pressed')).toBe('true')
})
it('refreshes the visible feed when a webhook finishes', async () => {
  vi.useFakeTimers()
  const { host } = await mount()
  await select(host, 'Webhooks')
  expect(host.textContent).toContain('Run in progress')
  vi.mocked(schedulerApi.getJobRuns).mockImplementation(async id => ({ runs: id === hook.id ? [{ ...webhookRun, status: 'success' }] : [cronRun], total: 1, limit: 30, offset: 0 }))
  await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
  expect(host.textContent).not.toContain('Run in progress')
  expect(host.textContent).toContain('Completed')
})
it('does not interpret a webhook received time as a cron slot', () => {
  expect(scheduleRunSlotLabel(hook, webhookRun)).toBeUndefined()
})

it('shows a read-only webhook delivery feed inside Triggers', async () => {
  const host = await mountDeliveryHistory()
  expect(host.textContent).toContain('Delivery history')
  expect(host.textContent).toContain('PR reviews')
  expect(host.textContent).not.toContain('Daily audit')
  expect(host.querySelector('button[aria-label="Webhooks"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Schedules"]')).toBeNull()
  expect([...host.querySelectorAll('button')].some(button => button.textContent === 'Open')).toBe(false)
  expect(agentApi.listChatHistorySessions).not.toHaveBeenCalled()
  expect(schedulerApi.listJobs).toHaveBeenCalledWith({ entity_type: 'product', limit: 100 })
})

it('keeps historical Crew conversations read-only and expands them in place', async () => {
  vi.mocked(agentApi.listChatHistorySessions).mockResolvedValue({ sessions: [{
    session_id: 'old-chat',
    title: 'Earlier project discussion',
    message_count: 2,
    created_at: '2026-09-10T10:00:00Z',
  }] })
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  const onSelect = vi.fn()
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" recentOnly readOnly onSelectSession={onSelect} />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })

  expect(host.textContent).not.toContain('Delete old')
  expect(host.querySelector('button[aria-label="Rename chat"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Delete this chat"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Open"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Open read-only conversation"]')).toBeNull()
  expect(host.querySelector('button[aria-label="History"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Recent"]')).toBeNull()
  const title = [...host.querySelectorAll('button')].find(button => button.textContent?.includes('Earlier project discussion'))
  expect(title).toBeDefined()
  await act(async () => title!.click())
  expect(onSelect).not.toHaveBeenCalled()
  expect(agentApi.getChatHistoryConversation).toHaveBeenCalledWith('old-chat', 'Workflow/test', expect.any(Number))
})

it('can open read-only Crew history in a separate tab without exposing management actions', async () => {
  vi.mocked(agentApi.listChatHistorySessions).mockResolvedValue({ sessions: [{
    session_id: 'isolated-trigger-chat',
    title: 'Isolated trigger run',
    message_count: 2,
    created_at: '2026-09-10T10:00:00Z',
  }] })
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  const onSelect = vi.fn()
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" recentOnly readOnly allowOpen openOnRowClick onSelectSession={onSelect} />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })

  expect(host.querySelector('button[aria-label="Rename chat"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Delete this chat"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Show chat details"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Open in new tab"]')).toBeNull()
  const row = [...host.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent?.includes('Isolated trigger run'))
  expect(row).toBeDefined()
  await act(async () => row!.click())
  expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'isolated-trigger-chat' }))
  expect(agentApi.getChatHistoryConversation).not.toHaveBeenCalled()
})

it('identifies the open persistent chat even before it appears in history', async () => {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel
      workspacePath="Workflow/test"
      activeSessionId="current-chat"
      recentOnly
      onSelectSession={vi.fn()}
    />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })

  expect(host.textContent).toContain('Current chat is open')
  expect(host.textContent).not.toContain('No conversation history yet')
  expect(host.textContent).not.toContain('This view keeps its earlier history')
})

it('shows every fetched Workshop chat without a load-more control', async () => {
  vi.mocked(agentApi.listChatHistorySessions).mockResolvedValue({
    sessions: Array.from({ length: 6 }, (_, index) => ({
      session_id: `chat-${index}`,
      title: `Saved chat ${index + 1}`,
      message_count: 2,
      created_at: `2026-09-${String(10 + index).padStart(2, '0')}T10:00:00Z`,
    })),
  })
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" fill showAll onSelectSession={vi.fn()} />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })

  expect(host.textContent).toContain('Saved chat 6')
  expect(host.textContent).not.toContain('Load 5 more')
})

it('loads and formats the webhook body when delivery details are opened', async () => {
  const { host } = await mount()
  await select(host, 'Webhooks')
  const details = host.querySelector('details')!
  await act(async () => {
    details.open = true
    details.dispatchEvent(new Event('toggle'))
  })
  expect(workflowWebhooksApi.getPayload).toHaveBeenCalledWith(hook.id, webhookRun.id)
  expect(host.textContent).toContain('"action": "opened"')
  expect(host.textContent).toContain('"number": 42')
})
