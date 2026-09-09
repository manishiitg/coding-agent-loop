import { AlertCircle, AlertTriangle, Loader2, RotateCcw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'

// ── Drill-in: WhatsApp setup ──────────────────────────────────────────────

type WhatsAppSetupBots = Pick<WorkflowBots,
  | 'readOnly' | 'waStatus' | 'waError' | 'qrImageURL' | 'qrLoading' | 'qrError' | 'unpairConfirm' | 'unpairing' | 'handleUnpairWhatsApp'
  | 'refreshWaQR'
>

export function WhatsAppSetup({ bots }: { bots: WhatsAppSetupBots }) {
  const {
    readOnly,
    waStatus, waError, qrImageURL, qrLoading, qrError, unpairConfirm, unpairing,
    refreshWaQR,
    handleUnpairWhatsApp,
  } = bots
  const [linkCopyState, setLinkCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')

  const handleCopyLinkCommand = async () => {
    const cmd = `link ${waStatus?.link_code || '123456'}`
    try {
      await navigator.clipboard.writeText(cmd)
      setLinkCopyState('copied')
    } catch {
      setLinkCopyState('failed')
    }
    window.setTimeout(() => setLinkCopyState('idle'), 2000)
  }

  return (
    <div className="space-y-4">
      {waError && <StatusBanner tone="error">{waError}</StatusBanner>}

      {/* Connector disabled at server startup */}
      {waStatus && !waStatus.enabled && (
        <Card className="p-4">
          <div className="flex items-start gap-3">
            <AlertTriangle className="w-5 h-5 text-amber-500 flex-shrink-0 mt-0.5" />
            <div className="space-y-1">
              <h3 className="text-sm font-medium text-foreground">WhatsApp connector is disabled</h3>
              <p className="text-xs text-muted-foreground">
                Remove <code className="px-1 py-0.5 bg-muted rounded">WHATSAPP_ENABLED=false</code> from
                the server's <code className="px-1 py-0.5 bg-muted rounded">.env</code> and restart the
                agent. The connector is enabled by default, and the per-user session directory can be
                overridden via <code className="px-1 py-0.5 bg-muted rounded">WHATSAPP_SESSION_DIR</code>.
              </p>
            </div>
          </div>
        </Card>
      )}

      {/* Status card */}
      {waStatus && waStatus.enabled && (
        <Card className="p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-sm font-medium text-foreground">Connection</h3>
              <p className="text-xs text-muted-foreground mt-0.5">Account-wide pairing shared by all workflows.</p>
              {waStatus.own_jid && <p className="mt-2 text-[11px] text-muted-foreground font-mono">{waStatus.own_jid}</p>}
              {(waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id) && (
                <p className="mt-1 text-[11px] text-muted-foreground">
                  bound to <span className="text-foreground">{waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id}</span>
                </p>
              )}
            </div>
            <div className="flex flex-col items-end gap-1 text-xs">
              <span className="inline-flex items-center gap-2">
                <span className={`h-2 w-2 rounded-full ${waStatus.connected ? 'bg-green-500' : waStatus.paired ? 'bg-amber-500' : 'bg-gray-400'}`} />
                <span className="text-foreground">
                  {waStatus.connected ? 'Connected' : waStatus.paired ? 'Paired, offline' : 'Not paired'}
                </span>
              </span>
              <span className="text-[11px] text-muted-foreground">
                {waStatus.enabled ? 'Enabled' : 'Disabled'} · {waStatus.paired ? 'Paired' : 'Unpaired'}
              </span>
            </div>
          </div>
          {!waStatus.paired && (
            <p className="mt-3 text-xs text-muted-foreground">
              Pair once via QR: <strong>Android</strong> ⋮ menu → Linked Devices → Link a device · <strong>iPhone</strong> Settings → Linked Devices → Link Device.
            </p>
          )}
        </Card>
      )}

      {/* QR pairing card — shown while unpaired */}
      {waStatus && waStatus.enabled && !waStatus.paired && (
        <Card className="p-4">
          <div className="flex flex-col items-center gap-3">
            <div className="flex w-full items-center justify-between gap-3">
              <div>
                <h3 className="text-sm font-medium text-foreground">Pair device</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">Scan this QR from WhatsApp → Linked Devices.</p>
              </div>
              <button
                type="button"
                onClick={refreshWaQR}
                disabled={qrLoading}
                className="inline-flex items-center gap-1 rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
                aria-label="Refresh WhatsApp QR"
              >
                <RotateCcw className="h-3.5 w-3.5" />
                Refresh
              </button>
            </div>
            {waStatus.qr_available ? (
              <>
                {qrImageURL ? (
                  <img
                    src={qrImageURL}
                    alt="WhatsApp pairing QR"
                    width={256}
                    height={256}
                    className="rounded border border-border bg-white p-2"
                  />
                ) : (
                  <div className="flex h-64 w-64 items-center justify-center rounded border border-border bg-muted/30 p-4 text-center">
                    {qrLoading ? (
                      <div className="flex items-center gap-2 text-sm text-muted-foreground">
                        <Loader2 className="w-4 h-4 animate-spin" />
                        Loading QR…
                      </div>
                    ) : qrError ? (
                      <div className="flex flex-col items-center gap-2 text-sm text-red-700 dark:text-red-300">
                        <AlertCircle className="w-5 h-5" />
                        <span>{qrError}</span>
                      </div>
                    ) : (
                      <span className="text-sm text-muted-foreground">QR not available yet.</span>
                    )}
                  </div>
                )}
                {waStatus.qr_expires_at && (
                  <p className="text-[11px] text-muted-foreground/80 text-center">QR expires {new Date(waStatus.qr_expires_at).toLocaleString()}.</p>
                )}
                {(waStatus.pairing_error || waStatus.pairing_message) && (
                  <div className="w-full space-y-2">
                    {waStatus.pairing_error && <StatusBanner tone="error">{waStatus.pairing_error}</StatusBanner>}
                    {waStatus.pairing_message && (
                      <div className="rounded-md border border-border bg-muted/30 p-3 text-[11px] text-muted-foreground">
                        {waStatus.pairing_message}
                        {waStatus.pairing_last_at && (
                          <div className="mt-1.5 text-muted-foreground/80">
                            Last attempt: {new Date(waStatus.pairing_last_at).toLocaleString()}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </>
            ) : (
              <div className="flex items-center gap-2 text-sm text-muted-foreground py-8">
                <Loader2 className="w-4 h-4 animate-spin" />
                Waiting for the server to generate a QR…
              </div>
            )}
          </div>
        </Card>
      )}

      {/* How-to-chat card — shown once paired. */}
      {waStatus && waStatus.enabled && waStatus.paired && (
        <Card className="p-4">
          <h3 className="text-sm font-medium text-foreground mb-1.5">How to chat</h3>
          <div className="space-y-2 text-xs text-muted-foreground">
            <div className="rounded-md border border-border bg-muted/30 p-3">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <div className="text-[11px] text-muted-foreground">Step 1: link a DM chat</div>
                  <code className="mt-1 block rounded bg-background px-2 py-1 font-mono text-[11px] text-muted-foreground">
                    link {waStatus.link_code || '123456'}
                  </code>
                </div>
                <button
                  type="button"
                  onClick={handleCopyLinkCommand}
                  className="shrink-0 rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  {linkCopyState === 'copied' ? 'Copied!' : linkCopyState === 'failed' ? 'Copy failed' : 'Copy'}
                </button>
              </div>
              {waStatus.link_code && (
                <div className="mt-2 text-[11px] text-muted-foreground/80">
                  Linked chats: {waStatus.bound_chat_count ?? 0}. Link code expires{' '}
                  {waStatus.link_code_expires_at ? new Date(waStatus.link_code_expires_at).toLocaleString() : 'soon'}.
                </div>
              )}
            </div>

            <div className="rounded-md border border-border bg-muted/30 p-3">
              <div className="text-[11px] text-muted-foreground">Step 2: route to a workflow</div>
              <p className="mt-1 text-[11px] text-muted-foreground">
                Start a message with <code>@slug</code> (for example <code>@invoice-processing</code>) to route it to that workflow.
              </p>
            </div>

            <p className="text-[11px] text-muted-foreground/80">
              Tip: for a separate-bot experience, pair a dedicated WhatsApp number (WhatsApp Business on a different number also works).
            </p>
          </div>
        </Card>
      )}

      {/* Unpair card — shown once paired */}
      {waStatus && waStatus.enabled && waStatus.paired && (
        <Card className="p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-medium text-foreground">Unpair</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Drops the current device link and deletes the session file. You'll need to scan a new QR
                to pair again.
              </p>
            </div>
            <Button
              onClick={handleUnpairWhatsApp}
              disabled={readOnly || unpairing}
              title={readOnly ? READ_ONLY_TITLE : undefined}
              variant={unpairConfirm ? 'destructive' : 'outline'}
              size="sm"
              className="flex-shrink-0 whitespace-nowrap"
            >
              {unpairing ? (
                <><Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> Unpairing…</>
              ) : unpairConfirm ? (
                <><Trash2 className="w-3.5 h-3.5 mr-1.5" /> Confirm unpair</>
              ) : (
                <>Unpair</>
              )}
            </Button>
          </div>
        </Card>
      )}

      {/* Loading placeholder */}
      {!waStatus && !waError && (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="w-8 h-8 animate-spin text-primary" />
        </div>
      )}
    </div>
  )
}
