import { ArrowLeft, BarChart3 } from 'lucide-react'
import { ReportView } from '../components/workflow/ReportViewer'

interface ReportPageProps {
  encodedPath: string
  onBack?: () => void
}

function decodeBase64Utf8(value: string): string | null {
  try {
    let normalized = value.trim().replace(/ /g, '+').replace(/-/g, '+').replace(/_/g, '/')
    while (normalized.length % 4 !== 0) normalized += '='
    const binary = atob(normalized)
    const bytes = Uint8Array.from(binary, char => char.charCodeAt(0))
    return new TextDecoder().decode(bytes)
  } catch {
    return null
  }
}

function isSafeWorkflowPath(path: string): boolean {
  const normalized = path.replace(/\\/g, '/').replace(/^\/+/, '')
  if (!normalized || normalized.split('/').includes('..')) return false
  return normalized === path && normalized.startsWith('Workflow/')
}

export function ReportPage({ encodedPath, onBack }: ReportPageProps) {
  const workspacePath = decodeBase64Utf8(encodedPath)
  const isValidPath = workspacePath !== null && isSafeWorkflowPath(workspacePath)

  if (!isValidPath) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background px-4 text-foreground">
        <div className="w-full max-w-md rounded-lg border border-border bg-card p-6 text-center shadow-sm">
          <BarChart3 className="mx-auto mb-4 h-10 w-10 text-muted-foreground" />
          <h1 className="mb-2 text-lg font-semibold">Invalid report URL</h1>
          <p className="mb-4 text-sm text-muted-foreground">
            The report URL must include a valid encoded workflow path.
          </p>
          {onBack && (
            <button
              type="button"
              onClick={onBack}
              className="rounded-md border border-border bg-background px-3 py-1.5 text-sm font-medium text-foreground hover:bg-muted"
            >
              Go back
            </button>
          )}
        </div>
      </div>
    )
  }

  const workflowName = workspacePath.split('/').filter(Boolean).pop() || 'Automation'

  return (
    <div className="flex h-screen min-h-screen flex-col overflow-hidden bg-background text-foreground">
      <div className="flex flex-shrink-0 items-center justify-between border-b border-border bg-background/95 px-3 py-2.5 shadow-sm backdrop-blur sm:px-5">
        <div className="flex min-w-0 items-center gap-3">
          {onBack && (
            <button
              type="button"
              onClick={onBack}
              className="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
              title="Back to app"
              aria-label="Back to app"
            >
              <ArrowLeft className="h-4 w-4" />
            </button>
          )}
          <BarChart3 className="h-5 w-5 flex-shrink-0 text-primary" />
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold text-foreground sm:text-base">
              {workflowName} Report
            </h1>
            <p className="truncate text-xs text-muted-foreground">{workspacePath}</p>
          </div>
        </div>
        <span className="hidden rounded-md border border-border bg-muted px-2 py-1 text-xs font-medium text-muted-foreground sm:inline-flex">
          Live Report
        </span>
      </div>
      <div className="min-h-0 flex-1">
        <ReportView workspacePath={workspacePath} />
      </div>
    </div>
  )
}
