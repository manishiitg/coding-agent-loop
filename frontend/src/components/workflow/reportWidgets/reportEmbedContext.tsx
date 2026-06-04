import { createContext, useContext } from 'react'
import type { ReactNode } from 'react'

// Renders a live report widget from a JSON spec — used for markdown documents,
// where a "report-widget" fenced block becomes a real widget rendered natively
// in the React tree.
export type EmbeddedWidgetRenderer = (spec: unknown) => ReactNode

// Live data access for HTML report documents. HTML renders its OWN visuals
// (charts/tables/branded CSS) — we just hand it the data. `query` runs read-only
// SQL against the workflow's db/db.sqlite (the primary data source); `get`/
// `getText` fetch any db/ knowledgebase/ docs file (markdown, text, assets) on
// demand. Exposed inside the iframe as `window.report`.
export interface ReportDataApi {
  workspacePath: string
  query: (sql: string) => Promise<Record<string, unknown>[]>
  get: (path: string) => Promise<unknown>
  getText: (path: string) => Promise<string | null>
  // getHtml renders a markdown (db/ knowledgebase/ docs/) file to an HTML string
  // (the app's markdown engine + GFM), wrapped in <div class="report-markdown">,
  // so an HTML report can drop a rendered .md inline: el.innerHTML = await
  // window.report.getHtml(path). The iframe ships a default .report-markdown prose
  // style (theme-aware, overridable).
  getHtml: (path: string) => Promise<string | null>
  // File access (parity with file widgets): fileUrl returns an authenticated
  // blob URL usable in <img src> / <a href> / <iframe src> for images, PDFs,
  // etc.; openFile opens the file in the in-report preview modal. Both scoped to
  // db/ knowledgebase/ docs/.
  fileUrl: (path: string) => Promise<string | null>
  openFile: (path: string) => void
}

export interface ReportRuntime {
  renderEmbeddedWidget: EmbeddedWidgetRenderer
  data: ReportDataApi
}

const ReportRuntimeContext = createContext<ReportRuntime | null>(null)

export const ReportEmbedProvider = ReportRuntimeContext.Provider

// Markdown embeds: returns the widget renderer, or null outside a report (then
// ```report-widget blocks render as plain code).
export function useEmbeddedWidgetRenderer(): EmbeddedWidgetRenderer | null {
  return useContext(ReportRuntimeContext)?.renderEmbeddedWidget ?? null
}

// HTML data injection: returns the live data API, or null outside a report.
export function useReportDataApi(): ReportDataApi | null {
  return useContext(ReportRuntimeContext)?.data ?? null
}
