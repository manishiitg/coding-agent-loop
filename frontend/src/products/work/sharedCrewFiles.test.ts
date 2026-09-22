import { describe, expect, it, vi } from 'vitest'

const listSharedProjectFiles = vi.hoisted(() => vi.fn())
const getSharedProjectFile = vi.hoisted(() => vi.fn())

vi.mock('../../services/api', () => ({
  agentApi: { listSharedProjectFiles, getSharedProjectFile },
  getApiBaseUrl: () => '',
  getAuthToken: () => null,
}))

import { sharedCrewFileClient, sharedCrewRelativePath } from './sharedCrewFiles'

const ROOT = '_users/owner-1/Chats/Work/projects/crew-shared'

describe('sharedCrewRelativePath', () => {
  it('relativizes paths under the crew root and rejects escapes', () => {
    expect(sharedCrewRelativePath(ROOT, `${ROOT}/skills/a.md`)).toBe('skills/a.md')
    expect(sharedCrewRelativePath(ROOT, ROOT)).toBe('')
    expect(sharedCrewRelativePath(ROOT, 'Chats/Work/projects/other/file.md')).toBeNull()
    expect(sharedCrewRelativePath(ROOT, `${ROOT}-other/file.md`)).toBeNull()
  })
})

describe('sharedCrewFileClient', () => {
  it('re-prefixes the crew-relative tree and scopes listings to the requested folder', async () => {
    listSharedProjectFiles.mockResolvedValue({
      files: [
        { path: 'MEMORY.md', type: 'file' },
        { path: 'skills', type: 'folder' },
        { path: 'skills/custom/deep.md', type: 'file' },
        { path: 'skills/review.md', type: 'file' },
      ],
    })
    const client = sharedCrewFileClient('crew-shared', ROOT)

    const scoped = await client.listFiles(`${ROOT}/skills`, -1, 4)

    expect(listSharedProjectFiles).toHaveBeenCalledWith('work', 'crew-shared')
    expect(scoped.success).toBe(true)
    expect(scoped.data.map(file => file.filepath)).toEqual([
      `${ROOT}/skills`,
      `${ROOT}/skills/custom/deep.md`,
      `${ROOT}/skills/review.md`,
    ])
    expect(scoped.data.find(file => file.filepath === `${ROOT}/skills`)?.type).toBe('folder')
  })

  it('reads crew-relative and rejects paths outside the crew root without calling the server', async () => {
    getSharedProjectFile.mockResolvedValue({ path: 'MEMORY.md', content: '# memory' })
    const client = sharedCrewFileClient('crew-shared', ROOT)

    const response = await client.readFile(`${ROOT}/MEMORY.md`)

    expect(getSharedProjectFile).toHaveBeenCalledWith('work', 'crew-shared', 'MEMORY.md')
    expect(response.success).toBe(true)
    expect(response.data.content).toBe('# memory')

    const outside = await client.readFile('Chats/Work/projects/other/file.md')
    expect(outside.success).toBe(false)
    expect(getSharedProjectFile).toHaveBeenCalledTimes(1)
  })

  it('mirrors the proxy 404 contract for missing files', async () => {
    getSharedProjectFile.mockRejectedValue({ response: { status: 404 } })
    const client = sharedCrewFileClient('crew-shared', ROOT)

    const failure = await client.readFile(`${ROOT}/missing.md`).catch(cause => cause)
    expect(failure).toBeInstanceOf(Error)
    expect((failure as { response?: { status?: number } }).response?.status).toBe(404)
  })
})
