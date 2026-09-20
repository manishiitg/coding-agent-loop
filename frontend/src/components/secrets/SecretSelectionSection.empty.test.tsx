// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import { SecretSelectionSection } from './SecretSelectionSection'

const mocks = vi.hoisted(() => ({ fetch: vi.fn().mockResolvedValue(undefined) }))
vi.mock('../../stores/useAuthStore', () => ({ useAuthStore: (selector: (state: unknown) => unknown) => selector({ user: { is_admin: true }, isMultiUserMode: true, isMultiUserModeChecked: true }) }))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({ useCanWriteWorkflow: () => true, READ_ONLY_TITLE: 'Read only' }))
vi.mock('../../api/secrets', () => ({ secretsApi: { promoteWorkflowSecret: vi.fn(), saveGlobalSecret: vi.fn(), deleteGlobalSecret: vi.fn(), decrypt: vi.fn() } }))
vi.mock('../../stores', () => {
  const state = { secrets: [], globalSecrets: [], storedUserSecrets: [], workflowSecretsByPath: {}, fetchGlobalSecrets: mocks.fetch, fetchStoredUserSecrets: mocks.fetch, fetchWorkflowSecrets: mocks.fetch, addWorkflowSecret: vi.fn(), removeWorkflowSecret: vi.fn() }
  return { useSecretsStore: (selector: (state: unknown) => unknown) => selector(state) }
})
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
afterEach(() => { document.body.innerHTML = '' })

it('renders no list box when there are no secrets to show', async () => {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(<SecretSelectionSection selectedSecrets={[]} onSecretChange={() => {}} workflowPath="Workflow/test" />))
  expect(host.textContent).toContain('Automation Secrets')
  expect(host.querySelectorAll('[role="checkbox"]').length).toBe(0)
  expect(host.querySelector('.border-border.bg-card')).toBeNull()
  await act(async () => root.unmount())
  host.remove()
})
