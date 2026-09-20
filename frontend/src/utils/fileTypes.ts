// Single source of truth for the file viewer's media/office extension lists.
// These used to be copied between the viewer's preview effects, its renderer
// dispatch, and the tall-surface check; they drifted more than once.

export const VIDEO_MIME_TYPES: Record<string, string> = {
  '.webm': 'video/webm',
  '.mov': 'video/quicktime',
  '.mp4': 'video/mp4',
}

export const AUDIO_MIME_TYPES: Record<string, string> = {
  '.wav': 'audio/wav',
  '.m4a': 'audio/mp4',
  '.aac': 'audio/aac',
  '.ogg': 'audio/ogg',
  '.oga': 'audio/ogg',
  '.flac': 'audio/flac',
  '.opus': 'audio/opus',
  '.mp3': 'audio/mpeg',
}

export function mimeForExtension(filePath: string, table: Record<string, string>): string | null {
  const lower = filePath.toLowerCase()
  for (const [ext, mime] of Object.entries(table)) {
    if (lower.endsWith(ext)) return mime
  }
  return null
}

export function isVideoPath(filePath: string): boolean {
  return mimeForExtension(filePath, VIDEO_MIME_TYPES) !== null
}

export function isAudioPath(filePath: string): boolean {
  return mimeForExtension(filePath, AUDIO_MIME_TYPES) !== null
}

const OFFICE_OR_PDF_EXTENSIONS = ['.xls', '.xlsx', '.docx', '.pdf']

export function isOfficeOrPdfPath(filePath: string): boolean {
  const lower = filePath.toLowerCase()
  return OFFICE_OR_PDF_EXTENSIONS.some(ext => lower.endsWith(ext))
}

// PDF, HTML, and video surfaces fill the pane instead of flowing as text.
export function isTallSurfacePath(filePath: string): boolean {
  const lower = filePath.toLowerCase()
  return (
    lower.endsWith('.pdf') ||
    lower.endsWith('.html') ||
    lower.endsWith('.htm') ||
    isVideoPath(lower)
  )
}
