---
name: update-preferences
description: Extract durable things the parent has said in chat and keep parent/preferences.md current — a Pulse-only skill, not triggered by a direct parent request.
---

# Update parent preferences

This is run by Pulse (the periodic check-in), never by a live parent request — the
parent never asks for this directly, so don't mention it to them as a step you're
doing; just fold the result into your normal Pulse reply if it's worth a line.

`parent/preferences.md` is a small, LIVING file — like the academic map and progress
report, it is fully rewritten each time, never appended to. It exists to remember
things the parent has told you in chat that aren't already captured by a dedicated
field (`set_child_profile`, `set_parent_label`, `set_teaching_style`) — so they never
have to repeat themselves in a later conversation.

1. **Read what's already captured**: `cat parent/preferences.md` if it exists (it may
   not, on a fresh family — that's fine, start from empty).

2. **Scan for anything durable the parent has said**: read across ALL of
   `parent/conversations/*.json`, not just the conversation Pulse is currently
   resuming — the parent may have said something durable in an older or different
   conversation (web or WhatsApp). Look for things like:
   - Exam/test dates or deadlines ("her board exam is in March")
   - Scheduling or pacing preferences ("short daily practice, not long weekend sessions")
   - Emotional/behavioral notes relevant to teaching ("she gets anxious with timed
     tests", "don't push too hard on Fridays")
   - Content preferences ("she doesn't respond well to word problems", "more visual
     examples")
   - Anything else the parent stated as a standing fact or preference, not a one-off
     question or a single message's context.
   Do NOT capture: routine chat content, one-off requests, anything already stored by
   a dedicated tool (grade/board/name, parent label, teaching style), or anything you
   are inferring/guessing rather than something the parent actually said.

3. **Write** `parent/preferences.md` as a short plain-Markdown bullet list (no HTML,
   this file is never shown directly to the parent or child, it's Quill's own working
   memory). Merge new durable statements in, drop anything clearly superseded or no
   longer true (the parent said something different since), and keep the whole file
   compact — a handful of bullets, not a growing log. If nothing durable has been said
   since you last checked, leave the file as-is; don't rewrite it just to touch it.

4. Nothing else needs to happen here — this file is read at the start of parent and
   child conversations (see the system prompt) so future turns apply it automatically.
   You do not need to announce this update to the parent unless it's genuinely worth
   one line in your normal Pulse reply.
