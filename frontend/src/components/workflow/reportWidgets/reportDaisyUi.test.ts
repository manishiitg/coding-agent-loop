import { describe, expect, it } from 'vitest'
import { withReportBootstrap } from './reportHostRuntime'

describe('daisyUI report runtime', () => {
  it('loads daisyUI from the CDN only for an opted-in report', () => {
    const enabled = withReportBootstrap('<html data-report-ui="daisyui"><head></head><body><div class="card"></div></body></html>')
    expect(enabled).toContain('id="__report_daisyui"')
    expect(enabled).toContain('data-version="5.7.38"')
    expect(enabled).toContain('https://cdn.jsdelivr.net/npm/daisyui@5.7.38/daisyui.css')

    const existing = withReportBootstrap('<html><head></head><body>Legacy report</body></html>')
    expect(existing).not.toContain('id="__report_daisyui"')
  })

  it('does not duplicate an explicit daisyUI CDN link', () => {
    const html = '<html data-report-ui="daisyui"><head><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/daisyui@5.7.38/daisyui.css"></head></html>'
    expect(withReportBootstrap(html).match(/cdn\.jsdelivr\.net\/npm\/daisyui/g)).toHaveLength(1)
  })
})
