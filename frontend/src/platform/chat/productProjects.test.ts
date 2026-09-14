import { describe, expect, it, vi } from 'vitest'

vi.mock('../../services/api', () => ({ agentApi: {} }))

import { parseProductProjectManifest } from './productProjects'

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
