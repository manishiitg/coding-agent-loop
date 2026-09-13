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
  expect(host.querySelector('select[aria-label="Browser sizing"]')).toBeNull()
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

it('fits the browser automatically while preserving its aspect ratio', async () => {
  api.get.mockResolvedValue({ data: { sessions: [testBrowser('pw-aspect')] } })
  const { host } = await mountBrowser()
  await act(async () => { FakeSocket.instances.at(-1)?.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/', metadata: { deviceWidth: 900, deviceHeight: 1600 } }) }) })
  const image = host.querySelector('img')!
  expect(image.className).toContain('max-h-full')
  expect(image.className).toContain('max-w-full')
  expect(image.className).toContain('h-auto')
  expect(image.className).toContain('w-auto')
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
  return { root, host, selector: host.querySelector('select[aria-label="Browser session"]') as HTMLSelectElement }
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
  expect(host.textContent).toContain('Completed')
  expect(host.querySelector('img')?.src).toBe('data:image/jpeg;base64,/9j/')
  expect(host.querySelector('img')?.alt).toBe('Last Playwright test frame')
  await pollBrowsers([shared, testBrowser('pw-two')])
  expect(selector.value).toBe('playwright-tests')
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/pw-two/stream')
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).not.toContain('Completed')
  await act(async () => { FakeSocket.instances.at(-1)?.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/new' }) }) })
  expect(host.querySelector('img')?.src).toBe('data:image/jpeg;base64,/9j/new')
})
it('offers a separate Playwright browser before a test starts and remembers that choice', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared] } })
  const { host, selector } = await mountBrowser()
  expect([...selector.options].map(option => option.textContent)).toContain('Follow latest test')
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

it('keeps the last frame on disconnect without claiming completion until the source disappears', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared, testBrowser('pw-one')] } })
  const { host } = await mountBrowser()
  const source = FakeSocket.instances.at(-1)!
  await act(async () => {
    source.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/old' }) })
    source.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/latest' }) })
    source.onclose?.()
  })
  expect(host.querySelector('img')?.src).toBe('data:image/jpeg;base64,/9j/latest')
  expect(host.textContent).toContain('Disconnected · Last frame')
  expect(host.textContent).not.toContain('Completed')
  await pollBrowsers([shared])
  expect(host.textContent).toContain('Completed · Last frame')
  expect(host.querySelector('img')?.src).toBe('data:image/jpeg;base64,/9j/latest')
})
it('clears retained frames when the workflow changes and ignores late messages from the old source', async () => {
  api.get.mockResolvedValue({ data: { sessions: [testBrowser('pw-one')] } })
  const { root, host } = await mountBrowser()
  const source = FakeSocket.instances.at(-1)!
  await act(async () => { source.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/private' }) }) })
  await pollBrowsers([])
  expect(host.textContent).toContain('Completed')
  await act(async () => { root.render(<WorkflowLiveBrowser workspacePath="Workflow/other" />) })
  await act(async () => { source.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/late' }) }) })
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).not.toContain('Completed')
})
it('does not show a retained test frame when the shared browser is selected', async () => {
  api.get.mockResolvedValue({ data: { sessions: [shared, testBrowser('pw-one')] } })
  const { host, selector } = await mountBrowser()
  await act(async () => { FakeSocket.instances.at(-1)?.onmessage?.({ data: JSON.stringify({ type: 'frame', data: '/9j/test' }) }) })
  await pollBrowsers([shared])
  await act(async () => { selector.value = 'shared-browser'; selector.dispatchEvent(new Event('change', { bubbles: true })) })
  expect(host.querySelector('img')).toBeNull()
  expect(host.textContent).not.toContain('Completed')
})

it('plays completed tests, offers download, and deletes the replay when the panel closes', async () => {
  const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:replay')
  const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  const completed = { ...testBrowser('pw-replay'), state: 'completed', recording_state: 'ready' }
  api.get.mockImplementation(async (url: string) => ({ data: url.endsWith('/recording') ? new Blob(['video'], { type: 'video/mp4' }) : { sessions: [shared, completed] } }))
  const { root, host } = await mountBrowser()
  expect(host.querySelector('video')?.getAttribute('src')).toBe('blob:replay')
  expect(host.querySelector('video')?.controls).toBe(true)
  expect(host.querySelector('a[download]')?.getAttribute('href')).toBe('blob:replay')
  expect(host.textContent).toContain('Closing this panel deletes')
  expect(FakeSocket.instances).toHaveLength(0)
  expect(api.post).not.toHaveBeenCalledWith(expect.anything(), { action: 'delete' }, expect.anything())
  await act(async () => { root.unmount(); await new Promise(resolve => setTimeout(resolve, 5)) })
  expect(api.post).toHaveBeenCalledWith('/api/browser/live/pw-replay/recording', { action: 'delete' }, { params: { workspace_path: 'Workflow/test' } })
  expect(revoke).toHaveBeenCalledWith('blob:replay')
  create.mockRestore(); revoke.mockRestore()
})

it('continues following live tests when an older completed replay remains', async () => {
  api.get.mockResolvedValue({ data: { sessions: [testBrowser('pw-first')] } })
  const { selector } = await mountBrowser()
  await pollBrowsers([{ ...testBrowser('pw-first'), state: 'completed', recording_state: 'saving' }, testBrowser('pw-next')])
  expect(selector.value).toBe('playwright-tests')
  expect(String(FakeSocket.instances.at(-1)?.url)).toContain('/pw-next/stream')
})

it('shows replay processing state instead of waiting for a completed browser live view', async () => {
  api.get.mockResolvedValue({ data: { sessions: [{ ...testBrowser('pw-queued'), state: 'completed', recording_state: 'queued' }] } })
  const { host } = await mountBrowser()
  expect(host.textContent).toContain('Replay queued for processing')
  expect(host.textContent).not.toContain('Waiting for the browser’s live view')
  await pollBrowsers([{ ...testBrowser('pw-queued'), state: 'completed', recording_state: 'saving' }])
  expect(host.textContent).toContain('Preparing video replay')
  expect(host.textContent).not.toContain('Waiting for the browser’s live view')
})


it('distinguishes repeated fixture names by run and keeps the same name for replay', async () => {
  const first = { ...testBrowser('pw-11111111-first'), label: 'auth_login_gate' }
  const second = { ...testBrowser('pw-22222222-second'), label: 'auth_login_gate' }
  api.get.mockResolvedValue({ data: { sessions: [first, second] } })
  const { selector } = await mountBrowser()
  expect(selector.selectedOptions[0].textContent).toBe('auth_login_gate · 11111111 · Live · Auto')
  expect([...selector.options].map(option => option.textContent)).toContain('auth_login_gate · 22222222 · Live')
  await act(async () => { selector.value = first.browser_session; selector.dispatchEvent(new Event('change', { bubbles: true })) })
  await pollBrowsers([{ ...first, state: 'completed', recording_state: 'saving' }, second])
  expect(selector.value).toBe(first.browser_session)
  expect(selector.selectedOptions[0].textContent).toBe('auth_login_gate · 11111111 · Replay')
  expect(selector.title).toBe('auth_login_gate · 11111111 · Replay')
})
