// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SavedLLM } from '../../services/api-types'
import type { ProviderManifestEntry } from '../../services/llm-config-api'

const { storeState } = vi.hoisted(() => ({
  storeState: {
    availableLLMs: [],
    providerManifest: [] as ProviderManifestEntry[],
    providerManifestLoaded: true,
    loadProviderManifest: vi.fn(),
    defaultsLoaded: true,
    loadDefaultsFromBackend: vi.fn(),
    getProviderDynamicModels: vi.fn(),
    isProviderSupported: vi.fn(() => true),
    llmConfigLocked: false,
    lockedProviders: [],
    savedLLMs: [] as SavedLLM[],
    bedrockConfig: {},
    openaiConfig: {},
    vertexConfig: {},
    anthropicConfig: {},
    azureConfig: {},
    setBedrockConfig: vi.fn(),
    setOpenaiConfig: vi.fn(),
    setVertexConfig: vi.fn(),
    setAnthropicConfig: vi.fn(),
    setAzureConfig: vi.fn(),
    testAPIKey: vi.fn(),
  },
}))

vi.mock('../../stores/useLLMStore', () => ({
  useLLMStore: (selector: (state: typeof storeState) => unknown) => selector(storeState),
}))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({
  READ_ONLY_TITLE: 'Read only',
  useCanWriteWorkflow: () => true,
}))
vi.mock('../../services/llm-config-api', () => ({
  llmConfigService: { getModelMetadata: vi.fn(async () => ({ models: [] })) },
}))
vi.mock('../llm/CodingAgentSection', () => ({ CodingAgentSection: () => null }))
vi.mock('../llm/APIProviderSection', () => ({ APIProviderSection: () => null }))
vi.mock('../WorkflowProviderCredentialField', () => ({ WorkflowProviderCredentialField: () => null }))

import WorkflowLLMConfigurationPanel from './WorkflowLLMConfigurationPanel'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const provider = (overrides: Partial<ProviderManifestEntry>): ProviderManifestEntry => ({
  id: 'claude-code',
  display_name: 'Claude Code',
  description: 'Claude coding agent',
  kind: 'local_cli',
  integration_kind: 'coding_agent',
  model_selection_mode: 'fixed_tier',
  auth_description: 'Claude login',
  runtime_command: 'claude',
  runtime_available: true,
  auth_configured: true,
  usable: true,
  requires_api_key: false,
  supports_dynamic_models: false,
  default_model_id: 'claude-sonnet',
  default_tier_models: {
    high: { provider: 'claude-code', model_id: 'claude-sonnet' },
    medium: { provider: 'claude-code', model_id: 'claude-sonnet' },
    low: { provider: 'claude-code', model_id: 'claude-sonnet' },
  },
  models: [],
  capabilities: [],
  ...overrides,
})

afterEach(() => {
  storeState.providerManifest = []
  storeState.llmConfigLocked = false
  storeState.savedLLMs = []
  document.body.innerHTML = ''
})

describe('WorkflowLLMConfigurationPanel coding-agent rows', () => {
  it('shows authenticated CLIs as connected and keeps testing out of workflow selection', async () => {
    storeState.providerManifest = [
      provider({}),
      provider({
        id: 'codex-cli',
        display_name: 'OpenAI Codex CLI',
        runtime_command: 'codex',
        auth_configured: false,
        usable: false,
        setup_hint: 'Sign in from Providers',
        default_model_id: 'codex',
        default_tier_models: {
          high: { provider: 'codex-cli', model_id: 'codex' },
          medium: { provider: 'codex-cli', model_id: 'codex' },
          low: { provider: 'codex-cli', model_id: 'codex' },
        },
      }),
    ]

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkflowLLMConfigurationPanel workspacePath="/workflow" onChange={vi.fn()} />,
      ))
      await act(async () => Promise.resolve())

      expect(host.textContent).toContain('Claude Code')
      expect(host.textContent).toContain('Connected')
      expect(host.textContent).toContain('OpenAI Codex CLI')
      expect(host.textContent).toContain('Needs setup')
      expect(Array.from(host.querySelectorAll('button')).some(button => button.textContent?.trim() === 'Test')).toBe(false)
      expect(Array.from(host.querySelectorAll('button')).filter(button => button.textContent?.trim() === 'Use')).toHaveLength(2)
      expect(host.textContent).toContain('Authentication is managed in Providers')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('can show the complete coding CLI set while keeping Pi as one provider row', async () => {
    storeState.getProviderDynamicModels.mockResolvedValue({ models: [], groups: [] })
    storeState.providerManifest = [
      provider({}),
      provider({ id: 'codex-cli', display_name: 'OpenAI Codex CLI' }),
      provider({ id: 'cursor-cli', display_name: 'Cursor CLI' }),
      provider({ id: 'pi-cli', display_name: 'Pi CLI', supports_dynamic_models: true, model_selection_mode: 'dynamic' }),
      provider({ id: 'muse-cli', display_name: 'Muse' }),
    ]

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkflowLLMConfigurationPanel
          workspacePath="/work-project"
          onChange={vi.fn()}
          allowedProviderIds={['claude-code', 'codex-cli', 'cursor-cli', 'pi-cli', 'muse-cli']}
          splitPiProviders={false}
        />,
      ))
      await act(async () => Promise.resolve())

      expect(host.textContent).toContain('Claude Code')
      expect(host.textContent).toContain('OpenAI Codex CLI')
      expect(host.textContent).toContain('Cursor CLI')
      expect(host.textContent).toContain('Pi CLI')
      expect(host.textContent).toContain('Muse')
      expect(Array.from(host.querySelectorAll('button')).filter(button => button.textContent?.trim() === 'Use')).toHaveLength(5)
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('keeps a project profile selection editable under the global workflow lock', async () => {
    storeState.llmConfigLocked = true
    storeState.savedLLMs = [{ id: 'default', name: 'Cursor', provider: 'cursor-cli', model_id: 'auto' }]
    storeState.providerManifest = [
      provider({}),
      provider({ id: 'cursor-cli', display_name: 'Cursor CLI', default_model_id: 'auto' }),
    ]

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(
        <WorkflowLLMConfigurationPanel
          workspacePath="/work-project"
          llmConfig={{ schema_version: 2, mode: 'provider_profile', provider: 'claude-code' }}
          onChange={vi.fn()}
          scopeNoun="project"
          canWriteOverride
          allowedProviderIds={['claude-code', 'cursor-cli']}
          configurationSource="agent_profile"
        />,
      ))
      await act(async () => Promise.resolve())

      expect(host.textContent).toContain('This project runs onClaude Code')
      expect(host.textContent).not.toContain('Set by your administrator')
      expect(Array.from(host.querySelectorAll('button')).some(button => button.textContent?.trim() === 'Change provider')).toBe(true)
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
