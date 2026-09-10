// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import WorkflowLiveBrowser from './WorkflowLiveBrowser'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../../services/api', () => ({ default: api, getApiBaseUrl: () => 'http://localhost', getAuthToken: () => 'viewer-token' }))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({ useCanWriteWorkflow: () => true }))
vi.mock('../../stores/useChatStore', () => ({ useChatStore: { getState: () => ({ addToast: vi.fn() }) } }))
vi.mock('../../stores/useWorkflowStore', () => ({ useWorkflowStore: { getState: () => ({ openWorkspaceView: vi.fn() }) } }))
class FakeSocket {
  static OPEN = 1
  static instances: FakeSocket[] = []
  readyState = 1
  onopen?: () => void
  onmessage?: (event: { data: string }) => void
  onclose?: () => void
  send = vi.fn()
  close = vi.fn()
  readonly url: string
  constructor(url: string) { this.url = url; FakeSocket.instances.push(this); queueMicrotask(() => this.onopen?.()) }
}
const cleanups: (() => void)[] = []
afterEach(() => { cleanups.splice(0).forEach(fn => fn()); vi.unstubAllGlobals(); vi.useRealTimers(); sessionStorage.clear(); vi.clearAllMocks(); FakeSocket.instances = [] })
it('shows both browser types but makes Playwright watch-only even for workflow writers', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', FakeSocket)
  api.get.mockResolvedValue({ data: { sessions: [
    { browser_session: 'pw-test', workflow_session: 'run', label: 'Checkout · retry 0', kind: 'playwright', read_only: 'true' },
    { browser_session: 'agent-test', workflow_session: 'run', label: 'Agent browser' },
  ] } })
  api.post.mockResolvedValue({ data: { recording: false } })
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  await act(async () => { root.render(<WorkflowLiveBrowser workspacePath="Workflow/test" />) })
  const buttons = () => [...host.querySelectorAll('button')].map(button => button.textContent)
  expect(host.textContent).toContain('Checkout · retry 0')
  expect(host.textContent).toContain('Agent browser')
  expect(host.textContent).toContain('Watch-only')
  expect(buttons()).not.toContain('Take control')
  expect(buttons()).not.toContain('Start recording')
  expect(api.post).not.toHaveBeenCalled()
  await act(async () => {
    FakeSocket.instances[0].onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/', metadata: { deviceWidth: 640, deviceHeight: 480 } }) })
    FakeSocket.instances[0].onmessage?.({ data: JSON.stringify({ type: 'tabs', tabs: [{ tabId: 't1', title: 'Checkout', url: 'http://localhost', active: false }] }) })
  })
  expect(host.querySelector('img')?.getAttribute('src')).toBe('data:image/jpeg;base64,/9j/')
  const tab = [...host.querySelectorAll('button')].find(button => button.textContent === 'Checkout')!
  expect(tab.disabled).toBe(true)
  const selector = host.querySelector('select[aria-label="Browser session"]') as HTMLSelectElement
  await act(async () => { selector.value = 'agent-test'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
  expect(buttons()).toContain('Take control')
  expect(buttons()).toContain('Start recording')
})

const shared = { browser_session: 'shared-browser', workflow_session: 'shared', label: 'Shared browser · all users' }
const testBrowser = (id: string) => ({ browser_session: id, workflow_session: 'child-run', label: `Login ${id}`, kind: 'playwright', read_only: 'true' })
async function mountBrowser() {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', FakeSocket)
  api.post.mockResolvedValue({ data: { recording: false } })
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  await act(async () => { root.render(<WorkflowLiveBrowser workspacePath="Workflow/test" />) })
  return { host, selector: host.querySelector('select[aria-label="Browser session"]') as HTMLSelectElement }
}
async function pollBrowsers(sessions: unknown[]) {
  api.get.mockResolvedValue({ data: { sessions } })
  await act(async () => { vi.advanceTimersByTime(1000) })
}
it('follows new Playwright cases instead of staying on the default shared blank browser', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared] } })
  const { host, selector } = await mountBrowser()
  expect(selector.value).toBe('shared-browser')
  await pollBrowsers([shared, testBrowser('pw-one')])
  expect(selector.value).toBe('playwright-tests')
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/pw-one/stream')
  await act(async () => { FakeSocket.instances.at(-1)?.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/' }) }) })
  expect(host.querySelector('img')?.src).toBe('data:image/jpeg;base64,/9j/')
  await pollBrowsers([shared])
  expect(selector.value).toBe('playwright-tests')
  expect(host.textContent).toContain('Waiting for a Playwright test')
  expect(host.querySelector('img')).toBeNull()
  await pollBrowsers([shared, testBrowser('pw-two')])
  expect(selector.value).toBe('playwright-tests')
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/pw-two/stream')
})
it('offers a separate Playwright browser before a test starts and remembers that choice', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared] } })
  const { host, selector } = await mountBrowser()
  expect([...selector.options].map(option => option.textContent)).toContain('Playwright tests')
  await act(async () => { selector.value = 'playwright-tests'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
  expect(host.textContent).toContain('Waiting for a Playwright test')
  expect(sessionStorage.getItem('browser-selection:Workflow/test')).toBe('playwright-tests')
  await pollBrowsers([shared])
  expect(selector.value).toBe('playwright-tests')
  expect([...host.querySelectorAll('button')].map(button => button.textContent)).not.toContain('Take control')
  await pollBrowsers([shared, testBrowser('pw-selected')])
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/pw-selected/stream')
})
it('respects an explicit shared-browser choice while tests are running', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared, testBrowser('pw-one')] } })
  const { selector } = await mountBrowser()
  expect(selector.value).toBe('playwright-tests')
  await act(async () => { selector.value = 'shared-browser'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
  await pollBrowsers([shared, testBrowser('pw-two')])
  expect(selector.value).toBe('shared-browser')
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/shared-browser/stream')
})
