import { beforeEach, describe, expect, it, vi } from 'vitest'

const getPlannerFileContent = vi.hoisted(() => vi.fn())
const updatePlannerFile = vi.hoisted(() => vi.fn().mockResolvedValue({}))

vi.mock('../../services/api', () => ({ agentApi: { getPlannerFileContent, updatePlannerFile } }))

import { agentApi } from '../../services/api'
import { parseProductProjectManifest, updateProductProjectIdentity, type ProductProject } from './productProjects'

describe('parseProductProjectManifest', () => {
  it('loads the project bot identity and icon', () => {
    const project = parseProductProjectManifest(JSON.stringify({
      schema_version: 1,
      product: 'work',
      id: 'project-1',
      title: 'Launch',
      description: 'Launch workspace',
      session_id: 'work:project:1',
      identity: {
        icon: '🚀',
        name: 'Nova',
        role: 'Launch partner',
        instructions: 'Keep decisions concise.',
      },
      capabilities: { selected_servers: [], selected_skills: [] },
    }), 'Chats/Work/projects/launch', 'work')

    expect(project?.identity).toEqual({
      icon: '🚀',
      name: 'Nova',
      role: 'Launch partner',
      instructions: 'Keep decisions concise.',
    })
  })
})

describe('updateProductProjectIdentity', () => {
  const project = {
    product: 'work',
    id: 'project-1',
    title: 'Launch',
    workspacePath: 'Chats/Work/projects/launch',
  } as ProductProject<'work'>

  beforeEach(() => {
    vi.clearAllMocks()
  })

  function mockManifest(identity: unknown) {
    getPlannerFileContent.mockResolvedValue({
      content: JSON.stringify({ schema_version: 1, identity }),
    })
  }

  it('merges the patch into product.json and preserves omitted fields', async () => {
    mockManifest({ icon: '🚀', name: 'Nova', role: 'Launch partner', instructions: 'Keep it short.' })
    const updated = await updateProductProjectIdentity(project, { name: '  Stella  ' }, 'Update identity')

    expect(agentApi.getPlannerFileContent).toHaveBeenCalledWith('Chats/Work/projects/launch/product.json')
    const written = JSON.parse(updatePlannerFile.mock.calls[0][1])
    expect(written.identity).toEqual({ icon: '🚀', name: 'Stella', role: 'Launch partner', instructions: 'Keep it short.' })
    expect(updated.identity?.name).toBe('Stella')
  })

  it('removes emptied fields and drops an emptied identity', async () => {
    mockManifest({ icon: '🚀', name: 'Nova' })
    await updateProductProjectIdentity(project, { icon: '', name: '' }, 'Update identity')

    const written = JSON.parse(updatePlannerFile.mock.calls[0][1])
    expect('identity' in written).toBe(false)
  })

  it('rejects invalid project configuration', async () => {
    getPlannerFileContent.mockResolvedValue({ content: 'not json' })
    await expect(updateProductProjectIdentity(project, { name: 'Nova' }, 'Update identity')).rejects.toThrow('invalid JSON')
  })
})
