// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('../../services/api', () => ({
  default: { get: vi.fn(), delete: vi.fn() },
  externalSkillApi: { downloadSkillZIP: vi.fn(), fetchSkillMD: vi.fn(), downloadCoworkPlugin: vi.fn() },
  getApiBaseUrl: vi.fn().mockReturnValue('https://agentworks.example.com'),
}))
import api, { externalSkillApi, getApiBaseUrl } from '../../services/api'
import { CliMcpSetupPanel } from './CliMcpSetupPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
beforeEach(() => {
  vi.mocked(api.get).mockResolvedValue({ data: { connections: [] } })
  vi.mocked(getApiBaseUrl).mockReturnValue('https://agentworks.example.com')
})
afterEach(() => vi.resetAllMocks())

async function renderPanel(host: HTMLElement) {
  const root = createRoot(host)
  await act(async () => root.render(<CliMcpSetupPanel />))
  return root
}
function button(host: HTMLElement, label: string) {
  const found = Array.from(host.querySelectorAll('button')).find(item => item.textContent?.includes(label))
  if (!found) throw new Error(`Missing button: ${label}`)
  return found
}

describe('CLI and MCP setup', () => {
  it('installs and signs in through the browser without a token', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain("install-agentworks.sh' | sh -s -- --server 'https://agentworks.example.com'")
      expect(host.textContent).not.toContain('Create connection')
      expect(host.textContent).not.toContain('aw_pat_')
      await act(async () => button(host, 'AI app on this computer').click())
      expect(host.textContent).toContain("claude mcp add agentworks -e AGENTWORKS_SERVER='https://agentworks.example.com' -- agentworks mcp serve")
      expect(host.textContent).not.toContain('AGENTWORKS_TOKEN')
      await act(async () => Array.from(host.querySelectorAll('button')).find(item => item.textContent === 'Codex')!.click())
      expect(host.textContent).toContain("codex mcp add agentworks --env AGENTWORKS_SERVER='https://agentworks.example.com' -- agentworks mcp serve")
      await act(async () => button(host, 'JSON MCP client').click())
      expect(host.querySelector('pre')?.textContent).toContain('"AGENTWORKS_SERVER"')
      expect(host.querySelector('pre')?.textContent).not.toContain('AGENTWORKS_TOKEN')
      await act(async () => button(host, 'Hosted AI app').click())
      expect(host.textContent).toContain('https://agentworks.example.com/api/external/v1/mcp')
      expect(host.textContent).toContain('Choose OAuth')
      expect(host.textContent).not.toContain('install-agentworks.sh')
      await act(async () => Array.from(host.querySelectorAll('button')).find(item => item.textContent === 'Claude Cowork')!.click())
      expect(host.textContent).toContain('Download Cowork plugin')
      expect(host.textContent).toContain('Customize → Plugins')
      expect(host.textContent).toContain('Connect manually instead')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('lists and revokes browser connections without exposing credentials', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: { connections: [{ id: 'family-1', client_name: 'AgentWorks CLI', scopes: ['workflows:read'], expires_at: '2099-01-01' }] } })
    vi.mocked(api.delete).mockResolvedValue({})
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('AgentWorks CLI')
      await act(async () => button(host, 'Revoke').click())
      expect(api.delete).toHaveBeenCalledWith('/api/oauth/mcp/connections/family-1')
      expect(host.textContent).not.toContain('AgentWorks CLI')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('downloads the Cowork plugin from the server', async () => {
    vi.mocked(externalSkillApi.downloadCoworkPlugin).mockRejectedValue(new Error('offline'))
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      await act(async () => button(host, 'Hosted AI app').click())
      await act(async () => Array.from(host.querySelectorAll('button')).find(item => item.textContent === 'Claude Cowork')!.click())
      await act(async () => button(host, 'Download Cowork plugin').click())
      expect(externalSkillApi.downloadCoworkPlugin).toHaveBeenCalledOnce()
      expect(host.textContent).toContain('Plugin download failed')
    } finally { await act(async () => root.unmount()); host.remove() }
  })

  it('keeps setup usable when an older server returns HTML for connected apps', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: '<!doctype html><html></html>' })
    const host = document.createElement('div'); document.body.append(host); const root = await renderPanel(host)
    try {
      expect(host.textContent).toContain('install-agentworks.sh')
      expect(host.textContent).toContain('Restart or update the AgentWorks server')
      expect(host.textContent).not.toContain('Revoke')
    } finally { await act(async () => root.unmount()); host.remove() }
  })
})
