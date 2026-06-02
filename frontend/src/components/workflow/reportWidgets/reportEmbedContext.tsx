import { createContext, useContext } from 'react'
import type { ReactNode } from 'react'

// Renders a live report widget from a JSON spec — used for markdown documents,
// where a "report-widget" fenced block becomes a real widget rendered natively
// in the React tree.
export type EmbeddedWidgetRenderer = (spec: unknown) => ReactNode

// Live data access for HTML report documents. HTML renders its OWN visuals
// (charts/tables/branded CSS) — we just hand it the data. `sources` is what the
// report plan already loaded; `get`/`getText` fetch any db/ knowledgebase/ docs
// file on demand. Exposed inside the iframe as `window.report`.
export interface ReportDataApi {
  sources: Record<string, unknown>
  workspacePath: string
  get: (path: string) => Promise<unknown>
  getText: (path: string) => Promise<string | null>
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
