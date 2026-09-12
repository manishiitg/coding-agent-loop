// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import WorkflowAPITriggersView from './WorkflowAPITriggersView'
import { workflowWebhooksApi } from '../../api/workflowWebhooks'
import { useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'

vi.mock('../../api/workflowWebhooks', () => ({ workflowWebhooksApi: { list: vi.fn(), save: vi.fn(), delete: vi.fn() }, apiTriggerURL: (path: string) => `https://agent.example${path}` }))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({ useCanWriteWorkflow: vi.fn(() => true) }))
vi.mock('../../stores/useWorkflowManifestStore', () => ({ useWorkflowManifestStore: { getState: () => ({ refreshWorkflows: vi.fn().mockResolvedValue(undefined) }) } }))
vi.mock('../../stores/useWorkflowStore', () => ({ useWorkflowStore: { getState: () => ({ openWorkspaceView: vi.fn() }) } }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const trigger = { id: 'trigger-1', name: 'Issues', enabled: true, auth_mode: 'github' as const, path: '/api/hooks/workflow/trigger-1', route_selections: { router: 'issues' }, group_names: ['prod'] }
const cleanups: (() => void)[] = []
beforeEach(() => {
  vi.mocked(useCanWriteWorkflow).mockReturnValue(true)
  vi.mocked(workflowWebhooksApi.list).mockResolvedValue({ triggers: [trigger], groups: ['prod'], routes: [{ step_id: 'router', step_title: 'Choose work', route_id: 'issues', route_name: 'Process issues' }] })
  vi.mocked(workflowWebhooksApi.save).mockImplementation(async value => ({ ...trigger, ...value }))
})
afterEach(() => { cleanups.splice(0).forEach(clean => clean()); vi.clearAllMocks() })
async function mount() {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(<WorkflowAPITriggersView workspacePath="Workflow/test" />))
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  return host
}
function button(host: HTMLElement, label: string) {
  const found = [...host.querySelectorAll('button')].find(node => node.textContent === label)
  if (!found) throw new Error(`Missing button ${label}`)
  return found
}
it('shows endpoint, saved route, authentication and group', async () => {
  const host = await mount()
  expect(host.textContent).toContain('https://agent.example/api/hooks/workflow/trigger-1')
  expect(host.textContent).toContain('Choose work → Process issues')
  expect(host.textContent).toContain('GitHub signature · Groups: prod')
  expect(host.textContent).toContain('Time triggers run on a schedule')
})
it('disables a trigger while retaining its route binding', async () => {
  const host = await mount()
  await act(async () => button(host, 'Disable').click())
  expect(workflowWebhooksApi.save).toHaveBeenCalledWith(expect.objectContaining({ workspace_path: 'Workflow/test', enabled: false, route_selections: { router: 'issues' } }), 'trigger-1')
})
it('rotates and displays a secret only until dismissed', async () => {
  vi.mocked(workflowWebhooksApi.save).mockResolvedValue({ ...trigger, secret: 'new-one-time-secret' })
  const host = await mount()
  expect(host.textContent).not.toContain('new-one-time-secret')
  await act(async () => button(host, 'Rotate secret').click())
  expect(workflowWebhooksApi.save).toHaveBeenCalledWith(expect.objectContaining({ rotate_secret: true }), 'trigger-1')
  expect(host.textContent).toContain('new-one-time-secret')
  expect(host.textContent).toContain('Setup pings do not start runs')
  await act(async () => button(host, 'Dismiss secret').click())
  expect(host.textContent).not.toContain('new-one-time-secret')
})
it('lets a reader inspect but not change triggers', async () => {
  vi.mocked(useCanWriteWorkflow).mockReturnValue(false)
  const host = await mount()
  expect(host.textContent).toContain('Process issues')
  expect(host.textContent).not.toContain('Rotate secret')
  expect(host.textContent).not.toContain('Add API trigger')
  expect(host.textContent).not.toContain('Remove')
})
it('directs creation and configuration to the builder chat without a manual form', async () => {
  const host = await mount()
  expect(host.textContent).toContain('through the workflow builder chat')
  expect(host.textContent).not.toContain('Add API trigger')
  expect(host.querySelector('form')).toBeNull()
  expect([...host.querySelectorAll('button')].some(node => node.textContent === 'Edit')).toBe(false)
})
it('removes only the selected trigger', async () => {
  const host = await mount()
  await act(async () => button(host, 'Remove').click())
  expect(workflowWebhooksApi.delete).toHaveBeenCalledWith('Workflow/test', 'trigger-1')
})
