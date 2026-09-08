import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('open report stability', () => {
  it('loads one workflow-owned HTML reporting experience', () => {
    const source = readFileSync('src/components/workflow/ReportViewer.tsx', 'utf8')

    expect(source).toContain('db/reports/index.html')
    expect(source).toContain('loadReportDocument')
    expect(source).not.toContain('ReportPageNavigation')
    expect(source).not.toContain('report_plan.json')
  })

  it('keeps report reload behind the explicit toolbar refresh action', () => {
    const canvas = readFileSync('src/components/workflow/canvas/WorkflowCanvas.tsx', 'utf8')
    const viewer = readFileSync('src/components/workflow/ReportViewer.tsx', 'utf8')

    expect(viewer).toContain('onClick={refresh}')
    expect(viewer).toContain('aria-label="Refresh report"')
    expect(canvas).not.toContain('window.dispatchEvent(new CustomEvent(WORKFLOW_REPORT_REFRESH_EVENT))')
  })

  it('mints streaming media URLs through the real API route in web deployments', () => {
    const viewer = readFileSync('src/components/workflow/ReportViewer.tsx', 'utf8')

    expect(viewer).toContain("api.post('/api/workflow/report-preview/media-url'")
    expect(viewer).not.toContain("api.post('/workflow/report-preview/media-url'")
  })

  it('does not let outer polling remount the report iframe', () => {
    const viewer = readFileSync('src/components/workflow/ReportViewer.tsx', 'utf8')
    const frame = readFileSync('src/components/workflow/reportWidgets/HtmlWidgetFrame.tsx', 'utf8')

    expect(viewer).toContain('refreshToken={refreshNonce}')
    expect(frame).toContain('export const HtmlReportFrame = memo(HtmlReportFrameComponent)')
    expect(frame).toContain('const refreshRequested = injectedRefreshTokenRef.current !== refreshToken')
    expect(frame).toContain('useLayoutEffect(() => {')
    // The invariant is that srcDoc is assigned IMPERATIVELY (so an outer polling
    // re-render cannot make Chromium treat it as a navigation and restart the
    // report), not that the assigned expression is the bare `html` variable —
    // it is now wrapped by withReportBootstrap() to prepend the report.ready()
    // stub. Assert the imperative assignment and the absence of the declarative
    // prop, which is what actually protects against the remount.
    expect(frame).toContain('frame.srcdoc = ')
    expect(frame).toContain('withReportBootstrap(html)')
    expect(frame).not.toContain('srcDoc={html}')
  })

  it('does not fight native scrolling with a corrective reset loop', () => {
    const viewer = readFileSync('src/components/workflow/ReportViewer.tsx', 'utf8')

    expect(viewer).toContain('aria-label="Report content"')
    expect(viewer).not.toContain('unexpected scroll reset restored')
    expect(viewer).not.toContain('onScroll={handleReportScroll}')
  })

  it('keeps decision-status refreshes visually silent when nothing changed', () => {
    const panel = readFileSync('src/components/workflow/ReportHumanInputPanel.tsx', 'utf8')

    expect(panel).toContain('keepPreviousInputsWhenUnchanged')
    expect(panel).toContain('const onRefresh = () => { void loadInputs(undefined, false) }')
    expect(panel).toContain('if (showLoading) setLoading(true)')
  })

  it('does not offer an arbitrary fourth answer for option-backed decisions', () => {
    const panel = readFileSync('src/components/workflow/ReportHumanInputPanel.tsx', 'utf8')

    expect(panel).toContain('input.allow_free_text && input.options.length === 0')
    expect(panel).not.toContain('Write a different answer or add a note')
  })
})
