import html2canvas from 'html2canvas'
import jsPDF from 'jspdf'

// ── Types ──────────────────────────────────────────────────────────────────────

interface ImageWithPrev extends HTMLImageElement {
  _prevCrossOrigin?: string | null
}

interface DomFixup<T = HTMLElement> {
  el: T
  prevStyle: string
}

interface InlineFixup {
  el: HTMLElement
  prevColor: string
  prevBg: string
}

interface ListFixup {
  el: HTMLElement
  prevStyle: string
  marker?: HTMLSpanElement
}

interface ImageFixup {
  el: HTMLImageElement
  prevSrc: string
  prevDisplay: string
}

interface PrepareResult {
  restore: () => void
}

// ── Dark→Light token color map (oneDark → prism light equivalents) ─────────

const darkToLightTokenMap: Record<string, string> = {
  'rgb(224, 108, 117)': '#e45649',
  'rgb(198, 120, 221)': '#a626a4',
  'rgb(97, 175, 239)': '#4078f2',
  'rgb(152, 195, 121)': '#50a14f',
  'rgb(209, 154, 102)': '#986801',
  'rgb(86, 182, 194)': '#0184bc',
  'rgb(229, 192, 123)': '#c18401',
  'rgb(171, 178, 191)': '#383a42',
  'rgb(190, 80, 70)': '#e45649',
  'rgb(92, 99, 112)': '#a0a1a7',
}

const spacingRules: Record<string, { mt: string; mb: string; extra?: string }> = {
  H1: { mt: '24px', mb: '12px', extra: 'font-size:1.5em;font-weight:700;' },
  H2: { mt: '20px', mb: '10px', extra: 'font-size:1.25em;font-weight:600;' },
  H3: { mt: '16px', mb: '8px', extra: 'font-size:1.125em;font-weight:600;' },
  H4: { mt: '12px', mb: '6px', extra: 'font-weight:600;' },
  H5: { mt: '10px', mb: '4px', extra: 'font-weight:600;' },
  H6: { mt: '8px', mb: '4px', extra: 'font-weight:600;' },
  P: { mt: '0', mb: '8px' },
  BLOCKQUOTE: { mt: '8px', mb: '8px' },
  HR: { mt: '16px', mb: '16px' },
}

// ── prepareDomForPdfExport ─────────────────────────────────────────────────

/**
 * Prepares the DOM for PDF export by forcing light mode, fixing inline styles,
 * injecting list markers, etc. Returns a `restore()` function that undoes all
 * mutations.
 */
