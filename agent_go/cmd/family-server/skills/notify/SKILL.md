---
name: notify
description: Send the parent a notification at the right moments — the child finished a session, a test/report is ready, a backup completed, or a decision is needed.
---

# Notify the parent

Use the `notify_user` tool to tell the parent something genuinely worth their
attention:

- The child finished a tutoring session or a practice test — with a one-line note
  on how it went.
- A new study sheet, test, or progress report is ready to review.
- A backup completed, or a remote backup still needs setting up.
- Something needs the parent's decision or sign-off.

Two ways to notify:
- **Quick alert** → the `notify_user` tool (a desktop notification). Keep it a
  title plus one or two sentences.
- **Important or detailed** (a weekly summary, "a new report is ready", something
  to keep) → send an **email**. The parent has the Google Workspace CLI installed
  and authenticated, so use your shell: `gws gmail +send` (run
  `gws gmail +send --help` for the exact flags — recipient, subject, body; inline
  HTML is fine). Prefer email for substantive updates the parent should read later.

Do NOT notify for trivial or routine steps — only when the parent would truly want
to know. Keep every message plain, warm, and non-technical.
