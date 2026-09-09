// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({
  authApi: { listAccessTokens: vi.fn(), createAccessToken: vi.fn(), revokeAccessToken: vi.fn() },
  agentApi: { listWorkflowManifests: vi.fn() },
  getApiBaseUrl: vi.fn(),
}))
import { authApi, agentApi, getApiBaseUrl } from '../../services/api'
import AccessTokensDialog from './AccessTokensDialog'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
afterEach(() => vi.resetAllMocks())
describe('Access token account dialog', () => {
  it.each(['https://agentworks.example.com', 'http://127.0.0.1:18743'])('creates, connects and revokes a token for %s', async (server) => {
    vi.mocked(getApiBaseUrl).mockReturnValue(server)
    const token = { id: 'one', name: 'Claude Code', scopes: ['workflows:read', 'files:read'], all_workflows: true, workflow_ids: [], created_at: new Date().toISOString(), expires_at: '2099-01-01T00:00:00Z', last_used_at: null, revoked_at: null }
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    vi.mocked(agentApi.listWorkflowManifests).mockResolvedValue({ success: true, total: 0, workflows: [] })
    vi.mocked(authApi.createAccessToken).mockResolvedValue({ token: 'aw_pat_once_only', access_token: token })
    vi.mocked(authApi.revokeAccessToken).mockResolvedValue()
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<AccessTokensDialog onClose={vi.fn()} />))
      const modal = document.querySelector('[role="dialog"]')!
      expect(modal.textContent).toContain('No tokens yet.')
      const name = modal.querySelector('input:not([type="checkbox"])') as HTMLInputElement
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(name, 'Claude Code')
        name.dispatchEvent(new Event('input', { bubbles: true }))
      })
      await act(async () => modal.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
      expect(authApi.createAccessToken).toHaveBeenCalledWith({ name: 'Claude Code', scopes: ['workflows:read', 'files:read'], all_workflows: true, workflow_ids: [], expires_in_days: 30 })
      expect((modal.querySelector('textarea') as HTMLTextAreaElement).value).toBe('aw_pat_once_only')
      expect(modal.textContent).toContain(`agentworks login --server ${JSON.stringify(server)} --token-stdin`)
      await act(async () => Array.from(modal.querySelectorAll('button')).find(b => b.textContent === 'Done')!.click())
      expect(modal.querySelector('textarea')).toBeNull()
      expect(modal.textContent).not.toContain('aw_pat_once_only')
      await act(async () => (modal.querySelector('[aria-label="Revoke Claude Code"]') as HTMLButtonElement).click())
      expect(authApi.revokeAccessToken).toHaveBeenCalledWith('one')
      expect(modal.textContent).toContain('Revoked')
    } finally { await act(async () => root.unmount()); host.remove() }
  })
  it('makes full Builder access explicit and removes it when workflows are restricted', async () => {
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    vi.mocked(agentApi.listWorkflowManifests).mockResolvedValue({ success: true, total: 0, workflows: [] })
    const host = document.createElement('div');document.body.append(host);const root = createRoot(host)
    try {
      await act(async () => root.render(<AccessTokensDialog onClose={vi.fn()} />))
      const modal = document.querySelector('[role="dialog"]')!
      const boxes = Array.from(modal.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'))
      expect(boxes.map(b => b.checked)).toEqual([true, true, false, false, false])
      await act(async () => boxes[4].click())
      expect(boxes.every(b => b.checked)).toBe(true)
      const workflowSelect = modal.querySelectorAll('select')[1]
      await act(async () => { workflowSelect.value = 'selected'; workflowSelect.dispatchEvent(new Event('change', { bubbles: true })) })
      expect(boxes[4].checked).toBe(false)
      expect((modal.querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(true)
    } finally { await act(async () => root.unmount());host.remove() }
  })
})
