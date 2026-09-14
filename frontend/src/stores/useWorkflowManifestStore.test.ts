import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DiscoveredWorkflow, LLMProvider, WorkflowManifest } from '../services/api-types'

vi.mock('../services/api', () => ({
  workflowManifestApi: {},
}))

import { useWorkflowManifestStore } from './useWorkflowManifestStore'

const manifest = (provider: LLMProvider): WorkflowManifest => ({
  schema_version: 1,
  id: 'workflow-1',
  label: 'Workflow 1',
  capabilities: {
    selected_servers: [],
    selected_tools: [],
    selected_skills: [],
    selected_secrets: [],
    selected_global_secret_names: null,
    browser_mode: 'none',
    use_code_execution_mode: false,
    llm_config: {
      schema_version: 2,
      mode: 'provider_profile',
      provider,
    },
  },
  execution_defaults: {},
  ownership: { employee_id: null },
  schedules: [],
})

describe('useWorkflowManifestStore', () => {
  beforeEach(() => {
    useWorkflowManifestStore.setState({
      workflows: [{ workspace_path: '/tmp/workflow/', manifest: manifest('muse-cli') } as DiscoveredWorkflow],
      lastRefreshed: null,
    })
  })

  it('replaces the shared manifest immediately using normalized workspace paths', () => {
    useWorkflowManifestStore.getState().replaceWorkflowManifest('/tmp/workflow', manifest('claude-code'))

    expect(useWorkflowManifestStore.getState().getWorkflowByPath('/tmp/workflow/')?.manifest.capabilities.llm_config?.provider)
      .toBe('claude-code')
  })

  it('adds the authoritative manifest when the shared cache has not loaded yet', () => {
    useWorkflowManifestStore.setState({ workflows: [] })
    useWorkflowManifestStore.getState().replaceWorkflowManifest('/tmp/workflow', manifest('codex-cli'))

    expect(useWorkflowManifestStore.getState().getWorkflowByPath('/tmp/workflow')?.manifest.capabilities.llm_config?.provider)
      .toBe('codex-cli')
  })
})
