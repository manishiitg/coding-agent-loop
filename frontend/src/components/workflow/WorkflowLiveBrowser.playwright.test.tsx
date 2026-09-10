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
  constructor() { FakeSocket.instances.push(this); queueMicrotask(() => this.onopen?.()) }
}
const cleanups: (() => void)[] = []
afterEach(() => { cleanups.splice(0).forEach(fn => fn()); vi.unstubAllGlobals(); vi.clearAllMocks(); FakeSocket.instances = [] })
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
