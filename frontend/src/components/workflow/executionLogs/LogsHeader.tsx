import { Braces, Filter, RefreshCw, Terminal } from 'lucide-react'
import type { RunFolderInfo } from '../../../services/api-types'
import { formatStartedAt } from '../../../utils/duration'
import { formatDeploymentDateTime } from '../../../utils/displayTime'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../ui/select'

export interface LogsHeaderProps {
  /** Embedded density: the pane header is one compact row, the modal's is two. */
  embedded: boolean
  startedAt?: string | null
  runFolderOptions: string[]
  runFolderInfos: RunFolderInfo[]
  selectedRunFolder: string
  setSelectedRunFolder: (folder: string) => void
  loading: boolean
  loadLogs: () => void
  onRefreshRunFolders?: () => void | Promise<void>
  headerAction?: React.ReactNode
  showWebhookPayload?: boolean
  webhookPayloadOpen?: boolean
  onToggleWebhookPayload?: () => void
}

// Header content only; InspectorShell owns the row wrapper and the close X.
export function LogsHeader({
  embedded,
  startedAt,
  runFolderOptions,
  runFolderInfos,
  selectedRunFolder,
  setSelectedRunFolder,
  loading,
  loadLogs,
  onRefreshRunFolders,
  headerAction,
  showWebhookPayload = false,
  webhookPayloadOpen = false,
  onToggleWebhookPayload,
}: LogsHeaderProps) {
  const timestampsByFolder = new Map(
    runFolderInfos.map(folder => [
      folder.name,
      folder.metadata?.started_at || folder.metadata?.created_at || null,
    ]),
  )
  const formatRunDateTime = (value?: string | null) => {
    return formatDeploymentDateTime(value)
  }
  const selectedTimestamp = formatRunDateTime(timestampsByFolder.get(selectedRunFolder))

  return (
          <div className={`flex min-w-0 flex-1 ${embedded ? 'items-center gap-3' : 'items-start gap-3'}`}>
            <h2 className={`${embedded ? 'text-sm' : 'text-lg'} flex shrink-0 items-center gap-2 font-semibold text-foreground`}>
              <Terminal className={`${embedded ? 'h-4 w-4' : 'h-5 w-5'} text-primary`} />
              Execution Logs
              {startedAt && (
                <span className="text-xs font-normal text-muted-foreground">{formatStartedAt(startedAt)}</span>
              )}
            </h2>
            <div className={`ml-auto flex min-w-0 flex-1 items-center justify-end gap-2 ${embedded ? '' : 'flex-wrap'}`}>
              {/* Run Folder Selector */}
              {runFolderOptions.length > 0 && (
                <div className="flex min-w-0 items-center gap-1.5">
                  <Filter className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <Select
                    value={selectedRunFolder}
                    onValueChange={setSelectedRunFolder}
                  >
                    <SelectTrigger className="h-8 w-80 max-w-[48vw] bg-card px-2 text-xs font-medium shadow-none" aria-label="Execution run">
                      <SelectValue placeholder="Select iteration/group">
                        {selectedRunFolder && (
                          <span className="flex min-w-0 items-center gap-2">
                            <span className="truncate">{selectedRunFolder}</span>
                            {selectedTimestamp && (
                              <span className="shrink-0 text-[10px] font-normal text-muted-foreground">
                                {selectedTimestamp}
                              </span>
                            )}
                          </span>
                        )}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent className="max-h-72">
                      {runFolderOptions.map(folder => {
                        const timestamp = formatRunDateTime(timestampsByFolder.get(folder))
                        return (
                          <SelectItem key={folder} value={folder} className="text-xs">
                            <span className="flex min-w-[25rem] items-center justify-between gap-6 pr-1">
                              <span>{folder}</span>
                              {timestamp && (
                                <span className="text-[10px] font-normal text-muted-foreground">
                                  {timestamp}
                                </span>
                              )}
                            </span>
                          </SelectItem>
                        )
                      })}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {showWebhookPayload && (
                <button
                  type="button"
                  onClick={onToggleWebhookPayload}
                  aria-expanded={webhookPayloadOpen}
                  className={`inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border px-2.5 text-xs font-medium transition-colors ${
                    webhookPayloadOpen
                      ? 'border-primary/40 bg-primary/10 text-foreground'
                      : 'border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground'
                  }`}
                  title="View the request payload that started this webhook run"
                >
                  <Braces className="h-3.5 w-3.5" />
                  Payload
                </button>
              )}

              {headerAction}
              {/* Refresh Button — also refreshes the run-folder list itself
                  (onRefreshRunFolders), not just the currently selected
                  folder's logs (loadLogs). Without this, a run folder that
                  appeared after this list was last loaded (e.g. a standalone
                  execute_step run) stays invisible in the dropdown no matter
                  how many times this button is clicked. */}
              <button
                onClick={() => {
                  loadLogs()
                  onRefreshRunFolders?.()
                }}
                disabled={loading || !selectedRunFolder}
                className="p-1.5 rounded-lg border border-border bg-card text-muted-foreground hover:text-foreground hover:bg-muted transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
                title="Refresh logs and run-folder list"
                aria-label="Refresh logs and run-folder list"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
              </button>
            </div>
          </div>
  )
}
import type React from 'react'
