import './wdyr';
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import ServerConnectionStatus from './components/ServerConnectionStatus'
import ErrorBoundary from './components/ErrorBoundary'

// Capture uncaught renderer errors so a blank screen always leaves a trace in
// the Electron main log (<userData>/logs/main.log) — DevTools is often
// impossible to open at the moment the window blanks out.
function reportRendererError(kind: string, detail: unknown) {
  try {
    const payload = {
      kind,
      message: detail instanceof Error ? detail.message : String(detail),
      stack: detail instanceof Error ? detail.stack : undefined,
      url: window.location.href,
      time: new Date().toISOString(),
    }
    // eslint-disable-next-line no-console
    console.error('[renderer-error]', payload)
    ;(window as unknown as { electronAPI?: { logRendererError?: (p: unknown) => void } })
      .electronAPI?.logRendererError?.(payload)
  } catch {
    /* never throw from the error reporter */
  }
}

window.addEventListener('error', (e) => reportRendererError('window.error', e.error ?? e.message))
window.addEventListener('unhandledrejection', (e) => reportRendererError('unhandledrejection', e.reason))

createRoot(document.getElementById('root')!).render(
  <ErrorBoundary onError={(error) => reportRendererError('react', error)}>
    <ServerConnectionStatus>
      <App />
    </ServerConnectionStatus>
  </ErrorBoundary>
)
