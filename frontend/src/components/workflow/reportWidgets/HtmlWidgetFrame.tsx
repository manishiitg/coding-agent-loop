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

// Drop the about:srcdoc UA default `body{margin:8px}` so an HTML report renders
// edge-to-edge inside the report pane (we strip all our own chrome for HTML — the
// 8px UA margin would otherwise read as a stray gap around every side). Inserted
// as the FIRST <head> child so the report's own CSS (later, equal specificity)
// still wins if it sets its own body margin/padding. Idempotent per document.
function injectBaseReset(doc: Document) {
  if (doc.getElementById('__report_base_reset')) return
  const style = doc.createElement('style')
  style.id = '__report_base_reset'
  style.textContent = 'html,body{margin:0;padding:0;}'
  const head = doc.head
  if (head) head.insertBefore(style, head.firstChild)
}

// Default prose style for markdown injected via window.report.getHtml() — the
// helper wraps output in `.report-markdown`, and this gives it readable,
// theme-aware typography (using the app tokens injected by injectThemeTokens) so
// an embedded .md looks right with zero effort. The report can override any of
// these in its own CSS. Inserted after the theme-token style so var(--…) resolve.
function injectMarkdownStyles(doc: Document) {
  if (doc.getElementById('__report_markdown_styles')) return
  const style = doc.createElement('style')
  style.id = '__report_markdown_styles'
  style.textContent = `
.report-markdown{color:hsl(var(--foreground,222 47% 11%));line-height:1.6;font-size:0.95rem}
.report-markdown h1,.report-markdown h2,.report-markdown h3,.report-markdown h4{line-height:1.25;font-weight:650;margin:1.4em 0 .5em}
.report-markdown h1{font-size:1.5em}.report-markdown h2{font-size:1.25em}.report-markdown h3{font-size:1.1em}
.report-markdown p,.report-markdown ul,.report-markdown ol{margin:.6em 0}
.report-markdown ul,.report-markdown ol{padding-left:1.4em}
.report-markdown a{color:hsl(var(--primary,222 89% 55%));text-decoration:underline;text-underline-offset:2px}
.report-markdown code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.88em;background:hsl(var(--muted,210 40% 96%));padding:.12em .35em;border-radius:4px}
.report-markdown pre{background:hsl(var(--muted,210 40% 96%));padding:.8em 1em;border-radius:8px;overflow:auto}
.report-markdown pre code{background:none;padding:0}
.report-markdown blockquote{margin:.6em 0;padding:.2em .9em;border-left:3px solid hsl(var(--border,214 32% 88%));color:hsl(var(--muted-foreground,215 16% 47%))}
.report-markdown table{border-collapse:collapse;width:100%;margin:.7em 0;font-size:.9em}
.report-markdown th,.report-markdown td{border:1px solid hsl(var(--border,214 32% 88%));padding:.4em .6em;text-align:left}
.report-markdown th{background:hsl(var(--muted,210 40% 96%));font-weight:600}
.report-markdown img{max-width:100%;height:auto}
.report-markdown hr{border:0;border-top:1px solid hsl(var(--border,214 32% 88%));margin:1.2em 0}
`.trim()
  doc.head?.appendChild(style)
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
//   await window.report.getHtml(path) // a markdown file rendered to an HTML string (wrapped in
//                                      // .report-markdown, default prose style injected) — drop a
//                                      // rendered .md inline: el.innerHTML = await window.report.getHtml(p)
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

    injectBaseReset(doc)
    injectMarkdownStyles(doc)

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
        const link = el?.closest?.('a[href]') as HTMLAnchorElement | null
        if (!link) return
        const href = link.getAttribute('href') || ''

        // In-page `#anchor` links (the report's tab nav): the srcDoc base URL is
        // about:srcdoc, so a default click reloads the whole document instead of
        // scrolling. Intercept and scroll manually.
        if (href.startsWith('#')) {
          const target = doc.getElementById(href.slice(1))
          if (!target) return
          e.preventDefault()
          target.scrollIntoView({ behavior: 'smooth', block: 'start' })
          return
        }

        // External links (Notion, Jira, GitHub, docs, …): the iframe sandbox has
        // no allow-popups/allow-top-navigation, so a click would be silently
        // swallowed. Open them in a new browser tab from the parent window
        // instead, keeping the sandbox locked down.
        if (/^https?:\/\//i.test(href)) {
          e.preventDefault()
          window.open(href, '_blank', 'noopener,noreferrer')
        }
      })
    }

    if (dataApi) {
      win.report = {
        workspacePath: dataApi.workspacePath,
        query: dataApi.query,
        get: dataApi.get,
        getText: dataApi.getText,
        getHtml: dataApi.getHtml,
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
