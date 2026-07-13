import { describe, expect, it } from 'vitest'
import type { PlannerFile } from '../../services/api-types'
import {
  flattenVisiblePlannerFiles,
  WORKSPACE_SCROLL_TO_FILE_EVENT,
} from '../../utils/plannerFileTree'

const file = (filepath: string, type: 'file' | 'folder' = 'file', children?: PlannerFile[]): PlannerFile => ({
  filepath,
  type,
  children,
})

describe('flattenVisiblePlannerFiles', () => {
  it('uses a stable event name for virtual row navigation', () => {
    expect(WORKSPACE_SCROLL_TO_FILE_EVENT).toBe('workspace-scroll-to-file')
  })

  it('sorts folders before files without mutating backend arrays', () => {
    const children = [file('root/z.txt'), file('root/a', 'folder')]
    const files = [file('z.txt'), file('root', 'folder', children), file('a.txt')]

    const rows = flattenVisiblePlannerFiles(files, new Set(['root']), false)

    expect(rows.map(row => [row.file.filepath, row.depth])).toEqual([
      ['root', 0],
      ['root/a', 1],
      ['root/z.txt', 1],
      ['a.txt', 0],
      ['z.txt', 0],
    ])
    expect(files.map(item => item.filepath)).toEqual(['z.txt', 'root', 'a.txt'])
    expect(children.map(item => item.filepath)).toEqual(['root/z.txt', 'root/a'])
  })

  it('only includes descendants of expanded folders', () => {
    const files = [file('root', 'folder', [file('root/child.txt')])]

    expect(flattenVisiblePlannerFiles(files, new Set(), false)).toHaveLength(1)
    expect(flattenVisiblePlannerFiles(files, new Set(), true)).toHaveLength(2)
  })
})
