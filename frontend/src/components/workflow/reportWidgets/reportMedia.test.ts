import { describe, expect, it } from 'vitest'
import { isStreamableReportMediaPath } from './reportMedia'

describe('isStreamableReportMediaPath', () => {
  it.each([
    'db/assets/run.webm',
    'db/assets/run.MP4',
    'db/assets/audio.m4a',
    'db/assets/audio.ogg',
  ])('recognizes streamable report media: %s', path => {
    expect(isStreamableReportMediaPath(path)).toBe(true)
  })

  it.each([
    'db/assets/screenshot.png',
    'db/reports/result.pdf',
    'db/assets/video.webm.json',
  ])('keeps non-media files on the normal file path: %s', path => {
    expect(isStreamableReportMediaPath(path)).toBe(false)
  })
})
