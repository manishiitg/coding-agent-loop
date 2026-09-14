import { describe, expect, it } from 'vitest'
import type { PlannerFile } from '../services/api-types'
import {
  EXPAND_FIRST_LEVEL_FOLDERS_BY_DEFAULT,
  getInitialExpandedWorkspaceFolders,
  isIterationFolder,
} from './workspacePathUtils'

describe('shared workspace folder defaults', () => {
  it('opens only the workspace root and keeps every first-level folder collapsed', () => {
    const root = {
      filepath: 'project',
      type: 'folder',
      children: [
        { filepath: 'project/.agents', type: 'folder' },
        { filepath: 'project/.codex', type: 'folder' },
        { filepath: 'project/frontend', type: 'folder' },
      ],
    } satisfies PlannerFile

    expect(EXPAND_FIRST_LEVEL_FOLDERS_BY_DEFAULT).toBe(false)
    expect([...getInitialExpandedWorkspaceFolders(root)]).toEqual(['project'])
    expect([...getInitialExpandedWorkspaceFolders(null)]).toEqual([])
  })
})

describe('trigger iteration paths', () => {
  it('recognizes hook and schedule roots and groups alongside ordinary runs', () => {
    for (const path of ['runs/iteration-1-hook', 'iteration-2-hook/dev', 'runs/iteration-3-sched', 'iteration-4-sched/measurement', 'iteration-0/dev']) {
      expect(isIterationFolder(path)).toBe(true)
    }
    expect(isIterationFolder('runs/iteration-1-hook/dev/execution')).toBe(false)
    expect(isIterationFolder('iteration-1-hook/../other')).toBe(false)
  })
})
