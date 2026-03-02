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

  // 7. Wait for repaint
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
    for (const { el, prevSrc, prevDisplay } of imageFixups) {
      el.src = prevSrc
      el.style.display = prevDisplay
      const prevCrossOrigin = (el as ImageWithPrev)._prevCrossOrigin
      if (prevCrossOrigin) el.crossOrigin = prevCrossOrigin
    }
  }

  return { restore }
}

