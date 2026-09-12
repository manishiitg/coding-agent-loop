// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../services/llm-config-api', () => ({
  llmConfigService: { getProviderManifest: vi.fn(), getProviderModels: vi.fn() },
}))
vi.mock('../../stores/useAuthStore', () => ({
  useAuthStore: (selector: (state: { isMultiUserMode: boolean; user: null }) => unknown) => selector({ isMultiUserMode: false, user: null }),
}))

import { llmConfigService, type ProviderManifestEntry } from '../../services/llm-config-api'
import CodingProvidersPanel from './CodingProvidersPanel'
import { CODING_PROVIDER_GUIDES } from './codingProviderGuides'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

beforeEach(() => {
  vi.mocked(llmConfigService.getProviderModels).mockImplementation(async providerId => ({
    provider: providerId,
    model_selection_mode: 'fixed_tier',
    models: [],
    source: 'test',
  }))
})

const provider = (overrides: Partial<ProviderManifestEntry>): ProviderManifestEntry => ({
  id: 'codex-cli',
  display_name: 'Codex CLI',
  description: 'OpenAI coding agent',
  kind: 'local_cli',
  integration_kind: 'coding_agent',
  model_selection_mode: 'dynamic',
  auth_description: 'Codex login',
  runtime_command: 'codex',
  runtime_available: true,
  auth_configured: true,
  auth_source: 'Codex home',
  usable: true,
  requires_api_key: false,
  supports_dynamic_models: true,
  default_model_id: 'gpt-5.3-codex',
  models: [],
  capabilities: ['live_input', 'interrupt'],
  ...overrides,
})

