## Managing the Gmail/Google Workspace bot's permissions

Read this when a user asks to connect, reconnect, or change what a Gmail
sending account can do — "give this workflow Drive access", "why can't it
read my Google Sheet", "increase the scope for gmail_001", "add Calendar
read-only".

### The two things that determine what an account can actually do

1. **The stored request** — `GmailConnection.AllowReadAccess` (Gmail read,
   on top of the always-granted send) and `GmailConnection.Services` (which
   Google Workspace services beyond Gmail — Drive, Sheets, Docs, Slides,
   Calendar — and whether each is read-only or read+write). This is what
   the connection is *configured* to ask for.
2. **What Google actually granted** — fixed the moment the user last
   completed Google's consent screen for this connection. Google has no API
   to widen or narrow a token's scope after the fact; the only way to change
   it is a fresh consent (Reconnect).

**These two can drift apart**, and when they do, the "Sending accounts"
panel's badges can be misleading — a connection authorized long ago under a
broader legacy flow can show "Send only" while the real token already has
full Gmail + Drive + Sheets + Docs + Slides + Calendar access, because the
badge reads the *stored request*, not the live grant. Never assume the
badge is the truth. If you need to know what an account can currently
actually do, read `auth.scopes` from its status (the raw granted OAuth
scopes), not `allow_read_access`/`services`.

### Changing what a connection is authorized for, from chat

Call `update_gmail_connection_grants`:

- `connection_id` — omit to target the account's default connection.
- `allow_read_access` — omit to leave Gmail read access unchanged.
- `services` — the **complete replacement list** of Workspace services this
  connection should be authorized for. Omit entirely to leave services
  unchanged. Pass `[]` to strip every service grant back to Gmail-only.
  Passing `[{"service":"drive"}]` when the connection already has Sheets
  authorized **removes Sheets** — always include everything that should
  remain, not just what's being added. Read the connection's current
  `services` first if you don't already know them.

This call only updates the **stored request** — it does not talk to Google
and does not change what the account can do yet. It returns a
`reconnect_url`. You must:

1. Tell the user to open `reconnect_url` and complete Google's consent
   screen. Nothing takes effect until they do.
2. Call `open_workspace_view(view="bots")` right after, so the Sending
   accounts panel is visible and they can see the updated request (and
   click Reconnect there instead, if they'd rather not use the link).

Never claim the new access is active before the user confirms they
completed the consent screen — the tool call succeeding only means the
*request* was saved.

### A workflow's own permission is a third, separate layer

Even once a connection's token genuinely has broad access, a specific
*workflow* can only use a Google service through `google_workspace_cli` if
that service is in the connection's `services` list — this is the exact
same allowlist `update_gmail_connection_grants` edits, so fixing "this
workflow can't use Drive" and "widen this account's Drive access" are the
same action, not two separate systems to reason about.

### Two backends exist; both are supported

A Gmail connection's actual sending call may run through `gws` or `gog`
(`GmailConfig.use_gog_backend`, a deployment-wide setting) — this is
transparent to a connection's stored request and to this tool; don't try to
detect or reason about which backend a deployment uses when managing
scopes. `google_workspace_cli` (Drive/Sheets/Docs/Slides/Calendar) always
uses `gog`, regardless of that setting.
