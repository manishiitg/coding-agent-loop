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
//
// autoHeight: size the iframe to its content (no inner scrollbar / clipping) and
// keep it in sync via a ResizeObserver as content renders. Used for the inline
// report view; the modal preview keeps a fixed height and scrolls internally.
export function HtmlReportFrame({
  html,
  title,
  className,
  autoHeight = false,
}: {
  html: string
  title: string
  className: string
  autoHeight?: boolean
}) {
  const dataApi = useReportDataApi()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const observerRef = useRef<ResizeObserver | null>(null)

  const resize = useCallback(() => {
    if (!autoHeight) return
    const frame = iframeRef.current
    const doc = frame?.contentDocument
    if (!frame || !doc) return
    const h = Math.max(doc.documentElement?.scrollHeight || 0, doc.body?.scrollHeight || 0)
    if (h > 0) frame.style.height = `${h}px`
  }, [autoHeight])

  const inject = useCallback(() => {
    const frame = iframeRef.current
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const win = frame?.contentWindow as any
    const doc = frame?.contentDocument
    if (!win || !doc) return

    if (dataApi) {
      win.report = {
        workspacePath: dataApi.workspacePath,
        query: dataApi.query,
        get: dataApi.get,
        getText: dataApi.getText,
      }
      try {
        win.dispatchEvent(new win.Event('report:data'))
      } catch {
        /* iframe may have navigated/reloaded */
      }
    }

    if (autoHeight) {
      observerRef.current?.disconnect()
      resize()
      try {
        const ro = new ResizeObserver(() => resize())
        if (doc.documentElement) ro.observe(doc.documentElement)
        if (doc.body) ro.observe(doc.body)
        observerRef.current = ro
      } catch {
        /* ResizeObserver unavailable — height stays at last measure */
      }
    }
  }, [dataApi, autoHeight, resize])

  // Re-inject when the report data changes (sources refreshed).
  useEffect(() => {
    inject()
  }, [inject])

  // Disconnect the observer on unmount.
  useEffect(() => () => observerRef.current?.disconnect(), [])

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
