import { describe, expect, it, vi } from 'vitest'

const updatePlannerFile = vi.hoisted(() => vi.fn().mockResolvedValue({}))
const getPlannerFileContent = vi.hoisted(() => vi.fn())
const createPlannerFolder = vi.hoisted(() => vi.fn().mockResolvedValue({}))

vi.mock('../../services/api', () => ({
  agentApi: { createPlannerFolder, getPlannerFileContent, updatePlannerFile },
  getApiBaseUrl: () => '',
  getAuthToken: () => null,
}))

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({
    runtime: {
      provider_options: [{
        id: 'muse-cli',
        provider: 'muse-cli',
        model_id: 'muse-spark-1.3-contributor',
        options: { reasoning_effort: 'max' },
        default: true,
      }],
    },
  }),
}))

import { updateProductProjectLLMConfig, updateProductProjectSelections } from '../../platform/chat/productProjects'
import { createWorkSession, parseSessionManifest, sessionSlug, workLLMConfigFromSelection, workLLMSelectionFromConfig } from './workSessions'

describe('sessionSlug', () => {
  it('slugifies titles and falls back', () => {
    expect(sessionSlug('My Website!')).toBe('my-website')
    expect(sessionSlug('  ')).toBe('workspace')
  })
})

describe('parseSessionManifest', () => {
  const manifest = JSON.stringify({
    schema_version: 1,
    product: 'work',
    id: 'abc',
    title: 'Site',
    description: 'Build it',
    session_id: 'work:project:abc',
    created_at: '2026-09-13T00:00:00Z',
    updated_at: '2026-09-13T01:00:00Z',
    capabilities: {
      selected_servers: ['github'],
      selected_skills: ['code-reviewer'],
      selected_secrets: ['GITHUB_TOKEN'],
      workflow_context_paths: ['Workflow/reference'],
      llm_config: {
        schema_version: 2,
        mode: 'explicit',
        builder_llm: { provider: 'muse-cli', model_id: 'muse-spark-1.3-contributor' },
      },
    },
  })

  it('parses a valid manifest', () => {
    const session = parseSessionManifest(manifest, 'Chats/Work/projects/site-abc')
    expect(session?.id).toBe('abc')
    expect(session?.workspacePath).toBe('Chats/Work/projects/site-abc')
    expect(session?.sessionId).toBe('work:project:abc')
    expect(session?.llmConfig?.builder_llm?.provider).toBe('muse-cli')
    expect(session?.selectedServers).toEqual(['github'])
    expect(session?.selectedSkills).toEqual(['code-reviewer'])
    expect(session?.selectedSecrets).toEqual(['GITHUB_TOKEN'])
    expect(session?.secretSelectionInitialized).toBe(true)
    expect(session?.workflowContextPaths).toEqual(['Workflow/reference'])
  })

  it('rejects other products and incomplete manifests', () => {
    expect(parseSessionManifest('not json', 'w')).toBeNull()
    expect(parseSessionManifest(JSON.stringify({ ...JSON.parse(manifest), product: 'video-studio' }), 'w')).toBeNull()
    expect(parseSessionManifest(JSON.stringify({ ...JSON.parse(manifest), session_id: '' }), 'w')).toBeNull()
  })
})

describe('createWorkSession', () => {
  it('creates its own project folder without requiring a workspace selection', async () => {
    const session = await createWorkSession('New project', '', '🧭')

    expect(session.workspacePath).toMatch(/^Chats\/Work\/projects\/new-project-/)
    const runtimeCall = updatePlannerFile.mock.calls.find(call => call[0] === `${session.workspacePath}/workflow.json`)
    const productCall = updatePlannerFile.mock.calls.find(call => call[0] === `${session.workspacePath}/product.json`)
    expect(runtimeCall).toBeTruthy()
    expect(productCall).toBeTruthy()
    const runtime = JSON.parse(runtimeCall![1] as string)
    const product = JSON.parse(productCall![1] as string)
    expect(product).not.toHaveProperty('workspace_id')
    expect(product).not.toHaveProperty('capabilities')
    expect(product.identity).toEqual({ name: 'New project', icon: '🧭' })
    expect(session.identity).toEqual({ name: 'New project', icon: '🧭' })
    expect(runtime.capabilities.llm_config.builder_llm).toMatchObject({
      provider: 'muse-cli',
      model_id: 'muse-spark-1.3-contributor',
    })
    expect(runtime.capabilities.selected_servers).toEqual([])
    expect(runtime.capabilities.selected_skills).toEqual([])
    expect(runtime.capabilities.selected_secrets).toEqual([])
    expect(runtime.workflow_context_paths).toEqual([])
    expect(createPlannerFolder).toHaveBeenCalledWith(
      `${session.workspacePath}/code`,
      expect.stringContaining('Initialize Work project code folder'),
    )
  })
})