export async function prepareDomForPdfExport(
  contentEl: HTMLElement
): Promise<PrepareResult> {
  // 1. Force light mode
  const htmlEl = document.documentElement
  const wasDark = htmlEl.classList.contains('dark')
  if (wasDark) {
    htmlEl.classList.remove('dark')
    htmlEl.classList.add('light')
  }

  // 2. Force white background on ancestors
  const ancestors: { el: HTMLElement; prev: string }[] = []
  let parent = contentEl.parentElement
  while (parent) {
    ancestors.push({ el: parent, prev: parent.style.backgroundColor })
    parent.style.backgroundColor = '#ffffff'
    parent = parent.parentElement
    if (parent === document.body) break
  }

  // 3. Fix inline styles (dark theme colors)
  const inlineFixups: InlineFixup[] = []
  contentEl.querySelectorAll('*').forEach((el) => {
    const htmlEl = el as HTMLElement
    const style = htmlEl.style
    const prevColor = style.color
    const prevBg = style.backgroundColor

    if (style.backgroundColor) {
      const bgRgb = window.getComputedStyle(htmlEl).backgroundColor.match(/\d+/g)
      if (bgRgb) {
        const [r, g, b] = bgRgb.map(Number)
        if (r < 60 && g < 60 && b < 60) {
          style.backgroundColor = '#f9fafb'
        }
      }
    }

    if (style.color) {
      const computedColor = window.getComputedStyle(htmlEl).color
      const mapped = darkToLightTokenMap[computedColor]
      if (mapped) {
        style.color = mapped
      } else {
        const cRgb = computedColor.match(/\d+/g)
        if (cRgb) {
          const [r, g, b] = cRgb.map(Number)
          if (r > 180 && g > 180 && b > 180) {
            style.color = '#1f2937'
          }
        }
      }
    }

    if (prevColor !== style.color || prevBg !== style.backgroundColor) {
      inlineFixups.push({ el: htmlEl, prevColor, prevBg })
    }
  })

  // 4. Fix lists — inject bullet/number markers
  const listFixups: ListFixup[] = []

  contentEl.querySelectorAll('ul, ol').forEach((list) => {
    const el = list as HTMLElement
    listFixups.push({ el, prevStyle: el.getAttribute('style') || '' })
    el.style.listStyleType = 'none'
    el.style.paddingLeft = '0'
    el.style.marginLeft = '0'
    el.style.marginTop = '4px'
    el.style.marginBottom = '4px'
  })

  contentEl.querySelectorAll('li').forEach((li) => {
    const el = li as HTMLElement
    const parentList = el.closest('ul, ol')
    const depth = (() => {
      let d = 0
      let p = el.parentElement
      while (p && contentEl.contains(p)) {
        if (p.tagName === 'UL' || p.tagName === 'OL') d++
        p = p.parentElement
      }
      return d
    })()

    listFixups.push({ el, prevStyle: el.getAttribute('style') || '' })
    el.style.paddingLeft = `${depth * 20}px`
    el.style.marginTop = '2px'
    el.style.marginBottom = '2px'
    el.style.display = 'flex'
    el.style.alignItems = 'flex-start'
    el.style.gap = '6px'

    const marker = document.createElement('span')
    marker.setAttribute('data-pdf-marker', 'true')
    marker.style.flexShrink = '0'
    marker.style.color = '#374151'
    marker.style.userSelect = 'none'
    marker.style.minWidth = '16px'

    if (parentList?.tagName === 'OL') {
      const siblings = Array.from(parentList.children).filter(c => c.tagName === 'LI')
      const index = siblings.indexOf(el) + 1
      marker.textContent = `${index}.`
    } else {
      const bullets = ['\u2022', '\u25E6', '\u25AA']
      marker.textContent = bullets[Math.min(depth - 1, bullets.length - 1)] || '\u2022'
    }

    el.prepend(marker)
    listFixups.push({ el, prevStyle: '', marker })
  })

  // 5. Fix heading/spacing
  const spacingFixups: DomFixup[] = []
  contentEl.querySelectorAll('h1, h2, h3, h4, h5, h6, p, blockquote, hr').forEach((el) => {
    const htmlEl = el as HTMLElement
    const rules = spacingRules[htmlEl.tagName]
    if (!rules) return
    spacingFixups.push({ el: htmlEl, prevStyle: htmlEl.getAttribute('style') || '' })
    htmlEl.style.marginTop = rules.mt
    htmlEl.style.marginBottom = rules.mb
    if (rules.extra) {
      htmlEl.style.cssText += rules.extra
    }
  })

  // 5b. Fix inline <code> elements
  const codeFixups: DomFixup[] = []
  contentEl.querySelectorAll('code').forEach((el) => {
    const htmlEl = el as HTMLElement
    if (htmlEl.closest('pre') || htmlEl.parentElement?.tagName === 'PRE') return
    codeFixups.push({ el: htmlEl, prevStyle: htmlEl.getAttribute('style') || '' })
    htmlEl.style.backgroundColor = '#f3f4f6'
    htmlEl.style.color = '#2563eb'
    htmlEl.style.padding = '1px 6px'
    htmlEl.style.borderRadius = '4px'
    htmlEl.style.fontSize = '0.75em'
    htmlEl.style.fontFamily = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'
    htmlEl.style.display = 'inline'
    htmlEl.style.wordBreak = 'break-word'
  })

  // 6. Convert images to data URLs and remove lazy loading
  const imageFixups: ImageFixup[] = []
  const images = contentEl.querySelectorAll('img')
  for (const img of Array.from(images)) {
    const prevSrc = img.src
    const prevDisplay = img.style.display
    const prevCrossOrigin = img.crossOrigin
    // Remove lazy loading and crossorigin so data URLs work cleanly
    img.removeAttribute('loading')
    img.removeAttribute('crossorigin')
    try {
      // Skip images that are already data URLs
      if (prevSrc.startsWith('data:')) {
        imageFixups.push({ el: img, prevSrc, prevDisplay })
        continue
      }
      const resp = await fetch(img.src)
      const blob = await resp.blob()
      const dataUrl = await new Promise<string>((resolve) => {
        const reader = new FileReader()
        reader.onloadend = () => resolve(reader.result as string)
        reader.readAsDataURL(blob)
      })
      imageFixups.push({ el: img, prevSrc, prevDisplay })
      // Set src and wait for it to fully load
      await new Promise<void>((resolve) => {
        img.onload = () => resolve()
        img.onerror = () => resolve()
        img.src = dataUrl
      })
    } catch {
      imageFixups.push({ el: img, prevSrc, prevDisplay })
      img.style.display = 'none'
    }
    // Store crossOrigin for restore
    ;(img as ImageWithPrev)._prevCrossOrigin = prevCrossOrigin
  }

  // 6b. Fix container — strip margins/padding for flush page edges.
  const containerPrev = contentEl.getAttribute('style') || ''
  contentEl.style.margin = '0'
  contentEl.style.padding = '0'
  contentEl.style.backgroundColor = '#ffffff'

  const firstChild = contentEl.querySelector(':scope > * > :first-child') as HTMLElement | null
  const firstChildPrevStyle = firstChild?.getAttribute('style') || ''
  if (firstChild) {
    firstChild.style.marginTop = '0'
  }
  const lastChild = contentEl.querySelector(':scope > * > :last-child') as HTMLElement | null
  const lastChildPrevStyle = lastChild?.getAttribute('style') || ''
  if (lastChild) {
    lastChild.style.marginBottom = '0'
  }

  // 7. Prevent page breaks cutting through content
  const breakFixups: { el: HTMLElement; prevStyle: string }[] = []
  contentEl.querySelectorAll('p, h1, h2, h3, h4, h5, h6, li, pre, table, tr, blockquote, .syntax-highlighter-wrapper').forEach((el) => {
    const htmlEl = el as HTMLElement
    breakFixups.push({ el: htmlEl, prevStyle: htmlEl.style.breakInside || '' })
    htmlEl.style.breakInside = 'avoid'
  })

  // 8. Wait for repaint
  await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))

  // ── restore() closure ──────────────────────────────────────────────────────

  const restore = () => {
    if (wasDark) {
      document.documentElement.classList.remove('light')
      document.documentElement.classList.add('dark')
    }
    for (const { el, prev } of ancestors) {
      el.style.backgroundColor = prev
    }
    for (const { el, prevColor, prevBg } of inlineFixups) {
      el.style.color = prevColor
      el.style.backgroundColor = prevBg
    }
    for (const fixup of listFixups) {
      if (fixup.marker) {
        fixup.marker.remove()
      } else if (fixup.prevStyle) {
        fixup.el.setAttribute('style', fixup.prevStyle)
      } else {
        fixup.el.removeAttribute('style')
      }
    }
    for (const { el, prevStyle } of spacingFixups) {
      if (prevStyle) el.setAttribute('style', prevStyle)
      else el.removeAttribute('style')
    }
    if (containerPrev) {
      contentEl.setAttribute('style', containerPrev)
    } else {
      contentEl.removeAttribute('style')
    }
    if (firstChild) {
      if (firstChildPrevStyle) firstChild.setAttribute('style', firstChildPrevStyle)
      else firstChild.removeAttribute('style')
    }
    if (lastChild && lastChild !== firstChild) {
      if (lastChildPrevStyle) lastChild.setAttribute('style', lastChildPrevStyle)
      else lastChild.removeAttribute('style')
    }
    for (const { el, prevStyle } of codeFixups) {
      if (prevStyle) el.setAttribute('style', prevStyle)
      else el.removeAttribute('style')
    }
    for (const { el, prevStyle } of breakFixups) {
      el.style.breakInside = prevStyle
    }
    for (const { el, prevSrc, prevDisplay } of imageFixups) {
      el.src = prevSrc
      el.style.display = prevDisplay
      const prevCrossOrigin = (el as ImageWithPrev)._prevCrossOrigin
      if (prevCrossOrigin) el.crossOrigin = prevCrossOrigin
    }
  }

  return { restore }
}

