// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { PreviousChatHistoryPanel } from './PreviousChatHistoryPanel'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import { productWebhooksApi } from '../api/productWebhooks'
import type { ChatHistorySession, ScheduledJob, ScheduledJobRun } from '../services/api-types'

vi.mock('../services/api', () => ({ agentApi: {
  listChatHistorySessions: vi.fn(),
  getChatHistoryConversation: vi.fn(),
  deleteChatHistorySession: vi.fn(),
} }))
vi.mock('../api/scheduler', () => ({ schedulerApi: { listJobs: vi.fn(), getJobRuns: vi.fn(), cleanupJobRuns: vi.fn() } }))
vi.mock('../api/productWebhooks', () => ({ productWebhooksApi: { list: vi.fn(), runs: vi.fn() } }))
vi.mock('../stores/useChatStore', () => {
  const state = { addToast: vi.fn() }
  return { useChatStore: (select: (value: typeof state) => unknown) => select(state) }
})
vi.mock('./ui/MarkdownRenderer', () => ({ ConversationMarkdownRenderer: () => null }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const chat = { session_id: 'chat-1', title: 'Builder chat', query: 'Builder chat', username: 'tester', message_count: 3, created_at: '2026-09-12T10:00:00Z', updated_at: '2026-09-12T10:00:00Z', can_resume: true, can_delete: true } as ChatHistorySession
const slackBot = { session_id: 'bot-slack--aaa', title: 'Slack thread', query: 'Slack thread', username: 'tester', bot_platform: 'slack', message_count: 2, created_at: '2026-09-12T11:00:00Z', updated_at: '2026-09-12T11:00:00Z', can_resume: true, can_delete: true } as ChatHistorySession
const whatsappBot = { session_id: 'bot-whatsapp--bbb', title: 'WhatsApp thread', query: 'WhatsApp thread', username: 'tester', message_count: 2, created_at: '2026-09-12T11:30:00Z', updated_at: '2026-09-12T11:30:00Z', can_resume: true, can_delete: true } as ChatHistorySession
const scheduled = { session_id: 'schedule-cron--ccc', title: 'Morning run', query: 'Morning run', username: 'tester', message_count: 5, created_at: '2026-09-12T09:00:00Z', updated_at: '2026-09-12T09:00:00Z', can_resume: true, can_delete: true } as ChatHistorySession
const cron = { id: 'cron', name: 'Daily audit', schedule_type: 'cron', workspace_path: 'Workflow/test' } as ScheduledJob
const cronRun: ScheduledJobRun = { id: 'cron-run', job_id: cron.id, trigger_source: 'cron', session_id: scheduled.session_id, started_at: '2026-09-12T09:00:00Z', status: 'success' }

const cleanups: (() => void)[] = []
beforeEach(() => {
  vi.mocked(agentApi.listChatHistorySessions).mockResolvedValue({ sessions: [chat, slackBot, whatsappBot, scheduled] })
  vi.mocked(agentApi.getChatHistoryConversation).mockResolvedValue({ session_id: 'chat-1', conversation_history: [] })
  vi.mocked(schedulerApi.listJobs).mockResolvedValue({ jobs: [cron], total: 1, limit: 100, offset: 0 })
  vi.mocked(schedulerApi.getJobRuns).mockResolvedValue({ runs: [cronRun], total: 1, limit: 30, offset: 0 })
  vi.mocked(productWebhooksApi.list).mockResolvedValue({ triggers: [] })
})
afterEach(() => { cleanups.splice(0).forEach(clean => clean()); vi.useRealTimers(); vi.clearAllMocks(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

async function mountHub() {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" title="" emptyText="No previous automation chats yet." onSelectSession={vi.fn()} fill showAll recentOnly includeAutomationChats />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}

async function select(host: HTMLElement, label: string) {
  const button = host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)
  expect(button).not.toBeNull()
  await act(async () => button!.click())
}

it('opens the hub feed on History with the four origin filters', async () => {
  const host = await mountHub()
  expect(host.querySelector('button[aria-label="All"]')).toBeNull()
  for (const label of ['History', 'Schedules', 'Bots', 'Triggers']) {
    expect(host.querySelector(`button[aria-label="${label}"]`)).not.toBeNull()
  }
  expect(host.querySelector('button[aria-label="History"]')?.getAttribute('aria-pressed')).toBe('true')
  expect(host.textContent).toContain('Builder chat')
  expect(host.textContent).not.toContain('Slack thread')
  expect(host.textContent).not.toContain('Morning run')
})

it('narrows the hub feed by origin kind', async () => {
  const host = await mountHub()
  await select(host, 'History')
  expect(host.textContent).toContain('Builder chat')
  expect(host.textContent).not.toContain('Slack thread')
  await select(host, 'Schedules')
  expect(host.textContent).toContain('Daily audit')
  expect(host.textContent).not.toContain('Builder chat')
})

it('filters bot chats by channel', async () => {
  const host = await mountHub()
  expect(host.querySelector('[aria-label="Filter bots by channel"]')).toBeNull()
  await select(host, 'Bots')
  expect(host.textContent).toContain('Slack thread')
  expect(host.textContent).toContain('WhatsApp thread')
  expect(host.textContent).not.toContain('Builder chat')
  const channel = host.querySelector<HTMLSelectElement>('[aria-label="Filter bots by channel"]')
  expect(channel).not.toBeNull()
  expect(channel!.textContent).toContain('WhatsApp')
  expect(channel!.textContent).toContain('Slack')
  await act(async () => {
    channel!.value = 'whatsapp'
    channel!.dispatchEvent(new Event('change', { bubbles: true }))
  })
  expect(host.textContent).toContain('WhatsApp thread')
  expect(host.textContent).not.toContain('Slack thread')
})

it('deletes old runs from the Schedules feed after confirming', async () => {
  vi.mocked(schedulerApi.cleanupJobRuns).mockResolvedValue({ deleted_count: 1, workspace_path: 'Workflow/test' })
  const host = await mountHub()
  await select(host, 'Schedules')
  const dropdown = host.querySelector<HTMLButtonElement>('button[title="Delete old runs"]')
  expect(dropdown).not.toBeNull()
  await act(async () => dropdown!.click())
  const option = [...host.querySelectorAll('button')].find(button => button.textContent?.includes('Delete >3d'))!
  await act(async () => option.click())
  expect(document.body.textContent).toContain('Delete 1 scheduled run record older than 3 days?')
  const confirm = [...document.body.querySelectorAll('button')].find(button => button.textContent === 'Delete runs')!
  const runsBefore = vi.mocked(schedulerApi.getJobRuns).mock.calls.length
  await act(async () => confirm.click())
  await act(async () => { await Promise.resolve() })
  expect(schedulerApi.cleanupJobRuns).toHaveBeenCalledWith({ workspace_path: 'Workflow/test', older_than_days: 3, schedule_ids: 'cron' })
  expect(vi.mocked(schedulerApi.getJobRuns).mock.calls.length).toBeGreaterThan(runsBefore)
  expect(document.body.textContent).not.toContain('Delete 1 scheduled run record older than 3 days?')
})

it('offers no run cleanup in product run feeds', async () => {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" onSelectSession={vi.fn()} fill showAll recentOnly includeAutomationChats runEntityType="product" />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  await select(host, 'Schedules')
  expect(host.querySelector('button[title="Delete old runs"]')).toBeNull()
})

it('offers rename, copy, and delete from the row actions menu', async () => {
  vi.mocked(agentApi.deleteChatHistorySession).mockResolvedValue({ success: true, result: { deleted_count: 1, deleted_paths: [], cutoff: '', scope: '' } })
  const host = await mountHub()
  await select(host, 'Bots')
  expect(host.querySelector('button[aria-label="Rename chat"]')).toBeNull()
  expect(host.querySelector('button[aria-label="Delete this chat"]')).toBeNull()
  const menu = host.querySelector<HTMLButtonElement>('button[aria-label="Chat actions"]')
  expect(menu).not.toBeNull()
  await act(async () => menu!.click())
  const items = [...host.querySelectorAll('[role="menuitem"]')].map(el => el.textContent?.trim())
  expect(items).toContain('Rename chat')
  expect(items).toContain('Copy session ID')
  expect(items).toContain('Delete chat')
  vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
  const deleteItem = [...host.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')]
    .find(el => el.textContent?.trim() === 'Delete chat')!
  await act(async () => deleteItem.click())
  await act(async () => { await Promise.resolve() })
  expect(agentApi.deleteChatHistorySession).toHaveBeenCalledWith('bot-whatsapp--bbb', 'Workflow/test')
  expect(host.textContent).not.toContain('WhatsApp thread')
})

it('keeps the recent rail on chats with no origin pills', async () => {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(
    <PreviousChatHistoryPanel workspacePath="Workflow/test" onSelectSession={vi.fn()} recentOnly />,
  ))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  expect(host.querySelector('button[aria-label="Bots"]')).toBeNull()
  expect(host.querySelector('button[aria-label="All"]')).toBeNull()
  expect(host.textContent).toContain('Builder chat')
  expect(host.textContent).not.toContain('Slack thread')
})
