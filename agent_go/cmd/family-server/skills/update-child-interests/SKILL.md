---
name: update-child-interests
description: Extract genuine interest signals from the child's own conversations and keep child/interests.md current — a Pulse-only skill, not triggered by a direct parent request.
---

# Update child interests

This is run by Pulse (the periodic check-in), never by a live parent or child
request — nobody asks for this directly, so don't mention it as a step you're
doing; just fold anything worth sharing into your normal Pulse reply to the
parent.

`child/interests.md` is a small, LIVING file — like `parent/preferences.md`, it
is fully rewritten each time, never appended to. It exists so
`skills/discover-something-new/SKILL.md` can tailor a fun activity to what the
child has genuinely responded well to, learned automatically over time instead
of guessed.

1. **Read what's already captured**: `cat child/interests.md` if it exists (it
   may not, on a fresh family or before she's had many conversations — that's
   fine, start from empty).

2. **Scan the child's own conversations**: read across ALL of
   `child/conversations/*.json` since you last checked, looking for genuine
   engagement signals — not what the PARENT said (that's preferences.md's job):
   - Clear enthusiasm about a topic (asked follow-up questions, said it was
     cool/fun/awesome, kept going on their own)
   - A specific interest she volunteered (a favorite animal, game, subject,
     hobby, character — anything that hints at what she'd enjoy exploring)
   - Clear DISINTEREST or boredom about a topic/theme (changed the subject
     quickly, said it was boring, gave short disengaged answers) — worth noting
     so future picks avoid it, not just noting what worked
   - Reactions specifically to a `shared/discoveries/*.html` activity she was
     given, if any conversation mentions one
   Do NOT capture: routine schoolwork engagement (that's just her doing the
   assigned work, not a discovered interest), one-off mentions with no real
   signal, or anything you are inferring/guessing beyond what she actually
   said or clearly reacted to.

3. **Write** `child/interests.md` as a short plain-Markdown bullet list (no HTML;
   never shown to the parent or child, it's Quill's own working memory). Merge
   new signals in, drop anything clearly superseded (she's since shown the
   opposite reaction), and keep it compact — a handful of bullets on what
   resonates and what doesn't, not a growing log. If nothing new turned up
   since you last checked, leave the file as-is.

4. Nothing else needs to happen here — this file is read by
   `skills/discover-something-new/SKILL.md` when the parent asks for a fun
   activity, so future requests apply it automatically. You do not need to
   announce this update to the parent unless it's genuinely worth one short
   line in your normal Pulse reply (e.g. "she seems really into space lately").
