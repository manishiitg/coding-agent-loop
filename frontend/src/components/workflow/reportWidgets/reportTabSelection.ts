// Reports own their navigation. Preserve only explicit tab controls, never
// arbitrary active buttons (which could perform an action when clicked).
const TAB_SELECTOR = '[role="tab"], button[data-tab], button[id^="tab-btn-"]'

export type ReportTabSelection = { attribute: 'id' | 'data-tab' | 'aria-controls'; value: string }

export function readReportTabSelection(doc: Document | null): ReportTabSelection | null {
  if (!doc) return null
  const tab = Array.from(doc.querySelectorAll<HTMLElement>(TAB_SELECTOR)).find(element =>
    element.getAttribute('aria-selected') === 'true' ||
    element.getAttribute('data-state') === 'active' ||
    element.classList.contains('active') || element.classList.contains('is-active'),
  )
  if (!tab) return null
  for (const attribute of ['id', 'data-tab', 'aria-controls'] as const) {
    const value = tab.getAttribute(attribute)
    if (value) return { attribute, value }
  }
  return null
}

export function restoreReportTabSelection(doc: Document, selection: ReportTabSelection): boolean {
  const tab = Array.from(doc.querySelectorAll<HTMLElement>(TAB_SELECTOR)).find(element =>
    element.getAttribute(selection.attribute) === selection.value,
  )
  if (!tab || tab.matches(':disabled, [aria-disabled="true"]')) return false
  tab.click()
  return true
}
