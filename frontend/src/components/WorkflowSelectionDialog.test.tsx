// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import WorkflowSelectionDialog from './WorkflowSelectionDialog'
import { workflowManifestApi } from '../services/api'

vi.mock('../services/api', () => ({ workflowManifestApi: { listWorkflowManifests: vi.fn() } }))
vi.mock('../stores/useAuthStore', () => ({ useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: 'reader' } }) }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const cleanups: (() => void)[] = []
afterEach(() => { cleanups.splice(0).forEach(fn => fn()); vi.clearAllMocks() })
const allowed = { success: true, total: 1, workflows: [{ workspace_path: 'Workflow/shared', manifest: { id: 'shared', label: 'Shared automation' } }] }
async function mount() {
  const host = document.createElement('div'); document.body.append(host)
  const root = createRoot(host)
  const render = async (isOpen: boolean) => { await act(async () => root.render(<WorkflowSelectionDialog isOpen={isOpen} onClose={() => {}} onSelectWorkflow={() => {}} searchQuery="" position={{ bottom: 0, left: 0 }} />)) }
  cleanups.push(() => { act(() => root.unmount()); host.remove() })
  await render(true)
  return { host, render }
}
it('shows only the fresh server list and drops revoked workflows when reopened', async () => {
  vi.mocked(workflowManifestApi.listWorkflowManifests).mockResolvedValueOnce(allowed as Awaited<ReturnType<typeof workflowManifestApi.listWorkflowManifests>>).mockResolvedValueOnce({ success: true, total: 0, workflows: [] })
  const { host, render } = await mount()
  expect(host.textContent).toContain('Shared automation')
  await render(false); await render(true)
  expect(host.textContent).not.toContain('Shared automation')
  expect(host.textContent).toContain('No accessible automations')
  expect(workflowManifestApi.listWorkflowManifests).toHaveBeenCalledTimes(2)
})
it('does not reuse stale results when checking permissions fails', async () => {
  vi.mocked(workflowManifestApi.listWorkflowManifests).mockResolvedValueOnce(allowed as Awaited<ReturnType<typeof workflowManifestApi.listWorkflowManifests>>).mockRejectedValueOnce(new Error('offline'))
  const { host, render } = await mount()
  await render(false); await render(true)
  expect(host.textContent).not.toContain('Shared automation')
  expect(host.textContent).toContain('Unable to load accessible automations')
})
