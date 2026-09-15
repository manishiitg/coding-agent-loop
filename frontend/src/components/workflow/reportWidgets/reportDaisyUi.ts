// A dashboard-focused daisyUI bundle. Importing component sources separately
// avoids shipping every theme and niche component in the 1.1 MB all-in bundle.
// `?raw` keeps these rules out of the app document; they are injected only into
// report iframes that explicitly opt in.
import properties from 'daisyui/base/properties.css?raw'
import reset from 'daisyui/base/reset.css?raw'
import rootColor from 'daisyui/base/rootcolor.css?raw'
import lightTheme from 'daisyui/theme/light.css?raw'
import darkTheme from 'daisyui/theme/dark.css?raw'
import typography from 'daisyui/utilities/typography.css?raw'
import radius from 'daisyui/utilities/radius.css?raw'
import join from 'daisyui/utilities/join.css?raw'
import alert from 'daisyui/components/alert.css?raw'
import badge from 'daisyui/components/badge.css?raw'
import button from 'daisyui/components/button.css?raw'
import card from 'daisyui/components/card.css?raw'
import collapse from 'daisyui/components/collapse.css?raw'
import divider from 'daisyui/components/divider.css?raw'
import input from 'daisyui/components/input.css?raw'
import loading from 'daisyui/components/loading.css?raw'
import menu from 'daisyui/components/menu.css?raw'
import modal from 'daisyui/components/modal.css?raw'
import navbar from 'daisyui/components/navbar.css?raw'
import progress from 'daisyui/components/progress.css?raw'
import select from 'daisyui/components/select.css?raw'
import skeleton from 'daisyui/components/skeleton.css?raw'
import stat from 'daisyui/components/stat.css?raw'
import table from 'daisyui/components/table.css?raw'
import tab from 'daisyui/components/tab.css?raw'
import textarea from 'daisyui/components/textarea.css?raw'
import toast from 'daisyui/components/toast.css?raw'
import tooltip from 'daisyui/components/tooltip.css?raw'

export const REPORT_DAISYUI_VERSION = '5.7.38'

export function reportUsesDaisyUi(html: string): boolean {
  return /data-report-ui\s*=\s*["']daisyui["']/i.test(html)
}

const css = [
  properties, reset, rootColor, lightTheme, darkTheme, typography, radius, join,
  alert, badge, button, card, collapse, divider, input, loading, menu, modal,
  navbar, progress, select, skeleton, stat, table, tab, textarea, toast, tooltip,
].join('\n')

export const REPORT_DAISYUI_STYLE = `<style id="__report_daisyui" data-version="${REPORT_DAISYUI_VERSION}">${css}</style>`