// ── exportPdfChunked ───────────────────────────────────────────────────────

// A4 dimensions in mm
const A4_WIDTH_MM = 210
const A4_HEIGHT_MM = 297
const MARGIN_H_MM = 10 // left + right margin each
const MARGIN_V_MM = 15 // top + bottom margin each
const CONTENT_WIDTH_MM = A4_WIDTH_MM - 2 * MARGIN_H_MM   // 190mm
const CONTENT_HEIGHT_MM = A4_HEIGHT_MM - 2 * MARGIN_V_MM  // 267mm

// Browser canvas height hard limit (stay comfortably under 32767)
const MAX_CANVAS_PX = 28000

// Fixed render width for PDF — gives clean A4 text density
const RENDER_WIDTH_PX = 800

/**
 * Renders a large DOM element to PDF by cloning it into an isolated off-screen
 * container (avoiding parent layout interference), chunking it vertically with
 * html2canvas crop options, and assembling pages via jsPDF.
 */
export async function exportPdfChunked(
  contentEl: HTMLElement,
  filename: string,
  scale: number,
  onProgress?: (current: number, total: number) => void
): Promise<void> {
  // Clone the content into an off-screen container with a controlled width.
  // This avoids the parent flex/grid layout constraining the render.
  const offscreen = document.createElement('div')
  offscreen.style.cssText = `
    position: fixed; left: -10000px; top: 0;
    width: ${RENDER_WIDTH_PX}px;
    background: #ffffff;
    overflow: visible;
    z-index: -9999;
  `
  const clone = contentEl.cloneNode(true) as HTMLElement
  clone.style.margin = '0'
  clone.style.padding = '0'
  clone.style.maxWidth = 'none'
  clone.style.width = '100%'
  clone.style.backgroundColor = '#ffffff'
  // Remove constraining classes
  clone.className = clone.className
    .replace(/\bmax-w-\S+/g, '')
    .replace(/\bmx-auto\b/g, '')
    .trim()

  offscreen.appendChild(clone)
  document.body.appendChild(offscreen)

  // Let the browser lay out the clone
  await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))

  try {
    const contentWidth = clone.offsetWidth
    const contentHeight = clone.scrollHeight

    // How many CSS px correspond to 1mm at our render width
    const pxPerMm = contentWidth / CONTENT_WIDTH_MM
    // One PDF page's worth of content in CSS px
    const pageHeightPx = CONTENT_HEIGHT_MM * pxPerMm
    // Maximum chunk height that stays within canvas limits
    const chunkHeightPx = Math.floor(MAX_CANVAS_PX / scale)

    const totalChunks = Math.ceil(contentHeight / chunkHeightPx)

    const pdf = new jsPDF({
      unit: 'mm',
      format: 'a4',
      orientation: 'portrait',
    })

    // Track how much of the current PDF page has been filled (in CSS px)
    let pdfPageUsedPx = 0
    let isFirstPage = true

    for (let chunkIdx = 0; chunkIdx < totalChunks; chunkIdx++) {
      const chunkY = chunkIdx * chunkHeightPx
      const thisChunkHeight = Math.min(chunkHeightPx, contentHeight - chunkY)

      onProgress?.(chunkIdx + 1, totalChunks)

      // Render this vertical band of the cloned content
      const chunkCanvas = await html2canvas(clone, {
        scale,
        backgroundColor: '#ffffff',
        scrollY: 0,
        x: 0,
        y: chunkY,
        width: contentWidth,
        height: thisChunkHeight,
        useCORS: true,
        allowTaint: true,
        imageTimeout: 30000,
      })

      // Slice the chunk canvas into page-sized pieces
      const chunkCanvasHeight = chunkCanvas.height // in device px
      const chunkCanvasWidth = chunkCanvas.width
      let canvasOffset = 0 // device px offset within chunkCanvas

      while (canvasOffset < chunkCanvasHeight) {
        // How much space remains on the current PDF page (in device px)
        const pageRemainingPx = (pageHeightPx - pdfPageUsedPx) * scale
        // How much of the chunk canvas is left
        const chunkRemainingPx = chunkCanvasHeight - canvasOffset
        // Take the smaller of the two
        const sliceHeight = Math.min(pageRemainingPx, chunkRemainingPx)

        if (sliceHeight <= 0) break

        // Extract slice from chunk canvas
        const sliceCanvas = document.createElement('canvas')
        sliceCanvas.width = chunkCanvasWidth
        sliceCanvas.height = Math.ceil(sliceHeight)
        const ctx = sliceCanvas.getContext('2d')!
        ctx.drawImage(
          chunkCanvas,
          0, canvasOffset,
          chunkCanvasWidth, Math.ceil(sliceHeight),
          0, 0,
          chunkCanvasWidth, Math.ceil(sliceHeight)
        )

        if (!isFirstPage && pdfPageUsedPx === 0) {
          pdf.addPage()
        }
        isFirstPage = false

        // Position on the PDF page (in mm)
        const yMm = MARGIN_V_MM + pdfPageUsedPx / pxPerMm
        const sliceHeightMm = (sliceHeight / scale) / pxPerMm

        // Pass canvas directly to jsPDF — avoids giant base64 strings
        pdf.addImage(
          sliceCanvas,
          'JPEG',
          MARGIN_H_MM,
          yMm,
          CONTENT_WIDTH_MM,
          sliceHeightMm,
          undefined,
          'FAST'
        )

        // Free slice canvas
        sliceCanvas.width = 0
        sliceCanvas.height = 0

        canvasOffset += sliceHeight
        pdfPageUsedPx += sliceHeight / scale

        // If the page is full, reset for next page
        if (pdfPageUsedPx >= pageHeightPx - 0.5) {
          pdfPageUsedPx = 0
        }
      }

      // Free chunk canvas memory
      chunkCanvas.width = 0
      chunkCanvas.height = 0
    }

    pdf.save(filename)
  } finally {
    // Clean up the off-screen container
    document.body.removeChild(offscreen)
  }
}
