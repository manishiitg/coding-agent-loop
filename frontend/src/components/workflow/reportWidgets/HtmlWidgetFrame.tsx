import { useCallback, useEffect, useRef } from 'react'
import { useReportDataApi } from './reportEmbedContext'

// App theme tokens (HSL triplets) exposed to the HTML report as CSS variables so
// it can match the app palette via hsl(var(--…)) and switch with light/dark. Read
// from the (themed) iframe host element so report-theme overrides are included.
const REPORT_THEME_VARS = [
  'background', 'foreground', 'card', 'card-foreground', 'popover', 'popover-foreground',
  'primary', 'primary-foreground', 'secondary', 'secondary-foreground',
  'muted', 'muted-foreground', 'accent', 'accent-foreground',
  'border', 'input', 'ring', 'destructive', 'destructive-foreground',
  'chart-1', 'chart-2', 'chart-3', 'chart-4', 'chart-5',
] as const

function injectThemeTokens(host: HTMLElement, doc: Document) {
  const cs = getComputedStyle(host)
  const decls = REPORT_THEME_VARS
    .map((v) => {
      const val = cs.getPropertyValue(`--${v}`).trim()
      return val ? `--${v}:${val};` : ''
    })
    .join('')
  if (!decls) return
  let style = doc.getElementById('__report_theme_tokens') as HTMLStyleElement | null
  if (!style) {
    style = doc.createElement('style')
    style.id = '__report_theme_tokens'
    doc.head?.appendChild(style)
  }
  style.textContent = `:root{${decls}}`
}

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
//   await window.report.fileUrl(path)// blob URL for <img>/<a>/<iframe> (images, PDFs, …)
//   window.report.openFile(path)     // open a file in the in-report preview modal
//   window.report.theme              // 'dark' | 'light' — the APP's current theme
//   window.addEventListener('report:data', render)   // fires on load + on data refresh
//   window.addEventListener('report:theme', restyle) // fires when the app theme toggles
//
// Theme: the iframe is a separate document and `@media (prefers-color-scheme)`
// only sees the OS, not the app's in-app light/dark toggle. So the frame mirrors
// the app theme onto the iframe's <html> as BOTH a `.dark` class and a
// `data-theme="dark|light"` attribute, exposes `window.report.theme`, and keeps
// them in sync via a MutationObserver when the user toggles. Author HTML to key
// off `:root.dark` / `[data-theme="dark"]` (and prefers-color-scheme as a
// standalone fallback).
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

  // Mirror the APP's light/dark theme onto the iframe document (the agent's HTML
  // designs its own palette but keys the active mode off this). The app uses a
  // `.dark` (or `.dark-plus`) class on <html>.
  const applyTheme = useCallback(() => {
    const frame = iframeRef.current
    if (!frame) return
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const win = frame.contentWindow as any
    const doc = frame.contentDocument
    if (!win || !doc?.documentElement) return
    const cl = document.documentElement.classList
    const theme: 'dark' | 'light' = cl.contains('dark') || cl.contains('dark-plus') ? 'dark' : 'light'
    doc.documentElement.classList.toggle('dark', theme === 'dark')
    doc.documentElement.setAttribute('data-theme', theme)
    if (win.report) win.report.theme = theme
    // Expose the app's resolved theme tokens (current light/dark + report theme)
    // as CSS variables inside the iframe so the HTML can use hsl(var(--…)).
    injectThemeTokens(frame, doc)
    try {
      win.dispatchEvent(new win.Event('report:theme'))
    } catch {
      /* iframe may have navigated/reloaded */
    }
  }, [])

  const resize = useCallback(() => {
    if (!autoHeight) return
    const frame = iframeRef.current
    const doc = frame?.contentDocument
    if (!frame || !doc) return
    const content = Math.max(doc.documentElement?.scrollHeight || 0, doc.body?.scrollHeight || 0)
    if (content <= 0) return
    // Grow to fit content, but cap at ~viewport height so a tall report can never
    // be cut off if the outer pane doesn't scroll — past the cap the iframe itself
    // scrolls (iframes scroll their document by default). Short reports fit exactly.
    const cap = Math.max(360, Math.round((window.innerHeight || 800) * 0.9))
    frame.style.height = `${Math.min(content, cap)}px`
  }, [autoHeight])

  const inject = useCallback(() => {
    const frame = iframeRef.current
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const win = frame?.contentWindow as any
    const doc = frame?.contentDocument
    if (!win || !doc) return

    // In a srcDoc iframe the base URL is about:srcdoc, so clicking an in-page
    // `#anchor` link (the report's tab nav) reloads the WHOLE document instead of
    // scrolling. Intercept those clicks and scroll manually. Bound once per loaded
    // document (the flag resets on reload, so a fresh doc re-binds).
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (!(doc as any).__anchorScrollBound) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(doc as any).__anchorScrollBound = true
      doc.addEventListener('click', (e: Event) => {
        const el = e.target as Element | null
        const anchor = el?.closest?.('a[href^="#"]') as HTMLAnchorElement | null
        if (!anchor) return
        const id = anchor.getAttribute('href')?.slice(1)
        if (!id) return
        const target = doc.getElementById(id)
        if (!target) return
        e.preventDefault()
        target.scrollIntoView({ behavior: 'smooth', block: 'start' })
      })
    }

    if (dataApi) {
      win.report = {
        workspacePath: dataApi.workspacePath,
        query: dataApi.query,
        get: dataApi.get,
        getText: dataApi.getText,
        fileUrl: dataApi.fileUrl,
        openFile: dataApi.openFile,
        theme: 'light',
      }
      applyTheme()
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

  // Keep the iframe theme in sync when the user toggles the app's light/dark mode
  // while the report is open (watches the app's <html> class).
  useEffect(() => {
    const mo = new MutationObserver(() => applyTheme())
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => mo.disconnect()
  }, [applyTheme])

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
