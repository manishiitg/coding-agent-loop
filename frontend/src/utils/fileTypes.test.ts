import { describe, expect, it } from 'vitest'
import {
  AUDIO_MIME_TYPES,
  VIDEO_MIME_TYPES,
  isAudioPath,
  isOfficeOrPdfPath,
  isTallSurfacePath,
  isVideoPath,
  mimeForExtension,
} from './fileTypes'

describe('fileTypes', () => {
  it('maps media extensions to MIME types, case-insensitively', () => {
    expect(mimeForExtension('clip.mp4', VIDEO_MIME_TYPES)).toBe('video/mp4')
    expect(mimeForExtension('CLIP.MOV', VIDEO_MIME_TYPES)).toBe('video/quicktime')
    expect(mimeForExtension('song.opus', AUDIO_MIME_TYPES)).toBe('audio/opus')
    expect(mimeForExtension('notes.md', VIDEO_MIME_TYPES)).toBeNull()
    expect(mimeForExtension('notes.md', AUDIO_MIME_TYPES)).toBeNull()
  })

  it('detects video and audio paths', () => {
    expect(isVideoPath('a/b.webm')).toBe(true)
    expect(isVideoPath('a/b.MP4')).toBe(true)
    expect(isVideoPath('a/b.mp3')).toBe(false)
    expect(isAudioPath('a/b.mp3')).toBe(true)
    expect(isAudioPath('a/b.ogg')).toBe(true)
    expect(isAudioPath('a/b.mp4')).toBe(false)
  })

  it('detects office documents and tall surfaces', () => {
    expect(isOfficeOrPdfPath('a/report.docx')).toBe(true)
    expect(isOfficeOrPdfPath('a/sheet.XLSX')).toBe(true)
    expect(isOfficeOrPdfPath('a/paper.pdf')).toBe(true)
    expect(isOfficeOrPdfPath('a/notes.md')).toBe(false)
    expect(isTallSurfacePath('a/paper.pdf')).toBe(true)
    expect(isTallSurfacePath('a/page.html')).toBe(true)
    expect(isTallSurfacePath('a/clip.mp4')).toBe(true)
    expect(isTallSurfacePath('a/song.mp3')).toBe(false)
    expect(isTallSurfacePath('a/notes.md')).toBe(false)
  })
})
