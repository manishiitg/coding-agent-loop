// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
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
    savedLLMs: [],
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
})
