// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({
  default: { get: vi.fn().mockResolvedValue({ data: { connections: [] } }), delete: vi.fn() },
  authApi: { createAccessToken: vi.fn(), revokeAccessToken: vi.fn(), listAccessTokens: vi.fn() },
  getApiBaseUrl: vi.fn(),
}))
import api, { authApi, getApiBaseUrl } from '../../services/api'
import { CliMcpSetupPanel } from './CliMcpSetupPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let stored: Map<string, string>
beforeEach(() => {
  vi.mocked(api.get).mockResolvedValue({ data: { connections: [] } })
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

function button(host: HTMLElement, label: string) {
  const found = Array.from(host.querySelectorAll('button')).find(item => item.textContent?.includes(label))
  if (!found) throw new Error(`Missing button: ${label}`)
  return found
}

describe('CLI & MCP setup panel', () => {
  it('shows hosted OAuth setup without creating a personal token', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      await act(async () => button(host, 'Hosted AI app').click())
      expect(host.textContent).toContain(`${SERVER}/api/external/v1/mcp`)
      expect(host.textContent).toContain('Choose OAuth')
      expect(host.textContent).not.toContain('Create connection')
      expect(host.textContent).not.toContain('YOUR_TOKEN')
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('shows one setup path at a time after creating a shared connection', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    vi.mocked(authApi.createAccessToken).mockResolvedValue({ token: 'aw_pat_test', access_token: { id: 'tok-1' } as never })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('Where will you use AgentWorks?')
      expect(host.textContent).toContain('Create a connection to see the instructions')
      expect(host.textContent).not.toContain('YOUR_TOKEN')
      expect(authApi.createAccessToken).not.toHaveBeenCalled()

      await act(async () => button(host, 'Create connection').click())
      expect(authApi.createAccessToken).toHaveBeenCalledWith({ name: 'Connect tab', expires_in_days: 30, scopes: ['workflows:read', 'files:read', 'runs:execute'], all_workflows: true, workflow_ids: [] })
      expect(host.textContent).toContain(`curl -fsSL "https://agentworks.example.com/api/downloads/cli/install-agentworks.sh" | sh -s -- --server "https://agentworks.example.com" --token 'aw_pat_test'`)
      expect(host.textContent).not.toContain('Remote MCP URL')
      expect(host.textContent).not.toContain('MCP client config')

      await act(async () => button(host, 'AI app on this computer').click())
      expect(host.textContent).toContain(`claude mcp add agentworks -e AGENTWORKS_SERVER='https://agentworks.example.com' -e AGENTWORKS_TOKEN='aw_pat_test' -- agentworks mcp serve`)
      expect(host.textContent).not.toContain('Remote MCP URL')
      const codex = Array.from(host.querySelectorAll('button')).find(item => item.textContent === 'Codex')!
      await act(async () => codex.click())
      expect(host.textContent).toContain(`codex mcp add agentworks --env AGENTWORKS_SERVER='https://agentworks.example.com' --env AGENTWORKS_TOKEN='aw_pat_test' -- agentworks mcp serve`)
      expect(host.textContent).not.toContain('claude mcp add')
      await act(async () => button(host, 'JSON MCP client').click())
      expect(host.textContent).toContain('MCP client config')
      expect(host.textContent).toContain('"mcpServers"')
      expect(host.textContent).toContain('"AGENTWORKS_TOKEN": "aw_pat_test"')
      expect(host.querySelector('pre')?.textContent).toContain('"serve"')
      expect(host.textContent).not.toContain('claude mcp add')

      await act(async () => button(host, 'Hosted AI app').click())
      expect(host.textContent).toContain('https://agentworks.example.com/api/external/v1/mcp')
      expect(host.textContent).not.toContain('?token=aw_pat_test')
      expect(host.textContent).not.toContain('install-agentworks.sh')
      expect(host.textContent).not.toContain('MCP client config')
      const cowork = Array.from(host.querySelectorAll('button')).find(item => item.textContent === 'Claude Cowork')!
      await act(async () => cowork.click())
      expect(host.textContent).toContain('Settings → Connectors → Add custom connector')

      expect(window.localStorage.getItem(KEY)).toBe(JSON.stringify({ id: 'tok-1', token: 'aw_pat_test' }))
      await act(async () => button(host, 'Terminal or scripts').click())
      await act(async () => button(host, 'Revoke').click())
      expect(authApi.revokeAccessToken).toHaveBeenCalledWith('tok-1')
      expect(host.textContent).toContain('Create a connection to see the instructions')
      expect(host.textContent).not.toContain('aw_pat_test')
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
      expect(host.textContent).not.toContain('Create connection')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('falls back to connection creation when the stored token was revoked elsewhere', async () => {
    vi.mocked(getApiBaseUrl).mockReturnValue(SERVER)
    window.localStorage.setItem(KEY, JSON.stringify({ id: 'tok-gone', token: 'aw_pat_gone' }))
    vi.mocked(authApi.listAccessTokens).mockResolvedValue({ tokens: [] })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('Create a connection to see the instructions')
      expect(host.textContent).not.toContain('aw_pat_gone')
      expect(window.localStorage.getItem(KEY)).toBeNull()
      expect(authApi.createAccessToken).not.toHaveBeenCalled()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
