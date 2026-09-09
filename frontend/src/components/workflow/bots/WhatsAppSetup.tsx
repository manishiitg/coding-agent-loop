import { AlertCircle, AlertTriangle, Loader2, Trash2 } from 'lucide-react'
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
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-sm font-medium text-foreground">Status</h3>
              <p className="text-xs text-muted-foreground mt-0.5">
                Uses the unofficial WhatsApp Web protocol (whatsmeow). Pair your personal number once
                by scanning the QR below. On Android: tap the ⋮ menu → Linked Devices → Link a device.
                On iPhone: Settings → Linked Devices → Link Device.
              </p>
            </div>
            <div className="flex flex-col items-end gap-0.5 text-xs">
              <span className="flex items-center gap-1.5">
                <span
                  className={`w-1.5 h-1.5 rounded-full ${
                    waStatus.connected ? 'bg-green-500' : waStatus.paired ? 'bg-amber-500' : 'bg-gray-400'
                  }`}
                />
                <span className="text-foreground">
                  {waStatus.connected ? 'Connected' : waStatus.paired ? 'Paired, offline' : 'Not paired'}
                </span>
              </span>
              {waStatus.own_jid && (
                <span className="text-muted-foreground font-mono text-[10px]">{waStatus.own_jid}</span>
              )}
              {(waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id) && (
                <span className="text-muted-foreground text-[10px]">
                  bound to{' '}
                  <span className="text-foreground">
                    {waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id}
                  </span>
                </span>
              )}
            </div>
          </div>
        </Card>
      )}

      {/* QR pairing card — shown while unpaired */}
      {waStatus && waStatus.enabled && !waStatus.paired && (
        <Card className="p-4">
          <div className="flex flex-col items-center gap-3">
            <div className="flex w-full items-center justify-between gap-3">
              <h3 className="text-sm font-medium text-foreground">Scan to pair</h3>
              <Button type="button" variant="outline" size="sm" onClick={refreshWaQR} disabled={qrLoading}>
                Refresh QR
              </Button>
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
                <p className="text-xs text-muted-foreground text-center max-w-sm">
                  Open WhatsApp on your phone. <strong>Android</strong>: ⋮ menu → Linked Devices → Link a
                  device. <strong>iPhone</strong>: Settings → Linked Devices → Link Device. Then scan this
                  code. The QR rotates every few seconds; this page refreshes it automatically.
                </p>
                {waStatus.qr_expires_at && (
                  <p className="text-[11px] text-muted-foreground/80 text-center">
                    QR expires {new Date(waStatus.qr_expires_at).toLocaleString()}.
                  </p>
                )}
                {(waStatus.pairing_error || waStatus.pairing_message) && (
                  <div className="w-full rounded-md border border-border bg-muted/30 p-3 text-[11px] text-muted-foreground">
                    {waStatus.pairing_error && (
                      <div className="text-red-700 dark:text-red-300">Pairing error: {waStatus.pairing_error}</div>
                    )}
                    {waStatus.pairing_message && (
                      <div className={waStatus.pairing_error ? 'mt-1.5' : ''}>{waStatus.pairing_message}</div>
                    )}
                    {waStatus.pairing_last_at && (
                      <div className="mt-1.5 text-muted-foreground/80">
                        Last attempt: {new Date(waStatus.pairing_last_at).toLocaleString()}
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
          <div className="space-y-1.5 text-xs text-muted-foreground">
            <p>
              Open WhatsApp → <strong>Message Yourself</strong> chat, or DM the paired WhatsApp number
              from another phone. First send <code>link {waStatus.link_code || '123456'}</code> from that
              chat to bind WhatsApp's current phone/LID identity. Then send messages normally. Start a
              message with <code>@slug</code> to route it to the workflow mapped for that slug.
            </p>
            {waStatus.link_code && (
              <p className="text-muted-foreground/80">
                Linked chats: {waStatus.bound_chat_count ?? 0}. Link code expires{' '}
                {waStatus.link_code_expires_at
                  ? new Date(waStatus.link_code_expires_at).toLocaleString()
                  : 'soon'}
                .
              </p>
            )}
            <p className="text-muted-foreground/80">
              For a proper separate-bot experience (like Slack's <code>@bot</code>), pair a dedicated
              WhatsApp number — a second SIM, WhatsApp Business with a different number, or a virtual
              number from Twilio. Only linked inbound DMs are handled as bot messages.
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
