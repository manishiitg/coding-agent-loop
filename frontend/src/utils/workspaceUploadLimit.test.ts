import { describe, expect, it } from 'vitest'
import { MAX_WORKSPACE_UPLOAD_BYTES, workspaceUploadSizeError } from './workspaceUploadLimit'

describe('workspace upload size limit', () => {
  it('allows a file at 10 MB', () => {
    expect(workspaceUploadSizeError({ size: MAX_WORKSPACE_UPLOAD_BYTES })).toBeNull()
  })

  it('rejects a file larger than 10 MB', () => {
    expect(workspaceUploadSizeError({ size: MAX_WORKSPACE_UPLOAD_BYTES + 1 })).toContain('10 MB')
  })
})
