// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { downscaleIconFile } from './downscaleImage'

describe('downscaleIconFile', () => {
  it('rejects non-image files without touching canvas', async () => {
    const file = new File(['hello'], 'notes.txt', { type: 'text/plain' })
    await expect(downscaleIconFile(file)).rejects.toThrow('Choose an image file')
  })
})
