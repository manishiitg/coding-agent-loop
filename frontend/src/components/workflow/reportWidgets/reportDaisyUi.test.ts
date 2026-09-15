import { describe, expect, it } from 'vitest'
import { withReportBootstrap } from './reportHostRuntime'

describe('daisyUI report runtime', () => {
  it('injects the bundled daisyUI stylesheet only for an opted-in report', () => {
    const enabled = withReportBootstrap('<html data-report-ui="daisyui"><head></head><body><div class="card"></div></body></html>')
    expect(enabled).toContain('id="__report_daisyui"')
    expect(enabled).toContain('data-version="5.7.38"')
    expect(enabled).not.toContain('cdn.jsdelivr.net')

    const existing = withReportBootstrap('<html><head></head><body>Legacy report</body></html>')
    expect(existing).not.toContain('id="__report_daisyui"')
  })
})
