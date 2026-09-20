import { describe, expect, it } from 'vitest'
import { isImageIcon } from './entityIcon'

describe('isImageIcon', () => {
  it('detects uploaded image data URLs', () => {
    expect(isImageIcon('data:image/png;base64,AAAA')).toBe(true)
    expect(isImageIcon('data:image/jpeg;base64,AAAA')).toBe(true)
    expect(isImageIcon('data:image/gif;base64,AAAA')).toBe(true)
    expect(isImageIcon('data:image/webp;base64,AAAA')).toBe(true)
  })

  it('rejects emoji, empty, and non-image values', () => {
    expect(isImageIcon('🚀')).toBe(false)
    expect(isImageIcon('')).toBe(false)
    expect(isImageIcon(null)).toBe(false)
    expect(isImageIcon(undefined)).toBe(false)
    expect(isImageIcon('data:image/png;base64,')).toBe(false)
    expect(isImageIcon('data:text/plain;base64,AAAA')).toBe(false)
    expect(isImageIcon('https://example.com/icon.png')).toBe(false)
  })
})
