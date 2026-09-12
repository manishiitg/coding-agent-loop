import { describe, expect, it } from 'vitest'
import { isIterationFolder } from './workspacePathUtils'

describe('webhook iteration paths', () => {
  it('recognizes hook roots and groups alongside ordinary runs', () => {
    for (const path of ['runs/iteration-1-hook', 'iteration-2-hook/dev', 'runs/iteration-3-hook/dev', 'iteration-0/dev']) {
      expect(isIterationFolder(path)).toBe(true)
    }
    expect(isIterationFolder('runs/iteration-1-hook/dev/execution')).toBe(false)
    expect(isIterationFolder('iteration-1-hook/../other')).toBe(false)
  })
})
