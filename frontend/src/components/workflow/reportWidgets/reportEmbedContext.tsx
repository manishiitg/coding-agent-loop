import { createContext, useContext } from 'react'

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
  // renderMarkdown renders a markdown STRING (not a file) to the same themed HTML
  // (<div class="report-markdown">…</div>, app markdown engine + GFM). Synchronous.
  // Use for markdown held in data — a db/sql value, knowledgebase field, inline
  // text: el.innerHTML = window.report.renderMarkdown(row.notes_md).
  renderMarkdown: (md: string) => string
  // File access (parity with file widgets): fileUrl returns an authenticated
  // blob URL usable in <img src> / <a href> / <iframe src> for images, PDFs,
  // etc.; openFile opens the file in the in-report preview modal. Both scoped to
  // db/ knowledgebase/ docs/.
  fileUrl: (path: string) => Promise<string | null>
  openFile: (path: string) => void
}

export interface ReportRuntime {
  data: ReportDataApi
}

const ReportRuntimeContext = createContext<ReportRuntime | null>(null)

export const ReportEmbedProvider = ReportRuntimeContext.Provider

// HTML data injection: returns the live data API, or null outside a report.
export function useReportDataApi(): ReportDataApi | null {
  return useContext(ReportRuntimeContext)?.data ?? null
}
