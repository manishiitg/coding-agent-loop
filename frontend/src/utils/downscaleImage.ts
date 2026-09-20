// Downscale an uploaded icon file to a small thumbnail data URL for inline
// manifest storage. Keeps transparency (PNG); falls back to JPEG when the
// PNG is still large. Rejects non-image files before touching canvas.
export const ICON_THUMBNAIL_EDGE_PX = 128
export const ICON_THUMBNAIL_JPEG_FALLBACK_CHARS = 48 * 1024

function loadImageElement(source: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('That file could not be read as an image.'))
    image.src = source
  })
}

export async function downscaleIconFile(file: File): Promise<string> {
  if (!file.type.startsWith('image/')) {
    throw new Error('Choose an image file (PNG, JPG, GIF, or WebP).')
  }
  if (typeof document === 'undefined' || typeof document.createElement !== 'function') {
    throw new Error('Image uploads are not available here.')
  }
  const source = await fileToDataURL(file)
  const image = await loadImageElement(source)
  const width = image.naturalWidth || image.width
  const height = image.naturalHeight || image.height
  if (!width || !height) throw new Error('That file could not be read as an image.')
  const scale = Math.min(1, ICON_THUMBNAIL_EDGE_PX / Math.max(width, height))
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(width * scale))
  canvas.height = Math.max(1, Math.round(height * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error('Image uploads are not available here.')
  context.drawImage(image, 0, 0, canvas.width, canvas.height)
  const png = canvas.toDataURL('image/png')
  if (png.length <= ICON_THUMBNAIL_JPEG_FALLBACK_CHARS) return png
  return canvas.toDataURL('image/jpeg', 0.85)
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === 'string') resolve(reader.result)
      else reject(new Error('That file could not be read as an image.'))
    }
    reader.onerror = () => reject(new Error('That file could not be read as an image.'))
    reader.readAsDataURL(file)
  })
}
