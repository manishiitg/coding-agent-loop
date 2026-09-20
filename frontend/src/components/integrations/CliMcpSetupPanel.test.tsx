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
import { CliMcpSetupPanel } from './CliMcpSetupPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
afterEach(() => vi.resetAllMocks())

describe('CLI & MCP setup panel', () => {
  it('renders installation-scoped commands and opens the token dialog', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue('https://agentworks.example.com')
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    vi.mocked(agentApi.listWorkflowManifests).mockResolvedValue({ success: true, total: 0, workflows: [] })
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<CliMcpSetupPanel />))
      expect(host.textContent).toContain('agentworks login --server "https://agentworks.example.com" --token-stdin')
      expect(host.textContent).toContain('agentworks workflows list --json')
      expect(host.textContent).toContain('claude mcp add --transport stdio agentworks -- agentworks mcp serve')
      expect(host.textContent).toContain('agentworks skills install --dir ~/.claude/skills')
      expect(document.querySelector('[role="dialog"]')).toBeNull()
      const open = Array.from(host.querySelectorAll('button')).find(b => b.textContent === 'Open access tokens')!
      await act(async () => open.click())
      expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Access tokens')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
