import { createContext, useContext } from 'react'
import type { ReactNode } from 'react'

// Lets a markdown/file widget render a live report widget embedded in a
// "report-widget" fenced block inside a generated .md document. The report
// viewer (ReportView) provides the renderer (it has WidgetCard + the loaded
// sources); MarkdownWidget / FileWidget consume it and hand it to
// MarkdownRenderer. Lives in its own module so ui/MarkdownRenderer and the
// widget files don't import ReportViewer (avoids an import cycle).
export type EmbeddedWidgetRenderer = (spec: unknown) => ReactNode

const ReportEmbedContext = createContext<EmbeddedWidgetRenderer | null>(null)

export const ReportEmbedProvider = ReportEmbedContext.Provider

// Returns the embedded-widget renderer, or null outside a report (e.g. chat),
// in which case ```report-widget blocks render as plain code.
export function useEmbeddedWidgetRenderer(): EmbeddedWidgetRenderer | null {
  return useContext(ReportEmbedContext)
}
