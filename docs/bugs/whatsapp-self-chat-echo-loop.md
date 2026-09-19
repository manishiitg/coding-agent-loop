# WhatsApp self-chat outbound echo loop

## Symptom

Immediately after pairing a WhatsApp "Message yourself" chat, the workflow
chooser could be sent repeatedly without further user input.

## Cause

Self-chat messages typed by the user and messages sent by AgentWorks both arrive
with WhatsApp's `IsFromMe` flag. We therefore cannot drop every `IsFromMe` event.
The connector recorded the returned outbound message ID, but a self-chat echo can
arrive before that ID is returned or can be mirrored under another ID. The echo
was dispatched as a new unrouted user message, which generated another chooser.

## Contract

Before sending self-chat text, the connector records a short-lived SHA-256 key
of the normalized chat JID and exact outbound text. The matching inbound echo
consumes one expected marker and is not dispatched. Counts support identical
concurrent sends. Failed sends cancel their marker, entries expire after 30
seconds, raw message content is not retained, and message-ID deduplication remains
as a second guard. Non-self chats and ordinary user-authored self-chat messages
continue through the existing routing path.

## Workflow access validation

WhatsApp routing is scoped to the paired AgentWorks account. A saved slug is a
destination pointer, not a permission grant. Before showing route choices,
resolving a slug, and dispatching a message, the server now checks the paired
workspace user's current access against the workflow manifest. Revoked and stale
routes are hidden and an active inaccessible route is cleared. The bot manager
also passes the paired workspace identity into its final authorization check.
Slack remains intentionally different: a configured Slack channel route is its
own Run/Owner grant for members of that channel.
