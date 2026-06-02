import { useCallback, useEffect, useRef } from 'react'
import { useReportDataApi } from './reportEmbedContext'

// HtmlReportFrame renders an HTML report in a sandboxed iframe and injects a live
// data API onto the iframe's window as `window.report`, then fires a `report:data`
// event. The HTML owns ALL rendering (its own charts/tables/branded CSS) — we
// only deliver data — which is the right model for HTML: full styling freedom,
// no React-in-iframe or theme-matching. Re-injects + re-fires when the report's
// data changes so the HTML can re-render without a reload. Outside a report (no
// data API in context) the HTML renders standalone.
//
// Inside the HTML report:
//   await window.report.query(sql)   // read-only SQL against db/db.sqlite -> array of row objects
//   await window.report.get(path)    // any db/ knowledgebase/ docs file -> parsed JSON (or text)
//   await window.report.getText(path)// raw file text
//   window.addEventListener('report:data', render)  // fires on load + on refresh
export function HtmlReportFrame({
  html,
  title,
  className,
}: {
  html: string
  title: string
  className: string
}) {
  const dataApi = useReportDataApi()
  const iframeRef = useRef<HTMLIFrameElement>(null)

  const inject = useCallback(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const win = iframeRef.current?.contentWindow as any
    if (!win || !dataApi) return
    win.report = {
      workspacePath: dataApi.workspacePath,
      query: dataApi.query,
      get: dataApi.get,
      getText: dataApi.getText,
    }
    try {
      // Use the iframe realm's Event constructor so the event belongs to it.
      win.dispatchEvent(new win.Event('report:data'))
    } catch {
      /* iframe may have navigated/reloaded */
    }
  }, [dataApi])

  // Re-inject when the report data changes (sources refreshed) so the HTML can
  // re-render against current data.
  useEffect(() => {
    inject()
  }, [inject])

  return (
    <iframe
      ref={iframeRef}
      title={title}
      srcDoc={html}
      sandbox="allow-same-origin allow-scripts"
      onLoad={inject}
      className={className}
    />
  )
}
