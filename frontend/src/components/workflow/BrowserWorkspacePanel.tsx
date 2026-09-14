import { useState, type ReactNode } from 'react'
import { Settings2, X } from 'lucide-react'
import BrowserAutomationSettings, { type BrowserAutomationMode } from '../BrowserAutomationSettings'
import WorkflowLiveBrowser from './WorkflowLiveBrowser'

interface BrowserWorkspacePanelProps {
  workspacePath: string | null
  browserMode: BrowserAutomationMode
  onBrowserModeChange: (mode: BrowserAutomationMode) => void
  cdpPort: number
  onCdpPortChange: (port: number) => void
  cdpConnected: boolean | null
  cdpError: string | null
  cdpChecking: boolean
  onCheckCdpConnection: (port: number) => void
  readOnly?: boolean
  dirty?: boolean
  saving?: boolean
  onSave?: () => void
  assistantControl?: ReactNode
  scopeNoun?: 'workflow' | 'project'
}

/**
 * Shared AgentWorks/Work browser view. Keeping the live browser and its
 * settings overlay together prevents product surfaces from inventing a
 * second place to configure browser access.
 */
export function BrowserWorkspacePanel({
  workspacePath,
  browserMode,
  onBrowserModeChange,
  cdpPort,
  onCdpPortChange,
  cdpConnected,
  cdpError,
  cdpChecking,
  onCheckCdpConnection,
  readOnly = false,
  dirty = false,
  saving = false,
  onSave,
  assistantControl,
  scopeNoun = 'workflow',
}: BrowserWorkspacePanelProps) {
  const [settingsOpen, setSettingsOpen] = useState(false)

  return (
    <div className="relative flex h-full min-h-0 flex-1 flex-col">
      <WorkflowLiveBrowser workspacePath={workspacePath} scopeNoun={scopeNoun} toolbar={<>
        {assistantControl}
        <button
          type="button"
          aria-label="Browser settings"
          title="Browser settings"
          onClick={() => setSettingsOpen(value => !value)}
          className="rounded p-1.5 text-muted-foreground hover:bg-muted"
        >
          <Settings2 className="h-4 w-4" />
        </button>
      </>} />
      {settingsOpen && (
        <div
          role="dialog"
          aria-label="Browser settings"
          className="absolute inset-x-2 top-12 z-10 max-h-[calc(100%-4rem)] overflow-y-auto rounded-lg border border-border bg-background p-4 shadow-xl"
        >
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-medium">Browser settings</h3>
            <button
              type="button"
              aria-label="Close browser settings"
              onClick={() => setSettingsOpen(false)}
              className="rounded p-1 hover:bg-muted"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <BrowserAutomationSettings
            browserMode={browserMode}
            onBrowserModeChange={onBrowserModeChange}
            cdpPort={cdpPort}
            onCdpPortChange={onCdpPortChange}
            cdpConnected={cdpConnected}
            cdpError={cdpError}
            cdpChecking={cdpChecking}
            onCheckCdpConnection={onCheckCdpConnection}
            readOnly={readOnly}
            scopeNoun={scopeNoun}
          />
          {!readOnly && onSave && (
            <div className="mt-3 flex items-center justify-end gap-3 border-t pt-3">
              {dirty && <span className="text-xs text-muted-foreground">Unsaved changes</span>}
              <button
                type="button"
                disabled={!dirty || saving}
                onClick={onSave}
                className="rounded bg-primary px-3 py-1.5 text-xs text-primary-foreground disabled:opacity-50"
              >
                {saving ? 'Saving…' : 'Save settings'}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
