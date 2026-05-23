export const TERMINAL_REFRESH_REQUEST_EVENT = 'terminal-center:refresh-request'

export function requestTerminalRefreshBurst() {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(TERMINAL_REFRESH_REQUEST_EVENT))
}
