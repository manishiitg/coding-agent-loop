// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import { productWebhooksApi } from '../../api/productWebhooks'
import { TriggerDeliveryHistoryPanel } from './TriggerDeliveryHistoryPanel'

const openChat = vi.fn()
vi.mock('../../api/productWebhooks', () => ({ productWebhooksApi: { list: vi.fn(), runs: vi.fn(), getPayload: vi.fn() } }))
vi.mock('../../api/workflowWebhooks', () => ({ workflowWebhooksApi: { getPayload: vi.fn() } }))
vi.mock('../../hooks/useResumePreviousChat', () => ({ useResumePreviousChat: () => openChat }))
vi.mock('../PreviousChatHistoryPanel', () => ({ PreviousChatHistoryPanel: () => null }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

afterEach(() => { vi.clearAllMocks() })

it('loads Crew trigger runs from the product trigger source and opens their chat tab', async () => {
  vi.mocked(productWebhooksApi.list).mockResolvedValue({ triggers: [{
    id: 'trigger-1', name: 'PR opened', enabled: true, message: 'Review the PR',
    auth_mode: 'github', path: '/api/hooks/product/trigger-1', run_destination: 'isolated',
  }] })
  vi.mocked(productWebhooksApi.runs).mockResolvedValue({ runs: [{
    id: 'run-1', job_id: 'product-project:work:crew-1:trigger-1', trigger_source: 'webhook',
    session_id: 'trigger-session', status: 'success', started_at: '2026-09-19T08:00:00Z',
    final_response: 'Reviewed PR 87 successfully.',
    webhook: { trigger_name: 'PR opened', delivery_id: 'delivery-87', event: 'pull_request', received_at: '2026-09-19T08:00:00Z' },
  }], total: 1, limit: 30, offset: 0 })
  vi.mocked(productWebhooksApi.getPayload).mockResolvedValue({ raw_payload: '{"action":"opened","number":87}' })
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(<TriggerDeliveryHistoryPanel
    workspacePath="Chats/Work/projects/crew-1"
    entityType="product"
    productTriggerScope={{ profileId: 'work', projectId: 'crew-1' }}
  />))

  expect(productWebhooksApi.runs).toHaveBeenCalledWith({ profileId: 'work', projectId: 'crew-1' }, 'trigger-1', 30)
  expect(host.textContent).toContain('PR opened')
  expect(host.textContent).toContain('Final response')
  expect(host.textContent).toContain('Reviewed PR 87 successfully.')
  expect(host.textContent).not.toContain('No webhooks configured')
  const viewChat = [...host.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent?.includes('View chat'))
  expect(viewChat).toBeDefined()
  await act(async () => viewChat!.click())
  expect(openChat).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'trigger-session', title: 'PR opened' }))

  const details = host.querySelector('details')!
  await act(async () => {
    details.open = true
    details.dispatchEvent(new Event('toggle', { bubbles: true }))
  })
  expect(productWebhooksApi.getPayload).toHaveBeenCalledWith({ profileId: 'work', projectId: 'crew-1' }, 'trigger-1', 'run-1')
  expect(host.textContent).toContain('"action": "opened"')

  act(() => root.unmount())
  host.remove()
})
