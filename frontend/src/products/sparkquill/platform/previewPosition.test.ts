import { describe, expect, it } from 'vitest'
import { withPreviewPositionScript } from './previewPosition'

describe('inline activity preview scroll bridge', () => {
  it('puts its script policy before generated scripts and restores the prior position', () => {
    const html = '<!doctype html><html><head><script>window.generated = true</script></head><body>Activity</body></html>'
    const result = withPreviewPositionScript(html, 243.6, 'test-nonce')

    expect(result.indexOf('Content-Security-Policy')).toBeLessThan(result.indexOf('window.generated'))
    expect(result).toContain("script-src 'nonce-test-nonce'")
    expect(result).toContain('<script nonce="test-nonce">')
    expect(result).toContain('var savedY = 244;')
    expect(result).toContain("op: 'preview-scroll'")
  })

  it('adds an early policy when the page has no head', () => {
    const html = '<html><body><script>window.generated = true</script></body></html>'
    const result = withPreviewPositionScript(html, 0, 'other-nonce')
    expect(result.indexOf('Content-Security-Policy')).toBeLessThan(result.indexOf('window.generated'))
  })
})
