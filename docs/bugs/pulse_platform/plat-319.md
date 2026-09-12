[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-319 — Slack notifications display plain text and formatted attachment twice

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | Slack fix tested; release pending. Reported email duplication not reproduced by code tests |
| Last synchronized | 2026-09-12 |

The shared Slack webhook formatter sent the full message as top-level `text`
and repeated it in attachment Block Kit sections. Top-level text is a visible
message alongside secondary attachments, not their hidden fallback.

The formatter now retains the styled attachment and puts the plain alternative
in that attachment's `fallback` field. Tests assert that no top-level text is
sent, the fallback survives and the formatted body appears once. This shared
path applies across workflows and local/server deployments. No test messages
were sent to real Slack channels.

Reference: https://docs.slack.dev/legacy/legacy-messaging/legacy-secondary-message-attachments

The user also reported email duplication and requested code-only investigation.
The shared notifier passes plain text and Gmail HTML separately. Gmail builds
one multipart/alternative body inside multipart/mixed; gws and gog raw delivery
preserve that MIME structure. A parser-based test verifies one plain alternative,
one HTML alternative and no additional visible body. It does not reproduce the
reported email symptom, nor prove that caller-authored HTML never repeats itself.
Email duplication remains unverified; do not close it based on the Slack fix.
