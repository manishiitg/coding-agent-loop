import { describe, expect, it } from 'vitest'
import { isWorkspaceRelativeReference, rewriteReportMarkdownReferences } from './reportMarkdownLinks'

const allow = (path: string) => (path.startsWith('db/') || path.startsWith('dashboard/') || path.startsWith('knowledgebase/') ? path : '')

describe('isWorkspaceRelativeReference', () => {
  it('is true only for a bare relative path', () => {
    expect(isWorkspaceRelativeReference('db/assets/x.png')).toBe(true)
    expect(isWorkspaceRelativeReference('./notes.md')).toBe(true)
    expect(isWorkspaceRelativeReference('https://x.test/a')).toBe(false)
    expect(isWorkspaceRelativeReference('mailto:a@b.c')).toBe(false)
    expect(isWorkspaceRelativeReference('data:image/png;base64,AAA')).toBe(false)
    expect(isWorkspaceRelativeReference('#section')).toBe(false)
    expect(isWorkspaceRelativeReference('/absolute')).toBe(false)
    expect(isWorkspaceRelativeReference('')).toBe(false)
  })
})

describe('rewriteReportMarkdownReferences', () => {
  it('routes workspace links through the preview and images through the blob channel', () => {
    const html = '<p><a href="db/reports/proof.pdf">proof</a> <img src="db/assets/chart.png" alt="c"></p>'
    const out = rewriteReportMarkdownReferences(html, allow)
    expect(out).toContain('data-report-open="db/reports/proof.pdf"')
    expect(out).toContain('href="#"')
    expect(out).toContain('data-report-src="db/assets/chart.png"')
    // Substring check must not false-positive on "...report-src=..." above.
    expect(out).not.toMatch(/(?<!-)\bsrc="db\/assets\/chart\.png"/)
  })

  it('leaves external links, anchors and data URIs alone', () => {
    const html = '<a href="https://x.test">x</a><a href="#top">top</a><img src="data:image/png;base64,AAA">'
    expect(rewriteReportMarkdownReferences(html, allow)).toBe(html)
  })

  it('leaves a reference the allow-list rejects untouched', () => {
    const html = '<a href="runs/iteration-1/out.txt">scratch</a>'
    expect(rewriteReportMarkdownReferences(html, allow)).toBe(html)
  })

  it('allows Work-native dashboard files', () => {
    const out = rewriteReportMarkdownReferences('<a href="dashboard/notes.md">notes</a>', allow)
    expect(out).toContain('data-report-open="dashboard/notes.md"')
  })

  it('resolves file-relative references against the markdown file folder, root-relative ones as-is', () => {
    const html = '<img src="./chart.png"><img src="assets/b.png"><a href="db/notes/other.md">o</a>'
    const out = rewriteReportMarkdownReferences(html, allow, 'db/notes')
    expect(out).toContain('data-report-src="db/notes/chart.png"')
    expect(out).toContain('data-report-src="db/notes/assets/b.png"')
    expect(out).toContain('data-report-open="db/notes/other.md"')
  })

  it('drops a query string or fragment from the resolved path', () => {
    const out = rewriteReportMarkdownReferences('<a href="db/notes/a.md#h2?x=1">a</a>', allow)
    expect(out).toContain('data-report-open="db/notes/a.md"')
  })

  it('decodes an HTML-entity-escaped href before resolving', () => {
    // renderToStaticMarkup escapes & in attribute values.
    const out = rewriteReportMarkdownReferences('<a href="db/notes/a.md?x=1&amp;y=2">a</a>', allow)
    expect(out).toContain('data-report-open="db/notes/a.md"')
  })

  it('preserves a self-closing img tag shape and other attributes/order', () => {
    const out = rewriteReportMarkdownReferences('<p>before</p><img alt="c" src="db/assets/chart.png" title="t"/><p>after</p>', allow)
    expect(out).toContain('<p>before</p>')
    expect(out).toContain('<p>after</p>')
    expect(out).toContain('alt="c"')
    expect(out).toContain('title="t"')
    expect(out).toContain('data-report-src="db/assets/chart.png"')
    expect(out.match(/<img[^>]*>/)?.[0]).toMatch(/\/>$/)
  })

  it('leaves an unrelated <a>/<img> without href/src untouched', () => {
    const html = '<a name="x">anchor</a><img alt="no src">'
    expect(rewriteReportMarkdownReferences(html, allow)).toBe(html)
  })
})
