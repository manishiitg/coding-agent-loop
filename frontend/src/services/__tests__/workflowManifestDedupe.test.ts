// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'

// Cuts a load-order cycle (llm-config-api reads the api base URL at import time).
vi.mock('../llm-config-api', () => ({}))
import api, { workflowManifestApi } from '../api'

describe('workflow manifest fetch sharing', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shares one request between concurrent callers and refetches after a write', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, manifest: { id: 'wf' } } })
    vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })

    const results = await Promise.all([
      workflowManifestApi.getWorkflowManifest('Workflow/share-test'),
      workflowManifestApi.getWorkflowManifest('Workflow/share-test'),
      workflowManifestApi.getWorkflowManifest('Workflow/share-test'),
    ])
    expect(get).toHaveBeenCalledTimes(1)
    expect(results.every(r => r === results[0])).toBe(true)

    await workflowManifestApi.updateWorkflowManifest({ workspace_path: 'Workflow/share-test' } as never)
    await workflowManifestApi.getWorkflowManifest('Workflow/share-test')
    expect(get).toHaveBeenCalledTimes(2)
  })
})
