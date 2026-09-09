import { AlertCircle, AlertTriangle, Loader2, RotateCcw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from '../../ui/Button'
import { Card } from '../../ui/Card'
import { Input } from '../../ui/Input'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import type { WorkflowBots } from './useWorkflowBots'
import { StatusBanner } from './StatusBanner'

// ── Drill-in: WhatsApp setup ──────────────────────────────────────────────

type WhatsAppSetupBots = Pick<WorkflowBots,
  | 'readOnly'
  | 'waStatus' | 'waError'
  | 'qrImageURL' | 'qrLoading' | 'qrError'
  | 'waAddDeviceOpen' | 'openAddWhatsAppDevice' | 'closeAddWhatsAppDevice'
  | 'waUnpairConfirmSlot' | 'waUnpairingSlot' | 'handleUnpairWhatsAppDevice'
  | 'waDeviceLabelDrafts' | 'setWaDeviceLabelDraft' | 'waDeviceLabelSavingSlot' | 'handleSaveWhatsAppDeviceLabel'
  | 'waPairDeviceLabel' | 'setWaPairDeviceLabel' | 'waPairDeviceLabelSaving' | 'waPairingDeviceSlot' | 'handleSaveWhatsAppPairDeviceLabel'
  | 'refreshWaQR'
>

function formatQRExpiry(expiresAt: string | undefined, nowMs: number): string | null {
  if (!expiresAt) return null
  const expiresMs = new Date(expiresAt).getTime()
  if (!Number.isFinite(expiresMs)) return null
  const remainingSeconds = Math.max(0, Math.ceil((expiresMs - nowMs) / 1000))
  if (remainingSeconds <= 0) return 'QR expired. Refreshing...'
  return `QR expires in ${remainingSeconds}s.`
}

export function WhatsAppSetup({ bots }: { bots: WhatsAppSetupBots }) {
  const {
    readOnly,
    waStatus, waError, qrImageURL, qrLoading, qrError,
    waAddDeviceOpen, openAddWhatsAppDevice, closeAddWhatsAppDevice,
    waUnpairConfirmSlot, waUnpairingSlot, handleUnpairWhatsAppDevice,
    waDeviceLabelDrafts, setWaDeviceLabelDraft, waDeviceLabelSavingSlot, handleSaveWhatsAppDeviceLabel,
    waPairDeviceLabel, setWaPairDeviceLabel, waPairDeviceLabelSaving, waPairingDeviceSlot, handleSaveWhatsAppPairDeviceLabel,
    refreshWaQR,
  } = bots
  const [linkCopyState, setLinkCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [editingLabelSlot, setEditingLabelSlot] = useState<string | null>(null)
  const [nowMs, setNowMs] = useState(() => Date.now())

  const devices = waStatus?.devices && waStatus.devices.length > 0
    ? waStatus.devices
    : (waStatus ? [{
      slot: '',
      paired: waStatus.paired,
      connected: waStatus.connected,
      own_jid: waStatus.own_jid,
      qr_available: waStatus.qr_available,
    }] : [])
  // Only show fully linked numbers in the list; an unpaired "next" slot can
  // exist while the user is adding one and shouldn't look like a broken
  // connection.
  const linkedDevices = devices.filter(device => device.paired)
  const mainDevice = devices.find(device => device.slot === '')
  const mainDevicePaired = mainDevice?.paired ?? !!waStatus?.paired
  const hasLinkedDevice = linkedDevices.length > 0
  const hasConnectedDevice = linkedDevices.some(device => device.connected)

  const showPrimaryPair = !!waStatus?.enabled && !mainDevicePaired && (!hasLinkedDevice || waAddDeviceOpen)
  const showNextPair = !!waStatus?.enabled && mainDevicePaired && waAddDeviceOpen
  const showPairCard = showPrimaryPair || showNextPair
  const nextPairDeviceActive = showNextPair
    && (!waPairingDeviceSlot || waStatus?.next_device?.slot === waPairingDeviceSlot)
  const qrAvailable = showNextPair ? nextPairDeviceActive && !!waStatus?.next_device?.qr_available : !!waStatus?.qr_available
  const qrExpiresAt = showNextPair && nextPairDeviceActive ? waStatus?.next_device?.qr_expires_at : waStatus?.qr_expires_at
  const qrExpiryLabel = formatQRExpiry(qrExpiresAt, nowMs)
  const pairTitle = showNextPair ? 'Add another number' : 'Pair device'
  const pairNameSaveReady = !showNextPair || waPairingDeviceSlot !== null || waStatus?.next_device !== undefined

  useEffect(() => {
    if (!qrExpiresAt) return
    setNowMs(Date.now())
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [qrExpiresAt])

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
              {(waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id) && (
                <p className="mt-1 text-[11px] text-muted-foreground">
                  bound to <span className="text-foreground">{waStatus.owner_email || waStatus.owner_username || waStatus.owner_user_id}</span>
                </p>
              )}
            </div>
            <div className="flex flex-col items-end gap-1 text-xs">
              <span className="inline-flex items-center gap-2">
                <span className={`h-2 w-2 rounded-full ${hasConnectedDevice ? 'bg-green-500' : hasLinkedDevice ? 'bg-amber-500' : 'bg-gray-400'}`} />
                <span className="text-foreground">
                  {hasConnectedDevice ? 'Connected' : hasLinkedDevice ? 'Paired, offline' : 'Not paired'}
                </span>
              </span>
              <span className="text-[11px] text-muted-foreground">
                {waStatus.enabled ? 'Enabled' : 'Disabled'} · {hasLinkedDevice ? `${linkedDevices.length} linked` : 'Unpaired'}
              </span>
            </div>
          </div>
          {!hasLinkedDevice && (
            <p className="mt-3 text-xs text-muted-foreground">
              Pair once via QR: <strong>Android</strong> ⋮ menu → Linked Devices → Link a device · <strong>iPhone</strong> Settings → Linked Devices → Link Device.
            </p>
          )}
        </Card>
      )}

      {/* Linked numbers/devices */}
      {waStatus && waStatus.enabled && hasLinkedDevice && (
        <Card className="p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-medium text-foreground">Linked numbers</h3>
              <p className="mt-0.5 text-xs text-muted-foreground">
                You can link multiple WhatsApp accounts and each will have its own chat history.
              </p>
            </div>
            <Button
              onClick={waAddDeviceOpen ? closeAddWhatsAppDevice : openAddWhatsAppDevice}
              disabled={readOnly}
              title={readOnly ? READ_ONLY_TITLE : undefined}
              variant="outline"
              size="sm"
              className="flex-shrink-0 whitespace-nowrap"
            >
              {waAddDeviceOpen ? 'Close' : 'Add number'}
            </Button>
          </div>

          <div className="mt-3 space-y-2">
            {linkedDevices.map(device => {
              const displayName = device.label?.trim() || 'Unnamed WhatsApp number'
              const draftLabel = waDeviceLabelDrafts[device.slot] ?? device.label ?? ''
              const labelDirty = draftLabel.trim() !== (device.label || '')
              const statusDot = device.connected ? 'bg-green-500' : device.paired ? 'bg-amber-500' : 'bg-gray-400'
              const statusText = device.connected ? 'Connected' : device.paired ? 'Paired, offline' : 'Not paired'
              const confirming = waUnpairConfirmSlot === device.slot
              const busy = waUnpairingSlot === device.slot
              const labelSaving = waDeviceLabelSavingSlot === device.slot
              const showLabelEditor = !device.label || editingLabelSlot === device.slot || labelSaving
              return (
                <div key={device.slot || 'primary'} className="flex flex-col gap-3 rounded-md border border-border bg-muted/20 px-3 py-2">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className={`h-2 w-2 rounded-full ${statusDot}`} />
                        <div className="text-xs text-foreground font-medium">{displayName}</div>
                        <div className="text-[11px] text-muted-foreground">{statusText}</div>
                      </div>
                      {device.own_jid && (
                        <div className="mt-1 text-[11px] text-muted-foreground font-mono truncate">{device.own_jid}</div>
                      )}
                    </div>
                    <div className="flex flex-shrink-0 items-center gap-2">
                      {device.label && !showLabelEditor && (
                        <Button
                          onClick={() => setEditingLabelSlot(device.slot)}
                          disabled={readOnly}
                          title={readOnly ? READ_ONLY_TITLE : undefined}
                          variant="outline"
                          size="sm"
                          className="whitespace-nowrap"
                        >
                          Edit name
                        </Button>
                      )}
                      <Button
                        onClick={() => handleUnpairWhatsAppDevice(device.slot)}
                        disabled={readOnly || !!waUnpairingSlot}
                        title={readOnly ? READ_ONLY_TITLE : undefined}
                        variant={confirming ? 'destructive' : 'outline'}
                        size="sm"
                        className="whitespace-nowrap"
                      >
                        {busy ? (
                          <><Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> Unpairing…</>
                        ) : confirming ? (
                          <><Trash2 className="w-3.5 h-3.5 mr-1.5" /> Confirm</>
                        ) : (
                          'Unpair'
                        )}
                      </Button>
                    </div>
                  </div>
                  {showLabelEditor && (
                    <div className="flex items-center gap-2">
                      <div className="min-w-0 flex-1">
                        <Input
                          value={draftLabel}
                          onChange={event => setWaDeviceLabelDraft(device.slot, event.target.value)}
                          disabled={readOnly || labelSaving}
                          placeholder="Person name"
                          maxLength={60}
                          className="h-8 text-xs"
                        />
                      </div>
                      <Button
                        onClick={() => {
                          void handleSaveWhatsAppDeviceLabel(device.slot, draftLabel).then(saved => {
                            if (saved && draftLabel.trim()) setEditingLabelSlot(null)
                          })
                        }}
                        disabled={readOnly || labelSaving || !labelDirty}
                        title={readOnly ? READ_ONLY_TITLE : undefined}
                        variant="outline"
                        size="sm"
                        className="flex-shrink-0 whitespace-nowrap"
                      >
                        {labelSaving ? <><Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> Saving</> : 'Save name'}
                      </Button>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </Card>
      )}

      {/* QR pairing card — shown while pairing (primary or "next") */}
      {waStatus && waStatus.enabled && showPairCard && (
        <Card className="p-4">
          <div className="flex flex-col items-center gap-3">
            <div className="flex w-full items-center justify-between gap-3">
              <div>
                <h3 className="text-sm font-medium text-foreground">{pairTitle}</h3>
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
            <div className="flex w-full items-center gap-2">
              <Input
                value={waPairDeviceLabel}
                onChange={event => setWaPairDeviceLabel(event.target.value)}
                disabled={readOnly || waPairDeviceLabelSaving}
                placeholder="Person name for this WhatsApp number"
                maxLength={60}
                className="h-8 text-xs"
              />
              <Button
                onClick={handleSaveWhatsAppPairDeviceLabel}
                disabled={readOnly || waPairDeviceLabelSaving || !pairNameSaveReady || waPairDeviceLabel.trim().length === 0}
                title={readOnly ? READ_ONLY_TITLE : undefined}
                variant="outline"
                size="sm"
                className="flex-shrink-0 whitespace-nowrap"
              >
                {waPairDeviceLabelSaving ? <><Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> Saving</> : 'Save name'}
              </Button>
            </div>
            {qrAvailable ? (
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
                {qrExpiryLabel && (
                  <p className="text-[11px] text-muted-foreground/80 text-center">{qrExpiryLabel}</p>
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
                A default slug is created from the automation name. You can delete it or add another slug for this automation, then start a message with <code>@slug</code> to route the chat.
              </p>
            </div>

            <p className="text-[11px] text-muted-foreground/80">
              Tip: for a separate-bot experience, pair a dedicated WhatsApp number (WhatsApp Business on a different number also works).
            </p>
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