afterEach(() => {
  vi.resetAllMocks()
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

describe('CodingProvidersPanel', () => {
  it('defines user-facing authentication guidance without server commands', () => {
    expect(Object.keys(CODING_PROVIDER_GUIDES)).toEqual([
      'claude-code',
      'codex-cli',
      'cursor-cli',
      'pi-cli',
      'muse-cli',
    ])
    for (const guide of Object.values(CODING_PROVIDER_GUIDES)) {
      expect(guide.authenticateNote).toBeTruthy()
      expect(guide.authenticateNote).not.toContain('npm install')
      expect(guide.authenticateNote).not.toContain('curl ')
    }
  })

  it('shows coding agents only and renders server status plus the four-step usage path', async () => {
    vi.mocked(llmConfigService.getProviderManifest).mockResolvedValue({
      providers: [
        provider({}),
        provider({
          id: 'claude-code',
          display_name: 'Claude Code',
          runtime_command: 'claude',
          runtime_available: false,
          auth_configured: false,
          auth_source: undefined,
          usable: false,
          setup_hint: 'Install Claude Code',
        }),
        provider({
          id: 'openai',
          display_name: 'OpenAI API',
          kind: 'api',
          integration_kind: 'api_model',
        }),
      ],
      provider_order: ['claude-code', 'codex-cli', 'openai'],
      integration_kinds: {},
    })

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<CodingProvidersPanel isOpen onClose={vi.fn()} />))
      await act(async () => Promise.resolve())

      const dialog = document.querySelector('[role="dialog"]')!
      expect(dialog.textContent).toContain('Providers')
      expect(dialog.textContent).toContain('Claude Code')
      expect(dialog.textContent).toContain('Codex CLI')
      expect(dialog.textContent).not.toContain('OpenAI API')
      expect(dialog.textContent).toContain('Not installed')
      expect(dialog.textContent).toContain('CLI availability')
      expect(dialog.textContent).toContain('Authenticate')
      expect(dialog.textContent).toContain('Verify the installation')
      expect(dialog.textContent).toContain('Use in a workflow')
      expect(dialog.textContent).toContain('missing from the AgentWorks installation')
      expect(dialog.textContent).not.toContain('npm install')

      await act(async () => Array.from(dialog.querySelectorAll('button')).find(button => button.textContent?.includes('Codex CLI'))!.click())
      expect(dialog.textContent).toContain('Authentication detected via Codex home')
      expect(dialog.textContent).toContain('Change sign-in')
      expect(dialog.textContent).toContain('Open terminal')
      expect(dialog.textContent).toContain('Type /status')
      expect(dialog.querySelector('[aria-label="Codex CLI is connected"]')).not.toBeNull()
      expect(Array.from(dialog.querySelectorAll('span')).filter(span => span.textContent === 'Connected')).toHaveLength(1)
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('expects deployment-installed Muse and offers guided sign-in only', async () => {
    vi.mocked(llmConfigService.getProviderManifest).mockResolvedValue({
      providers: [
        provider({
          id: 'muse-cli',
          display_name: 'Muse Code',
          runtime_command: 'muse',
          runtime_available: false,
          auth_configured: false,
          auth_source: undefined,
          usable: false,
          setup_hint: 'Install Muse Code',
        }),
      ],
      provider_order: ['muse-cli'],
      integration_kinds: {},
    })

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<CodingProvidersPanel isOpen onClose={vi.fn()} />))
      await act(async () => Promise.resolve())

      const dialog = document.querySelector('[role="dialog"]')!
      expect(dialog.textContent).toContain('Muse Code')
      expect(dialog.textContent).toContain('missing from the AgentWorks installation')
      expect(dialog.textContent).not.toContain('https://dev.meta.ai/install.sh')
      expect(dialog.textContent).not.toContain('Install on server')

      vi.mocked(llmConfigService.getProviderManifest).mockResolvedValue({
        providers: [provider({
          id: 'muse-cli',
          display_name: 'Muse Code',
          runtime_command: 'muse',
          runtime_available: true,
          auth_configured: false,
          auth_source: undefined,
          usable: false,
        })],
        provider_order: ['muse-cli'],
        integration_kinds: {},
      })
      const refresh = Array.from(dialog.querySelectorAll('button')).find(button => button.getAttribute('aria-label') === 'Refresh provider status')!
      await act(async () => refresh.click())
      await act(async () => Promise.resolve())

      expect(dialog.textContent).toContain('Start sign-in')
      expect(dialog.textContent).toContain('No SSH or direct server access is required')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('shows fixed provider models and Pi’s grouped live model inventory', async () => {
    vi.mocked(llmConfigService.getProviderModels).mockImplementation(async (providerId, _full, availableOnly) => ({
      provider: providerId,
      model_selection_mode: providerId === 'pi-cli' ? 'dynamic' : 'fixed_tier',
      models: providerId === 'claude-code' ? [
        { model_id: 'claude-sonnet-5', model_name: 'Claude Sonnet 5', is_default: true },
        { model_id: 'claude-opus-5', model_name: 'Claude Opus 5' },
      ] : providerId === 'pi-cli' && availableOnly ? [
        { model_id: 'google/gemini-3.8-flash', model_name: 'Gemini 3.8 Flash', group: 'Gemini', is_default: true },
        { model_id: 'openrouter/openrouter/free', model_name: 'OpenRouter Free', group: 'OpenRouter', is_free: true },
      ] : [],
      groups: providerId === 'pi-cli' ? ['Gemini', 'OpenRouter'] : undefined,
      source: 'test',
    }))
    vi.mocked(llmConfigService.getProviderManifest).mockResolvedValue({
      providers: [
        provider({
          id: 'claude-code',
          display_name: 'Claude Code',
          runtime_command: 'claude',
          default_model_id: 'claude-sonnet-5',
        }),
        provider({
          id: 'pi-cli',
          display_name: 'Pi CLI',
          runtime_command: 'pi',
          model_selection_mode: 'dynamic',
        }),
      ],
      provider_order: ['claude-code', 'pi-cli'],
      integration_kinds: {},
    })

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<CodingProvidersPanel isOpen onClose={vi.fn()} />))
      await act(async () => Promise.resolve())
      await act(async () => Promise.resolve())

      const dialog = document.querySelector('[role="dialog"]')!
      expect(dialog.querySelector('[data-testid="provider-model-catalog"]')).not.toBeNull()
      expect(dialog.textContent).toContain('Claude Sonnet 5')
      expect(dialog.textContent).toContain('Claude Opus 5')
      expect(dialog.querySelector('[aria-label="Claude Code is connected"]')).not.toBeNull()

      await act(async () => Array.from(dialog.querySelectorAll('button')).find(button => button.textContent?.includes('Pi CLI'))!.click())
      await act(async () => Promise.resolve())
      await act(async () => Promise.resolve())
      expect(dialog.querySelector('[data-testid="pi-provider-model-catalog"]')).not.toBeNull()
      expect(dialog.textContent).toContain('Connected model providers')
      expect(dialog.textContent).toContain('Gemini 1')
      expect(dialog.textContent).toContain('OpenRouter 1')
      expect(dialog.textContent).toContain('google/gemini-3.8-flash')
      expect(dialog.textContent).toContain('AgentWorks will not silently replace it')
      expect(dialog.textContent).toContain('Manage provider logins')
      expect(llmConfigService.getProviderModels).toHaveBeenCalledWith('pi-cli', false, true)
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
