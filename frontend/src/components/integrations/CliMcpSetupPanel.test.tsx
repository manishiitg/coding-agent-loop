// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({
  authApi: { createAccessToken: vi.fn(), revokeAccessToken: vi.fn() },
  getApiBaseUrl: vi.fn(),
}))
import { authApi, getApiBaseUrl } from '../../services/api'
import { CliMcpSetupPanel } from './CliMcpSetupPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
afterEach(() => vi.resetAllMocks())

describe('CLI & MCP setup panel', () => {
  it('provisions its own token and prefills every command', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue('https://agentworks.example.com')
    vi.mocked(authApi.createAccessToken).mockResolvedValue({ token: 'aw_pat_test', access_token: { id: 'tok-1' } as never })
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<CliMcpSetupPanel />))
      expect(host.textContent).toContain('Connect this installation to your terminal or an AI assistant')
      expect(host.textContent).toContain('Nothing is created until you generate')
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
      const generate = Array.from(host.querySelectorAll('button')).find(b => b.textContent?.includes('Generate connection'))!
      await act(async () => generate.click())
      expect(authApi.createAccessToken).toHaveBeenCalledWith({ name: 'Connect tab', expires_in_days: 30, scopes: ['workflows:read', 'files:read'], all_workflows: true, workflow_ids: [] })
      expect(host.textContent).toContain(`printf '%s' 'aw_pat_test' | agentworks login --server "https://agentworks.example.com" --token-stdin`)
      expect(host.textContent).toContain(`AGENTWORKS_TOKEN='aw_pat_test' agentworks workflows list --json`)
      expect(host.textContent).toContain(`--env AGENTWORKS_TOKEN='aw_pat_test' agentworks -- agentworks mcp serve`)
      const revoke = Array.from(host.querySelectorAll('button')).find(b => b.textContent === 'Revoke')!
      await act(async () => revoke.click())
      expect(authApi.revokeAccessToken).toHaveBeenCalledWith('tok-1')
      expect(host.textContent).toContain('Nothing is created until you generate')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
