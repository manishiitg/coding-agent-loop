import { describe, expect, it, vi } from 'vitest'
import { buildProfileAtFileTree, loadProfileAtFiles, profileRelativePath } from './profileAtFiles'
import type { PlannerFile } from '../services/api-types'

const root = 'Chats/Work/projects/sde-1'

describe('profileRelativePath', () => {
  it('relativizes logical and physical spellings of the crew root', () => {
    expect(profileRelativePath(root, 'Chats/Work/projects/sde-1/code/app.py')).toBe('code/app.py')
    expect(profileRelativePath(root, '_users/u1/Chats/Work/projects/sde-1/MEMORY.md')).toBe('MEMORY.md')
    expect(profileRelativePath('_users/owner/Chats/Work/projects/sde-1', '_users/owner/Chats/Work/projects/sde-1/db/x.sql')).toBe('db/x.sql')
  })
  it('rejects paths outside the crew and the root itself', () => {
    expect(profileRelativePath(root, 'Chats/Work/projects/other/a.md')).toBeNull()
    expect(profileRelativePath(root, 'Workflow/w/plan.json')).toBeNull()
    expect(profileRelativePath(root, root)).toBeNull()
  })
})

describe('buildProfileAtFileTree', () => {
  it('builds a crew-relative tree from a nested listing and skips heavy folders', () => {
    const listing: PlannerFile[] = [
      { filepath: `${root}/code`, type: 'folder', children: [
        { filepath: `${root}/code/app.py`, type: 'file' },
        { filepath: `${root}/code/node_modules`, type: 'folder', children: [{ filepath: `${root}/code/node_modules/x.js`, type: 'file' }] },
      ] },
      { filepath: `${root}/MEMORY.md`, type: 'file' },
    ]
    const tree = buildProfileAtFileTree(root, listing)
    expect(tree.map(f => f.filepath)).toEqual(['code', 'MEMORY.md'])
    expect(tree[0].children?.map(f => f.filepath)).toEqual(['code/app.py'])
  })
  it('builds parent folders from a flat shared-crew listing', () => {
    const tree = buildProfileAtFileTree('_users/o/Chats/Work/projects/c', [
      { filepath: '_users/o/Chats/Work/projects/c/reports/q3/summary.md', type: 'file' },
    ])
    expect(tree[0]).toMatchObject({ filepath: 'reports', type: 'folder' })
    expect(tree[0].children?.[0]).toMatchObject({ filepath: 'reports/q3', type: 'folder' })
    expect(tree[0].children?.[0].children?.[0]).toMatchObject({ filepath: 'reports/q3/summary.md', type: 'file' })
  })
})

describe('loadProfileAtFiles', () => {
  it('lists the crew root and falls back to the shared client when refused', async () => {
    const primary = { listFiles: vi.fn().mockRejectedValue(new Error('403')) }
    const fallback = { listFiles: vi.fn().mockResolvedValue({ success: true, data: [{ filepath: '_users/o/Chats/Work/projects/c/a.md', type: 'file' }] }) }
    const files = await loadProfileAtFiles('_users/o/Chats/Work/projects/c', primary, fallback)
    expect(primary.listFiles).toHaveBeenCalledWith('_users/o/Chats/Work/projects/c', -1, expect.any(Number))
    expect(files.map(f => f.filepath)).toEqual(['a.md'])
  })
  it('uses the workspace listing for an owned crew', async () => {
    const primary = { listFiles: vi.fn().mockResolvedValue({ success: true, data: [{ filepath: `${root}/notes.md`, type: 'file' }] }) }
    expect((await loadProfileAtFiles(root, primary)).map(f => f.filepath)).toEqual(['notes.md'])
  })
})
