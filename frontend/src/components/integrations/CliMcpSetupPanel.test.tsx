// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({
  authApi: { createAccessToken: vi.fn(), revokeAccessToken: vi.fn(), listAccessTokens: vi.fn() },
  getApiBaseUrl: vi.fn(),
}))
import { authApi, getApiBaseUrl } from '../../services/api'
import { CliMcpSetupPanel } from './CliMcpSetupPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let stored: Map<string, string>
beforeEach(() => {
  stored = new Map()
  Object.defineProperty(window, 'localStorage', {
    value: {
      getItem: (key: string) => stored.get(key) ?? null,
      setItem: (key: string, value: string) => { stored.set(key, value) },
      removeItem: (key: string) => { stored.delete(key) },
      clear: () => { stored.clear() },
    },
    configurable: true,
  })
})
afterEach(() => vi.resetAllMocks())

const SERVER = 'https://agentworks.example.com'
const KEY = `agentworks.connectToken.${SERVER}`

async function renderPanel(host: HTMLElement) {
  const root = createRoot(host)
  await act(async () => root.render(<CliMcpSetupPanel />))
  await act(async () => {})
  return root
}

describe('CLI & MCP setup panel', () => {
  it('provisions its own token and prefills the three commands', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    vi.mocked(authApi.createAccessToken).mockResolvedValue({ token: 'aw_pat_test', access_token: { id: 'tok-1' } as never })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('Connect this installation to your terminal or an AI assistant')
      expect(host.textContent).toContain('Nothing is created until you generate')
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
      const generate = Array.from(host.querySelectorAll('button')).find(b => b.textContent?.includes('Generate connection'))!
      await act(async () => generate.click())
      expect(authApi.createAccessToken).toHaveBeenCalledWith({ name: 'Connect tab', expires_in_days: 30, scopes: ['workflows:read', 'files:read'], all_workflows: true, workflow_ids: [] })
      expect(host.textContent).toContain(`curl -fsSL "https://agentworks.example.com/api/downloads/cli/install-agentworks.sh" | sh -s -- --server "https://agentworks.example.com" --token 'aw_pat_test'`)
      expect(host.textContent).not.toContain('agentworks login')
      expect(host.textContent).toContain(`--env AGENTWORKS_TOKEN='aw_pat_test' agentworks -- agentworks mcp serve`)
      expect(host.textContent).toContain('agentworks skills install --dir ~/.claude/skills')
      expect(host.textContent).not.toContain('workflows list')
      expect(host.textContent).not.toContain('guidance context')
      expect(host.textContent).not.toContain(`'aw_pat_test' agentworks mcp serve`)
      expect(window.localStorage.getItem(KEY)).toBe(JSON.stringify({ id: 'tok-1', token: 'aw_pat_test' }))
      const revoke = Array.from(host.querySelectorAll('button')).find(b => b.textContent === 'Revoke')!
      await act(async () => revoke.click())
      expect(authApi.revokeAccessToken).toHaveBeenCalledWith('tok-1')
      expect(host.textContent).toContain('Nothing is created until you generate')
      expect(window.localStorage.getItem(KEY)).toBeNull()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('reuses the stored token while the server still lists it', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    window.localStorage.setItem(KEY, JSON.stringify({ id: 'tok-9', token: 'aw_pat_old' }))
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [{ id: 'tok-9', name: 'Connect tab', revoked_at: null, expires_at: '2099-01-01T00:00:00Z' } as never] })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain(`install-agentworks.sh" | sh -s -- --server "https://agentworks.example.com" --token 'aw_pat_old'`)
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
      expect(host.querySelector('button')?.textContent).not.toContain('Generate connection')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('falls back to generate when the stored token was revoked elsewhere', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    window.localStorage.setItem(KEY, JSON.stringify({ id: 'tok-gone', token: 'aw_pat_gone' }))
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('Nothing is created until you generate')
      expect(host.textContent).not.toContain('aw_pat_gone')
      expect(window.localStorage.getItem(KEY)).toBeNull()
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
