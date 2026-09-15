// daisyUI is an optional report-only CDN dependency. It is never bundled into
// the AgentWorks/Work frontend and never installed as a project package.
export const REPORT_DAISYUI_VERSION = '5.7.38'
export const REPORT_DAISYUI_CDN_URL = `https://cdn.jsdelivr.net/npm/daisyui@${REPORT_DAISYUI_VERSION}/daisyui.css`

export function reportUsesDaisyUi(html: string): boolean {
  return /data-report-ui\s*=\s*["']daisyui["']/i.test(html)
}

export function reportDaisyUiHead(html: string): string {
  if (!reportUsesDaisyUi(html)) return ''
  if (/cdn\.jsdelivr\.net\/npm\/daisyui@[^"']+\/daisyui\.css/i.test(html)) return ''
  return `<link id="__report_daisyui" data-version="${REPORT_DAISYUI_VERSION}" rel="stylesheet" href="${REPORT_DAISYUI_CDN_URL}">`
}
