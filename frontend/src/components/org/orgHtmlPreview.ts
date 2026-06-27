export const ORG_HTML_PREVIEW_PREFERENCE_KEY = 'org_html_preview_device'
export const ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT = 'org-html-preview-device-changed'

export type OrgHtmlPreviewDevice = 'mobile' | 'tablet' | 'desktop'

export function getOrgHtmlPreviewDevice(): OrgHtmlPreviewDevice {
  try {
    const saved = localStorage.getItem(ORG_HTML_PREVIEW_PREFERENCE_KEY)
    return saved === 'mobile' || saved === 'tablet' || saved === 'desktop' ? saved : 'mobile'
  } catch {
    return 'mobile'
  }
}

export function setOrgHtmlPreviewDevice(device: OrgHtmlPreviewDevice) {
  try { localStorage.setItem(ORG_HTML_PREVIEW_PREFERENCE_KEY, device) } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, { detail: { preference: device } }))
}

export function orgHtmlPreviewShellClass(device: OrgHtmlPreviewDevice): string {
  return device === 'mobile'
    ? 'mx-auto h-full w-full max-w-[480px]'
    : device === 'tablet'
      ? 'mx-auto h-full w-full max-w-[880px]'
      : 'h-full w-full'
}
