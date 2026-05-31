import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  onError?: (error: Error, info: ErrorInfo) => void
}

interface State {
  error: Error | null
}

/**
 * Root error boundary. Without this, any uncaught render-time exception unwinds
 * the whole React tree and React unmounts the root → blank screen (recoverable
 * only by a manual reload). This catches the error, reports it (so it lands in
 * the Electron main log), and shows a fallback with a Reload button instead.
 */
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.props.onError?.(error, info)
  }

  private handleReload = () => {
    window.location.reload()
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children

    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          gap: 16,
          padding: 24,
          color: '#e5e7eb',
          background: '#1e1e1e',
          fontFamily: 'system-ui, -apple-system, sans-serif',
          textAlign: 'center',
        }}
      >
        <div style={{ fontSize: 18, fontWeight: 600 }}>Something went wrong</div>
        <div style={{ fontSize: 13, opacity: 0.85, maxWidth: 600, whiteSpace: 'pre-wrap' }}>
          {error.message}
        </div>
        <button
          onClick={this.handleReload}
          style={{
            padding: '8px 18px',
            borderRadius: 6,
            border: '1px solid #374151',
            background: '#2563eb',
            color: 'white',
            cursor: 'pointer',
            fontSize: 14,
          }}
        >
          Reload
        </button>
        {error.stack && (
          <details style={{ maxWidth: 720, fontSize: 11, opacity: 0.6, textAlign: 'left' }}>
            <summary style={{ cursor: 'pointer' }}>Error details</summary>
            <pre style={{ whiteSpace: 'pre-wrap', overflowX: 'auto' }}>{error.stack}</pre>
          </details>
        )}
      </div>
    )
  }
}