describe('updateProductProjectSelections', () => {
  it('persists Work runtime selections in workflow.json while preserving other capabilities', async () => {
    const project = parseSessionManifest(JSON.stringify({
      schema_version: 1,
      product: 'work',
      id: 'abc',
      title: 'Site',
      session_id: 'work:project:abc',
      capabilities: { custom_feature: { enabled: true } },
    }), 'Chats/Work/projects/site-abc')!
    getPlannerFileContent.mockResolvedValueOnce({
      success: true,
      data: { content: JSON.stringify({
        schema_version: 1,
        id: 'abc',
        label: 'Site',
        capabilities: { custom_feature: { enabled: true } },
      }) },
    })

    const updated = await updateProductProjectSelections(project, {
      selectedServers: ['google_sheets', 'google_sheets'],
      selectedSkills: ['work-dashboard'],
      selectedSecrets: ['GITHUB_TOKEN', 'GITHUB_TOKEN'],
      workflowContextPaths: ['Workflow/reference', 'Workflow/reference'],
    }, 'Update integrations', 'workflow.json')

    expect(updated.selectedServers).toEqual(['google_sheets'])
    expect(updated.selectedSkills).toEqual(['work-dashboard'])
    expect(updated.selectedSecrets).toEqual(['GITHUB_TOKEN'])
    expect(updated.workflowContextPaths).toEqual(['Workflow/reference'])
    const [path, content] = updatePlannerFile.mock.calls.at(-1)!
    expect(path).toBe(`${project.workspacePath}/workflow.json`)
    const manifest = JSON.parse(content as string)
    expect(manifest.capabilities.custom_feature).toEqual({ enabled: true })
    expect(manifest.capabilities.selected_servers).toEqual(['google_sheets'])
    expect(manifest.capabilities.selected_skills).toEqual(['work-dashboard'])
    expect(manifest.capabilities.selected_secrets).toEqual(['GITHUB_TOKEN'])
    expect(manifest.workflow_context_paths).toEqual(['Workflow/reference'])
  })
})

describe('updateProductProjectLLMConfig', () => {
  it('preserves other project capabilities while replacing the shared LLM configuration', async () => {
    const project = parseSessionManifest(JSON.stringify({
      schema_version: 1,
      product: 'work',
      id: 'abc',
      title: 'Site',
      session_id: 'work:project:abc',
      capabilities: { custom_feature: { enabled: true } },
    }), 'Chats/Work/projects/site-abc')!
    getPlannerFileContent.mockResolvedValueOnce({
      success: true,
      data: {
        content: JSON.stringify({
          schema_version: 1,
          id: 'abc',
          label: 'Site',
          capabilities: { custom_feature: { enabled: true } },
        }),
      },
    })

    const llmConfig = workLLMConfigFromSelection({
      provider: 'codex-cli',
      modelId: 'gpt-6-astra',
      reasoningEffort: 'high',
    })
    const updated = await updateProductProjectLLMConfig(project, llmConfig, 'Update model', 'workflow.json')

    expect(updated.llmConfig).toEqual(llmConfig)
    const [path, content] = updatePlannerFile.mock.calls.at(-1)!
    expect(path).toBe(`${project.workspacePath}/workflow.json`)
    const manifest = JSON.parse(content as string)
    expect(manifest.capabilities.custom_feature).toEqual({ enabled: true })
    expect(manifest.capabilities.llm_config).toEqual(llmConfig)
  })
})

describe('private account persistence',()=>{
 it('round-trips the selected account separately from the model',()=>{
 const selected={provider:'codex-cli' as const,modelId:'gpt-test',connectionId:'account-B'}
 const config=workLLMConfigFromSelection(selected)
 expect(config.builder_llm?.connection_id).toBe('account-B')
 expect(workLLMSelectionFromConfig(config)).toMatchObject(selected)
 })
})
