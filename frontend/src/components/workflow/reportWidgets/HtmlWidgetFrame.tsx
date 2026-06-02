import { useCallback, useEffect, useRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { useEmbeddedWidgetRenderer } from './reportEmbedContext'

// Clone the parent document's stylesheets into the iframe so widgets mounted
// inside it pick up Tailwind/theme styling and CSS variables, and carry the
// dark-mode class. Runs once per (re)loaded iframe document.
function injectIframeStyles(doc: Document) {
  if (doc.getElementById('__report_widget_styles')) return
  const marker = doc.createElement('meta')
  marker.id = '__report_widget_styles'
  doc.head?.appendChild(marker)
  document.querySelectorAll('style, link[rel="stylesheet"]').forEach((node) => {
    try {
      doc.head?.appendChild(node.cloneNode(true))
    } catch {
      /* ignore individual clone failures */
    }
  })
  const root = document.documentElement
  if (root.classList.contains('dark')) doc.documentElement.classList.add('dark')
  if (root.classList.contains('dark-plus')) doc.documentElement.classList.add('dark-plus')
}

function parseSpec(node: HTMLElement): unknown {
  const raw = node.getAttribute('data-report-widget') || ''
  try {
    return JSON.parse(raw)
  } catch {
    return { kind: '__invalid__' }
  }
}

// HtmlWithEmbeddedWidgets renders an HTML report in a sandboxed iframe and mounts
// live report widgets into any `<div data-report-widget='{…spec…}'></div>`
// placeholders inside it. Each placeholder gets its OWN React root created in the
// iframe document (via createRoot) — NOT a portal from the parent. That matters:
// React attaches event delegation to the root's container, so a root inside the
// iframe makes clicks/sorting/hover work, whereas a parent-side portal would
// render the DOM but drop all events at the document boundary. Widget state lives
// in module-singleton stores, so e.g. clicking an embedded file row still opens
// the parent's preview modal. The iframe keeps the HTML's style/script isolation.
// Outside a report (no embedded renderer in context) the HTML renders as-is and
// placeholders keep whatever static fallback content the author put inside them.
export function HtmlWithEmbeddedWidgets({
  html,
  title,
  className,
}: {
  html: string
  title: string
  className: string
}) {
  const renderEmbeddedWidget = useEmbeddedWidgetRenderer()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const mountedRef = useRef<{ spec: unknown; root: Root }[]>([])

  const unmountAll = useCallback(() => {
    for (const m of mountedRef.current) {
      try {
        m.root.unmount()
      } catch {
        /* node may already be gone after an iframe reload */
      }
    }
    mountedRef.current = []
  }, [])

  // (Re)scan the loaded iframe and mount a dedicated root per placeholder.
  const mount = useCallback(() => {
    const doc = iframeRef.current?.contentDocument
    if (!doc) return
    unmountAll()
    if (!renderEmbeddedWidget) return
    injectIframeStyles(doc)
    const nodes = Array.from(doc.querySelectorAll<HTMLElement>('[data-report-widget]'))
    mountedRef.current = nodes.map((node) => {
      const spec = parseSpec(node)
      node.innerHTML = '' // drop any static fallback before mounting the live widget
      const root = createRoot(node)
      root.render(renderEmbeddedWidget(spec))
      return { spec, root }
    })
  }, [renderEmbeddedWidget, unmountAll])

  // When the embedded renderer changes (e.g. report sources refreshed), re-render
  // existing roots in place so embedded widgets pick up new data without a full
  // iframe reload. If nothing is mounted yet, the next onLoad handles it.
  useEffect(() => {
    if (!renderEmbeddedWidget || mountedRef.current.length === 0) return
    for (const m of mountedRef.current) {
      m.root.render(renderEmbeddedWidget(m.spec))
    }
  }, [renderEmbeddedWidget])

  // Tear down roots when this frame unmounts.
  useEffect(() => () => unmountAll(), [unmountAll])

  return (
    <iframe
      ref={iframeRef}
      title={title}
      srcDoc={html}
      sandbox="allow-same-origin allow-scripts"
      onLoad={mount}
      className={className}
    />
  )
}
