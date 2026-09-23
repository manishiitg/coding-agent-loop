import { describe, expect, it } from 'vitest'
import { withPreviewPositionScript } from './previewPosition'

describe('inline activity preview scroll bridge', () => {
  it('allows the test\'s inline button script while blocking external scripts and restoring position', () => {
    const html = '<!doctype html><html><head><script>window.generated = true</script></head><body>Activity</body></html>'
    const result = withPreviewPositionScript(html, 243.6)

    expect(result.indexOf('Content-Security-Policy')).toBeLessThan(result.indexOf('window.generated'))
    expect(result).toContain("script-src 'unsafe-inline'")
    expect(result).toContain('<script>')
    expect(result).toContain('var savedY = 244;')
    expect(result).toContain("op: 'preview-scroll'")
  })

  it('adds an early policy when the page has no head', () => {
    const html = '<html><body><script>window.generated = true</script></body></html>'
    const result = withPreviewPositionScript(html, 0)
    expect(result.indexOf('Content-Security-Policy')).toBeLessThan(result.indexOf('window.generated'))
  })
})
