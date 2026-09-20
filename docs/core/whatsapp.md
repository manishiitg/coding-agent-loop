# WhatsApp Connector

WhatsApp is the thread-less, owner-paired channel: each owner links their
own phone number(s), and workflow selection happens in-chat via slugs
(`@list`, `@switch`, `@<slug>`, `@off`). There is no platform-level token
to share, so WhatsApp never needed the per-workflow connection registry
that Slack has — account scope is per owner, route scope is per message.

## Model

```text
WhatsAppServiceManager ("whatsapp")
  services[userKey]            primary phone (slot "")
  services[userKey␟phone-2]    extra devices (slot "phone-2", "phone-3", …)
        |  one WhatsAppService per linked phone
        v
BotConversationManager (BotConnector; Capabilities: WorkflowProgress only)
```

- Account key is `whatsappUserKey(userID)`; each linked phone is a service
  with its own multi-device pairing state in a per-device SQLite file:
  `<baseDir>/<userKey>/whatsapp.db`, extras under
  `devices/<slot>/whatsapp.db`. Restart reconnects from disk — no re-pair.
- `NextPairingDevice` fills the primary slot first, then reuses an unpaired
  extra slot, then mints `phone-2`, `phone-3`, … Pairing is QR-based
  (`EnsurePairingQR` / `GetQR`); `IsPaired` gates readiness.
- `UnpairDevice` forgets one phone: the primary resets to a fresh pairing
  (its slot stays), an extra device is removed entirely with its owner data.
- Devices carry labels (`SetDeviceLabel`, persisted offline without a
  WhatsApp handshake) and are listed via `Devices` / found via `DeviceByJID`.
- Per-device access state (`whatsapp_meta` row) holds a 6-digit link code
  (auto-rotated, 24h expiry) and the bound-DM-chat list with last-seen times.

## Ingress

Every DM arrives with `IsMention=true` — a message to the paired number
always addresses the bot. The service pre-resolves owner identity
(`UserEmail`, `WorkspaceUserID`) and attaches the route before the manager
sees it:

- `PresetWorkflow`: the slug-selected `ChannelRoute` (`WhatsAppRouting` is
  `map[slug]ChannelRoute`), or nil for generic chat / default-profile turns.
- `PresetProfile`: set when the route names a product profile conversation.
- Voice notes are transcribed before they reach a conversation (on-device
  STT via `SetVoiceTranscriber`); if the model isn't installed the sender
  is asked to set it up instead of blocking on a download.

## Routing commands

- `@list` shows candidate workflows; `@switch <number|name> [run|workshop]`
  and a direct `@<slug>` select one; `@off` drops back to generic chat.
  Selection persists per chat until changed.
- Modes are `run` (execute pinned) or `workshop`; the mode is part of the
  route key, so switching modes is a conversation boundary.
- Access is re-checked per message against the paired owner
  (`workflowRouteAllowed` → `WhatsAppWorkflowAccessFunc`): routes are
  user-scoped, and a saved slug never confers access by itself.

## Sessions and identity

- Thread-less: the chat JID is the thread. Replies continue the bound
  conversation within the 1-hour idle window; past it, a new conversation
  starts with a short preamble from the old one.
- The chat → session pointer persists in the device's own store
  (`botSessionBindingStore`), filtered by route key on load, so a restart
  restores the conversation but never a different workflow's.
  Route-change isolation (P1) and the full lifecycle live in
  `bot_connectors_combined.md`.
- Turns run as `bot_owner`: the sender was authenticated at ingress
  (pairing ownership + per-slug access), so the turn executes as the paired
  owner with nothing further to revalidate. See "Bot principals" in
  `slack_connections.md`.
- Capabilities declare `WorkflowProgress` only: no threads, streaming,
  reactions, message edits, or history reads.

## Key files

| Area | File |
|---|---|
| Manager, device slots, pairing, unpair | `agent_go/cmd/server/services/whatsapp_manager.go` |
| Service, QR, slugs, ingress, bindings, link codes | `agent_go/cmd/server/services/whatsapp_service.go` |
| Voice wiring, access func, profile router | `agent_go/cmd/server/server.go` |
| Lifecycle, route isolation, mention policy | `agent_go/cmd/server/services/bot_connector.go` |
| Principal model (`bot_owner`) | `docs/core/slack_connections.md` |
