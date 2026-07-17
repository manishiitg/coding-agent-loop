import { describe, expect, it } from 'vitest'
import { buildPulseTimelineHtml } from './pulseTimelineHtml'

describe('buildPulseTimelineHtml', () => {
  it('installs a reusable module renderer without baking in one module', () => {
    const result = buildPulseTimelineHtml('<html><body><div class="wrap"></div></body></html>')

    expect(result).toContain('window.__runloopRenderPulseModule = render')
    expect(result).toContain("querySelectorAll('.pulse-record,.run,.entry')")
    expect(result).not.toContain("return 'bug_review'")
  })

  it('preserves the report and appends the filter before body close', () => {
    const result = buildPulseTimelineHtml('<html><body><div id="original">Pulse</div></body></html>')

    expect(result).toContain('<div id="original">Pulse</div>')
    expect(result.indexOf('__runloop_pulse_section_script')).toBeLessThan(result.indexOf('</body>'))
  })
})
